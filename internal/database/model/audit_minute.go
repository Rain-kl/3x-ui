package model

// AuditMinuteStat stores compact, deduplicated counters for the last 24 hours.
type AuditMinuteStat struct {
	Minute      int64  `json:"time" gorm:"primaryKey;autoIncrement:false;index:idx_audit_minute_node,priority:2;index:idx_audit_minute_client,priority:2"`
	NodeID      int    `json:"nodeId" gorm:"primaryKey;autoIncrement:false;index:idx_audit_minute_node,priority:1"`
	Client      string `json:"client" gorm:"primaryKey;index:idx_audit_minute_client,priority:1"`
	Site        string `json:"site" gorm:"primaryKey"`
	Connections int64  `json:"connections"`
}
