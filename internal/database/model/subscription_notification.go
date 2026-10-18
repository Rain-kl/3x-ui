package model

type SubscriptionNotification struct {
	Id             int      `json:"id" gorm:"primaryKey;autoIncrement"`
	Title          string   `json:"title" example:"Maintenance"`
	Body           string   `json:"body" example:"Scheduled maintenance at 02:00."`
	TargetType     string   `json:"targetType" example:"clients"`
	ClientIDs      []int    `json:"clientIds" gorm:"serializer:json;type:text"`
	Groups         []string `json:"groups" gorm:"serializer:json;type:text"`
	PublishToNodes bool     `json:"publishToNodes"`
	Published      bool     `json:"published" gorm:"index:idx_notification_published_order,priority:1"`
	SortIndex      int      `json:"sortIndex" gorm:"index:idx_notification_published_order,priority:2"`
	PublishedAt    int64    `json:"publishedAt"`
	CreatedAt      int64    `json:"createdAt" gorm:"autoCreateTime:milli"`
	UpdatedAt      int64    `json:"updatedAt" gorm:"autoUpdateTime:milli"`
}
