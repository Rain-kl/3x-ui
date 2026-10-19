package panelai

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type AITaskInput struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Prompt        string `json:"prompt"`
	Model         string `json:"model"`
	IntervalHours int    `json:"intervalHours"`
	Enabled       bool   `json:"enabled"`
	Telegram      bool   `json:"telegram"`
}

type AIConversationPage struct {
	Items []model.AIConversation `json:"items"`
	Total int64                  `json:"total"`
}
type AITaskPage struct {
	Items []model.AITask `json:"items"`
	Total int64          `json:"total"`
}
type AIRunPage struct {
	Items []model.AIRun `json:"items"`
	Total int64         `json:"total"`
}
type AIConversationView struct {
	Conversation model.AIConversation `json:"conversation"`
	Runs         AIRunPage            `json:"runs"`
}

func Conversations(page int) (*AIConversationPage, error) {
	result := &AIConversationPage{Items: []model.AIConversation{}}
	db := database.GetDB()
	if err := db.Model(&model.AIConversation{}).Count(&result.Total).Error; err != nil {
		return nil, fmt.Errorf("count AI conversations: %w", err)
	}
	err := db.Order("created_at DESC, id").Limit(20).Offset((page - 1) * 20).Find(&result.Items).Error
	return result, err
}

func NewConversation(title string) (*model.AIConversation, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "新对话"
	}
	if utf8.RuneCountInString(title) > 200 {
		return nil, fmt.Errorf("AI conversation title exceeds 200 characters")
	}
	value := model.AIConversation{ID: randomID(), Title: title, CreatedAt: time.Now().UTC()}
	if err := database.GetDB().Create(&value).Error; err != nil {
		return nil, fmt.Errorf("create AI conversation: %w", err)
	}
	return &value, nil
}

func Conversation(id string, page int) (*AIConversationView, error) {
	result := &AIConversationView{}
	if err := database.GetDB().First(&result.Conversation, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("load AI conversation: %w", err)
	}
	runs, err := Runs("", id, page)
	if err != nil {
		return nil, err
	}
	result.Runs = *runs
	return result, nil
}

func Runs(taskID, conversationID string, page int) (*AIRunPage, error) {
	result := &AIRunPage{Items: []model.AIRun{}}
	base := func() *gorm.DB {
		query := database.GetDB().Model(&model.AIRun{})
		if taskID == "" && conversationID == "" {
			query = query.Where("task_id <> ?", "")
		}
		if taskID != "" {
			query = query.Where("task_id = ?", taskID)
		}
		if conversationID != "" {
			query = query.Where("conversation_id = ?", conversationID)
		}
		return query
	}
	if err := base().Count(&result.Total).Error; err != nil {
		return nil, fmt.Errorf("count AI runs: %w", err)
	}
	err := base().Order("started_at DESC, id").Limit(20).Offset((page - 1) * 20).Find(&result.Items).Error
	return result, err
}

func Run(id string) (*model.AIRun, error) {
	var value model.AIRun
	err := database.GetDB().First(&value, "id = ?", id).Error
	return &value, err
}

func (m *Manager) DeleteRun(id string) error {
	m.runMu.Lock()
	defer m.runMu.Unlock()
	if _, active := m.active[id]; active {
		return fmt.Errorf("cancel the AI run and wait for it to finish before deleting its record")
	}
	row, err := Run(id)
	if err != nil {
		return fmt.Errorf("load AI execution record for deletion: %w", err)
	}
	if row.State == "running" {
		return fmt.Errorf("cancel the AI run and wait for it to finish before deleting its record")
	}
	result := database.GetDB().Where("id = ? AND state <> ?", id, "running").Delete(&model.AIRun{})
	if result.Error != nil {
		return fmt.Errorf("delete AI execution record %s: %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("AI execution record %s was not found or is still running", id)
	}
	m.streams.Delete(id)
	return nil
}

func Tasks(page int) (*AITaskPage, error) {
	result := &AITaskPage{Items: []model.AITask{}}
	if err := database.GetDB().Model(&model.AITask{}).Count(&result.Total).Error; err != nil {
		return nil, err
	}
	err := database.GetDB().Order("created_at DESC, id").Limit(20).Offset((page - 1) * 20).Find(&result.Items).Error
	return result, err
}

func SaveTask(input AITaskInput) (*model.AITask, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Prompt = strings.TrimSpace(input.Prompt)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > 200 || input.Prompt == "" || utf8.RuneCountInString(input.Prompt) > 16000 || len(input.Model) > 512 || input.IntervalHours < 1 || input.IntervalHours > 8760 {
		return nil, fmt.Errorf("AI task requires a name (1..200 characters), goal (1..16000 characters), model (up to 512 bytes) and interval (1..8760 hours)")
	}
	var result model.AITask
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		if input.ID != "" {
			if err := tx.First(&result, "id = ?", input.ID).Error; err != nil {
				return fmt.Errorf("load AI task: %w", err)
			}
		} else {
			result.ID = randomID()
			result.CreatedAt = time.Now().UTC()
		}
		if result.NextRun.IsZero() || result.IntervalHours != input.IntervalHours || (!result.Enabled && input.Enabled) {
			result.NextRun = time.Now().UTC().Add(time.Duration(input.IntervalHours) * time.Hour)
		}
		result.Name, result.Prompt, result.Model = input.Name, input.Prompt, input.Model
		result.IntervalHours, result.Telegram, result.Enabled = input.IntervalHours, input.Telegram, input.Enabled
		return tx.Save(&result).Error
	})
	if err != nil {
		return nil, fmt.Errorf("save AI task: %w", err)
	}
	return &result, nil
}

func (m *Manager) DeleteConversation(id string) error {
	m.runMu.Lock()
	defer m.runMu.Unlock()
	var row model.AIConversation
	if err := database.GetDB().First(&row, "id = ?", id).Error; err != nil {
		return err
	}
	var count int64
	if err := database.GetDB().Model(&model.AIRun{}).Where("conversation_id = ? AND state = ?", id, "running").Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("cancel the running conversation before deleting it")
	}
	if row.SessionID != "" {
		if err := m.removeRemoteSession(row.SessionID); err != nil {
			return err
		}
	}
	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("conversation_id = ?", id).Delete(&model.AIRun{}).Error; err != nil {
			return err
		}
		return tx.Delete(&row).Error
	})
}

func (m *Manager) DeleteTask(id string) error {
	m.runMu.Lock()
	defer m.runMu.Unlock()
	var count int64
	if err := database.GetDB().Model(&model.AIRun{}).Where("task_id = ? AND state = ?", id, "running").Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("cancel the running task before deleting it")
	}
	result := database.GetDB().Where("id = ?", id).Delete(&model.AITask{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("AI task was not found")
	}
	return nil
}
