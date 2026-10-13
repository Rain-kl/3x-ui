package service

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type AuditRetention struct {
	ClearBefore time.Time `json:"-"`
	DetailHours int       `json:"detailHours" example:"24"`
	SummaryDays int       `json:"summaryDays" example:"30"`
}

func (AuditService) Retention() (AuditRetention, error) {
	svc := &SettingService{}
	hours, err := svc.getInt("auditDetailHours")
	if err != nil {
		return AuditRetention{}, fmt.Errorf("read audit detail retention: %w", err)
	}
	days, err := svc.getInt("auditSummaryDays")
	if err != nil {
		return AuditRetention{}, fmt.Errorf("read audit summary retention: %w", err)
	}
	clear, err := svc.getString("auditClearBefore")
	if err != nil {
		return AuditRetention{}, fmt.Errorf("read audit cleanup watermark: %w", err)
	}
	clearBefore, _ := time.Parse(time.RFC3339Nano, clear)
	policy := AuditRetention{DetailHours: hours, SummaryDays: days, ClearBefore: clearBefore}
	return policy, validateAuditRetention(policy)
}

func validateAuditRetention(policy AuditRetention) error {
	if policy.DetailHours < 1 || policy.DetailHours > 720 || policy.SummaryDays < 1 || policy.SummaryDays > 365 || policy.DetailHours > policy.SummaryDays*24 {
		return fmt.Errorf("audit details must be 1..720 hours and summaries 1..365 days covering the detail window")
	}
	return nil
}

func (svc AuditService) SetRetention(policy AuditRetention) error {
	if err := validateAuditRetention(policy); err != nil {
		return err
	}
	auditRetentionMu.Lock()
	defer auditRetentionMu.Unlock()
	if (AuditService{}).IsChild() {
		return fmt.Errorf("manage audit retention and cleanup on the master panel")
	}
	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		for key, value := range map[string]int{"auditDetailHours": policy.DetailHours, "auditSummaryDays": policy.SummaryDays} {
			result := tx.Model(&model.Setting{}).Where("key = ?", key).Update("value", strconv.Itoa(value))
			if result.Error != nil {
				return fmt.Errorf("save %s: %w", key, result.Error)
			}
			if result.RowsAffected == 0 {
				if err := tx.Create(&model.Setting{Key: key, Value: strconv.Itoa(value)}).Error; err != nil {
					return err
				}
			}
		}
		return pruneAudit(tx, policy)
	})
}

func (AuditService) Clear(olderThanDays int) error {
	if olderThanDays < 0 || olderThanDays > 3650 {
		return fmt.Errorf("clear audit cutoff must be 0..3650 days")
	}
	auditRetentionMu.Lock()
	defer auditRetentionMu.Unlock()
	if (AuditService{}).IsChild() {
		return fmt.Errorf("manage audit retention and cleanup on the master panel")
	}
	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		cutoff := time.Now().UTC()
		if olderThanDays > 0 {
			cutoff, _ = time.Parse("2006-01-02", cutoff.AddDate(0, 0, -olderThanDays).Format("2006-01-02"))
		}
		var previous model.Setting
		err := tx.Where("key = ?", "auditClearBefore").First(&previous).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		existing, _ := time.Parse(time.RFC3339Nano, previous.Value)
		if cutoff.After(existing) {
			result := tx.Model(&model.Setting{}).Where("key = ?", "auditClearBefore").Update("value", cutoff.Format(time.RFC3339Nano))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				if err := tx.Create(&model.Setting{Key: "auditClearBefore", Value: cutoff.Format(time.RFC3339Nano)}).Error; err != nil {
					return err
				}
			}
		}
		details := tx.Where("1 = 1")
		summaries := tx.Where("1 = 1")
		if olderThanDays > 0 {
			day := time.Now().UTC().AddDate(0, 0, -olderThanDays).Format("2006-01-02")
			details = tx.Where("time < ?", cutoff)
			summaries = tx.Where("day < ?", day)
		}
		if err := details.Delete(&model.AuditEvent{}).Error; err != nil {
			return err
		}
		if err := summaries.Delete(&model.AuditDailyStat{}).Error; err != nil {
			return err
		}
		receipts := tx.Where("1 = 1")
		if olderThanDays > 0 {
			receipts = tx.Where("time < ?", cutoff)
		}
		return receipts.Delete(&model.AuditReceipt{}).Error
	})
}
