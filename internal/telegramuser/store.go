package telegramuser

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf16"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type ConversationView struct {
	TgID          int64  `json:"tgId"`
	Username      string `json:"username"`
	Name          string `json:"name"`
	ClientID      int    `json:"clientId"`
	ClientEmail   string `json:"clientEmail"`
	LastMessageAt int64  `json:"lastMessageAt"`
	Unread        int64  `json:"unread"`
}

type ConversationPage struct {
	Items []ConversationView `json:"items"`
	Total int64              `json:"total"`
}

type TelegramMessagePage struct {
	Items []model.TelegramMessage `json:"items"`
	Total int64                   `json:"total"`
}

type TelegramLogPage struct {
	Items []model.TelegramLog `json:"items"`
	Total int64               `json:"total"`
}

type TelegramBindingView struct {
	Id        int    `json:"id"`
	TgID      int64  `json:"tgId"`
	ClientID  int    `json:"clientId"`
	Email     string `json:"email"`
	Username  string `json:"username"`
	CreatedAt int64  `json:"createdAt"`
}

type TelegramBindingPage struct {
	Items []TelegramBindingView `json:"items"`
	Total int64                 `json:"total"`
}

func Conversations(search string, page, size int) (ConversationPage, error) {
	out := ConversationPage{Items: []ConversationView{}}
	db := database.GetDB().Table("telegram_conversations AS c").
		Joins("LEFT JOIN telegram_bindings b ON b.tg_id = c.tg_id").
		Joins("LEFT JOIN clients cl ON cl.id = b.client_id")
	if search != "" {
		pattern := "%" + strings.ToLower(search) + "%"
		db = db.Where("LOWER(c.username) LIKE ? OR LOWER(c.name) LIKE ? OR LOWER(cl.email) LIKE ?", pattern, pattern, pattern)
	}
	if err := db.Count(&out.Total).Error; err != nil {
		return out, fmt.Errorf("count Telegram conversations: %w", err)
	}
	err := db.Select("c.tg_id, c.username, c.name, c.last_message_at, COALESCE(b.client_id, 0) AS client_id, COALESCE(cl.email, '') AS client_email, (SELECT COUNT(*) FROM telegram_messages m WHERE m.tg_id = c.tg_id AND m.direction = 'incoming' AND m.role = 'chat' AND m.id > c.read_through) AS unread").
		Order("c.last_message_at DESC, c.tg_id").Offset((page - 1) * size).Limit(size).Scan(&out.Items).Error
	return out, err
}

func Messages(tgID int64, page, size int) (TelegramMessagePage, error) {
	out := TelegramMessagePage{Items: []model.TelegramMessage{}}
	db := database.GetDB().Model(&model.TelegramMessage{}).Where("tg_id = ? AND delivery_bot <> ?", tgID, "admin")
	if err := db.Count(&out.Total).Error; err != nil {
		return out, err
	}
	err := db.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&out.Items).Error
	return out, err
}

func Logs(event string, tgID int64, page, size int) (TelegramLogPage, error) {
	out := TelegramLogPage{Items: []model.TelegramLog{}}
	db := database.GetDB().Model(&model.TelegramLog{})
	if event != "" {
		db = db.Where("event = ?", event)
	}
	if tgID > 0 {
		db = db.Where("tg_id = ?", tgID)
	}
	if err := db.Count(&out.Total).Error; err != nil {
		return out, err
	}
	err := db.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&out.Items).Error
	return out, err
}

func Bindings(page, size int) (TelegramBindingPage, error) {
	out := TelegramBindingPage{Items: []TelegramBindingView{}}
	db := database.GetDB().Table("telegram_bindings AS b").Joins("LEFT JOIN clients c ON c.id = b.client_id").Joins("LEFT JOIN telegram_conversations u ON u.tg_id = b.tg_id")
	if err := db.Count(&out.Total).Error; err != nil {
		return out, err
	}
	err := db.Select("b.id, b.tg_id, b.client_id, COALESCE(c.email, '') AS email, COALESCE(u.username, '') AS username, b.created_at").Order("b.id DESC").Offset((page - 1) * size).Limit(size).Scan(&out.Items).Error
	return out, err
}

func RecordLog(tgID, messageID int64, event, detail string) error {
	if len(detail) > 8000 {
		detail = detail[:8000]
	}
	return database.GetDB().Create(&model.TelegramLog{TgID: tgID, MessageID: messageID, Event: event, Detail: detail}).Error
}

func userBlocked(tgID int64) (bool, error) {
	var count int64
	if err := database.GetDB().Model(&model.TelegramBlockedUser{}).Where("tg_id = ?", tgID).Count(&count).Error; err != nil {
		return false, fmt.Errorf("check Telegram blocked user: %w", err)
	}
	return count > 0, nil
}

func blockUser(tgID int64, attempts int, reason string) error {
	row := model.TelegramBlockedUser{TgID: tgID, Attempts: attempts, Reason: reason}
	return database.GetDB().Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
}

func recordMessage(message *model.TelegramMessage, username, name string) (bool, error) {
	inserted := false
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(message)
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		inserted = true
		conversation := model.TelegramConversation{TgID: message.TgID, Username: username, Name: name, LastMessageAt: time.Now().UnixMilli()}
		columns := []string{"last_message_at"}
		if username != "" || name != "" {
			columns = append(columns, "username", "name")
		}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "tg_id"}}, DoUpdates: clause.AssignmentColumns(columns)}).Create(&conversation).Error; err != nil {
			return err
		}
		return tx.Create(&model.TelegramLog{TgID: message.TgID, MessageID: message.Id, Event: "message." + message.Direction, Detail: message.Kind + " · " + message.Role}).Error
	})
	return inserted, err
}

func MarkRead(tgID, throughID int64) error {
	var last model.TelegramMessage
	if err := database.GetDB().Where("tg_id = ? AND id = ?", tgID, throughID).First(&last).Error; err != nil {
		return err
	}
	return database.GetDB().Model(&model.TelegramConversation{}).Where("tg_id = ? AND read_through < ?", tgID, last.Id).Update("read_through", last.Id).Error
}

func QueueText(tgID int64, text string, replyID int64, role string) (model.TelegramMessage, error) {
	row := model.TelegramMessage{TgID: tgID, TargetChatID: tgID, Direction: "outgoing", Kind: "text", Text: strings.TrimSpace(text), Status: "pending", DeliveryBot: "user", Role: role, ReplyID: replyID}
	if tgID <= 0 || row.Text == "" || len(utf16.Encode([]rune(row.Text))) > 3500 || !Default.Running() {
		return row, fmt.Errorf("send requires a running user Bot, a valid conversation and 1–3500 characters")
	}
	var conversation model.TelegramConversation
	if err := database.GetDB().First(&conversation, "tg_id = ?", tgID).Error; err != nil {
		return row, fmt.Errorf("load Telegram conversation: %w", err)
	}
	if client, err := BoundClient(tgID); err == nil {
		row.ClientID = client.Id
	}
	if replyID > 0 {
		var original model.TelegramMessage
		if err := database.GetDB().First(&original, "id = ? AND tg_id = ? AND delivery_bot <> ?", replyID, tgID, "admin").Error; err != nil {
			return row, fmt.Errorf("reply message is not in this conversation")
		}
	}
	_, err := recordMessage(&row, "", "")
	return row, err
}

func Clear(kind string, days int) error {
	if days < 0 || days > 3650 {
		return fmt.Errorf("cleanup days must be between 0 (all) and 3650")
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		switch kind {
		case "logs":
			return tx.Where("created_at < ?", cutoff).Delete(&model.TelegramLog{}).Error
		case "messages":
			if err := tx.Where("created_at < ? AND status NOT IN ?", cutoff, []string{"pending", "sending"}).Delete(&model.TelegramMessage{}).Error; err != nil {
				return err
			}
			return tx.Where("user_message_id NOT IN (SELECT id FROM telegram_messages)").Delete(&model.TelegramAdminRelay{}).Error
		default:
			return fmt.Errorf("unknown Telegram cleanup kind %q", kind)
		}
	})
}
