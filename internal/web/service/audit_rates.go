package service

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type AuditRateCounts struct {
	Connections int64 `json:"connections" example:"600"`
	Sites       int64 `json:"sites" example:"60"`
}

type AuditRateAlert struct {
	Client      string `json:"client" example:"alice"`
	Connections int64  `json:"connections" example:"600"`
	Sites       int64  `json:"sites" example:"60"`
}

type AuditRateSnapshot struct {
	WindowStart time.Time        `json:"windowStart" example:"2026-10-14T00:00:00Z"`
	WindowEnd   time.Time        `json:"windowEnd" example:"2026-10-14T00:01:00Z"`
	Current     AuditRateCounts  `json:"current"`
	Alerts      []AuditRateAlert `json:"alerts"`
	Centralized bool             `json:"centralized"`
}

type AuditRatePoint struct {
	Time        int64 `json:"time" example:"1791936000"`
	Connections int64 `json:"connections" example:"120"`
	Sites       int64 `json:"sites" example:"30"`
}

type AuditRateHistory struct {
	WindowStart   time.Time        `json:"windowStart" example:"2026-10-13T00:01:00Z"`
	WindowEnd     time.Time        `json:"windowEnd" example:"2026-10-14T00:01:00Z"`
	AvailableFrom time.Time        `json:"availableFrom" example:"2026-10-13T00:01:00Z"`
	Points        []AuditRatePoint `json:"points"`
	Centralized   bool             `json:"centralized"`
}

func validateAuditRateFilter(f AuditFilter) error {
	if len(f.Client) > 256 || len(f.Site) > 256 {
		return fmt.Errorf("audit rate filter is too long")
	}
	return nil
}

func (AuditService) QueryRates(f AuditFilter) (*AuditRateSnapshot, error) {
	if err := validateAuditRateFilter(f); err != nil {
		return nil, err
	}
	result := &AuditRateSnapshot{Alerts: []AuditRateAlert{}}
	err := auditReadSnapshot(func(db *gorm.DB) error {
		return queryAuditRates(db, f, time.Now().UTC(), result)
	})
	return result, err
}

func queryAuditRates(db *gorm.DB, f AuditFilter, now time.Time, result *AuditRateSnapshot) error {
	state, err := readAuditControl(db)
	if err != nil {
		return err
	}
	result.WindowStart, result.WindowEnd = now.Add(-time.Minute), now
	if isAuditChild(db) {
		result.Centralized = true
		return nil
	}
	base := func() *gorm.DB {
		return auditDimensions(db.Model(&model.AuditEvent{}).Where("inbound <> ? AND time >= ? AND time <= ? AND time >= ?", "api", result.WindowStart, now, state.ClearBefore), f)
	}
	if err := base().Select("COUNT(*) AS connections, COUNT(DISTINCT NULLIF(site, '')) AS sites").Scan(&result.Current).Error; err != nil {
		return fmt.Errorf("query current audit rates: %w", err)
	}
	conditions, args := []string{}, []any{}
	if state.ConnectionRateLimit > 0 {
		conditions, args = append(conditions, "COUNT(*) >= ?"), append(args, state.ConnectionRateLimit*60)
	}
	if state.SiteRateLimit > 0 {
		conditions, args = append(conditions, "COUNT(DISTINCT NULLIF(site, '')) >= ?"), append(args, state.SiteRateLimit*60)
	}
	if len(conditions) == 0 {
		return nil
	}
	if err := base().Where("client <> ''").Select("client, COUNT(*) AS connections, COUNT(DISTINCT NULLIF(site, '')) AS sites").Group("client").Having(strings.Join(conditions, " OR "), args...).Order("connections DESC, client ASC").Limit(20).Scan(&result.Alerts).Error; err != nil {
		return fmt.Errorf("query high-frequency audit clients: %w", err)
	}
	return nil
}

func (AuditService) QueryRateHistory(f AuditFilter) (*AuditRateHistory, error) {
	if err := validateAuditRateFilter(f); err != nil {
		return nil, err
	}
	result := &AuditRateHistory{Points: []AuditRatePoint{}}
	err := auditReadSnapshot(func(db *gorm.DB) error {
		return queryAuditRateHistory(db, f, time.Now().UTC(), result)
	})
	return result, err
}

func queryAuditRateHistory(db *gorm.DB, f AuditFilter, now time.Time, result *AuditRateHistory) error {
	state, err := readAuditPolicyState(db)
	if err != nil {
		return err
	}
	end := now.Truncate(time.Minute)
	start := end.Add(-24 * time.Hour)
	result.WindowStart, result.WindowEnd = start, end
	result.AvailableFrom = maxTime(start, state.rateAvailableFrom)
	if isAuditChild(db) {
		result.Centralized = true
		return nil
	}
	compact := auditDimensions(db.Model(&model.AuditMinuteStat{}).Where("minute >= ? AND minute < ?", start.Unix(), end.Unix()), f)
	var rows []AuditRatePoint
	if err := compact.
		Select("minute AS time, SUM(connections) AS connections, COUNT(DISTINCT NULLIF(site, '')) AS sites").Group("minute").Order("minute").Scan(&rows).Error; err != nil {
		return fmt.Errorf("query audit minute history: %w", err)
	}
	result.AvailableFrom = maxTime(result.AvailableFrom, state.ClearBefore)
	lookup := make(map[int64]AuditRatePoint, len(rows))
	for _, row := range rows {
		lookup[row.Time] = row
	}
	result.Points = make([]AuditRatePoint, 0, 1440)
	for minute := start.Unix(); minute < end.Unix(); minute += 60 {
		row := lookup[minute]
		row.Time = minute
		result.Points = append(result.Points, row)
	}
	return nil
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func upsertAuditMinutes(tx *gorm.DB, rows []model.AuditMinuteStat) error {
	if len(rows) == 0 {
		return nil
	}
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "minute"}, {Name: "node_id"}, {Name: "client"}, {Name: "site"}},
		DoUpdates: clause.Assignments(map[string]any{"connections": gorm.Expr("audit_minute_stats.connections + excluded.connections")}),
	}).CreateInBatches(rows, 200).Error; err != nil {
		return fmt.Errorf("update audit minute counters: %w", err)
	}
	return nil
}

func expireAuditMinutes(tx *gorm.DB, now time.Time) error {
	if err := tx.Where("minute < ? OR minute > ?", now.Add(-24*time.Hour).Truncate(time.Minute).Unix(), now.Unix()).Delete(&model.AuditMinuteStat{}).Error; err != nil {
		return fmt.Errorf("expire audit minute metrics: %w", err)
	}
	return nil
}

func addAuditMinute(groups map[string]model.AuditMinuteStat, row model.AuditEvent) {
	minute := row.Time.Truncate(time.Minute).Unix()
	key := fmt.Sprintf("%d\x00%d\x00%s\x00%s", minute, row.NodeID, row.Client, row.Site)
	value := groups[key]
	value.Minute, value.NodeID, value.Client, value.Site = minute, row.NodeID, row.Client, row.Site
	value.Connections++
	groups[key] = value
}

// RETURNING prevents duplicate node deliveries from incrementing minute counters.
func insertAuditDetails(tx *gorm.DB, rows []model.AuditEvent) (map[string]bool, error) {
	accepted := make(map[string]bool, len(rows))
	for start := 0; start < len(rows); start += 200 {
		batch := rows[start:min(start+200, len(rows))]
		values, args := make([]string, 0, len(batch)), make([]any, 0, len(batch)*13)
		for _, row := range batch {
			values = append(values, "(?,?,?,?,?,?,?,?,?,?,?,?,?)")
			args = append(args, row.Delivered, row.ID, row.NodeID, row.Time, row.Inbound, row.Outbound, row.Client, row.Source, row.Destination, row.Host, row.Site, row.Uplink, row.Downlink)
		}
		var inserted []struct{ ID string }
		query := "INSERT INTO audit_events (delivered,id,node_id,time,inbound,outbound,client,source,destination,host,site,uplink,downlink) VALUES " + strings.Join(values, ",") + " ON CONFLICT (id) DO NOTHING RETURNING id"
		if err := tx.Raw(query, args...).Scan(&inserted).Error; err != nil {
			return nil, fmt.Errorf("insert audit details for minute accounting: %w", err)
		}
		for _, row := range inserted {
			accepted[row.ID] = true
		}
	}
	return accepted, nil
}
