package service

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mhsanaei/3x-ui/v3/internal/telegramuser"
)

func UserTelegramHooks() telegramuser.Hooks {
	return telegramuser.Hooks{SubscriptionDetails: userTelegramSubscriptionDetails, Network: userTelegramNetwork, Subscription: userTelegramSubscription, Usage: userTelegramUsage}
}

func userTelegramNetwork() (telegramuser.Network, error) {
	s := &SettingService{}
	var result telegramuser.Network
	var err error
	if result.Proxy, err = s.GetTgBotProxy(); err != nil {
		return result, err
	}
	if result.APIServer, err = s.GetTgBotAPIServer(); err != nil {
		return result, err
	}
	if result.AdminToken, err = s.GetTgBotToken(); err != nil {
		return result, err
	}
	raw, err := s.GetTgBotChatId()
	if err != nil {
		return result, err
	}
	for _, value := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\n' }) {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id == 0 {
			return result, fmt.Errorf("invalid configured administrator Telegram chat ID")
		}
		result.AdminIDs = append(result.AdminIDs, id)
	}
	return result, nil
}

func userTelegramSubscription(clientID int, reset bool) (string, error) {
	s := &ClientService{}
	record, err := s.GetByID(clientID)
	if err != nil {
		return "", fmt.Errorf("load subscription client: %w", err)
	}
	if reset || record.SubID == "" {
		client := record.ToClient()
		client.SubID = uuid.NewString()
		needRestart, updateErr := s.Update(&InboundService{}, clientID, *client, record.LimitHwid)
		if needRestart {
			(&XrayService{}).SetToNeedRestart()
		}
		if updateErr != nil {
			return "", fmt.Errorf("reset user subscription: %w", updateErr)
		}
		record.SubID = client.SubID
	}
	cfg, err := telegramuser.Config()
	if err != nil {
		return "", fmt.Errorf("read public user Bot subscription address: %w", err)
	}
	if cfg.PublicSubURL == "" {
		return "", fmt.Errorf("public user Bot subscription address is not configured")
	}
	return cfg.PublicSubURL + url.PathEscape(record.SubID), nil
}

func userTelegramUsage(clientID int) (string, error) {
	record, err := (&ClientService{}).GetByID(clientID)
	if err != nil {
		return "", err
	}
	traffic, err := (&InboundService{}).GetClientTrafficByEmail(record.Email)
	if err != nil {
		return "", fmt.Errorf("read user subscription traffic: %w", err)
	}
	remaining := "不限"
	used := int64(0)
	if traffic != nil {
		used = traffic.Up + traffic.Down
	}
	if record.TotalGB > 0 {
		remaining = fmt.Sprintf("%.2f GiB", float64(max(record.TotalGB-used, 0))/(1<<30))
	}
	days := "不限"
	if record.ExpiryTime > 0 {
		days = fmt.Sprintf("%d 天", int64(math.Ceil(float64(max(record.ExpiryTime-time.Now().UnixMilli(), 0))/float64((24*time.Hour).Milliseconds()))))
	} else if record.ExpiryTime < 0 {
		days = fmt.Sprintf("尚未开始计时，%d 天", int64(math.Ceil(float64(-record.ExpiryTime)/float64((24*time.Hour).Milliseconds()))))
	}
	status := "正常"
	if !record.Enable {
		status = "已禁用"
	} else if record.ExpiryTime > 0 && record.ExpiryTime <= time.Now().UnixMilli() {
		status = "已到期"
	} else if record.TotalGB > 0 && used >= record.TotalGB {
		status = "流量已用尽"
	}
	return fmt.Sprintf("客户端：%s\n状态：%s\n剩余流量：%s\n已用流量：%.2f GiB\n剩余时间：%s", record.Email, status, remaining, float64(used)/(1<<30), days), nil
}

// FORK(telegram-subscription-details): Depend on the registered subscription renderer to preserve protocol-specific behavior.
func userTelegramSubscriptionDetails(clientID int) (telegramuser.SubscriptionDetails, error) {
	provider, ok := registeredSubLinkProvider.(interface {
		BotSubscriptionDetails(string, string) (telegramuser.SubscriptionDetails, error)
	})
	if !ok {
		return telegramuser.SubscriptionDetails{}, fmt.Errorf("subscription details provider is not registered")
	}
	record, err := (&ClientService{}).GetByID(clientID)
	if err != nil {
		return telegramuser.SubscriptionDetails{}, fmt.Errorf("load subscription details client: %w", err)
	}
	if record.SubID == "" {
		return telegramuser.SubscriptionDetails{}, fmt.Errorf("client has no subscription identifier; request /subscription first")
	}
	cfg, err := telegramuser.Config()
	if err != nil {
		return telegramuser.SubscriptionDetails{}, fmt.Errorf("load subscription public URL: %w", err)
	}
	address, err := url.Parse(cfg.PublicSubURL)
	if err != nil || address.Hostname() == "" {
		return telegramuser.SubscriptionDetails{}, fmt.Errorf("public user Bot subscription URL is invalid")
	}
	return provider.BotSubscriptionDetails(address.Hostname(), record.SubID)
}
