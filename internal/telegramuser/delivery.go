package telegramuser

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func (r *receiver) forward(original *model.TelegramMessage) error {
	if r.admin == nil || len(r.network.AdminIDs) == 0 {
		return fmt.Errorf("administrator Bot token or recipient chat IDs are not configured")
	}
	name := fmt.Sprintf("用户 %d", original.TgID)
	if client, err := BoundClient(original.TgID); err == nil {
		name += " · " + client.Email
	}
	for _, chatID := range r.network.AdminIDs {
		copy := *original
		copy.Id, copy.DedupKey, copy.CreatedAt, copy.UpdatedAt, copy.ExternalID = 0, nil, 0, 0, 0
		copy.Direction, copy.Role, copy.DeliveryBot, copy.Status = "outgoing", "relay", "admin", "pending"
		copy.TargetChatID, copy.ReplyID = chatID, original.Id
		copy.Text = fmt.Sprintf("[%s]（回复此消息可联系用户）\n%s", name, original.Text)
		if err := database.GetDB().Create(&copy).Error; err != nil {
			return fmt.Errorf("queue administrator relay: %w", err)
		}
	}
	return nil
}

func QueueAdminReply(message *telego.Message) (bool, error) {
	if message == nil || message.ReplyToMessage == nil {
		return false, nil
	}
	r, runtimeErr := Default.receiver()
	if runtimeErr != nil {
		return true, runtimeErr
	}
	var relay model.TelegramAdminRelay
	err := database.GetDB().Where("bot_key = ? AND admin_chat_id = ? AND admin_message_id = ?", codeHash(r.network.AdminToken), message.Chat.ID, message.ReplyToMessage.MessageID).First(&relay).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return true, fmt.Errorf("resolve administrator Telegram reply: %w", err)
	}
	if !Default.Running() {
		return true, fmt.Errorf("user Telegram Bot is not running")
	}
	var conversation model.TelegramConversation
	if err := database.GetDB().First(&conversation, "tg_id = ?", relay.TgID).Error; err != nil {
		return true, fmt.Errorf("reply conversation no longer exists: %w", err)
	}
	row := messageFromTelegram(message)
	row.TgID, row.TargetChatID, row.ReplyID = relay.TgID, relay.TgID, relay.UserMessageID
	row.Direction, row.SourceBot, row.DeliveryBot, row.Role, row.Status = "outgoing", "admin", "user", "admin", "pending"
	key := fmt.Sprintf("admin:%s:%d:%d", codeHash(r.network.AdminToken)[:12], message.Chat.ID, message.MessageID)
	row.DedupKey = &key
	if client, err := BoundClient(relay.TgID); err == nil {
		row.ClientID = client.Id
	}
	_, err = recordMessage(&row, "", "")
	return true, err
}

func HasAdminRelay(chatID int64, messageID int) bool {
	r, err := Default.receiver()
	if err != nil {
		return false
	}
	var count int64
	if err := database.GetDB().Model(&model.TelegramAdminRelay{}).Where("bot_key = ? AND admin_chat_id = ? AND admin_message_id = ?", codeHash(r.network.AdminToken), chatID, messageID).Count(&count).Error; err != nil {
		r.failure("reply.lookup.failed", fmt.Errorf("load administrator Telegram reply mapping: %w", err))
		return false
	}
	return count > 0
}

func (r *receiver) deliver() {
	for r.ctx.Err() == nil {
		var row model.TelegramMessage
		err := database.GetDB().Where("status = ? AND next_attempt_at <= ?", "pending", time.Now().UnixMilli()).Order("priority, id").First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if !pause(r.ctx, 300*time.Millisecond) {
				return
			}
			continue
		}
		if err != nil {
			r.failure("queue.read.failed", err)
			if !pause(r.ctx, 2*time.Second) {
				return
			}
			continue
		}
		if row.DeliveryBot == "user" {
			blocked, err := userBlocked(row.TgID)
			if err != nil {
				r.failure("blacklist.check.failed", err)
				pause(r.ctx, time.Second)
				continue
			}
			if blocked {
				if err := database.GetDB().Model(&row).Update("status", "cancelled").Error; err != nil {
					r.failure("queue.cancel.failed", err)
				}
				r.log(row.TgID, row.Id, "message.cancelled", "recipient is blocked")
				continue
			}
		}
		if row.NoticeID > 0 {
			valid, err := validNoticeDelivery(&row)
			if err != nil {
				r.failure("notification.check.failed", err)
				pause(r.ctx, time.Second)
				continue
			}
			if !valid {
				if err := database.GetDB().Model(&row).Update("status", "cancelled").Error; err != nil {
					r.failure("queue.cancel.failed", err)
				}
				r.log(row.TgID, row.Id, "notification.cancelled", "notification version, audience or binding changed")
				continue
			}
		}
		if row.Role == "relay" {
			allowed, err := r.canRelay(row.TgID)
			if err != nil {
				r.failure("message.relay.check.failed", err)
				pause(r.ctx, time.Second)
				continue
			}
			if !allowed {
				if err := database.GetDB().Model(&row).Update("status", "cancelled").Error; err != nil {
					r.failure("queue.cancel.failed", err)
				}
				r.log(row.TgID, row.Id, "message.relay.cancelled", "forwarding disabled or anonymous chat requires a valid binding")
				continue
			}
		}
		r.log(row.TgID, row.Id, "message.send.attempt", fmt.Sprintf("attempt=%d bot=%s", row.Attempts+1, row.DeliveryBot))
		sent, err := r.send(&row)
		r.complete(&row, sent, err, true)
		if !pause(r.ctx, 100*time.Millisecond) {
			return
		}
	}
}

func (r *receiver) send(row *model.TelegramMessage) (telego.Message, error) {
	var sent telego.Message
	api := r.user
	if row.DeliveryBot == "admin" {
		api = r.admin
	}
	if api == nil {
		return sent, fmt.Errorf("destination Telegram Bot is not configured")
	}
	params := map[string]any{"chat_id": row.TargetChatID}
	if row.ReplyID > 0 && row.DeliveryBot == "user" {
		var original model.TelegramMessage
		if err := database.GetDB().First(&original, "id = ? AND tg_id = ?", row.ReplyID, row.TgID).Error; err == nil && original.ExternalID > 0 {
			params["reply_parameters"] = map[string]any{"message_id": original.ExternalID, "allow_sending_without_reply": true}
		}
	}
	if row.FileID == "" {
		params["text"] = row.Text
		if row.Role == "relay" {
			params["text"] = boundedText(row.Text, 3500)
		}
		err := api.call(r.ctx, "sendMessage", params, &sent)
		return sent, err
	}
	method, field := mediaMethod(row.Kind)
	if row.Kind != "sticker" && row.Kind != "video_note" && row.Text != "" {
		params["caption"] = row.Text
		if row.Role == "relay" {
			params["caption"] = boundedText(row.Text, 900)
		}
	}
	if row.SourceBot == row.DeliveryBot {
		params[field] = row.FileID
		err := api.call(r.ctx, method, params, &sent)
		return sent, err
	}
	source := r.user
	if row.SourceBot == "admin" {
		source = r.admin
	}
	if source == nil {
		return sent, fmt.Errorf("source Telegram Bot for attachment is unavailable")
	}
	data, _, err := source.download(r.ctx, row.FileID)
	if err != nil {
		return sent, fmt.Errorf("bridge Telegram attachment: %w", err)
	}
	name := row.FileName
	if name == "" {
		name = "attachment"
	}
	err = api.upload(r.ctx, method, field, name, data, params, &sent)
	return sent, err
}

func (r *receiver) complete(row *model.TelegramMessage, sent telego.Message, sendErr error, retry bool) {
	updates := map[string]any{"attempts": row.Attempts + 1}
	if sendErr == nil {
		updates["status"], updates["external_id"], updates["error"] = "sent", sent.MessageID, ""
		if row.FileID != "" {
			media := messageFromTelegram(&sent)
			updates["file_id"], updates["source_bot"] = media.FileID, row.DeliveryBot
		}
	} else {
		updates["status"], updates["error"] = "failed", sendErr.Error()
		if canRetry, delay := isRetryable(sendErr); retry && canRetry && row.Attempts < 4 && r.ctx.Err() == nil {
			updates["status"] = "pending"
			updates["next_attempt_at"] = time.Now().Add(delay + time.Duration(row.Attempts*row.Attempts)*time.Second).UnixMilli()
		}
	}
	if err := database.GetDB().Model(row).Updates(updates).Error; err != nil {
		r.failure("message.persist.failed", err)
		return
	}
	if sendErr != nil {
		r.log(row.TgID, row.Id, "message.send.failed", sendErr.Error())
		if row.DeliveryBot == "admin" && row.FileID != "" && updates["status"] == "failed" && r.admin != nil {
			var fallback telego.Message
			text := boundedText("附件转发失败，请在网页 Telegram 消息页查看原消息。\n"+row.Text, 3500)
			if err := r.admin.call(r.ctx, "sendMessage", map[string]any{"chat_id": row.TargetChatID, "text": text}, &fallback); err == nil {
				r.saveRelay(row, fallback.MessageID)
			} else {
				r.log(row.TgID, row.Id, "message.relay.fallback.failed", err.Error())
			}
		}
		return
	}
	r.log(row.TgID, row.Id, "message.sent", fmt.Sprintf("bot=%s external=%d", row.DeliveryBot, sent.MessageID))
	if row.DeliveryBot == "admin" {
		r.saveRelay(row, sent.MessageID)
	}
	next := time.Now().Add(time.Second).UnixMilli()
	if err := database.GetDB().Model(&model.TelegramMessage{}).Where("status = ? AND delivery_bot = ? AND target_chat_id = ? AND next_attempt_at < ?", "pending", row.DeliveryBot, row.TargetChatID, next).Update("next_attempt_at", next).Error; err != nil {
		r.failure("queue.rate.failed", err)
	}
}

func (r *receiver) saveRelay(row *model.TelegramMessage, externalID int) {
	entry := model.TelegramAdminRelay{BotKey: codeHash(r.network.AdminToken), AdminChatID: row.TargetChatID, AdminMessageID: externalID, TgID: row.TgID, UserMessageID: row.ReplyID}
	if err := database.GetDB().Create(&entry).Error; err != nil {
		r.log(row.TgID, row.Id, "reply.mapping.failed", err.Error())
	}
}

func boundedText(value string, maxBytes int) string {
	chunks := splitText(value, maxBytes)
	if len(chunks) == 0 {
		return "[Telegram 消息]"
	}
	if len(chunks) > 1 {
		return chunks[0] + "…"
	}
	return chunks[0]
}

func splitText(value string, limit int) []string {
	var result []string
	var part strings.Builder
	for _, char := range value {
		if part.Len()+len(string(char)) > limit && part.Len() > 0 {
			result = append(result, part.String())
			part.Reset()
		}
		part.WriteRune(char)
	}
	if part.Len() > 0 {
		result = append(result, part.String())
	}
	return result
}

func (m *Manager) Download(ctx context.Context, id int64) ([]byte, string, string, error) {
	r, err := m.receiver()
	if err != nil {
		return nil, "", "", err
	}
	var row model.TelegramMessage
	if err := database.GetDB().First(&row, id).Error; err != nil {
		return nil, "", "", err
	}
	if row.FileID == "" {
		return nil, "", "", fmt.Errorf("message has no downloadable attachment")
	}
	api := r.user
	if row.SourceBot == "admin" {
		api = r.admin
	}
	if api == nil {
		return nil, "", "", fmt.Errorf("attachment source Bot is unavailable")
	}
	data, contentType, err := api.download(ctx, row.FileID)
	if err != nil {
		r.log(row.TgID, row.Id, "attachment.download.failed", err.Error())
	}
	return data, contentType, row.FileName, err
}

func (r *receiver) canRelay(tgID int64) (bool, error) {
	if !r.cfg.ForwardAdmins {
		return false, nil
	}
	if r.cfg.AllowAnonymousChat {
		return true, nil
	}
	var count int64
	err := database.GetDB().Table("telegram_bindings AS b").Joins("JOIN clients c ON c.id = b.client_id").Where("b.tg_id = ?", tgID).Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("check Telegram relay client binding: %w", err)
	}
	return count > 0, nil
}
