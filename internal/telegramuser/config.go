package telegramuser

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type UserTelegramSettings struct {
	Enabled            bool   `json:"enabled"`
	Token              string `json:"token,omitempty"`
	HasToken           bool   `json:"hasToken"`
	PublicSubURL       string `json:"publicSubUrl"`
	ForwardAdmins      bool   `json:"forwardAdmins"`
	AllowAnonymousChat bool   `json:"allowAnonymousChat"`
	LogDays            int    `json:"logDays"`
	MessageDays        int    `json:"messageDays"`
	BotUsername        string `json:"botUsername"`
}

func Config() (model.TelegramUserConfig, error) {
	value := model.TelegramUserConfig{Id: 1, ForwardAdmins: true, LogDays: 30, MessageDays: 90}
	err := database.GetDB().First(&value, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return value, nil
	}
	return value, err
}

func ReadSettings() (UserTelegramSettings, error) {
	c, err := Config()
	return UserTelegramSettings{Enabled: c.Enabled, HasToken: c.Token != "", PublicSubURL: c.PublicSubURL, ForwardAdmins: c.ForwardAdmins, AllowAnonymousChat: c.AllowAnonymousChat, LogDays: c.LogDays, MessageDays: c.MessageDays, BotUsername: c.BotUsername}, err
}

func SaveSettings(value UserTelegramSettings) error {
	c, err := Config()
	if err != nil {
		return fmt.Errorf("load user Telegram settings: %w", err)
	}
	token := strings.TrimSpace(value.Token)
	if token != "" {
		if !regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]+$`).MatchString(token) {
			return fmt.Errorf("invalid user Bot token format")
		}
		if c.Token != token {
			c.PollOffset, c.BotUsername = 0, ""
		}
		c.Token = token
	}
	if value.LogDays < 1 || value.LogDays > 3650 || value.MessageDays < 1 || value.MessageDays > 3650 {
		return fmt.Errorf("Telegram retention days must be between 1 and 3650")
	}
	base := strings.TrimSpace(value.PublicSubURL)
	if base != "" {
		u, parseErr := url.Parse(base)
		if parseErr != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("public subscription URL must be an HTTP(S) prefix without credentials, query or fragment")
		}
		base = strings.TrimRight(base, "/") + "/"
		if len(base) > 2048 {
			return fmt.Errorf("public subscription URL prefix exceeds 2048 bytes")
		}
	}
	if value.Enabled && (c.Token == "" || base == "") {
		return fmt.Errorf("enabling the user Bot requires its token and public subscription URL prefix")
	}
	_, hooks := Default.configuration()
	if value.Enabled && hooks.Network != nil {
		network, networkErr := hooks.Network()
		if networkErr != nil {
			return fmt.Errorf("load administrator Telegram configuration: %w", networkErr)
		}
		if c.Token != "" && c.Token == network.AdminToken {
			return fmt.Errorf("user Bot and administrator Bot must use different tokens")
		}
	}
	c.Enabled, c.PublicSubURL, c.ForwardAdmins = value.Enabled, base, value.ForwardAdmins
	c.AllowAnonymousChat = value.AllowAnonymousChat
	c.LogDays, c.MessageDays = value.LogDays, value.MessageDays
	c.TokenHash = codeHash(c.Token)
	if err := database.GetDB().Session(&gorm.Session{Logger: gormlogger.Discard}).Save(&c).Error; err != nil {
		return fmt.Errorf("save user Telegram settings: %w", err)
	}
	if err := RecordLog(0, 0, "settings.updated", fmt.Sprintf("enabled=%t forwardAdmins=%t allowAnonymousChat=%t logDays=%d messageDays=%d", c.Enabled, c.ForwardAdmins, c.AllowAnonymousChat, c.LogDays, c.MessageDays)); err != nil {
		return fmt.Errorf("record user Bot settings update: %w", err)
	}
	return Default.Reload()
}
