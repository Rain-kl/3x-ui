package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/audit"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func (AuditService) IsChild() bool {
	if os.Getenv("XUI_AUDIT_CHILD") == "1" {
		return true
	}
	var role model.Setting
	err := database.GetDB().Where("key = ?", "auditChildMode").First(&role).Error
	if err == nil && role.Value == "true" {
		return true
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return true
	}
	var count int64
	err = database.GetDB().Model(&model.ApiToken{}).Where("scope = ? AND enabled = ? AND (expires_at = 0 OR expires_at > ?)", model.ApiScopeNodeSync, true, time.Now().Unix()).Count(&count).Error
	return err != nil || count > 0
}

func (AuditService) IngestLocalPending(spool *audit.Spool) error {
	auditRetentionMu.Lock()
	defer auditRetentionMu.Unlock()
	if (AuditService{}).IsChild() {
		return nil
	}
	for range 5 {
		events := spool.Snapshot(1000)
		if len(events) == 0 {
			return nil
		}
		if err := ingestAudit(0, events); err != nil {
			return fmt.Errorf("ingest local audit: %w", err)
		}
		ids := make([]string, 0, len(events))
		for _, event := range events {
			ids = append(ids, event.ID)
		}
		if err := spool.Ack(ids); err != nil {
			return fmt.Errorf("ack local audit: %w", err)
		}
		if err := markAuditDelivered(0, ids); err != nil {
			return err
		}
	}
	return nil
}

func (AuditService) FetchPending(spool *audit.Spool, summarySupport ...bool) ([]audit.Event, error) {
	auditRetentionMu.Lock()
	defer auditRetentionMu.Unlock()
	var downstream int64
	if err := database.GetDB().Model(&model.Node{}).Where("enable = ?", true).Count(&downstream).Error; err != nil {
		return nil, err
	}
	if downstream > 0 {
		return nil, fmt.Errorf("audit export requires a direct child; a panel with downstream nodes must remain the audit master")
	}
	if err := (&SettingService{}).setBool("auditChildMode", true); err != nil {
		return nil, fmt.Errorf("persist audit child role: %w", err)
	}
	policy, err := (AuditService{}).Retention()
	if err != nil {
		return nil, err
	}
	// Recover all still-retained legacy detail, including visits already older than the detail window.
	since := time.Now().UTC().AddDate(0, 0, -policy.SummaryDays+1).Format("2006-01-02")
	legacySince, _ := time.Parse("2006-01-02", since)
	if detailSince := time.Now().UTC().Add(-time.Duration(policy.DetailHours) * time.Hour); detailSince.Before(legacySince) {
		legacySince = detailSince
	}
	if err := database.GetDB().Where("node_id = 0 AND (inbound = ? OR time < ? OR time > ?)", "api", legacySince, time.Now().UTC()).Delete(&model.AuditEvent{}).Error; err != nil {
		return nil, err
	}
	if err := database.GetDB().Where("node_id = 0 AND day < ?", since).Delete(&model.AuditDailyStat{}).Error; err != nil {
		return nil, err
	}
	var rows []model.AuditEvent
	err = database.GetDB().Where("node_id = 0 AND time >= ? AND time <= ? AND inbound <> ?", legacySince, time.Now().UTC(), "api").Order("time, id").Limit(1000).Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("read pending legacy audit: %w", err)
	}
	events := make([]audit.Event, 0, 1000)
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		observed := row.Host
		originalHost, _ := auditHost(row.Destination)
		if observed == originalHost {
			observed = ""
		}
		if _, err := strconv.Atoi(observed); err == nil {
			observed = ""
		}
		events = append(events, audit.Event{ID: row.ID, Time: row.Time, Inbound: row.Inbound, Outbound: row.Outbound, Client: row.Client, Source: row.Source, Destination: row.Destination, SniffedDomain: observed, Uplink: row.Uplink, Downlink: row.Downlink})
		seen[row.ID] = true
	}
	for _, event := range spool.Snapshot(1000) {
		if len(events) == 1000 {
			break
		}
		if !seen[event.ID] {
			events = append(events, event)
			seen[event.ID] = true
		}
	}
	if len(events) < 1000 && (len(summarySupport) == 0 || summarySupport[0]) {
		err := database.GetDB().Transaction(func(tx *gorm.DB) error {
			var days []model.AuditDailyStat
			if err := tx.Where("node_id = 0 AND day >= ? AND day <= ?", since, time.Now().UTC().Format("2006-01-02")).Order("day, client, site").Limit(1000 - len(events)).Find(&days).Error; err != nil {
				return err
			}
			for _, day := range days {
				if day.ExportID == "" {
					var id [16]byte
					if _, err := rand.Read(id[:]); err != nil {
						return err
					}
					day.ExportID = hex.EncodeToString(id[:])
					if err := tx.Model(&model.AuditDailyStat{}).Where("day = ? AND node_id = ? AND client = ? AND site = ?", day.Day, day.NodeID, day.Client, day.Site).Update("export_id", day.ExportID).Error; err != nil {
						return err
					}
				}
				when, err := time.Parse("2006-01-02", day.Day)
				if err != nil {
					return err
				}
				if !seen[day.ExportID] {
					events = append(events, audit.Event{ID: day.ExportID, Time: when, Client: day.Client, Destination: day.Site, Uplink: &day.Uplink, Downlink: &day.Downlink, Summary: &audit.SummaryMetrics{Connections: day.Connections, Measured: day.Measured}})
					seen[day.ExportID] = true
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return events, nil
}

func (AuditService) AckPending(spool *audit.Spool, ids []string) error {
	auditRetentionMu.Lock()
	defer auditRetentionMu.Unlock()
	if err := spool.Ack(ids); err != nil {
		return fmt.Errorf("ack audit spool: %w", err)
	}
	if len(ids) == 0 {
		return nil
	}
	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("node_id = 0 AND id IN ?", ids).Delete(&model.AuditEvent{}).Error; err != nil {
			return err
		}
		return tx.Where("node_id = 0 AND export_id <> ? AND export_id IN ?", "", ids).Delete(&model.AuditDailyStat{}).Error
	})
}

func (AuditService) MarkDelivered(nodeID int, ids []string) error {
	auditRetentionMu.Lock()
	defer auditRetentionMu.Unlock()
	return markAuditDelivered(nodeID, ids)
}

func markAuditDelivered(nodeID int, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.AuditEvent{}).Where("node_id = ? AND id IN ?", nodeID, ids).Update("delivered", true).Error; err != nil {
			return err
		}
		// Keep sparse receipts for delayed arrivals until expiry, including after ACK.
		return nil
	})
}
