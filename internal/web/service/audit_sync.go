package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/audit"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
)

// SyncNodes broadcasts persisted desired state without holding the audit lock.
// The audit job retries it on reconnect and after process restarts.
func (svc AuditService) SyncNodes(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	state, err := svc.Control()
	if err != nil {
		return err
	}
	nodes, err := (&NodeService{}).GetAll()
	if err != nil {
		return fmt.Errorf("master audit policy saved; list nodes for broadcast: %w", err)
	}
	results := make([]error, len(nodes))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, node := range nodes {
		if !node.Enable {
			continue
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			results[i] = fmt.Errorf("node %s (%d): broadcast deadline: %w", node.Name, node.Id, ctx.Err())
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			rt := runtime.NewRemote(node, nil)
			if mgr := runtime.GetManager(); mgr != nil {
				var err error
				rt, err = mgr.RemoteFor(node)
				if err != nil {
					results[i] = fmt.Errorf("node %s (%d): %w", node.Name, node.Id, err)
					return
				}
			}
			nodeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			if err := rt.SyncAudit(nodeCtx, state); err != nil {
				results[i] = fmt.Errorf("node %s (%d): %w", node.Name, node.Id, err)
			}
		}()
	}
	wg.Wait()
	if err := errors.Join(results...); err != nil {
		return fmt.Errorf("master audit policy saved; node synchronization pending (automatic retry): %w", err)
	}
	return nil
}

func auditHistoryStart(policy AuditRetention, now time.Time) time.Time {
	since, _ := time.Parse("2006-01-02", now.UTC().AddDate(0, 0, -policy.SummaryDays+1).Format("2006-01-02"))
	if detail := now.Add(-time.Duration(policy.DetailHours) * time.Hour); detail.Before(since) {
		since = detail
	}
	return since
}

func pruneChildAudit(tx *gorm.DB, policy AuditRetention) error {
	now := time.Now().UTC()
	// FORK(audit-workbench): Expire standalone minute history after the panel becomes a child.
	if err := expireAuditMinutes(tx, now); err != nil {
		return err
	}
	if err := clearAuditBefore(tx, policy.ClearBefore); err != nil {
		return err
	}
	if err := tx.Where("node_id = 0 AND (inbound = ? OR time < ? OR time > ?)", "api", auditHistoryStart(policy, now), now.Add(5*time.Minute)).Delete(&model.AuditEvent{}).Error; err != nil {
		return err
	}
	day := now.AddDate(0, 0, -policy.SummaryDays+1).Format("2006-01-02")
	if err := tx.Where("node_id = 0 AND day < ?", day).Delete(&model.AuditDailyStat{}).Error; err != nil {
		return err
	}
	return tx.Where("time < ?", day).Delete(&model.AuditReceipt{}).Error
}

// ApplyControl is called only by the authenticated parent RPC, never the child UI.
func (svc AuditService) ApplyControl(spool *audit.Spool, state audit.Control) error {
	if err := validateAuditRetention(retentionFromControl(state)); err != nil {
		return err
	}
	if state.Revision < 0 || state.ClearBefore.After(time.Now().UTC().Add(5*time.Minute)) {
		return fmt.Errorf("invalid parent audit revision or cleanup watermark")
	}
	auditRetentionMu.Lock()
	defer auditRetentionMu.Unlock()
	var policy AuditRetention
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		var downstream int64
		if err := tx.Model(&model.Node{}).Where("enable = ?", true).Count(&downstream).Error; err != nil {
			return err
		}
		if downstream > 0 {
			return fmt.Errorf("audit policy sync requires a direct child without enabled downstream nodes")
		}
		current, err := readAuditControl(tx)
		if err != nil {
			return err
		}
		var versions []model.Setting
		if err := tx.Where("key = ?", "auditParentRevision").Find(&versions).Error; err != nil {
			return err
		}
		version := int64(-1)
		if len(versions) > 0 {
			version, err = strconv.ParseInt(versions[0].Value, 10, 64)
			if err != nil {
				return fmt.Errorf("read parent audit revision: %w", err)
			}
		}
		if state.Revision > version {
			current.DetailHours, current.SummaryDays = state.DetailHours, state.SummaryDays
			// FORK(audit-workbench): Parent versioning applies to high-frequency thresholds too.
			current.ConnectionRateLimit, current.SiteRateLimit = state.ConnectionRateLimit, state.SiteRateLimit
			if err := saveAuditValue(tx, "auditParentRevision", strconv.FormatInt(state.Revision, 10)); err != nil {
				return err
			}
		}
		if state.ClearBefore.After(current.ClearBefore) {
			current.ClearBefore = state.ClearBefore
		}
		if err := saveAuditControl(tx, current); err != nil {
			return err
		}
		if err := saveAuditValue(tx, "auditChildMode", "true"); err != nil {
			return err
		}
		policy = retentionFromControl(current)
		return pruneChildAudit(tx, policy)
	})
	if err != nil {
		return fmt.Errorf("apply parent audit policy: %w", err)
	}
	return spool.Prune(auditHistoryStart(policy, time.Now().UTC()), policy.ClearBefore)
}

func (svc AuditService) PruneChild(spool *audit.Spool) error {
	auditRetentionMu.Lock()
	defer auditRetentionMu.Unlock()
	policy, err := svc.Retention()
	if err != nil {
		return err
	}
	if err := database.GetDB().Transaction(func(tx *gorm.DB) error { return pruneChildAudit(tx, policy) }); err != nil {
		return err
	}
	return spool.Prune(auditHistoryStart(policy, time.Now().UTC()), policy.ClearBefore)
}
