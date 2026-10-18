package service

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type AuditBandwidthCurrent struct {
	Time       int64   `json:"time"`
	Upload     float64 `json:"upload"`
	Download   float64 `json:"download"`
	Fresh      bool    `json:"fresh"`
	StaleNodes int     `json:"staleNodes"`
}
type AuditBandwidthPoint struct {
	Time           int64   `json:"time"`
	Upload         float64 `json:"upload"`
	Download       float64 `json:"download"`
	CoveredSeconds float64 `json:"coveredSeconds"`
}
type AuditBandwidthHistory struct {
	Points []AuditBandwidthPoint `json:"points"`
}

func (AuditService) QueryBandwidth(f AuditFilter) (*AuditBandwidthCurrent, error) {
	if err := validateAuditRateFilter(f); err != nil {
		return nil, err
	}
	if f.NodeID < -1 {
		return nil, fmt.Errorf("invalid bandwidth node")
	}
	result := &AuditBandwidthCurrent{}
	err := auditReadSnapshot(func(tx *gorm.DB) error {
		control, err := readAuditControl(tx)
		if err != nil {
			return err
		}
		if isAuditChild(tx) {
			return fmt.Errorf("bandwidth audit is centralized; view the master panel")
		}
		now := time.Now().UTC()
		auditBandwidth.mu.Lock()
		defer auditBandwidth.mu.Unlock()
		for id, node := range auditBandwidth.nodes {
			if (f.NodeID > 0 && id != f.NodeID) || (f.NodeID == -1 && id != 0) {
				continue
			}
			if now.Sub(node.At) > 15*time.Second || node.From.Before(control.ClearBefore) || node.At.Sub(node.From) > 30*time.Second {
				result.StaleNodes++
				continue
			}
			if f.Client != "" {
				rate, ok := node.Rates[f.Client]
				if !ok {
					continue
				}
				result.Upload += rate[0]
				result.Download += rate[1]
			} else {
				for _, rate := range node.Rates {
					result.Upload += rate[0]
					result.Download += rate[1]
				}
			}
			result.Fresh = true
			if result.Time == 0 || node.At.UnixMilli() < result.Time {
				result.Time = node.At.UnixMilli()
			}
		}
		return nil
	})
	return result, err
}

func (AuditService) QueryBandwidthHistory(f AuditFilter) (*AuditBandwidthHistory, error) {
	if err := validateAuditRateFilter(f); err != nil {
		return nil, err
	}
	if f.NodeID < -1 {
		return nil, fmt.Errorf("invalid bandwidth node")
	}
	result := &AuditBandwidthHistory{Points: []AuditBandwidthPoint{}}
	err := auditReadSnapshot(func(tx *gorm.DB) error {
		state, err := readAuditControl(tx)
		if err != nil {
			return err
		}
		if isAuditChild(tx) {
			return fmt.Errorf("bandwidth audit is centralized; view the master panel")
		}
		end := time.Now().UTC().Truncate(time.Minute).Unix()
		start := end - 24*60*60
		base := func() *gorm.DB {
			return auditDimensions(tx.Model(&model.AuditBandwidthMinute{}).Where("minute >= ? AND minute >= ? AND minute < ?", start, state.ClearBefore.Unix(), end), AuditFilter{NodeID: f.NodeID})
		}
		var usage, coverage []struct {
			Minute                           int64
			Upload, Download, CoveredSeconds float64
		}
		query := base().Where("client <> ''")
		if f.Client != "" {
			query = query.Where("client = ?", f.Client)
		}
		if err := query.Select("minute,SUM(uplink)/60.0 AS upload,SUM(downlink)/60.0 AS download").Group("minute").Scan(&usage).Error; err != nil {
			return fmt.Errorf("query bandwidth usage: %w", err)
		}
		if err := base().Where("client = ''").Select("minute,SUM(covered_seconds) AS covered_seconds").Group("minute").Scan(&coverage).Error; err != nil {
			return fmt.Errorf("query bandwidth coverage: %w", err)
		}
		lookup := map[int64]AuditBandwidthPoint{}
		for _, row := range usage {
			lookup[row.Minute] = AuditBandwidthPoint{Time: row.Minute, Upload: row.Upload, Download: row.Download}
		}
		for _, row := range coverage {
			point := lookup[row.Minute]
			point.CoveredSeconds = row.CoveredSeconds
			lookup[row.Minute] = point
		}
		for minute := start; minute < end; minute += 60 {
			point := lookup[minute]
			point.Time = minute
			result.Points = append(result.Points, point)
		}
		return nil
	})
	return result, err
}
