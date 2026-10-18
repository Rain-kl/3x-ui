package telegramuser

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func EnqueueNotification(tx *gorm.DB, notice model.SubscriptionNotification) error {
	if !notice.Published {
		return nil
	}
	var cfg model.TelegramUserConfig
	if err := tx.First(&cfg, 1).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	} else if err != nil {
		return fmt.Errorf("load user Bot notification settings: %w", err)
	}
	if !cfg.Enabled {
		return nil
	}
	var recipients []model.TelegramBinding
	query := tx.Table("telegram_bindings AS b").Select("b.*").Joins("JOIN clients c ON c.id = b.client_id")
	if notice.TargetType == "clients" {
		query = query.Where("b.client_id IN ?", notice.ClientIDs)
	} else {
		query = query.Where("c.group_name IN ?", notice.Groups)
	}
	if err := query.Find(&recipients).Error; err != nil {
		return fmt.Errorf("resolve bound notification audience: %w", err)
	}
	var queue []model.TelegramMessage
	parts := splitText(notice.Title+"\n\n"+notice.Body, 3500)
	for _, recipient := range recipients {
		for _, text := range parts {
			queue = append(queue, model.TelegramMessage{TgID: recipient.TgID, TargetChatID: recipient.TgID, ClientID: recipient.ClientID, Direction: "outgoing", Kind: "text", Text: text, DeliveryBot: "user", SourceBot: "user", Role: "notification", Status: "pending", Priority: 10, NoticeID: notice.Id, NoticeVersion: notice.UpdatedAt})
		}
	}
	if len(queue) == 0 {
		return nil
	}
	if err := tx.CreateInBatches(&queue, 100).Error; err != nil {
		return fmt.Errorf("queue targeted Telegram notification: %w", err)
	}
	return tx.Create(&model.TelegramLog{Event: "notification.queued", Detail: fmt.Sprintf("notice=%d recipients=%d messages=%d at=%d", notice.Id, len(recipients), len(queue), time.Now().UnixMilli())}).Error
}

func validNoticeDelivery(message *model.TelegramMessage) (bool, error) {
	var notice model.SubscriptionNotification
	if err := database.GetDB().First(&notice, message.NoticeID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if !notice.Published || notice.UpdatedAt != message.NoticeVersion {
		return false, nil
	}
	var count int64
	err := database.GetDB().Model(&model.TelegramBinding{}).Where("tg_id = ? AND client_id = ?", message.TgID, message.ClientID).Count(&count).Error
	if err != nil || count == 0 {
		return false, err
	}
	var client model.ClientRecord
	if err := database.GetDB().First(&client, message.ClientID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if notice.TargetType == "clients" {
		return slices.Contains(notice.ClientIDs, client.Id), nil
	}
	return slices.Contains(notice.Groups, client.Group), nil
}
