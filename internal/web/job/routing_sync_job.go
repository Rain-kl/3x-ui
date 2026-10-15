package job

import (
	"context"
	"sync"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

type RoutingSyncJob struct{ running sync.Mutex }

func NewRoutingSyncJob() *RoutingSyncJob { return &RoutingSyncJob{} }

func (j *RoutingSyncJob) Run() {
	if !j.running.TryLock() {
		return
	}
	defer j.running.Unlock()
	if err := (service.RoutingSyncService{}).SyncNodes(context.Background()); err != nil {
		logger.Warning("routing synchronization: ", err)
	}
}
