package service

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/mhsanaei/3x-ui/v3/internal/audit"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

type AuditFilter struct {
	Day    string
	Hours  int
	NodeID int
	Client string
	Site   string
	Offset int
}

// FORK(audit-workbench): Keep ranking API examples derived from the response contract.
type AuditAggregate struct {
	Name        string `json:"name" example:"example.com"`
	Connections int64  `json:"connections" example:"12"`
	Uplink      int64  `json:"uplink" example:"1024"`
	Downlink    int64  `json:"downlink" example:"8192"`
	Measured    int64  `json:"measured" example:"12"`
	UniqueSites int64  `json:"uniqueSites" example:"1"`
}

type AuditSummary struct {
	Days        []model.AuditDailyStat `json:"days"`
	DailyTotal  int64                  `json:"dailyTotal"`
	Centralized bool                   `json:"centralized"`
	Connections int64                  `json:"connections"`
	Uplink      int64                  `json:"uplink"`
	Downlink    int64                  `json:"downlink"`
	Measured    int64                  `json:"measured"`
	Sites       []AuditAggregate       `json:"sites"`
	Clients     []AuditAggregate       `json:"clients"`
	Alerts      []AuditAggregate       `json:"alerts"`
	Hours       []AuditAggregate       `json:"hours"`
	Events      []model.AuditEvent     `json:"events"`
	Total       int64                  `json:"total"`
	Nodes       []AuditNode            `json:"nodes"`
}

type AuditNode struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type AuditService struct{}

var auditRetentionMu sync.Mutex

func auditHours(hours int) int {
	if hours > 0 && hours <= 8760 {
		return hours
	}
	return 24
}

func auditDimensions(db *gorm.DB, f AuditFilter) *gorm.DB {
	if f.NodeID > 0 {
		db = db.Where("node_id = ?", f.NodeID)
	} else if f.NodeID == -1 {
		db = db.Where("node_id = ?", 0)
	}
	if f.Client != "" {
		db = db.Where("client = ?", f.Client)
	}
	if f.Site != "" {
		db = db.Where("site = ?", strings.ToLower(f.Site))
	}
	return db
}

func auditHost(destination string) (string, string) {
	// FORK(sniffed-audit-domains): HTTP access targets are URLs, not tcp-prefixed addresses.
	address := strings.TrimPrefix(strings.TrimPrefix(destination, "tcp:"), "udp:")
	if strings.HasPrefix(address, "//") || strings.Contains(address, "://") {
		if parsed, err := url.Parse(address); err == nil && parsed.Host != "" {
			address = parsed.Host
		}
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	host = strings.ToLower(strings.Trim(strings.TrimSpace(host), "[] ."))
	if host == "" || net.ParseIP(host) != nil {
		return host, host
	}
	site, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		site = host
	}
	return host, site
}

func (AuditService) Ingest(nodeID int, events []audit.Event) error {
	auditRetentionMu.Lock()
	defer auditRetentionMu.Unlock()
	return ingestAudit(nodeID, events)
}

func ingestAudit(nodeID int, events []audit.Event) error {
	policy, err := (AuditService{}).Retention()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	cutoff := now.Add(-time.Duration(policy.DetailHours) * time.Hour)
	retainedDay := now.AddDate(0, 0, -policy.SummaryDays+1).Format("2006-01-02")
	rows := make([]model.AuditEvent, 0, len(events))
	pending := false
	summaries := make(map[string]*audit.SummaryMetrics)
	for _, e := range events {
		if e.Time.After(now) {
			if e.Time.After(now.Add(5 * time.Minute)) {
				continue
			}
			e.Time = now
		}
		if e.Summary != nil {
			if e.Summary.Connections < 1 || e.Summary.Measured < 0 || e.Summary.Measured > e.Summary.Connections || e.Time.UTC().Format("2006-01-02") < retainedDay {
				continue
			}
			summaries[e.ID] = e.Summary
		}
		// FORK(audit-workbench): Ignore legacy and child-node control-plane events.
		if e.Inbound == "api" || e.ID == "" || e.Time.IsZero() || e.Time.Before(policy.ClearBefore) || (e.Time.Before(cutoff) && e.Time.UTC().Format("2006-01-02") < retainedDay) || e.Destination == "" {
			continue
		}
		host, site := auditHost(e.Destination)
		// FORK(sniffed-audit-domains): Attribute websites by observed domain while preserving original IP.
		if e.SniffedDomain != "" {
			host, site = auditHost(e.SniffedDomain)
		}
		_, client, scoped := xray.ParseScopedEmail(e.Client)
		if !scoped {
			client = e.Client
		}
		rows = append(rows, model.AuditEvent{
			Delivered: &pending, ID: e.ID, NodeID: nodeID, Time: e.Time, Inbound: e.Inbound,
			Outbound: e.Outbound, Client: client, Source: e.Source,
			Destination: e.Destination, Host: host, Site: site,
			Uplink: e.Uplink, Downlink: e.Downlink,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		fresh := make([]model.AuditEvent, 0, len(rows))
		groups := make(map[string]model.AuditDailyStat)
		// FORK(audit-workbench): Only newly accepted visits contribute to compact minute history.
		minutes := make(map[string]model.AuditMinuteStat)

		archived := make([]model.AuditEvent, 0, len(rows))
		for _, row := range rows {
			if !row.Time.Before(cutoff) && summaries[row.ID] == nil {
				fresh = append(fresh, row)
			} else {
				archived = append(archived, row)
			}
		}
		accepted, err := recordAuditReceipts(tx, archived)
		if err != nil {
			return err
		}
		for _, row := range archived {
			if !accepted[row.ID] {
				continue
			}
			delete(accepted, row.ID)

			day := row.Time.UTC().Format("2006-01-02")
			if summaries[row.ID] == nil && !row.Time.Before(now.Add(-24*time.Hour).Truncate(time.Minute)) {
				addAuditMinute(minutes, row)
			}
			key := fmt.Sprintf("%s\x00%d\x00%s\x00%s", day, nodeID, row.Client, row.Site)
			group := groups[key]
			group.Day = day
			group.NodeID = nodeID
			group.Client = row.Client
			group.Site = row.Site
			connections, measured := int64(1), int64(0)
			if row.Uplink != nil {
				measured = 1
			}
			if summary := summaries[row.ID]; summary != nil {
				connections, measured = summary.Connections, summary.Measured
			}
			group.Connections += connections
			group.Measured += measured
			if row.Uplink != nil {
				group.Uplink += *row.Uplink
			}
			if row.Downlink != nil {
				group.Downlink += *row.Downlink
			}
			groups[key] = group
		}
		if len(fresh) > 0 {
			accepted, err := insertAuditDetails(tx, fresh)
			if err != nil {
				return err
			}
			for _, row := range fresh {
				if accepted[row.ID] && !row.Time.Before(now.Add(-24*time.Hour).Truncate(time.Minute)) {
					addAuditMinute(minutes, row)
					delete(accepted, row.ID)
				}
			}
		}
		if err := upsertAuditDays(tx, groupsToSlice(groups)); err != nil {
			return err
		}
		minuteRows := make([]model.AuditMinuteStat, 0, len(minutes))
		for _, row := range minutes {
			minuteRows = append(minuteRows, row)
		}
		return upsertAuditMinutes(tx, minuteRows)
	})
}

func groupsToSlice(groups map[string]model.AuditDailyStat) []model.AuditDailyStat {
	rows := make([]model.AuditDailyStat, 0, len(groups))
	for _, row := range groups {
		rows = append(rows, row)
	}
	return rows
}

func upsertAuditDays(tx *gorm.DB, groups []model.AuditDailyStat) error {
	if len(groups) == 0 {
		return nil
	}
	assignments := map[string]any{}
	for _, col := range []string{"connections", "uplink", "downlink", "measured"} {
		assignments[col] = gorm.Expr("audit_daily_stats." + col + " + excluded." + col)
	}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "day"}, {Name: "node_id"}, {Name: "client"}, {Name: "site"}}, DoUpdates: clause.Assignments(assignments)}).CreateInBatches(groups, 200).Error
}

func auditScopedQuery(db *gorm.DB, f AuditFilter, policy AuditRetention, now time.Time) *gorm.DB {
	// FORK(audit-retention): Detail pagination never exposes archived visits.
	hours := min(auditHours(f.Hours), policy.DetailHours)
	db = db.Where("inbound <> ? AND time >= ? AND time <= ?", "api", now.Add(-time.Duration(hours)*time.Hour), now)
	if f.Day != "" {
		start, _ := time.Parse("2006-01-02", f.Day)
		db = db.Where("time >= ? AND time < ?", start, start.AddDate(0, 0, 1))
	}
	return auditDimensions(db, f)
}

func auditMetricsQuery(db *gorm.DB, f AuditFilter, policy AuditRetention, now time.Time) *gorm.DB {
	hours := auditHours(f.Hours)
	since := now.Add(-time.Duration(hours) * time.Hour)
	if hours > policy.DetailHours {
		since, _ = time.Parse("2006-01-02", now.AddDate(0, 0, -(hours+23)/24+1).Format("2006-01-02"))
	}
	dateExpr := "strftime('%Y-%m-%d', time)"
	if database.IsPostgres() {
		dateExpr = "to_char(time AT TIME ZONE 'UTC', 'YYYY-MM-DD')"
	}
	sql := "SELECT " + dateExpr + " AS day, node_id, client, site, 1 AS connections, COALESCE(uplink,0) AS uplink, COALESCE(downlink,0) AS downlink, CASE WHEN uplink IS NULL THEN 0 ELSE 1 END AS measured FROM audit_events WHERE inbound <> ? AND time >= ?"
	args := []any{"api", since}
	if f.Day != "" {
		start, _ := time.Parse("2006-01-02", f.Day)
		since = start
		sql += " AND time < ?"
		args = []any{"api", since, start.AddDate(0, 0, 1)}
		sql += " AND time >= ?"
		args = append(args, now.AddDate(0, 0, -policy.SummaryDays+1).Format("2006-01-02"))
	}
	sql += " AND time <= ?"
	args = append(args, now)
	if hours > policy.DetailHours || f.Day != "" {
		day := now.AddDate(0, 0, -(hours+23)/24+1).Format("2006-01-02")
		if f.Day != "" {
			day = now.AddDate(0, 0, -policy.SummaryDays+1).Format("2006-01-02")
		}
		sql += " UNION ALL SELECT day, node_id, client, site, connections, uplink, downlink, measured FROM audit_daily_stats WHERE day >= ?"
		args = append(args, day)
		if f.Day != "" {
			sql += " AND day = ?"
			args = append(args, f.Day)
		}

	}
	// FORK(audit-retention): Enforce configured retention and reject abnormal future rows in all windows.
	if hours > policy.DetailHours || f.Day != "" {
		sql = "SELECT * FROM (" + sql + ") AS source_metrics WHERE day >= ? AND day <= ?"
		args = append(args, now.AddDate(0, 0, -policy.SummaryDays+1).Format("2006-01-02"), now.Format("2006-01-02"))
	}
	return auditDimensions(db.Table("(?) AS audit_metrics", db.Raw(sql, args...)), f)
}

// FORK(audit-retention): Consistent read snapshots keep dashboard scans from blocking source ingestion.
func (AuditService) Query(f AuditFilter) (*AuditSummary, error) {
	var result *AuditSummary
	err := auditReadSnapshot(func(tx *gorm.DB) error {
		var err error
		result, err = queryAudit(tx, f)
		return err
	})
	return result, err
}

func queryAudit(db *gorm.DB, f AuditFilter) (*AuditSummary, error) {
	now := time.Now().UTC()
	state, err := readAuditControl(db)
	policy := retentionFromControl(state)
	if err != nil {
		return nil, err
	}
	if f.Day != "" {
		if _, err := time.Parse("2006-01-02", f.Day); err != nil {
			return nil, fmt.Errorf("invalid audit day %q: %w", f.Day, err)
		}
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	if f.Offset > 100000 {
		f.Offset = 100000
	}
	result := &AuditSummary{
		Days:  []model.AuditDailyStat{},
		Sites: []AuditAggregate{}, Clients: []AuditAggregate{}, Alerts: []AuditAggregate{},
		Hours: []AuditAggregate{}, Events: []model.AuditEvent{}, Nodes: []AuditNode{},
	}
	// FORK(audit-node-consistency): Child panels do not advertise independent local audit totals.
	if isAuditChild(db) {
		result.Centralized = true
		return result, nil
	}
	if err := db.Model(&model.Node{}).Select("id, name").Order("id").Scan(&result.Nodes).Error; err != nil {
		return nil, err
	}
	base := func() *gorm.DB { return auditMetricsQuery(db, f, policy, now) }
	details := func() *gorm.DB { return auditScopedQuery(db.Model(&model.AuditEvent{}), f, policy, now) }
	if f.Day == "" && auditHours(f.Hours) <= policy.DetailHours {
		if err := details().Count(&result.Total).Error; err != nil {
			return nil, err
		}
	}
	type totals struct {
		Connections int64
		Uplink      int64
		Downlink    int64
		Measured    int64
	}
	var total totals
	if err := base().Select("COALESCE(SUM(connections), 0) AS connections, COALESCE(SUM(uplink), 0) AS uplink, COALESCE(SUM(downlink), 0) AS downlink, COALESCE(SUM(measured),0) AS measured").Scan(&total).Error; err != nil {
		return nil, err
	}
	result.Connections, result.Uplink, result.Downlink, result.Measured = total.Connections, total.Uplink, total.Downlink, total.Measured
	for _, item := range []struct {
		field  string
		target *[]AuditAggregate
	}{{"site", &result.Sites}, {"client", &result.Clients}} {
		selectExpr := item.field + " AS name, SUM(connections) AS connections, COALESCE(SUM(uplink), 0) AS uplink, COALESCE(SUM(downlink), 0) AS downlink, SUM(measured) AS measured, COUNT(DISTINCT site) AS unique_sites"
		query := base().Select(selectExpr).Group(item.field).Order("connections DESC")
		// FORK(audit-workbench): Dashboard and full site ranking must share grouping and stable ordering.
		if item.field == "site" {
			query = auditSiteRanking(base())
		}
		if err := query.Limit(20).Scan(item.target).Error; err != nil {
			return nil, err
		}
	}
	if auditHours(f.Hours) > policy.DetailHours || f.Day != "" {
		// FORK(audit-retention): A row is one UTC date/client/site/node, including fresh visits for that date.
		grouped := base().Select("day,node_id,client,site").Group("day,node_id,client,site")
		if err := db.Table("(?) AS daily_groups", grouped).Count(&result.DailyTotal).Error; err != nil {
			return nil, err
		}
		if err := base().Select("day,node_id,client,site,SUM(connections) AS connections,SUM(uplink) AS uplink,SUM(downlink) AS downlink,SUM(measured) AS measured").Group("day,node_id,client,site").Order("day DESC, SUM(uplink)+SUM(downlink) DESC").Limit(100).Offset(f.Offset).Scan(&result.Days).Error; err != nil {
			return nil, err
		}
	}
	// FORK(audit-workbench): High-frequency detection belongs to the fixed one-minute rate query.
	if auditHours(f.Hours) <= policy.DetailHours && f.Day == "" {
		hourExpr := "strftime('%Y-%m-%dT%H:00:00Z', time)"
		if database.IsPostgres() {
			hourExpr = "to_char(date_trunc('hour', time AT TIME ZONE 'UTC'), 'YYYY-MM-DD\"T\"HH24:00:00\"Z\"')"
		}
		if err := details().Select(hourExpr + " AS name, COUNT(*) AS connections, COALESCE(SUM(uplink), 0) AS uplink, COALESCE(SUM(downlink), 0) AS downlink, COUNT(uplink) AS measured").Group(hourExpr).Order("name").Scan(&result.Hours).Error; err != nil {
			return nil, err
		}
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	if f.Offset > 100000 {
		f.Offset = 100000
	}
	if f.Day == "" && auditHours(f.Hours) <= policy.DetailHours {
		if err := details().Order("time DESC").Limit(100).Offset(f.Offset).Find(&result.Events).Error; err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (AuditService) Prune() error {
	// FORK(audit-retention): Archive and delete in one transaction so retries cannot duplicate totals.
	auditRetentionMu.Lock()
	defer auditRetentionMu.Unlock()
	policy, err := (AuditService{}).Retention()
	if err != nil {
		return err
	}
	return database.GetDB().Transaction(func(tx *gorm.DB) error { return pruneAudit(tx, policy) })
}

func pruneAudit(tx *gorm.DB, policy AuditRetention) error {
	now := time.Now().UTC()
	cutoff := now.Add(-time.Duration(policy.DetailHours) * time.Hour)
	day := now.AddDate(0, 0, -policy.SummaryDays+1).Format("2006-01-02")
	// FORK(audit-workbench): Minute counters retain a fixed 24-hour history independent of detail retention.
	if err := expireAuditMinutes(tx, now); err != nil {
		return err
	}
	if err := tx.Where("inbound = ? OR (time < ? AND time < ?) OR time > ?", "api", day, cutoff, now).Delete(&model.AuditEvent{}).Error; err != nil {
		return err
	}
	dateExpr := "strftime('%Y-%m-%d', time)"
	if database.IsPostgres() {
		dateExpr = "to_char(time AT TIME ZONE 'UTC', 'YYYY-MM-DD')"
	}
	var groups []model.AuditDailyStat
	err := tx.Model(&model.AuditEvent{}).Where("time < ?", cutoff).
		Select(dateExpr + " AS day, node_id, client, site, COUNT(*) AS connections, COALESCE(SUM(uplink),0) AS uplink, COALESCE(SUM(downlink),0) AS downlink, COUNT(uplink) AS measured").
		Group(dateExpr + ", node_id, client, site").Scan(&groups).Error
	if err != nil {
		return err
	}
	var pending []model.AuditEvent
	if err := tx.Select("id,node_id,time").Where("time < ? AND delivered = ?", cutoff, false).Find(&pending).Error; err != nil {
		return err
	}
	receipts := make([]model.AuditReceipt, 0, len(pending))
	for _, row := range pending {
		receipts = append(receipts, model.AuditReceipt{ID: row.ID, NodeID: row.NodeID, Time: row.Time})
	}
	if len(receipts) > 0 {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(receipts, 200).Error; err != nil {
			return err
		}
	}
	if err := upsertAuditDays(tx, groups); err != nil {
		return err
	}
	if err := tx.Where("time < ?", cutoff).Delete(&model.AuditEvent{}).Error; err != nil {
		return err
	}
	if err := tx.Where("day < ? OR day > ?", day, now.Format("2006-01-02")).Delete(&model.AuditDailyStat{}).Error; err != nil {
		return err
	}
	return tx.Where("time < ?", day).Delete(&model.AuditReceipt{}).Error
}
