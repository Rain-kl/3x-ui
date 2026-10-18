package model

type TelegramCustomCommand struct {
	Command     string `json:"command" gorm:"primaryKey;size:32"`
	Description string `json:"description"`
	Reply       string `json:"reply"`
	Enabled     bool   `json:"enabled"`
	SortOrder   int    `json:"sortOrder"`
}
