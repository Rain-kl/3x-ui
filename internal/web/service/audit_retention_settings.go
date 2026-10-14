package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/audit"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type AuditRetention struct {
	// FORK(audit-workbench): Persist adjustable per-second thresholds without changing retention semantics.
	ConnectionRateLimit float64   `json:"connectionRateLimit" example:"10"`
	SiteRateLimit       float64   `json:"siteRateLimit" example:"1"`
	ClearBefore         time.Time `json:"-"`
	DetailHours         int       `json:"detailHours" example:"24"`
	SummaryDays         int       `json:"summaryDays" example:"30"`
}

func readAuditControl(tx *gorm.DB) (audit.Control, error) {
	state, err := readAuditPolicyState(tx)
	return state.Control, err
}

// Local rate coverage is not part of the master's broadcast policy.
type auditPolicyState struct {
	audit.Control
	rateAvailableFrom time.Time
}

func readAuditPolicyState(tx *gorm.DB) (auditPolicyState, error) {
	var rows []model.Setting
	// FORK(audit-workbench): Read the entire audit policy in one database query.
	if err := tx.Where("key IN ?", []string{"auditDetailHours", "auditSummaryDays", "auditClearBefore", "auditPolicyRevision", "auditConnectionRateLimit", "auditSiteRateLimit", "auditRateAvailableFrom"}).Find(&rows).Error; err != nil {
		return auditPolicyState{}, fmt.Errorf("read audit policy: %w", err)
	}
	values := map[string]string{"auditDetailHours": defaultValueMap["auditDetailHours"], "auditSummaryDays": defaultValueMap["auditSummaryDays"], "auditClearBefore": "", "auditPolicyRevision": "0"}
	values["auditConnectionRateLimit"], values["auditSiteRateLimit"] = defaultValueMap["auditConnectionRateLimit"], defaultValueMap["auditSiteRateLimit"]
	for _, row := range rows {
		values[row.Key] = row.Value
	}
	hours, err := strconv.Atoi(values["auditDetailHours"])
	if err != nil {
		return auditPolicyState{}, fmt.Errorf("read audit detail retention: %w", err)
	}
	days, err := strconv.Atoi(values["auditSummaryDays"])
	if err != nil {
		return auditPolicyState{}, fmt.Errorf("read audit summary retention: %w", err)
	}
	revision, err := strconv.ParseInt(values["auditPolicyRevision"], 10, 64)
	if err != nil || revision < 0 {
		return auditPolicyState{}, fmt.Errorf("invalid audit policy revision %q", values["auditPolicyRevision"])
	}
	var clear time.Time
	if values["auditClearBefore"] != "" {
		clear, err = time.Parse(time.RFC3339Nano, values["auditClearBefore"])
		if err != nil {
			return auditPolicyState{}, fmt.Errorf("read audit cleanup watermark: %w", err)
		}
	}
	connectionLimit, err := strconv.ParseFloat(values["auditConnectionRateLimit"], 64)
	if err != nil {
		return auditPolicyState{}, fmt.Errorf("read audit connection rate limit: %w", err)
	}
	siteLimit, err := strconv.ParseFloat(values["auditSiteRateLimit"], 64)
	if err != nil {
		return auditPolicyState{}, fmt.Errorf("read audit site rate limit: %w", err)
	}
	policy := audit.Control{DetailHours: hours, SummaryDays: days, Revision: revision, ClearBefore: clear, ConnectionRateLimit: connectionLimit, SiteRateLimit: siteLimit}
	state := auditPolicyState{Control: policy}
	if values["auditRateAvailableFrom"] != "" {
		state.rateAvailableFrom, err = time.Parse(time.RFC3339Nano, values["auditRateAvailableFrom"])
		if err != nil {
			return auditPolicyState{}, fmt.Errorf("read audit rate history coverage: %w", err)
		}
	}
	return state, validateAuditRetention(retentionFromControl(policy))
}

func retentionFromControl(c audit.Control) AuditRetention {
	return AuditRetention{DetailHours: c.DetailHours, SummaryDays: c.SummaryDays, ClearBefore: c.ClearBefore, ConnectionRateLimit: c.ConnectionRateLimit, SiteRateLimit: c.SiteRateLimit}
}

func (AuditService) Retention() (AuditRetention, error) {
	policy, err := readAuditControl(database.GetDB())
	return retentionFromControl(policy), err
}

func (AuditService) Control() (audit.Control, error) {
	auditRetentionMu.Lock()
	defer auditRetentionMu.Unlock()
	return readAuditControl(database.GetDB())
}

func validateAuditRetention(policy AuditRetention) error {
	// FORK(audit-workbench): Zero explicitly disables a threshold; non-finite values are never valid.
	for _, value := range []float64{policy.ConnectionRateLimit, policy.SiteRateLimit} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1000000 {
			return fmt.Errorf("audit rate thresholds must be finite values between 0 and 1000000 per second")
		}
	}
	if policy.DetailHours < 1 || policy.DetailHours > 720 || policy.SummaryDays < 1 || policy.SummaryDays > 365 || policy.DetailHours > policy.SummaryDays*24 {
		return fmt.Errorf("audit details must be 1..720 hours and summaries 1..365 days covering the detail window")
	}
	return nil
}

func saveAuditValue(tx *gorm.DB, key, value string) error {
	var stored model.Setting
	err := tx.Where("key = ?", key).First(&stored).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return tx.Create(&model.Setting{Key: key, Value: value}).Error
	}
	if err != nil {
		return err
	}
	if stored.Value == value {
		return nil
	}
	return tx.Model(&model.Setting{}).Where("key = ?", key).Update("value", value).Error
}

func saveAuditControl(tx *gorm.DB, c audit.Control) error {
	for _, item := range []struct{ key, value string }{
		{"auditConnectionRateLimit", strconv.FormatFloat(c.ConnectionRateLimit, 'f', -1, 64)},
		{"auditSiteRateLimit", strconv.FormatFloat(c.SiteRateLimit, 'f', -1, 64)},
		{"auditDetailHours", strconv.Itoa(c.DetailHours)},
		{"auditSummaryDays", strconv.Itoa(c.SummaryDays)},
		{"auditPolicyRevision", strconv.FormatInt(c.Revision, 10)},
		{"auditClearBefore", c.ClearBefore.UTC().Format(time.RFC3339Nano)},
	} {
		if err := saveAuditValue(tx, item.key, item.value); err != nil {
			return fmt.Errorf("save %s: %w", item.key, err)
		}
	}
	return nil
}

func (svc AuditService) SetRetention(policy AuditRetention) error {
	if err := validateAuditRetention(policy); err != nil {
		return err
	}
	auditRetentionMu.Lock()
	err := func() error {
		if svc.IsChild() {
			return fmt.Errorf("manage audit retention and cleanup on the master panel")
		}
		return database.GetDB().Transaction(func(tx *gorm.DB) error {
			current, err := readAuditControl(tx)
			if err != nil {
				return err
			}
			current.DetailHours, current.SummaryDays = policy.DetailHours, policy.SummaryDays
			current.ConnectionRateLimit, current.SiteRateLimit = policy.ConnectionRateLimit, policy.SiteRateLimit
			current.Revision++
			if err := saveAuditControl(tx, current); err != nil {
				return err
			}
			return pruneAudit(tx, retentionFromControl(current))
		})
	}()
	auditRetentionMu.Unlock()
	if err != nil {
		return err
	}
	return svc.SyncNodes(context.Background())
}

func clearAuditBefore(tx *gorm.DB, cutoff time.Time) error {
	if cutoff.IsZero() {
		return nil
	}
	// FORK(audit-workbench): Rate history must disappear together with cleared connection logs.
	if err := tx.Where("minute < ?", cutoff.Unix()).Delete(&model.AuditMinuteStat{}).Error; err != nil {
		return fmt.Errorf("clear audit minute metrics: %w", err)
	}
	if err := tx.Where("time < ?", cutoff).Delete(&model.AuditEvent{}).Error; err != nil {
		return err
	}
	day := cutoff.UTC().Format("2006-01-02")
	midnight, _ := time.Parse("2006-01-02", day)
	op := "day < ?"
	if !cutoff.Equal(midnight) {
		op = "day <= ?"
	}
	if err := tx.Where(op, day).Delete(&model.AuditDailyStat{}).Error; err != nil {
		return err
	}
	return tx.Where("time < ?", cutoff).Delete(&model.AuditReceipt{}).Error
}

func (svc AuditService) Clear(olderThanDays int) error {
	if olderThanDays < 0 || olderThanDays > 3650 {
		return fmt.Errorf("clear audit cutoff must be 0..3650 days")
	}
	committed := false
	auditRetentionMu.Lock()
	err := func() error {
		if svc.IsChild() {
			return fmt.Errorf("manage audit retention and cleanup on the master panel")
		}
		var state audit.Control
		err := database.GetDB().Transaction(func(tx *gorm.DB) error {
			var err error
			state, err = readAuditControl(tx)
			if err != nil {
				return err
			}
			cutoff := time.Now().UTC()
			if olderThanDays > 0 {
				cutoff, _ = time.Parse("2006-01-02", cutoff.AddDate(0, 0, -olderThanDays).Format("2006-01-02"))
			}
			if cutoff.After(state.ClearBefore) {
				state.ClearBefore = cutoff
			}
			state.Revision++
			if err := saveAuditControl(tx, state); err != nil {
				return err
			}
			return clearAuditBefore(tx, state.ClearBefore)
		})
		if err != nil {
			return err
		}
		committed = true
		if err := audit.Local().Prune(time.Time{}, state.ClearBefore); err != nil {
			return fmt.Errorf("master audit cleanup saved; remove local spool: %w", err)
		}
		return nil
	}()
	auditRetentionMu.Unlock()
	// Persisted desired state is retried even if removing the local spool failed.
	if !committed {
		return err
	}
	return errors.Join(err, svc.SyncNodes(context.Background()))
}
