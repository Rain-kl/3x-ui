package model

import "time"

// AuditDailyStat stores compact metrics; Day identifies the UTC calendar bucket and retention expiry.
type AuditDailyStat struct {
	ExportID    string `json:"-" gorm:"index:idx_audit_daily_export,where:export_id <> ''"`
	Day         string `json:"day" gorm:"primaryKey;size:10"`
	NodeID      int    `json:"nodeId" gorm:"primaryKey;autoIncrement:false"`
	Client      string `json:"client" gorm:"primaryKey"`
	Site        string `json:"site" gorm:"primaryKey"`
	Connections int64  `json:"connections"`
	Uplink      int64  `json:"uplink"`
	Downlink    int64  `json:"downlink"`
	Measured    int64  `json:"measured"`
}

// AuditReceipt deduplicates only delayed or unacknowledged archived events.
type AuditReceipt struct {
	ID     string `gorm:"primaryKey;size:32"`
	NodeID int
	Time   time.Time `gorm:"index"`
}
