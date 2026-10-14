package job

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/audit"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

type AuditSyncJob struct {
	running        sync.Mutex
	nodes          service.NodeService
	lastPrune      time.Time
	policyWarnings sync.Map
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
	hasEnabledDownstream := false
	for _, node := range nodes {
		hasEnabledDownstream = hasEnabledDownstream || node.Enable
	}
	if !hasEnabledDownstream && auditChildMode() {
		// FORK(audit-retention): Offline children expire source history using the last persisted master policy.
		if time.Since(j.lastPrune) >= 5*time.Minute {
			if err := (service.AuditService{}).PruneChild(audit.Local()); err != nil {
				logger.Warning("audit: child prune failed:", err)
			} else {
				j.lastPrune = time.Now()
			}
		}
		return
	}
	j.ingestLocal()
	if time.Since(j.lastPrune) >= 5*time.Minute {
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
	control, err := (service.AuditService{}).Control()
	if err != nil {
		logger.Warning("audit: read parent policy failed:", err)
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
			j.pullNode(mgr, node, control)
		})
	}
	wg.Wait()
}

func auditChildMode() bool { return (service.AuditService{}).IsChild() }

func auditIDs(events []audit.Event) []string {
	ids := make([]string, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.ID)
	}
	return ids
}

func (j *AuditSyncJob) ingestLocal() {
	// FORK(audit-node-consistency): Role transition and local ACK must not race.
	if err := (service.AuditService{}).IngestLocalPending(audit.Local()); err != nil {
		logger.Warning("audit: local ingest failed:", err)
	}
}

func (j *AuditSyncJob) pullNode(mgr *runtime.Manager, node *model.Node, control audit.Control) {
	rt, err := mgr.RemoteFor(node)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	// FORK(audit-retention): Persistent master policy and watermark converge before source export.
	if err := rt.SyncAudit(ctx, control); err != nil {
		last, ok := j.policyWarnings.Load(node.Id)
		if !ok || time.Since(last.(time.Time)) >= time.Minute {
			logger.Warningf("audit: policy sync to %s failed: %v", node.Name, err)
			j.policyWarnings.Store(node.Id, time.Now())
		}
		if !strings.Contains(err.Error(), "HTTP 404") {
			return
		}
		// Older children can still export safely: the master enforces its cleanup watermark.
		// The broadcast endpoint reports their unsupported policy synchronization explicitly.
	}
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
		if err := (service.AuditService{}).MarkDelivered(node.Id, auditIDs(events)); err != nil {
			logger.Warning("audit: mark node delivery failed:", err)
			return
		}
		if len(events) < 1000 {
			return
		}
	}
}
