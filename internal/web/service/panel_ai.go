package service

import (
	"context"
	"fmt"

	"github.com/mhsanaei/3x-ui/v3/internal/panelai"
	"github.com/mhsanaei/3x-ui/v3/internal/telegramuser"
)

func PanelAIHooks() panelai.Hooks {
	return panelai.Hooks{SQL: queryAISQL, Environment: aiAuditEnvironment, Notify: func(ctx context.Context, text string) error {
		enabled, err := (&SettingService{}).GetTgbotEnabled()
		if err != nil {
			return fmt.Errorf("read administrator Bot state: %w", err)
		}
		if !enabled {
			return fmt.Errorf("administrator Telegram Bot is disabled")
		}
		network, err := userTelegramNetwork()
		if err != nil {
			return err
		}
		return telegramuser.SendAdminReport(ctx, network, text)
	}}
}
