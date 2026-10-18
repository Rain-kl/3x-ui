package model

import "time"

type AIConversation struct {
	ID        string    `json:"id" gorm:"primaryKey;size:36"`
	Title     string    `json:"title"`
	SessionID string    `json:"-"`
	CreatedAt time.Time `json:"createdAt"`
}

type AITask struct {
	ID            string    `json:"id" gorm:"primaryKey;size:36"`
	Name          string    `json:"name"`
	Prompt        string    `json:"prompt"`
	Model         string    `json:"model"`
	IntervalHours int       `json:"intervalHours"`
	Telegram      bool      `json:"telegram"`
	Enabled       bool      `json:"enabled"`
	NextRun       time.Time `json:"nextRun" gorm:"index"`
	CreatedAt     time.Time `json:"createdAt"`
}

type AIRun struct {
	ID             string     `json:"id" gorm:"primaryKey;size:36"`
	ConversationID string     `json:"conversationId" gorm:"index"`
	TaskID         string     `json:"taskId" gorm:"index"`
	SessionID      string     `json:"-"`
	Prompt         string     `json:"prompt"`
	Model          string     `json:"model"`
	Result         string     `json:"result"`
	Error          string     `json:"error"`
	NotifyError    string     `json:"notifyError"`
	State          string     `json:"state" gorm:"index"`
	Tools          string     `json:"tools"`
	StartedAt      time.Time  `json:"startedAt" gorm:"index"`
	FinishedAt     *time.Time `json:"finishedAt"`
}
