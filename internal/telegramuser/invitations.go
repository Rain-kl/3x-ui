package telegramuser

import (
	"fmt"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type InvitationView struct {
	ClientID  int    `json:"clientId"`
	Email     string `json:"email"`
	Code      string `json:"code"`
	URL       string `json:"url"`
	CreatedAt int64  `json:"createdAt"`
	ExpiresAt int64  `json:"expiresAt"`
	Status    string `json:"status"`
}
type InvitationPage struct {
	Items []InvitationView `json:"items"`
	Total int64            `json:"total"`
}

func Invitations(page, size int) (InvitationPage, error) {
	result := InvitationPage{Items: []InvitationView{}}
	if page < 1 || size < 1 || size > 100 || page-1 > int(^uint(0)>>1)/size {
		return result, fmt.Errorf("invalid invitation pagination")
	}
	cfg, err := Config()
	if err != nil {
		return result, err
	}
	var rows []struct {
		model.TelegramBindCode
		Email string
	}
	query := database.GetDB().Table("telegram_bind_codes AS codes").Joins("JOIN clients ON clients.id = codes.client_id")
	if err := query.Count(&result.Total).Error; err != nil {
		return result, fmt.Errorf("count invitations: %w", err)
	}
	if err := query.Select("codes.*, clients.email").Order("codes.created_at DESC,codes.client_id").Limit(size).Offset((page - 1) * size).Scan(&rows).Error; err != nil {
		return result, fmt.Errorf("list invitations: %w", err)
	}
	now := time.Now().UnixMilli()
	for _, row := range rows {
		status := "active"
		switch {
		case row.RevokedAt > 0:
			status = "revoked"
		case row.UsedAt > 0:
			status = "used"
		case row.ExpiresAt <= now:
			status = "expired"
		case row.Code == "":
			status = "legacy"
		}
		link := ""
		if row.Code != "" && cfg.BotUsername != "" {
			link = "https://t.me/" + cfg.BotUsername + "?start=" + row.Code
		}
		result.Items = append(result.Items, InvitationView{ClientID: row.ClientID, Email: row.Email, Code: row.Code, URL: link, CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt, Status: status})
	}
	return result, nil
}

func RevokeInvitation(clientID int) error {
	if clientID <= 0 {
		return fmt.Errorf("invalid invitation client ID")
	}
	inviteMu.Lock()
	defer inviteMu.Unlock()
	result := database.GetDB().Model(&model.TelegramBindCode{}).Where("client_id = ? AND used_at = 0 AND revoked_at = 0", clientID).Update("revoked_at", time.Now().UnixMilli())
	if result.Error != nil {
		return fmt.Errorf("revoke invitation: %w", result.Error)
	}
	return RecordLog(0, 0, "invitation.revoked", fmt.Sprintf("client=%d", clientID))
}
