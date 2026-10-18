package model

type AuditBandwidthMinute struct {
	Epoch          string  `json:"epoch" gorm:"primaryKey;size:36"`
	Minute         int64   `json:"time" gorm:"primaryKey;autoIncrement:false;index;index:idx_bandwidth_node_minute,priority:2;index:idx_bandwidth_client_minute,priority:2"`
	NodeID         int     `json:"nodeId" gorm:"primaryKey;autoIncrement:false;index:idx_bandwidth_node_minute,priority:1"`
	Client         string  `json:"client" gorm:"primaryKey;index:idx_bandwidth_client_minute,priority:1"`
	Uplink         float64 `json:"uplink"`
	Downlink       float64 `json:"downlink"`
	CoveredSeconds float64 `json:"coveredSeconds"`
}
