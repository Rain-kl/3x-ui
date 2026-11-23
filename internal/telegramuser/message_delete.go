package telegramuser

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type TelegramMessageDeleteResult struct {
	DeletedID int64 `json:"deletedId" example:"42"`
}

type TelegramConversationDeleteResult struct {
	TgID                int64 `json:"tgId" example:"265758444"`
	DeletedMessages     int64 `json:"deletedMessages" example:"12"`
	PreservedPending    int64 `json:"preservedPending" example:"0"`
	ConversationDeleted bool  `json:"conversationDeleted" example:"true"`
}

func conversationAccessible(tgID int64) error {
	var conversation model.TelegramConversation
	err := database.GetDB().First(&conversation, "tg_id = ?", tgID).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("load Telegram conversation %d: %w", tgID, err)
	}
	if _, bindErr := BoundClient(tgID); bindErr == nil {
		return nil
	} else {
		return fmt.Errorf("load Telegram conversation %d (%w); binding check: %w", tgID, err, bindErr)
	}
}

func refreshConversationLastMessage(tx *gorm.DB, tgID int64) error {
	var latest int64
	if err := tx.Model(&model.TelegramMessage{}).
		Select("COALESCE(MAX(created_at), 0)").
		Where("tg_id = ? AND delivery_bot <> ?", tgID, "admin").
		Scan(&latest).Error; err != nil {
		return fmt.Errorf("query latest Telegram message timestamp for %d: %w", tgID, err)
	}
	if err := tx.Model(&model.TelegramConversation{}).
		Where("tg_id = ?", tgID).
		Update("last_message_at", latest).Error; err != nil {
		return fmt.Errorf("update Telegram conversation %d timestamp: %w", tgID, err)
	}
	return nil
}

func DeleteMessage(messageID int64) (TelegramMessageDeleteResult, error) {
	out := TelegramMessageDeleteResult{DeletedID: messageID}
	if messageID <= 0 {
		return out, fmt.Errorf("invalid Telegram message ID")
	}
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		var row model.TelegramMessage
		if err := tx.Where("id = ? AND delivery_bot <> ?", messageID, "admin").First(&row).Error; err != nil {
			return fmt.Errorf("load Telegram message %d: %w", messageID, err)
		}
		if row.Status == "pending" || row.Status == "sending" {
			return fmt.Errorf("pending or sending Telegram messages cannot be deleted")
		}
		res := tx.Where("id = ? AND status NOT IN ?", messageID, []string{"pending", "sending"}).Delete(&model.TelegramMessage{})
		if res.Error != nil {
			return fmt.Errorf("delete Telegram message %d: %w", messageID, res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("pending or sending Telegram messages cannot be deleted")
		}
		if err := tx.Where("tg_id = ? AND delivery_bot = ? AND reply_id = ? AND status NOT IN ?", row.TgID, "admin", messageID, []string{"pending", "sending"}).Delete(&model.TelegramMessage{}).Error; err != nil {
			return fmt.Errorf("delete forwarded Telegram copies for message %d: %w", messageID, err)
		}
		if err := tx.Where("tg_id = ? AND user_message_id = ?", row.TgID, messageID).Delete(&model.TelegramAdminRelay{}).Error; err != nil {
			return fmt.Errorf("delete Telegram relay for message %d: %w", messageID, err)
		}
		if err := refreshConversationLastMessage(tx, row.TgID); err != nil {
			return err
		}
		return tx.Create(&model.TelegramLog{
			TgID:      row.TgID,
			MessageID: row.Id,
			Event:     "message.deleted.admin",
			Detail:    "message deleted in control panel",
		}).Error
	})
	return out, err
}

func DeleteConversationMessages(tgID int64) (TelegramConversationDeleteResult, error) {
	out := TelegramConversationDeleteResult{TgID: tgID}
	if tgID <= 0 {
		return out, fmt.Errorf("invalid Telegram conversation ID")
	}
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		var conv model.TelegramConversation
		if err := tx.First(&conv, "tg_id = ?", tgID).Error; err != nil {
			return fmt.Errorf("load Telegram conversation %d: %w", tgID, err)
		}
		res := tx.Where("tg_id = ? AND status NOT IN ?", tgID, []string{"pending", "sending"}).Delete(&model.TelegramMessage{})
		if res.Error != nil {
			return fmt.Errorf("delete Telegram conversation %d messages: %w", tgID, res.Error)
		}
		out.DeletedMessages = res.RowsAffected
		if err := tx.Where("tg_id = ? AND user_message_id NOT IN (SELECT id FROM telegram_messages)", tgID).Delete(&model.TelegramAdminRelay{}).Error; err != nil {
			return fmt.Errorf("clean Telegram admin relays for %d: %w", tgID, err)
		}
		var remaining int64
		if err := tx.Model(&model.TelegramMessage{}).Where("tg_id = ?", tgID).Count(&remaining).Error; err != nil {
			return fmt.Errorf("count remaining Telegram messages for %d: %w", tgID, err)
		}
		if err := tx.Model(&model.TelegramMessage{}).Where("tg_id = ? AND status IN ?", tgID, []string{"pending", "sending"}).Count(&out.PreservedPending).Error; err != nil {
			return fmt.Errorf("count pending Telegram messages for %d: %w", tgID, err)
		}
		if remaining == 0 {
			if err := tx.Where("tg_id = ?", tgID).Delete(&model.TelegramConversation{}).Error; err != nil {
				return fmt.Errorf("delete Telegram conversation %d: %w", tgID, err)
			}
			out.ConversationDeleted = true
			return tx.Create(&model.TelegramLog{
				TgID:   tgID,
				Event:  "conversation.deleted.admin",
				Detail: fmt.Sprintf("deleted %d messages and removed conversation in control panel", out.DeletedMessages),
			}).Error
		}
		if err := refreshConversationLastMessage(tx, tgID); err != nil {
			return err
		}
		out.ConversationDeleted = false
		return tx.Create(&model.TelegramLog{
			TgID:   tgID,
			Event:  "conversation.cleared.admin",
			Detail: fmt.Sprintf("deleted %d messages, preserved %d pending/sending messages", out.DeletedMessages, out.PreservedPending),
		}).Error
	})
	return out, err
}
