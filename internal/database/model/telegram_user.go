package model

type TelegramUserConfig struct {
	Id            int    `json:"id" gorm:"primaryKey"`
	Enabled       bool   `json:"enabled"`
	Token         string `json:"-"`
	TokenHash     string `json:"-"`
	PublicSubURL  string `json:"publicSubUrl"`
	ForwardAdmins bool   `json:"forwardAdmins"`
	LogDays       int    `json:"logDays"`
	MessageDays   int    `json:"messageDays"`
	PollOffset    int64  `json:"-"`
	BotUsername   string `json:"botUsername"`
}

type TelegramBinding struct {
	Id        int   `json:"id" gorm:"primaryKey;autoIncrement"`
	ClientID  int   `json:"clientId" gorm:"uniqueIndex;not null"`
	TgID      int64 `json:"tgId" gorm:"uniqueIndex;not null"`
	CreatedAt int64 `json:"createdAt" gorm:"autoCreateTime:milli"`
}

type TelegramBindCode struct {
	Hash      string `json:"-" gorm:"primaryKey"`
	ClientID  int    `json:"clientId" gorm:"index"`
	ExpiresAt int64  `json:"expiresAt" gorm:"index"`
}

type TelegramConversation struct {
	TgID          int64  `json:"tgId" gorm:"primaryKey;autoIncrement:false"`
	Username      string `json:"username"`
	Name          string `json:"name"`
	LastMessageAt int64  `json:"lastMessageAt" gorm:"index"`
	ReadThrough   int64  `json:"readThrough"`
}

type TelegramMessage struct {
	Id            int64   `json:"id" gorm:"primaryKey;autoIncrement"`
	DedupKey      *string `json:"-" gorm:"uniqueIndex"`
	TgID          int64   `json:"tgId" gorm:"index:idx_telegram_chat,priority:1"`
	ClientID      int     `json:"clientId"`
	Direction     string  `json:"direction"`
	Kind          string  `json:"kind"`
	Text          string  `json:"text"`
	FileID        string  `json:"-"`
	FileName      string  `json:"fileName"`
	MIME          string  `json:"mime"`
	FileSize      int64   `json:"fileSize"`
	SourceBot     string  `json:"sourceBot"`
	DeliveryBot   string  `json:"deliveryBot"`
	TargetChatID  int64   `json:"-"`
	ExternalID    int     `json:"externalId"`
	ReplyID       int64   `json:"replyId"`
	Role          string  `json:"role"`
	Status        string  `json:"status" gorm:"index:idx_telegram_pending,priority:1"`
	Priority      int     `json:"-" gorm:"default:0;index:idx_telegram_pending,priority:2"`
	Attempts      int     `json:"attempts"`
	NextAttemptAt int64   `json:"nextAttemptAt" gorm:"index:idx_telegram_pending,priority:3"`
	Error         string  `json:"error"`
	NoticeID      int     `json:"noticeId"`
	NoticeVersion int64   `json:"-"`
	CreatedAt     int64   `json:"createdAt" gorm:"autoCreateTime:milli;index:idx_telegram_chat,priority:2"`
	UpdatedAt     int64   `json:"updatedAt" gorm:"autoUpdateTime:milli"`
}

type TelegramLog struct {
	Id        int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	TgID      int64  `json:"tgId" gorm:"index"`
	MessageID int64  `json:"messageId"`
	Event     string `json:"event"`
	Detail    string `json:"detail"`
	CreatedAt int64  `json:"createdAt" gorm:"autoCreateTime:milli;index"`
}

type TelegramAdminRelay struct {
	Id             int    `json:"id" gorm:"primaryKey;autoIncrement"`
	BotKey         string `json:"-" gorm:"uniqueIndex:idx_telegram_relay,priority:1"`
	AdminChatID    int64  `json:"adminChatId" gorm:"uniqueIndex:idx_telegram_relay,priority:2"`
	AdminMessageID int    `json:"adminMessageId" gorm:"uniqueIndex:idx_telegram_relay,priority:3"`
	TgID           int64  `json:"tgId"`
	UserMessageID  int64  `json:"userMessageId" gorm:"index"`
}
