package service

import (
	"net"
	"net/url"
	"strings"
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
	Hours  int
	NodeID int
	Client string
	Site   string
	Offset int
}

type AuditAggregate struct {
	Name        string `json:"name"`
	Connections int64  `json:"connections"`
	Uplink      int64  `json:"uplink"`
	Downlink    int64  `json:"downlink"`
	Measured    int64  `json:"measured"`
	UniqueSites int64  `json:"uniqueSites"`
}

type AuditSummary struct {
	Connections int64              `json:"connections"`
	Uplink      int64              `json:"uplink"`
	Downlink    int64              `json:"downlink"`
	Measured    int64              `json:"measured"`
	Sites       []AuditAggregate   `json:"sites"`
	Clients     []AuditAggregate   `json:"clients"`
	Alerts      []AuditAggregate   `json:"alerts"`
	Hours       []AuditAggregate   `json:"hours"`
	Events      []model.AuditEvent `json:"events"`
	Total       int64              `json:"total"`
	Nodes       []AuditNode        `json:"nodes"`
}

type AuditNode struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type AuditService struct{}

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
	if len(events) == 0 {
		return nil
	}
	rows := make([]model.AuditEvent, 0, len(events))
	for _, e := range events {
		if e.ID == "" || e.Time.IsZero() || e.Destination == "" {
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
			ID: e.ID, NodeID: nodeID, Time: e.Time, Inbound: e.Inbound,
			Outbound: e.Outbound, Client: client, Source: e.Source,
			Destination: e.Destination, Host: host, Site: site,
			Uplink: e.Uplink, Downlink: e.Downlink,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return database.GetDB().Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(rows, 200).Error
}

func auditScopedQuery(db *gorm.DB, f AuditFilter) *gorm.DB {
	hours := f.Hours
	if hours != 3 && hours != 12 && hours != 24 {
		hours = 24
	}
	db = db.Where("time >= ?", time.Now().UTC().Add(-time.Duration(hours)*time.Hour))
	if f.NodeID > 0 {
		db = db.Where("node_id = ?", f.NodeID)
	}
	if f.Client != "" {
		db = db.Where("client = ?", f.Client)
	}
	if f.Site != "" {
		db = db.Where("site = ?", strings.ToLower(f.Site))
	}
	return db
}

func (AuditService) Query(f AuditFilter) (*AuditSummary, error) {
	db := database.GetDB()
	result := &AuditSummary{
		Sites: []AuditAggregate{}, Clients: []AuditAggregate{}, Alerts: []AuditAggregate{},
		Hours: []AuditAggregate{}, Events: []model.AuditEvent{}, Nodes: []AuditNode{},
	}
	if err := db.Model(&model.Node{}).Select("id, name").Order("id").Scan(&result.Nodes).Error; err != nil {
		return nil, err
	}
	base := func() *gorm.DB { return auditScopedQuery(db.Model(&model.AuditEvent{}), f) }
	if err := base().Count(&result.Total).Error; err != nil {
		return nil, err
	}
	type totals struct {
		Uplink   int64
		Downlink int64
		Measured int64
	}
	var total totals
	if err := base().Select("COALESCE(SUM(uplink), 0) AS uplink, COALESCE(SUM(downlink), 0) AS downlink, COUNT(uplink) AS measured").Scan(&total).Error; err != nil {
		return nil, err
	}
	result.Connections, result.Uplink, result.Downlink, result.Measured = result.Total, total.Uplink, total.Downlink, total.Measured
	for _, item := range []struct {
		field  string
		target *[]AuditAggregate
	}{{"site", &result.Sites}, {"client", &result.Clients}} {
		selectExpr := item.field + " AS name, COUNT(*) AS connections, COALESCE(SUM(uplink), 0) AS uplink, COALESCE(SUM(downlink), 0) AS downlink, COUNT(uplink) AS measured, COUNT(DISTINCT site) AS unique_sites"
		order := "connections DESC"
		if item.field == "site" {
			order = "COALESCE(SUM(uplink), 0) + COALESCE(SUM(downlink), 0) DESC"
		}
		if err := base().Select(selectExpr).Group(item.field).Order(order).Limit(20).Scan(item.target).Error; err != nil {
			return nil, err
		}
	}
	alertHours := f.Hours
	if alertHours != 3 && alertHours != 12 && alertHours != 24 {
		alertHours = 24
	}
	alertExpr := "client AS name, COUNT(*) AS connections, COALESCE(SUM(uplink), 0) AS uplink, COALESCE(SUM(downlink), 0) AS downlink, COUNT(uplink) AS measured, COUNT(DISTINCT site) AS unique_sites"
	if err := base().Select(alertExpr).Group("client").Having("COUNT(*) >= ? OR COUNT(DISTINCT site) >= ?", 100*alertHours, 50).Order("connections DESC").Limit(20).Scan(&result.Alerts).Error; err != nil {
		return nil, err
	}
	hourExpr := "strftime('%Y-%m-%dT%H:00:00Z', time)"
	if database.IsPostgres() {
		hourExpr = "to_char(date_trunc('hour', time AT TIME ZONE 'UTC'), 'YYYY-MM-DD\"T\"HH24:00:00\"Z\"')"
	}
	if err := base().Select(hourExpr + " AS name, COUNT(*) AS connections, COALESCE(SUM(uplink), 0) AS uplink, COALESCE(SUM(downlink), 0) AS downlink, COUNT(uplink) AS measured").Group(hourExpr).Order("name").Scan(&result.Hours).Error; err != nil {
		return nil, err
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	if f.Offset > 100000 {
		f.Offset = 100000
	}
	if err := base().Order("time DESC").Limit(100).Offset(f.Offset).Find(&result.Events).Error; err != nil {
		return nil, err
	}
	return result, nil
}

func (AuditService) Prune() error {
	return database.GetDB().Where("time < ?", time.Now().UTC().Add(-7*24*time.Hour)).Delete(&model.AuditEvent{}).Error
}
