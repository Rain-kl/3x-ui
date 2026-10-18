package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

const subscriptionWindow = 10 * time.Minute

func subscriptionHash(token string) string {
	value := sha256.Sum256([]byte(token))
	return hex.EncodeToString(value[:])
}

// The serialized writer makes the sixth distinct IP atomic with the block decision.
func CaptureSubscriptionAccess(token, ip, method, format string) (string, bool, error) {
	if len(token) > 256 || token == "" {
		return "", false, nil
	}
	requestID, hash := uuid.NewString(), subscriptionHash(token)
	now := time.Now().UnixMilli()
	blocked, triggered, known := false, false, false
	err := runSerializedTx(func(tx *gorm.DB) error {
		var clients []model.ClientRecord
		if err := tx.Select("id,email,enable").Where("sub_id = ?", token).Find(&clients).Error; err != nil {
			return fmt.Errorf("resolve subscription access owner: %w", err)
		}
		if len(clients) == 0 {
			return nil
		}
		known = true
		ids := make([]int, 0, len(clients))
		active := false
		for _, client := range clients {
			ids = append(ids, client.Id)
			active = active || client.Enable
		}
		var risk model.SubscriptionLeak
		err := tx.Where("link_hash = ?", hash).Take(&risk).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("read subscription risk state: %w", err)
		}
		if risk.DetectedAt > 0 && risk.ResolvedAt == 0 && risk.DisabledAt > 0 && active {
			if err := tx.Model(&risk).Update("resolved_at", now).Error; err != nil {
				return err
			}
			risk.ResolvedAt = now
			if err := tx.Where("link_hash = ?", hash).Delete(&model.SubscriptionAccessIP{}).Error; err != nil {
				return err
			}
		}
		blocked = risk.DetectedAt > 0 && risk.ResolvedAt == 0
		if !blocked && active && method == "GET" {
			row := model.SubscriptionAccessIP{LinkHash: hash, IP: ip, SeenAt: now}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "link_hash"}, {Name: "ip"}}, DoUpdates: clause.AssignmentColumns([]string{"seen_at"})}).Create(&row).Error; err != nil {
				return fmt.Errorf("update subscription IP window: %w", err)
			}
			var count int64
			if err := tx.Model(&model.SubscriptionAccessIP{}).Where("link_hash = ? AND seen_at > ?", hash, now-subscriptionWindow.Milliseconds()).Count(&count).Error; err != nil {
				return err
			}
			if count > 5 {
				body, err := json.Marshal(ids)
				if err != nil {
					return err
				}
				risk = model.SubscriptionLeak{LinkHash: hash, Episode: uuid.NewString(), Clients: string(body), DetectedAt: now, IPCount: count}
				if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&risk).Error; err != nil {
					return fmt.Errorf("persist subscription leak: %w", err)
				}
				blocked = true
				triggered = true
			}
		}
		status := 200
		if blocked {
			status = 403
		}
		rows := make([]model.SubscriptionAccess, 0, len(clients))
		for _, client := range clients {
			rows = append(rows, model.SubscriptionAccess{RequestID: requestID, Time: now, ClientID: client.Id, Client: client.Email, IP: ip, Method: method, Format: format, Status: status, Blocked: blocked})
		}
		if err := tx.CreateInBatches(rows, 100).Error; err != nil {
			return fmt.Errorf("record subscription access: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", true, err
	}
	if !known {
		return "", false, nil
	}
	if triggered {
		if err := ProcessSubscriptionLeak(hash); err != nil {
			logger.Warningf("subscription leak response pending: %v", err)
		}
	}
	return requestID, blocked, nil
}

func CompleteSubscriptionAccess(requestID string, status int) error {
	if requestID == "" {
		return nil
	}
	return submitTrafficWrite(func() error {
		return database.GetDB().Model(&model.SubscriptionAccess{}).Where("request_id = ?", requestID).Update("status", status).Error
	})
}
