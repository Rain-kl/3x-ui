package database

import (
	"fmt"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func migrateAuditRateHistory(tx *gorm.DB) error {
	if err := tx.AutoMigrate(&model.Setting{}, &model.AuditMinuteStat{}); err != nil {
		return err
	}
	var settings []model.Setting
	if err := tx.Where("key IN ?", []string{"auditRateAvailableFrom", "auditDetailHours"}).Find(&settings).Error; err != nil {
		return err
	}
	hours := 24
	for _, setting := range settings {
		if setting.Key == "auditRateAvailableFrom" {
			return nil
		}
		if setting.Key == "auditDetailHours" {
			value, err := strconv.Atoi(setting.Value)
			if err != nil || value < 1 || value > 720 {
				return fmt.Errorf("invalid audit detail hours during rate migration: %q", setting.Value)
			}
			hours = min(hours, value)
		}
	}
	now := time.Now().UTC()
	minute := "(CAST(strftime('%s', time) AS INTEGER) / 60) * 60"
	if IsPostgres() {
		minute = "CAST(FLOOR(EXTRACT(EPOCH FROM time) / 60) AS BIGINT) * 60"
	}
	query := "INSERT INTO audit_minute_stats (minute,node_id,client,site,connections) SELECT " + minute + ",node_id,client,site,COUNT(*) FROM audit_events WHERE inbound <> ? AND time >= ? AND time <= ? GROUP BY " + minute + ",node_id,client,site ON CONFLICT (minute,node_id,client,site) DO NOTHING"
	if err := tx.Exec(query, "api", now.Add(-24*time.Hour).Truncate(time.Minute), now).Error; err != nil {
		return fmt.Errorf("backfill audit minute history: %w", err)
	}
	return tx.Create(&model.Setting{Key: "auditRateAvailableFrom", Value: now.Add(-time.Duration(hours) * time.Hour).Format(time.RFC3339Nano)}).Error
}
