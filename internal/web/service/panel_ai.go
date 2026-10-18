package service

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/panelai"
	"github.com/mhsanaei/3x-ui/v3/internal/telegramuser"
)

func PanelAIHooks() panelai.Hooks {
	return panelai.Hooks{Audit: queryAIAudit, Notify: func(ctx context.Context, text string) error {
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

func queryAIAudit(parent context.Context, q panelai.AuditQuery) (any, error) {
	filter := AuditFilter{Hours: q.Hours, Client: q.Client, Site: q.Site, NodeID: q.NodeID}
	if q.Mode == "rates" {
		return (AuditService{}).QueryRates(filter)
	}
	if q.Mode == "history" {
		return (AuditService{}).QueryRateHistory(filter)
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	var result any
	err := auditReadSnapshotContext(ctx, func(tx *gorm.DB) error {
		if isAuditChild(tx) {
			return fmt.Errorf("audit data is centralized; use the master panel AI")
		}
		state, err := readAuditPolicyState(tx)
		if err != nil {
			return err
		}
		policy := retentionFromControl(state.Control)
		now := time.Now().UTC()
		if q.Mode == "metadata" {
			result = map[string]any{"serverTime": now, "retention": policy, "rateAvailableFrom": state.rateAvailableFrom, "tables": []string{"audit_events", "audit_daily_stats", "audit_minute_stats", "audit_receipts"}, "units": "uplink/downlink in bytes; rates per second over the last 60 seconds; minute history retains 24 hours", "coverage": "Detailed events and archived daily summaries are disjoint. Queries merge them once. Details cannot be reconstructed after aggregation. Paginate clients and client_sites; sites are registered-domain groups and may include IP targets."}
			return nil
		}
		limit := q.Limit
		if q.Mode == "details" {
			rows := []model.AuditEvent{}
			if err := auditScopedQuery(tx.Model(&model.AuditEvent{}), filter, policy, now).Order("time DESC, id").Limit(limit + 1).Offset((q.Page - 1) * limit).Find(&rows).Error; err != nil {
				return fmt.Errorf("query AI audit details: %w", err)
			}
			more := len(rows) > limit
			if more {
				rows = rows[:limit]
			}
			result = map[string]any{"items": rows, "hasMore": more, "page": q.Page, "limit": limit}
			return nil
		}
		group := ""
		switch q.Mode {
		case "clients":
			group = "client"
		case "sites":
			group = "site"
		case "client_sites":
			group = "client, site"
		default:
			return fmt.Errorf("unknown audit query mode %q", q.Mode)
		}
		rows := []map[string]any{}
		base := auditMetricsQuery(tx, filter, policy, now)
		selection := group + ", SUM(connections) AS connections, COALESCE(SUM(uplink),0) AS uplink, COALESCE(SUM(downlink),0) AS downlink, SUM(measured) AS measured"
		if err := base.Select(selection).Group(group).Order("COALESCE(SUM(uplink),0)+COALESCE(SUM(downlink),0) DESC, " + group).Limit(limit + 1).Offset((q.Page - 1) * limit).Scan(&rows).Error; err != nil {
			return fmt.Errorf("query AI audit %s: %w", q.Mode, err)
		}
		more := len(rows) > limit
		if more {
			rows = rows[:limit]
		}
		result = map[string]any{"items": rows, "hasMore": more, "page": q.Page, "limit": limit, "hours": q.Hours, "asOf": now}
		return nil
	})
	return result, err
}
