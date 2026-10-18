package panelai

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

type Manager struct {
	connectionMu  sync.Mutex
	runMu         sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc
	hooks         Hooks
	client        *openCode
	processCancel context.CancelFunc
	server        *http.Server
	mcpURL, token string
	active        map[string]context.CancelFunc
	slots         chan struct{}
	workers       sync.WaitGroup
}

var Default = &Manager{}

func randomID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes[:])
}

func (m *Manager) Start(parent context.Context, hooks Hooks) error {
	m.Stop()
	m.ctx, m.cancel = context.WithCancel(parent)
	m.hooks = hooks
	m.active = map[string]context.CancelFunc{}
	m.slots = make(chan struct{}, 2)
	m.token = randomID() + randomID()
	listener, err := (&net.ListenConfig{}).Listen(m.ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		m.cancel()
		return fmt.Errorf("listen for audit MCP: %w", err)
	}
	m.mcpURL = "http://" + listener.Addr().String() + "/mcp"
	m.server = &http.Server{Handler: http.HandlerFunc(m.serveMCP), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() {
		if err := m.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			logger.Errorf("panel audit MCP: %v", err)
		}
	}()
	now := time.Now().UTC()
	if err = database.GetDB().Model(&model.AIRun{}).Where("state = ?", "running").Updates(map[string]any{"state": "interrupted", "error": "Panel restarted before this run completed; retry explicitly.", "finished_at": now}).Error; err != nil {
		m.Stop()
		return fmt.Errorf("recover interrupted AI runs: %w", err)
	}
	return nil
}

func (m *Manager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.ResetConnection()
	m.workers.Wait()
	if m.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = m.server.Shutdown(ctx)
	}
}

func (m *Manager) ResetConnection() {
	m.runMu.Lock()
	for _, cancel := range m.active {
		cancel()
	}
	m.runMu.Unlock()
	m.connectionMu.Lock()
	defer m.connectionMu.Unlock()
	if m.processCancel != nil {
		m.processCancel()
		m.processCancel = nil
	}
	if m.client != nil {
		m.client.http.CloseIdleConnections()
		m.client = nil
	}
}

func (m *Manager) removeRemoteSession(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := m.connect(ctx)
	if err != nil {
		return fmt.Errorf("connect to remove OpenCode session: %w", err)
	}
	return client.call(ctx, "DELETE", "/session/"+id, nil, nil)
}

func (m *Manager) Cancel(id string) error {
	m.runMu.Lock()
	defer m.runMu.Unlock()
	cancel, ok := m.active[id]
	if !ok {
		return fmt.Errorf("AI run is no longer active")
	}
	cancel()
	return nil
}

func (m *Manager) Tick() {
	cfg, err := ReadSettings()
	if err != nil {
		logger.Errorf("read AI schedule settings: %v", err)
		return
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -cfg.RetentionDays)
	if err = database.GetDB().Where("started_at < ? AND state <> ?", cutoff, "running").Delete(&model.AIRun{}).Error; err != nil {
		logger.Errorf("prune AI runs: %v", err)
	}
	if !cfg.Enabled {
		return
	}
	var due []model.AITask
	if err = database.GetDB().Where("enabled = ? AND next_run <= ?", true, time.Now().UTC()).Order("next_run").Limit(10).Find(&due).Error; err != nil {
		logger.Errorf("query due AI tasks: %v", err)
		return
	}
	for _, task := range due {
		if _, err = m.Begin("", task.ID, "", "", true); err != nil {
			logger.Warningf("start AI task %s: %v", task.ID, err)
		}
	}
}
