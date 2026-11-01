package telegramuser

import (
	"fmt"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type renewalRecipient struct {
	ClientID   int
	TgID       int64
	Email      string
	ExpiryTime int64
}

func queueRenewalReminders(now time.Time) error {
	var recipients []renewalRecipient
	err := database.GetDB().Table("telegram_bindings b").Select("b.client_id,b.tg_id,c.email,c.expiry_time").Joins("JOIN clients c ON c.id = b.client_id").Where("c.enable = ? AND c.expiry_time > ? AND c.expiry_time <= ?", true, now.UnixMilli(), now.Add(7*24*time.Hour).UnixMilli()).Scan(&recipients).Error
	if err != nil {
		return fmt.Errorf("read renewal reminder audience: %w", err)
	}
	for _, c := range recipients {
		days := (c.ExpiryTime - now.UnixMilli() + int64(24*time.Hour/time.Millisecond) - 1) / int64(24*time.Hour/time.Millisecond)
		if days != 7 && days != 3 && days != 1 {
			continue
		}
		err := database.GetDB().Transaction(func(tx *gorm.DB) error {
			key := fmt.Sprintf("telegramRenewal:%d:%d:%d", c.ClientID, c.TgID, days)
			value := strconv.FormatInt(c.ExpiryTime, 10)
			var state model.Setting
			result := tx.Where("key = ?", key).Find(&state)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected > 0 && state.Value == value {
				return nil
			}
			if result.RowsAffected == 0 {
				state = model.Setting{Key: key, Value: value}
				if err := tx.Create(&state).Error; err != nil {
					return err
				}
			} else if err := tx.Model(&state).Update("value", value).Error; err != nil {
				return err
			}
			text := fmt.Sprintf("续费提醒：客户端 %s 将在 %d 天内到期。\n到期时间：%s\n请及时联系管理员续费。", c.Email, days, time.UnixMilli(c.ExpiryTime).UTC().Format("2006-01-02 15:04 UTC"))
			row := model.TelegramMessage{ClientID: c.ClientID, TgID: c.TgID, TargetChatID: c.TgID, Direction: "outgoing", Kind: "text", Text: text, Status: "pending", DeliveryBot: "user", Role: "renewal", SourceBot: "user", NoticeVersion: c.ExpiryTime}
			return tx.Create(&row).Error
		})
		if err != nil {
			return fmt.Errorf("queue renewal for client %d: %w", c.ClientID, err)
		}
	}
	return nil
}

func (r *receiver) remindRenewals() {
	for {
		if err := queueRenewalReminders(time.Now()); err != nil {
			r.failure("renewal.queue.failed", err)
		}
		if !pause(r.ctx, time.Minute) {
			return
		}
	}
}

func validRenewal(row *model.TelegramMessage) (bool, error) {
	var count int64
	err := database.GetDB().Table("telegram_bindings b").Joins("JOIN clients c ON c.id=b.client_id").Where("b.client_id=? AND b.tg_id=? AND c.enable=? AND c.expiry_time=? AND c.expiry_time>?", row.ClientID, row.TgID, true, row.NoticeVersion, time.Now().UnixMilli()).Count(&count).Error
	return count > 0, err
}
