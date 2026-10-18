package telegramuser

import (
	"context"
	"errors"
	"fmt"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// Security alerts must be deliverable even when the user polling receiver is stopped.
func SendSecurityAlert(ctx context.Context, network Network, bot string, chatID int64, clientID int, text string) error {
	token := network.AdminToken
	if bot == "user" {
		cfg, err := Config()
		if err != nil {
			return err
		}
		token = cfg.Token
	}
	if token == "" {
		return fmt.Errorf("%s Bot token is not configured", bot)
	}
	api, err := newBotAPI(token, network)
	if err != nil {
		return err
	}
	row := model.TelegramMessage{TgID: chatID, TargetChatID: chatID, ClientID: clientID, Direction: "outgoing", Kind: "text", Text: text, Role: "security", DeliveryBot: bot, Status: "sending"}
	if _, err := recordMessage(&row, "", ""); err != nil {
		return fmt.Errorf("record security alert: %w", err)
	}
	var response struct {
		MessageID int `json:"message_id"`
	}
	sendErr := api.call(ctx, "sendMessage", map[string]any{"chat_id": chatID, "text": text}, &response)
	status, detail := "sent", ""
	if sendErr != nil {
		status, detail = "failed", sendErr.Error()
	}
	if err := database.GetDB().Model(&row).Updates(map[string]any{"status": status, "error": detail, "external_id": response.MessageID}).Error; err != nil {
		return errors.Join(sendErr, fmt.Errorf("save security alert result: %w", err))
	}
	return sendErr
}
