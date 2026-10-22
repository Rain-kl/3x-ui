package telegramuser

import (
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type AdminBindInput struct {
	ClientID int   `json:"clientId" example:"1"`
	TgID     int64 `json:"tgId" example:"123456789"`
}

func AdminBind(input AdminBindInput) (model.TelegramBinding, error) {
	inviteMu.Lock()
	defer inviteMu.Unlock()
	var binding model.TelegramBinding
	if input.ClientID <= 0 || input.TgID <= 0 || input.TgID > 9007199254740991 {
		return binding, fmt.Errorf("manual binding requires a positive client ID and a valid numeric Telegram account ID")
	}
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		var err error
		binding, err = claimBinding(tx, input.TgID, input.ClientID)
		if err != nil {
			return err
		}
		conversation := model.TelegramConversation{TgID: input.TgID}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&conversation).Error; err != nil {
			return fmt.Errorf("create manually bound Telegram conversation: %w", err)
		}
		if err := tx.Create(&model.TelegramLog{TgID: input.TgID, Event: "binding.created.admin", Detail: fmt.Sprintf("client=%d bound in control panel", input.ClientID)}).Error; err != nil {
			return fmt.Errorf("record administrator Telegram binding: %w", err)
		}
		return nil
	})
	return binding, err
}
