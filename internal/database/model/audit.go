package model

import "time"

type AuditEvent struct {
	Delivered *bool  `json:"-" gorm:"default:true"`
	ID        string `json:"id" gorm:"primaryKey;size:32"`
	NodeID    int    `json:"nodeId" gorm:"index:idx_audit_time_node,priority:1"`
	// FORK(audit-workbench): Bound per-client rate reads by time rather than scanning all retained visits.
	Time        time.Time `json:"time" gorm:"index:idx_audit_time_node,priority:2;index:idx_audit_client_time,priority:2;index"`
	Inbound     string    `json:"inbound" gorm:"index"`
	Outbound    string    `json:"outbound"`
	Client      string    `json:"client" gorm:"index;index:idx_audit_client_time,priority:1"`
	Source      string    `json:"source"`
	Destination string    `json:"destination"`
	Host        string    `json:"host" gorm:"index"`
	Site        string    `json:"site" gorm:"index"`
	Uplink      *int64    `json:"uplink"`
	Downlink    *int64    `json:"downlink"`
}
