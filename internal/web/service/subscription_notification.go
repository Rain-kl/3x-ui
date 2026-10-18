package service

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"

	// FORK(telegram-user-bot): Notification delivery must commit with its targeted publish event.
	"github.com/mhsanaei/3x-ui/v3/internal/telegramuser"
)

type NotificationPage struct {
	Items []model.SubscriptionNotification `json:"items"`
	Total int64                            `json:"total"`
}

type NotificationClient struct {
	Id    int    `json:"id"`
	Email string `json:"email"`
	Group string `json:"group" gorm:"column:group_name"`
}

type NotificationService struct{}

func (NotificationService) List(page, size int) (NotificationPage, error) {
	out := NotificationPage{Items: []model.SubscriptionNotification{}}
	db := database.GetDB().Model(&model.SubscriptionNotification{})
	if err := db.Count(&out.Total).Error; err != nil {
		return out, fmt.Errorf("count subscription notifications: %w", err)
	}
	err := db.Order("sort_index, id").Offset((page - 1) * size).Limit(size).Find(&out.Items).Error
	if err != nil {
		return out, fmt.Errorf("list subscription notifications: %w", err)
	}
	return out, nil
}

func (NotificationService) Clients(search string, ids []int, page, size int) ([]NotificationClient, error) {
	rows := []NotificationClient{}
	db := database.GetDB().Model(&model.ClientRecord{})
	if len(ids) > 0 {
		db = db.Where("id IN ?", ids)
	} else {
		if search != "" {
			db = db.Where("LOWER(email) LIKE ?", "%"+strings.ToLower(search)+"%")
		}
		db = db.Offset((page - 1) * size).Limit(size)
	}
	err := db.Select("id, email, group_name").Order("email, id").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("search notification clients: %w", err)
	}
	return rows, nil
}

func (NotificationService) Save(input model.SubscriptionNotification) (model.SubscriptionNotification, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Body = strings.TrimSpace(input.Body)
	if input.Id < 0 || input.Title == "" || utf8.RuneCountInString(input.Title) > 120 || input.Body == "" || utf8.RuneCountInString(input.Body) > 4000 {
		return input, fmt.Errorf("notification requires title (1–120 characters) and body (1–4000 characters)")
	}
	if input.SortIndex < 0 || input.SortIndex > 1000000 {
		return input, fmt.Errorf("notification order must be between 0 and 1000000")
	}
	if len(input.ClientIDs) > 2000 || len(input.Groups) > 2000 {
		return input, fmt.Errorf("notification audience exceeds 2000 targets")
	}
	for _, id := range input.ClientIDs {
		if id <= 0 {
			return input, fmt.Errorf("notification client ID must be positive: %d", id)
		}
	}
	slices.Sort(input.ClientIDs)
	input.ClientIDs = slices.Compact(input.ClientIDs)
	if input.ClientIDs == nil {
		input.ClientIDs = []int{}
	}
	if input.Groups == nil {
		input.Groups = []string{}
	}
	for i := range input.Groups {
		input.Groups[i] = strings.TrimSpace(input.Groups[i])
	}
	slices.Sort(input.Groups)
	input.Groups = slices.Compact(input.Groups)
	switch input.TargetType {
	case "clients":
		input.Groups = []string{}
		if input.Published && len(input.ClientIDs) == 0 {
			return input, fmt.Errorf("select at least one notification client")
		}
	case "groups":
		input.ClientIDs = []int{}
		if input.Published && len(input.Groups) == 0 {
			return input, fmt.Errorf("select at least one notification group")
		}
	case "both":
		if input.Published && len(input.ClientIDs) == 0 && len(input.Groups) == 0 {
			return input, fmt.Errorf("select at least one notification client or group")
		}
	default:
		return input, fmt.Errorf("notification targetType must be clients, groups or both")
	}
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		if input.Published && len(input.ClientIDs) > 0 {
			var count int64
			if err := tx.Model(&model.ClientRecord{}).Where("id IN ?", input.ClientIDs).Count(&count).Error; err != nil {
				return fmt.Errorf("validate notification clients: %w", err)
			}
			if count != int64(len(input.ClientIDs)) {
				return fmt.Errorf("notification contains clients that no longer exist")
			}
		}
		if input.Published && len(input.Groups) > 0 {
			var names []string
			if err := tx.Raw("SELECT name FROM client_groups UNION SELECT group_name AS name FROM clients WHERE group_name <> ''").Scan(&names).Error; err != nil {
				return fmt.Errorf("validate notification groups: %w", err)
			}
			available := make(map[string]bool, len(names))
			for _, name := range names {
				available[name] = true
			}
			for _, name := range input.Groups {
				if !available[name] {
					return fmt.Errorf("notification group %q no longer exists", name)
				}
			}
		}
		var previous model.SubscriptionNotification
		if input.Id > 0 {
			if err := tx.First(&previous, input.Id).Error; err != nil {
				return fmt.Errorf("load notification %d for update: %w", input.Id, err)
			}
		}
		input.CreatedAt = previous.CreatedAt
		input.UpdatedAt = 0
		input.PublishedAt = previous.PublishedAt
		if input.Published && !previous.Published {
			input.PublishedAt = time.Now().UnixMilli()
		}
		if err := tx.Save(&input).Error; err != nil {
			return fmt.Errorf("save notification %d: %w", input.Id, err)
		}
		// FORK-BEGIN(telegram-user-bot)
		if input.UpdatedAt <= previous.UpdatedAt {
			input.UpdatedAt = previous.UpdatedAt + 1
			if err := tx.Model(&input).UpdateColumn("updated_at", input.UpdatedAt).Error; err != nil {
				return fmt.Errorf("advance notification delivery version: %w", err)
			}
		}
		if err := telegramuser.EnqueueNotification(tx, input); err != nil {
			return err
		}
		// FORK-END(telegram-user-bot)
		return nil
	})
	return input, err
}

func (NotificationService) Delete(id int) error {
	result := database.GetDB().Delete(&model.SubscriptionNotification{}, id)
	if result.Error != nil {
		return fmt.Errorf("delete notification %d: %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("delete notification %d: %w", id, gorm.ErrRecordNotFound)
	}
	return nil
}
