package telegramuser

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mymmrac/telego"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type SubscriptionDetails struct {
	Mixed string
	Nodes []string
}

func (r *receiver) replySubscription(tgID int64, clientID int, link string) {
	if r.hooks.SubscriptionDetails == nil {
		r.log(tgID, 0, "subscription.details.failed", "subscription detail provider is unavailable")
		r.reply(tgID, "读取订阅连接信息失败，请联系管理员。", nil)
		return
	}
	info, err := r.hooks.SubscriptionDetails(clientID)
	if err != nil {
		r.log(tgID, 0, "subscription.details.failed", err.Error())
		r.reply(tgID, "读取订阅连接信息失败，请稍后重试或联系管理员。", nil)
		return
	}
	text := "你的订阅链接为:\n\n" + link
	if info.Mixed != "" {
		text += "\n\nMIXED连接信息为:\n\n" + info.Mixed
	}
	text += "\n\n提示:\n1. Clash/Singbox 用户需搭配订阅转换工具使用：https://sublink.osur.eu\n2. 此消息在30秒后删除，请尽快保存。"
	for _, part := range splitText(text, 4000) {
		r.replyExpiring(tgID, part)
	}
}

func (r *receiver) replyNodes(tgID int64) {
	client, err := BoundClient(tgID)
	if err != nil {
		r.reply(tgID, err.Error(), nil)
		return
	}
	if r.hooks.SubscriptionDetails == nil {
		r.reply(tgID, "节点信息暂不可用，请联系管理员。", nil)
		return
	}
	info, err := r.hooks.SubscriptionDetails(client.Id)
	if err != nil {
		r.log(tgID, 0, "nodes.failed", err.Error())
		r.reply(tgID, "读取订阅节点失败，请稍后重试或联系管理员。", nil)
		return
	}
	if len(info.Nodes) == 0 {
		r.reply(tgID, "当前订阅没有可用节点。", nil)
		return
	}
	for _, part := range splitText(strings.Join(info.Nodes, "\n"), 4000) {
		r.reply(tgID, part, nil)
	}
}

func (r *receiver) replyExpiring(tgID int64, text string) {
	row := model.TelegramMessage{TgID: tgID, TargetChatID: tgID, Direction: "outgoing", Kind: "text", Text: text, Role: "command", Status: "sending", SourceBot: "user", DeliveryBot: "user"}
	if _, err := recordMessage(&row, "", ""); err != nil {
		r.failure("message.persist.failed", err)
		return
	}
	var sent telego.Message
	err := r.user.call(r.ctx, "sendMessage", map[string]any{"chat_id": tgID, "text": text, "disable_web_page_preview": true, "reply_markup": map[string]any{"remove_keyboard": true}}, &sent)
	if err == nil {
		job := model.TelegramMessageDeletion{BotKey: codeHash(r.cfg.Token), ChatID: tgID, MessageID: sent.MessageID, DueAt: time.Now().Add(30 * time.Second).UnixMilli()}
		if err := database.GetDB().Create(&job).Error; err != nil {
			r.failure("message.delete.schedule.failed", fmt.Errorf("schedule subscription reply %d: %w", sent.MessageID, err))
		}
	}
	r.complete(&row, sent, err, false)
}

func (r *receiver) deleteExpiredMessages() {
	for r.ctx.Err() == nil {
		var jobs []model.TelegramMessageDeletion
		err := database.GetDB().Where("bot_key = ? AND due_at <= ?", codeHash(r.cfg.Token), time.Now().UnixMilli()).Order("due_at, id").Limit(20).Find(&jobs).Error
		if err != nil {
			r.failure("message.delete.queue.failed", err)
		} else {
			for _, job := range jobs {
				if r.ctx.Err() != nil {
					return
				}
				r.deleteMessage(job)
			}
		}
		if !pause(r.ctx, time.Second) {
			return
		}
	}
}

func (r *receiver) deleteMessage(job model.TelegramMessageDeletion) {
	ctx, cancel := context.WithTimeout(r.ctx, 5*time.Second)
	defer cancel()
	err := r.user.call(ctx, "deleteMessage", map[string]any{"chat_id": job.ChatID, "message_id": job.MessageID}, nil)
	if r.ctx.Err() != nil {
		return
	}
	var apiErr *apiError
	gone := errors.As(err, &apiErr) && apiErr.Code == 400 && strings.Contains(strings.ToLower(apiErr.Text), "message to delete not found")
	if err == nil || gone {
		if err := database.GetDB().Delete(&job).Error; err != nil {
			r.failure("message.delete.persist.failed", err)
			return
		}
		r.log(job.ChatID, 0, "message.deleted", fmt.Sprintf("external=%d", job.MessageID))
		return
	}
	r.log(job.ChatID, 0, "message.delete.failed", fmt.Sprintf("external=%d attempt=%d: %v", job.MessageID, job.Attempts+1, err))
	retry, delay := isRetryable(err)
	if retry && job.Attempts < 4 {
		if err := database.GetDB().Model(&job).Updates(map[string]any{"attempts": job.Attempts + 1, "due_at": time.Now().Add(max(delay, 5*time.Second) + time.Duration(job.Attempts*job.Attempts)*time.Second).UnixMilli()}).Error; err != nil {
			r.failure("message.delete.persist.failed", err)
		}
	} else if err := database.GetDB().Delete(&job).Error; err != nil {
		r.failure("message.delete.persist.failed", err)
	}
}
