package model

type TelegramMessageDeletion struct {
	Id        int64  `gorm:"primaryKey;autoIncrement"`
	BotKey    string `gorm:"uniqueIndex:idx_telegram_deletion_target,priority:1;index:idx_telegram_deletion_due,priority:1"`
	ChatID    int64  `gorm:"uniqueIndex:idx_telegram_deletion_target,priority:2"`
	MessageID int    `gorm:"uniqueIndex:idx_telegram_deletion_target,priority:3"`
	DueAt     int64  `gorm:"index:idx_telegram_deletion_due,priority:2"`
	Attempts  int
}
