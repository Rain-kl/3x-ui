package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type SubscriptionAccessPage struct {
	Items  []model.SubscriptionAccess        `json:"items"`
	Total  int64                             `json:"total"`
	Risks  []model.SubscriptionLeak          `json:"risks"`
	Alerts []model.SubscriptionSecurityAlert `json:"alerts"`
}

func QuerySubscriptionAccess(client, ip, day string, page, size int) (SubscriptionAccessPage, error) {
	result := SubscriptionAccessPage{Items: []model.SubscriptionAccess{}, Risks: []model.SubscriptionLeak{}, Alerts: []model.SubscriptionSecurityAlert{}}
	if len(client) > 256 || len(ip) > 64 || page < 1 || (size != 20 && size != 50 && size != 100) || page-1 > int(^uint(0)>>1)/size {
		return result, fmt.Errorf("invalid subscription audit filter or pagination")
	}
	var start int64
	if day != "" {
		date, err := time.Parse("2006-01-02", day)
		if err != nil {
			return result, fmt.Errorf("invalid subscription audit date: %w", err)
		}
		start = date.UnixMilli()
	}
	err := auditReadSnapshot(func(tx *gorm.DB) error {
		base := func() *gorm.DB {
			query := tx.Model(&model.SubscriptionAccess{})
			if client != "" {
				query = query.Where("client = ?", client)
			}
			if ip != "" {
				query = query.Where("ip = ?", ip)
			}
			if day != "" {
				query = query.Where("time >= ? AND time < ?", start, start+(24*time.Hour).Milliseconds())
			}
			return query
		}
		if err := base().Count(&result.Total).Error; err != nil {
			return err
		}
		if err := base().Order("time DESC,id DESC").Limit(size).Offset((page - 1) * size).Find(&result.Items).Error; err != nil {
			return err
		}
		if err := tx.Order("detected_at DESC").Limit(100).Find(&result.Risks).Error; err != nil {
			return err
		}
		idSet := map[int]bool{}
		for _, risk := range result.Risks {
			var ids []int
			if err := json.Unmarshal([]byte(risk.Clients), &ids); err != nil {
				return fmt.Errorf("decode risk recipients: %w", err)
			}
			for _, id := range ids {
				idSet[id] = true
			}
		}
		ids := make([]int, 0, len(idSet))
		for id := range idSet {
			ids = append(ids, id)
		}
		var clients []model.ClientRecord
		for _, batch := range chunkInts(ids, sqlInChunk) {
			var rows []model.ClientRecord
			if err := tx.Select("id,email").Where("id IN ?", batch).Find(&rows).Error; err != nil {
				return err
			}
			clients = append(clients, rows...)
		}
		names := map[int]string{}
		for _, client := range clients {
			names[client.Id] = client.Email
		}
		for i, risk := range result.Risks {
			var ids []int
			_ = json.Unmarshal([]byte(risk.Clients), &ids)
			list := make([]string, 0, len(ids))
			for _, id := range ids {
				if name, ok := names[id]; ok {
					list = append(list, name)
				} else {
					list = append(list, fmt.Sprintf("#%d", id))
				}
			}
			result.Risks[i].ClientNames = strings.Join(list, ", ")
		}
		return tx.Where("state IN ?", []string{"pending", "failed"}).Order("created_at DESC").Limit(100).Find(&result.Alerts).Error
	})
	return result, err
}

func ClearSubscriptionAccess(days int) error {
	if days < 0 || days > 3650 {
		return fmt.Errorf("invalid subscription access retention")
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
	return submitTrafficWrite(func() error {
		return database.GetDB().Where("time < ?", cutoff).Delete(&model.SubscriptionAccess{}).Error
	})
}

func RetrySubscriptionAlert(id int64) error {
	if id <= 0 {
		return fmt.Errorf("invalid security alert ID")
	}
	return submitTrafficWrite(func() error {
		result := database.GetDB().Model(&model.SubscriptionSecurityAlert{}).Where("id = ? AND state = ?", id, "failed").Updates(map[string]any{"state": "pending", "attempts": 0, "next_attempt_at": 0, "error": ""})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("failed security alert not found")
		}
		return nil
	})
}
