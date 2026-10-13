package job

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/audit"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

type AuditSyncJob struct {
	running   sync.Mutex
	nodes     service.NodeService
	lastPrune time.Time
}

func NewAuditSyncJob() *AuditSyncJob { return &AuditSyncJob{} }

func (j *AuditSyncJob) Run() {
	if !j.running.TryLock() {
		return
	}
	defer j.running.Unlock()
	nodes, err := j.nodes.GetAll()
	if err != nil {
		return
	}
	if len(nodes) == 0 && auditChildMode() {
		return
	}
	j.ingestLocal()
	if time.Since(j.lastPrune) >= time.Hour {
		if err := (service.AuditService{}).Prune(); err != nil {
			logger.Warning("audit: prune failed:", err)
		} else {
			j.lastPrune = time.Now()
		}
	}
	if len(nodes) == 0 {
		return
	}
	mgr := runtime.GetManager()
	if mgr == nil {
		return
	}
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, node := range nodes {
		if !node.Enable || node.Status != "online" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		node := node
		common.GoRecover("audit-sync:"+node.Name, func() {
			defer wg.Done()
			defer func() { <-sem }()
			j.pullNode(mgr, node)
		})
	}
	wg.Wait()
}

func auditChildMode() bool {
	// A child without downstream nodes holds a node-sync token; keep its events
	// in the bounded spool while standalone panels ingest their own events.
	if os.Getenv("XUI_AUDIT_CHILD") == "1" {
		return true
	}
	var childTokens int64
	err := database.GetDB().Model(&model.ApiToken{}).
		Where("scope = ? AND enabled = ? AND (expires_at = 0 OR expires_at > ?)",
			model.ApiScopeNodeSync, true, time.Now().Unix()).Count(&childTokens).Error
	return err != nil || childTokens > 0
}

func auditIDs(events []audit.Event) []string {
	ids := make([]string, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.ID)
	}
	return ids
}

func (j *AuditSyncJob) ingestLocal() {
	for range 5 {
		events := audit.Local().Snapshot(1000)
		if len(events) == 0 {
			return
		}
		if err := (service.AuditService{}).Ingest(0, events); err != nil {
			logger.Warning("audit: local ingest failed:", err)
			return
		}
		if err := audit.Local().Ack(auditIDs(events)); err != nil {
			logger.Warning("audit: local ack failed:", err)
			return
		}
	}
}

func (j *AuditSyncJob) pullNode(mgr *runtime.Manager, node *model.Node) {
	rt, err := mgr.RemoteFor(node)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	for range 5 {
		events, err := rt.FetchAudit(ctx)
		if err != nil {
			if !strings.Contains(err.Error(), "HTTP 404") {
				logger.Warningf("audit: fetch from %s failed: %v", node.Name, err)
			}
			return
		}
		if len(events) == 0 {
			return
		}
		if err := (service.AuditService{}).Ingest(node.Id, events); err != nil {
			logger.Warningf("audit: ingest from %s failed: %v", node.Name, err)
			return
		}
		if err := rt.AckAudit(ctx, auditIDs(events)); err != nil {
			logger.Warningf("audit: ack to %s failed: %v", node.Name, err)
			return
		}
		if len(events) < 1000 {
			return
		}
	}
}
