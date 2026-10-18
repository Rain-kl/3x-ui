package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/telegramuser"
)

var (
	subscriptionSecurityWorker  sync.Mutex
	subscriptionSecurityCleanup time.Time
)

func ProcessSubscriptionLeak(hash string) error {
	subscriptionMutationMu.Lock()
	defer subscriptionMutationMu.Unlock()
	var risk model.SubscriptionLeak
	if err := database.GetDB().Where("link_hash = ?", hash).Take(&risk).Error; err != nil {
		return fmt.Errorf("load pending subscription leak: %w", err)
	}
	if (risk.DisabledAt > 0 && risk.AlertsQueued) || risk.ResolvedAt > 0 {
		return nil
	}
	var ids []int
	if err := json.Unmarshal([]byte(risk.Clients), &ids); err != nil {
		return fmt.Errorf("decode risk clients: %w", err)
	}
	var records []model.ClientRecord
	if err := database.GetDB().Where("id IN ?", ids).Find(&records).Error; err != nil {
		return err
	}
	var emails []string
	for _, record := range records {
		if subscriptionHash(record.SubID) == hash {
			emails = append(emails, record.Email)
		}
	}
	if len(emails) == 0 {
		return database.GetDB().Model(&risk).Update("resolved_at", time.Now().UnixMilli()).Error
	}
	var disableErr error
	if risk.DisabledAt == 0 {
		result, reload, mutationErr := (&ClientService{}).BulkSetEnable(&InboundService{}, emails, false)
		disableErr = mutationErr
		if len(result.Skipped) > 0 {
			disableErr = errors.Join(disableErr, fmt.Errorf("subscription disable skipped: %v", result.Skipped))
		}
		if reload {
			(&XrayService{}).SetToNeedRestart()
		}
		if reload || risk.Error != "" {
			var localCount int64
			if err := database.GetDB().Table("inbounds").Joins("JOIN client_inbounds ci ON ci.inbound_id = inbounds.id").Where("ci.client_id IN ? AND inbounds.node_id IS NULL", ids).Count(&localCount).Error; err != nil {
				disableErr = errors.Join(disableErr, err)
			} else if localCount > 0 {
				disableErr = errors.Join(disableErr, (&XrayService{}).RestartXray(false))
			}
		}
		for _, record := range records {
			if subscriptionHash(record.SubID) == hash {
				disableErr = errors.Join(disableErr, enforceSubscriptionRemoteState(record.Id))
			}
		}
	}
	alertErr := queueSubscriptionAlerts(risk, records, disableErr == nil)
	outcome := errors.Join(disableErr, alertErr)
	updates := map[string]any{"error": "", "next_attempt_at": time.Now().Add(30 * time.Second).UnixMilli()}
	if outcome != nil {
		updates["error"] = outcome.Error()
	}
	if disableErr == nil {
		if risk.DisabledAt == 0 {
			updates["disabled_at"] = time.Now().UnixMilli()
		}
		if alertErr == nil {
			updates["alerts_queued"] = true
		}
	}
	if err := submitTrafficWrite(func() error { return database.GetDB().Model(&risk).Updates(updates).Error }); err != nil {
		return errors.Join(outcome, fmt.Errorf("save subscription disable outcome: %w", err))
	}
	return outcome
}

func queueSubscriptionAlerts(risk model.SubscriptionLeak, clients []model.ClientRecord, disabled bool) error {
	network, err := userTelegramNetwork()
	if err != nil {
		return fmt.Errorf("resolve security notification recipients: %w", err)
	}
	var rows []model.SubscriptionSecurityAlert
	names := []string{}
	ids := []int{}
	for _, client := range clients {
		if subscriptionHash(client.SubID) == risk.LinkHash {
			names = append(names, client.Email)
			ids = append(ids, client.Id)
		}
	}
	if disabled {
		var bindings []model.TelegramBinding
		if err := database.GetDB().Where("client_id IN ?", ids).Find(&bindings).Error; err != nil {
			return err
		}
		for _, binding := range bindings {
			rows = append(rows, model.SubscriptionSecurityAlert{Episode: risk.Episode, Bot: "user", ChatID: binding.TgID, ClientID: binding.ClientID, Text: "你的订阅链接已被泄露,系统已自动停用, 请联系管理员解决。", State: "pending"})
		}
	}
	status := "已停用客户端及订阅。"
	if !disabled {
		status = "已阻断订阅获取，客户端停用尚待确认，系统将重试，请检查节点状态。"
	}
	text := fmt.Sprintf("订阅泄露告警\n客户端：%s\n最近10分钟不同IP：%d\n%s", strings.Join(names, ", "), risk.IPCount, status)
	if len([]rune(text)) > 3400 {
		text = string([]rune(text)[:3400])
	}
	for _, chatID := range network.AdminIDs {
		rows = append(rows, model.SubscriptionSecurityAlert{Episode: risk.Episode, Bot: "admin", ChatID: chatID, Text: text, State: "pending"})
	}
	if len(rows) == 0 {
		return nil
	}
	return submitTrafficWrite(func() error {
		return database.GetDB().Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(rows, 100).Error
	})
}

func MaintainSubscriptionSecurity() {
	if !subscriptionSecurityWorker.TryLock() {
		return
	}
	defer subscriptionSecurityWorker.Unlock()
	now := time.Now().UnixMilli()
	var pending []model.SubscriptionLeak
	if err := database.GetDB().Where("(disabled_at = 0 OR alerts_queued = ?) AND resolved_at = 0 AND next_attempt_at <= ?", false, now).Order("next_attempt_at").Limit(5).Find(&pending).Error; err != nil {
		logger.Warningf("query pending subscription security: %v", err)
		return
	}
	for _, risk := range pending {
		if err := ProcessSubscriptionLeak(risk.LinkHash); err != nil {
			logger.Warningf("retry subscription security %s: %v", risk.Episode, err)
		}
	}
	if err := deliverSubscriptionAlerts(now); err != nil {
		logger.Warningf("deliver subscription security alerts: %v", err)
	}
	if time.Since(subscriptionSecurityCleanup) > time.Minute {
		err := runSerializedTx(func(tx *gorm.DB) error {
			if err := tx.Where("seen_at <= ?", now-subscriptionWindow.Milliseconds()).Delete(&model.SubscriptionAccessIP{}).Error; err != nil {
				return err
			}
			cutoff := now - (30 * 24 * time.Hour).Milliseconds()
			if err := tx.Where("time < ?", cutoff).Delete(&model.SubscriptionAccess{}).Error; err != nil {
				return err
			}
			if err := tx.Where("created_at < ? AND state IN ?", cutoff, []string{"sent", "cancelled"}).Delete(&model.SubscriptionSecurityAlert{}).Error; err != nil {
				return err
			}
			return tx.Where("resolved_at > 0 AND resolved_at < ?", cutoff).Delete(&model.SubscriptionLeak{}).Error
		})
		if err != nil {
			logger.Warningf("clean subscription access history: %v", err)
		} else {
			subscriptionSecurityCleanup = time.Now()
		}
	}
}

func deliverSubscriptionAlerts(now int64) error {
	var rows []model.SubscriptionSecurityAlert
	if err := database.GetDB().Where("state = ? AND next_attempt_at <= ?", "pending", now).Order("id").Limit(10).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	network, err := userTelegramNetwork()
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Bot == "user" {
			var count int64
			if err := database.GetDB().Model(&model.TelegramBinding{}).Joins("JOIN clients ON clients.id = telegram_bindings.client_id").Where("client_id = ? AND telegram_bindings.tg_id = ? AND clients.enable = ?", row.ClientID, row.ChatID, false).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				if err := database.GetDB().Model(&row).Update("state", "cancelled").Error; err != nil {
					return err
				}
				continue
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		sendErr := telegramuser.SendSecurityAlert(ctx, network, row.Bot, row.ChatID, row.ClientID, row.Text)
		cancel()
		updates := map[string]any{"attempts": row.Attempts + 1, "state": "sent", "error": ""}
		if sendErr != nil {
			updates["error"] = sendErr.Error()
			updates["state"] = "pending"
			updates["next_attempt_at"] = time.Now().Add(time.Duration(min(row.Attempts+1, 20)) * 30 * time.Second).UnixMilli()
			if row.Attempts >= 11 {
				updates["state"] = "failed"
			}
		}
		if err := submitTrafficWrite(func() error { return database.GetDB().Model(&row).Updates(updates).Error }); err != nil {
			return fmt.Errorf("save subscription alert delivery: %w", err)
		}
	}
	return nil
}
