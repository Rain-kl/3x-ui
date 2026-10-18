package model

type SubscriptionAccess struct {
	ID        int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	RequestID string `json:"-" gorm:"index;size:36"`
	Time      int64  `json:"time" gorm:"index"`
	ClientID  int    `json:"clientId" gorm:"index"`
	Client    string `json:"client"`
	IP        string `json:"ip" gorm:"index;size:64"`
	Method    string `json:"method"`
	Format    string `json:"format"`
	Status    int    `json:"status"`
	Blocked   bool   `json:"blocked"`
}

type SubscriptionAccessIP struct {
	LinkHash string `json:"-" gorm:"primaryKey;size:64;index:idx_sub_window,priority:1"`
	IP       string `json:"ip" gorm:"primaryKey;size:64"`
	SeenAt   int64  `json:"seenAt" gorm:"index;index:idx_sub_window,priority:2"`
}

type SubscriptionLeak struct {
	AlertsQueued  bool   `json:"alertsQueued" gorm:"default:false"`
	ClientNames   string `json:"clientNames" gorm:"-"`
	LinkHash      string `json:"-" gorm:"primaryKey;size:64"`
	Episode       string `json:"episode" gorm:"size:36"`
	Clients       string `json:"clients"`
	DetectedAt    int64  `json:"detectedAt"`
	IPCount       int64  `json:"ipCount"`
	DisabledAt    int64  `json:"disabledAt"`
	ResolvedAt    int64  `json:"resolvedAt"`
	NextAttemptAt int64  `json:"nextAttemptAt" gorm:"index"`
	Error         string `json:"error"`
}

type SubscriptionSecurityAlert struct {
	ID            int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	Episode       string `json:"episode" gorm:"uniqueIndex:idx_sub_alert,priority:1;size:36"`
	Bot           string `json:"bot" gorm:"uniqueIndex:idx_sub_alert,priority:2;size:16"`
	ChatID        int64  `json:"chatId" gorm:"uniqueIndex:idx_sub_alert,priority:3"`
	ClientID      int    `json:"clientId"`
	Text          string `json:"text"`
	State         string `json:"state" gorm:"index"`
	Attempts      int    `json:"attempts"`
	NextAttemptAt int64  `json:"nextAttemptAt" gorm:"index"`
	Error         string `json:"error"`
	CreatedAt     int64  `json:"createdAt" gorm:"autoCreateTime:milli"`
}
