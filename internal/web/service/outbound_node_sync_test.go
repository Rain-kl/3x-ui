package service_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/op/go-logging"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	xuilogger "github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/job"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

func initOutboundSyncTestEnv(t *testing.T) {
	t.Helper()
	xuilogger.InitLogger(logging.ERROR)
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	service.StartTrafficWriter()
	t.Cleanup(service.StopTrafficWriter)
	runtime.SetManager(runtime.NewManager(runtime.LocalDeps{
		APIPort:        func() int { return 0 },
		SetNeedRestart: func() {},
	}))
	t.Cleanup(func() { runtime.SetManager(nil) })
}

// TestOutboundNodeSync_AdoptWorkerOutbound verifies master adopts remote worker's proxy outbounds.
func TestOutboundNodeSync_AdoptWorkerOutbound(t *testing.T) {
	initOutboundSyncTestEnv(t)
	db := database.GetDB()

	initTemplate := `{"outbounds":[{"protocol":"freedom","tag":"direct"},{"protocol":"blackhole","tag":"blocked"}]}`
	xraySettingSvc := &service.XraySettingService{}
	if err := xraySettingSvc.SaveXraySetting(initTemplate); err != nil {
		t.Fatalf("save initial template: %v", err)
	}

	workerProxyObs := []map[string]any{
		{
			"protocol": "vmess",
			"tag":      "worker-vmess-1",
			"settings": map[string]any{
				"vnext": []any{
					map[string]any{
						"address": "127.0.0.1",
						"port":    443,
						"users":   []any{map[string]any{"id": "00000000-0000-0000-0000-000000000001"}},
					},
				},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "server/outbounds") && r.Method == http.MethodGet:
			raw, _ := json.Marshal(workerProxyObs)
			_, _ = w.Write([]byte(`{"success":true,"obj":` + string(raw) + `}`))
			return
		case strings.HasSuffix(r.URL.Path, "inbounds/list"):
			_, _ = w.Write([]byte(`{"success":true,"obj":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())

	workerNode := &model.Node{
		Name:                "worker-node",
		Scheme:              "http",
		Address:             u.Hostname(),
		Port:                port,
		BasePath:            "/",
		ApiToken:            "dummy-tok",
		Enable:              true,
		Status:              "online",
		AllowPrivateAddress: true,
	}
	if err := db.Create(workerNode).Error; err != nil {
		t.Fatalf("create worker node: %v", err)
	}

	otherNode := &model.Node{
		Name:     "other-node",
		Scheme:   "http",
		Address:  "127.0.0.1",
		Port:     12345,
		BasePath: "/",
		ApiToken: "other-tok",
		Enable:   true,
		Status:   "online",
	}
	if err := db.Create(otherNode).Error; err != nil {
		t.Fatalf("create other node: %v", err)
	}

	job.NewNodeTrafficSyncJob().Run()

	storedTmpl, err := xraySettingSvc.GetXrayConfigTemplate()
	if err != nil {
		t.Fatalf("get template: %v", err)
	}
	masterObs, err := service.GetProxyOutboundsFromTemplate(storedTmpl)
	if err != nil {
		t.Fatalf("get proxy outbounds from template: %v", err)
	}
	if len(masterObs) != 1 || masterObs[0]["tag"] != "worker-vmess-1" {
		t.Fatalf("expected worker-vmess-1 adopted, got: %+v", masterObs)
	}

	var updatedOther model.Node
	if err := db.First(&updatedOther, otherNode.Id).Error; err != nil {
		t.Fatalf("fetch other node: %v", err)
	}
	if !updatedOther.ConfigDirty {
		t.Errorf("other node should be marked dirty, got dirty=%v", updatedOther.ConfigDirty)
	}

	var updatedWorker model.Node
	if err := db.First(&updatedWorker, workerNode.Id).Error; err != nil {
		t.Fatalf("fetch worker node: %v", err)
	}
	if updatedWorker.ConfigDirty {
		t.Errorf("reporting worker node should not be dirty, got dirty=%v", updatedWorker.ConfigDirty)
	}
}

// TestOutboundNodeSync_ReconcilePushesMasterOutbounds verifies master pushes outbounds to worker during reconcile.
func TestOutboundNodeSync_ReconcilePushesMasterOutbounds(t *testing.T) {
	initOutboundSyncTestEnv(t)
	db := database.GetDB()

	masterTemplate := `{
		"outbounds": [
			{"protocol": "freedom", "tag": "direct"},
			{
				"protocol": "vless",
				"tag": "master-vless-1",
				"settings": {
					"vnext": [{
						"address": "127.0.0.1",
						"port": 8443,
						"users": [{"id": "00000000-0000-0000-0000-000000000001", "encryption": "none"}]
					}]
				}
			}
		]
	}`
	xraySettingSvc := &service.XraySettingService{}
	if err := xraySettingSvc.SaveXraySetting(masterTemplate); err != nil {
		t.Fatalf("save master template: %v", err)
	}

	var mu sync.Mutex
	var pushedOutbounds []map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "server/outbounds") && r.Method == http.MethodPost:
			var obs []map[string]any
			if err := json.NewDecoder(r.Body).Decode(&obs); err == nil {
				mu.Lock()
				pushedOutbounds = obs
				mu.Unlock()
			}
			_, _ = w.Write([]byte(`{"success":true}`))
			return
		case strings.HasSuffix(r.URL.Path, "server/outbounds") && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"success":true,"obj":[]}`))
			return
		case strings.HasSuffix(r.URL.Path, "inbounds/list"):
			_, _ = w.Write([]byte(`{"success":true,"obj":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())

	workerNode := &model.Node{
		Name:                "dirty-worker",
		Scheme:              "http",
		Address:             u.Hostname(),
		Port:                port,
		BasePath:            "/",
		ApiToken:            "tok",
		Enable:              true,
		Status:              "online",
		ConfigDirty:         true,
		ConfigDirtyAt:       time.Now().UnixMilli(),
		AllowPrivateAddress: true,
	}
	if err := db.Create(workerNode).Error; err != nil {
		t.Fatalf("create worker node: %v", err)
	}

	job.NewNodeTrafficSyncJob().Run()

	mu.Lock()
	gotPushed := pushedOutbounds
	mu.Unlock()

	if len(gotPushed) != 1 || gotPushed[0]["tag"] != "master-vless-1" {
		t.Fatalf("expected master-vless-1 pushed to worker, got: %+v", gotPushed)
	}

	var updatedWorker model.Node
	if err := db.First(&updatedWorker, workerNode.Id).Error; err != nil {
		t.Fatalf("fetch worker node: %v", err)
	}
	if updatedWorker.ConfigDirty {
		t.Errorf("worker node dirty flag should be cleared after reconcile, got dirty=%v", updatedWorker.ConfigDirty)
	}
}

// TestSaveXraySetting_MarksAllNodesDirtyOnOutboundChange verifies SaveXraySetting marks nodes dirty on change.
func TestSaveXraySetting_MarksAllNodesDirtyOnOutboundChange(t *testing.T) {
	initOutboundSyncTestEnv(t)
	db := database.GetDB()

	n1 := &model.Node{Name: "n1", Enable: true, Status: "online"}
	n2 := &model.Node{Name: "n2", Enable: true, Status: "online"}
	if err := db.Create(n1).Error; err != nil {
		t.Fatalf("create n1: %v", err)
	}
	if err := db.Create(n2).Error; err != nil {
		t.Fatalf("create n2: %v", err)
	}

	xraySettingSvc := &service.XraySettingService{}
	initial := `{"outbounds":[{"protocol":"freedom","tag":"direct"}]}`
	if err := xraySettingSvc.SaveXraySetting(initial); err != nil {
		t.Fatalf("save initial: %v", err)
	}

	// Reset dirty flags.
	db.Model(&model.Node{}).Where("1 = 1").Updates(map[string]any{"config_dirty": false, "config_dirty_at": 0})

	// Add proxy outbound.
	withProxy := `{"outbounds":[{"protocol":"freedom","tag":"direct"},{"protocol":"trojan","tag":"trojan-out","settings":{"servers":[{"address":"127.0.0.1","port":443,"password":"secret"}]}}]}`
	if err := xraySettingSvc.SaveXraySetting(withProxy); err != nil {
		t.Fatalf("save withProxy: %v", err)
	}

	var nodes []model.Node
	if err := db.Find(&nodes).Error; err != nil {
		t.Fatalf("find nodes: %v", err)
	}
	for _, n := range nodes {
		if !n.ConfigDirty {
			t.Errorf("node %s should be marked dirty on outbound change", n.Name)
		}
	}

	// Save identical configuration; nodes should not be marked dirty.
	db.Model(&model.Node{}).Where("1 = 1").Updates(map[string]any{"config_dirty": false, "config_dirty_at": 0})
	if err := xraySettingSvc.SaveXraySetting(withProxy); err != nil {
		t.Fatalf("save withProxy again: %v", err)
	}

	nodes = nil
	if err := db.Find(&nodes).Error; err != nil {
		t.Fatalf("find nodes: %v", err)
	}
	for _, n := range nodes {
		if n.ConfigDirty {
			t.Errorf("node %s should not be dirty when outbounds unchanged", n.Name)
		}
	}
}

// TestOutboundNodeSync_PushOutboundsFailureMarksReconcileFailed verifies that
// PushProxyOutbounds failure keeps the node dirty and aborts outbound adoption.
func TestOutboundNodeSync_PushOutboundsFailureMarksReconcileFailed(t *testing.T) {
	initOutboundSyncTestEnv(t)
	db := database.GetDB()

	masterTemplate := `{
		"outbounds": [
			{"protocol": "freedom", "tag": "direct"},
			{
				"protocol": "vless",
				"tag": "master-vless-1",
				"settings": {
					"vnext": [{
						"address": "127.0.0.1",
						"port": 8443,
						"users": [{"id": "00000000-0000-0000-0000-000000000001", "encryption": "none"}]
					}]
				}
			}
		]
	}`
	xraySettingSvc := &service.XraySettingService{}
	if err := xraySettingSvc.SaveXraySetting(masterTemplate); err != nil {
		t.Fatalf("save master template: %v", err)
	}

	workerProxyObs := []map[string]any{
		{
			"protocol": "vmess",
			"tag":      "worker-vmess-resurrect",
			"settings": map[string]any{
				"vnext": []any{
					map[string]any{
						"address": "10.0.0.99",
						"port":    443,
						"users":   []any{map[string]any{"id": "00000000-0000-0000-0000-000000000002"}},
					},
				},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "server/outbounds") && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"success":false,"msg":"disk full"}`))
			return
		case strings.HasSuffix(r.URL.Path, "server/outbounds") && r.Method == http.MethodGet:
			raw, _ := json.Marshal(workerProxyObs)
			_, _ = w.Write([]byte(`{"success":true,"obj":` + string(raw) + `}`))
			return
		case strings.HasSuffix(r.URL.Path, "inbounds/list"):
			_, _ = w.Write([]byte(`{"success":true,"obj":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())

	workerNode := &model.Node{
		Name:                "failing-worker",
		Scheme:              "http",
		Address:             u.Hostname(),
		Port:                port,
		BasePath:            "/",
		ApiToken:            "tok",
		Enable:              true,
		Status:              "online",
		ConfigDirty:         true,
		ConfigDirtyAt:       time.Now().UnixMilli(),
		AllowPrivateAddress: true,
	}
	if err := db.Create(workerNode).Error; err != nil {
		t.Fatalf("create worker node: %v", err)
	}

	job.NewNodeTrafficSyncJob().Run()

	var updatedWorker model.Node
	if err := db.First(&updatedWorker, workerNode.Id).Error; err != nil {
		t.Fatalf("fetch worker node: %v", err)
	}
	if !updatedWorker.ConfigDirty {
		t.Errorf("worker node dirty flag should NOT be cleared when push outbounds fails")
	}

	storedTmpl, err := xraySettingSvc.GetXrayConfigTemplate()
	if err != nil {
		t.Fatalf("get template: %v", err)
	}
	masterObs, err := service.GetProxyOutboundsFromTemplate(storedTmpl)
	if err != nil {
		t.Fatalf("get proxy outbounds from template: %v", err)
	}
	for _, ob := range masterObs {
		if ob["tag"] == "worker-vmess-resurrect" {
			t.Errorf("deleted/unreconciled outbound should not be adopted while node is dirty")
		}
	}
}

// TestOutboundNodeSync_ThrottlesOutboundPull verifies adoption pulls are throttled
// by nodeOutboundSyncInterval.
func TestOutboundNodeSync_ThrottlesOutboundPull(t *testing.T) {
	initOutboundSyncTestEnv(t)
	db := database.GetDB()

	initTemplate := `{"outbounds":[{"protocol":"freedom","tag":"direct"}]}`
	xraySettingSvc := &service.XraySettingService{}
	if err := xraySettingSvc.SaveXraySetting(initTemplate); err != nil {
		t.Fatalf("save initial template: %v", err)
	}

	var mu sync.Mutex
	workerProxyObs := []map[string]any{
		{
			"protocol": "vmess",
			"tag":      "first-vmess",
			"settings": map[string]any{
				"vnext": []any{
					map[string]any{
						"address": "10.0.0.1",
						"port":    443,
						"users":   []any{map[string]any{"id": "00000000-0000-0000-0000-000000000001"}},
					},
				},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "server/outbounds") && r.Method == http.MethodGet:
			mu.Lock()
			raw, _ := json.Marshal(workerProxyObs)
			mu.Unlock()
			_, _ = w.Write([]byte(`{"success":true,"obj":` + string(raw) + `}`))
			return
		case strings.HasSuffix(r.URL.Path, "inbounds/list"):
			_, _ = w.Write([]byte(`{"success":true,"obj":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())

	workerNode := &model.Node{
		Name:                "throttled-worker",
		Scheme:              "http",
		Address:             u.Hostname(),
		Port:                port,
		BasePath:            "/",
		ApiToken:            "tok",
		Enable:              true,
		Status:              "online",
		AllowPrivateAddress: true,
	}
	if err := db.Create(workerNode).Error; err != nil {
		t.Fatalf("create worker node: %v", err)
	}

	syncJob := job.NewNodeTrafficSyncJob()
	syncJob.Run()

	storedTmpl, err := xraySettingSvc.GetXrayConfigTemplate()
	if err != nil {
		t.Fatalf("get template: %v", err)
	}
	masterObs, _ := service.GetProxyOutboundsFromTemplate(storedTmpl)
	if len(masterObs) != 1 || masterObs[0]["tag"] != "first-vmess" {
		t.Fatalf("expected first-vmess adopted on first run, got: %+v", masterObs)
	}

	// Update worker with a second outbound and immediately run the same job again.
	mu.Lock()
	workerProxyObs = append(workerProxyObs, map[string]any{
		"protocol": "trojan",
		"tag":      "second-trojan",
		"settings": map[string]any{
			"servers": []any{map[string]any{"address": "10.0.0.2", "port": float64(443)}},
		},
	})
	mu.Unlock()

	syncJob.Run()

	storedTmpl2, err := xraySettingSvc.GetXrayConfigTemplate()
	if err != nil {
		t.Fatalf("get template: %v", err)
	}
	masterObs2, _ := service.GetProxyOutboundsFromTemplate(storedTmpl2)
	if len(masterObs2) != 1 {
		t.Errorf("second outbound should NOT be adopted immediately due to interval throttling, got %d outbounds", len(masterObs2))
	}
}

// TestOutboundNodeSync_ConcurrentAdoption verifies concurrent syncOne workers
// safely merge outbounds without race conditions.
func TestOutboundNodeSync_ConcurrentAdoption(t *testing.T) {
	initOutboundSyncTestEnv(t)
	db := database.GetDB()

	initTemplate := `{"outbounds":[{"protocol":"freedom","tag":"direct"}]}`
	xraySettingSvc := &service.XraySettingService{}
	if err := xraySettingSvc.SaveXraySetting(initTemplate); err != nil {
		t.Fatalf("save initial template: %v", err)
	}

	makeWorker := func(name, host string, port int, obs []map[string]any) *httptest.Server {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case strings.HasSuffix(r.URL.Path, "server/outbounds") && r.Method == http.MethodGet:
				raw, _ := json.Marshal(obs)
				_, _ = w.Write([]byte(`{"success":true,"obj":` + string(raw) + `}`))
				return
			case strings.HasSuffix(r.URL.Path, "inbounds/list"):
				_, _ = w.Write([]byte(`{"success":true,"obj":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true}`))
		}))
		return srv
	}

	w1Obs := []map[string]any{{
		"protocol": "vmess",
		"tag":      "w1-vmess",
		"settings": map[string]any{"vnext": []any{map[string]any{"address": "10.1.0.1", "port": 443, "users": []any{map[string]any{"id": "00000000-0000-0000-0000-000000000001"}}}}},
	}}
	w2Obs := []map[string]any{{
		"protocol": "vmess",
		"tag":      "w2-vmess",
		"settings": map[string]any{"vnext": []any{map[string]any{"address": "10.2.0.1", "port": 443, "users": []any{map[string]any{"id": "00000000-0000-0000-0000-000000000002"}}}}},
	}}

	srv1 := makeWorker("w1", "127.0.0.1", 0, w1Obs)
	defer srv1.Close()
	u1, _ := url.Parse(srv1.URL)
	p1, _ := strconv.Atoi(u1.Port())

	srv2 := makeWorker("w2", "127.0.0.1", 0, w2Obs)
	defer srv2.Close()
	u2, _ := url.Parse(srv2.URL)
	p2, _ := strconv.Atoi(u2.Port())

	n1 := &model.Node{Name: "worker-1", Scheme: "http", Address: u1.Hostname(), Port: p1, BasePath: "/", ApiToken: "tok1", Enable: true, Status: "online", AllowPrivateAddress: true}
	n2 := &model.Node{Name: "worker-2", Scheme: "http", Address: u2.Hostname(), Port: p2, BasePath: "/", ApiToken: "tok2", Enable: true, Status: "online", AllowPrivateAddress: true}
	if err := db.Create(n1).Error; err != nil {
		t.Fatalf("create n1: %v", err)
	}
	if err := db.Create(n2).Error; err != nil {
		t.Fatalf("create n2: %v", err)
	}

	job.NewNodeTrafficSyncJob().Run()

	storedTmpl, err := xraySettingSvc.GetXrayConfigTemplate()
	if err != nil {
		t.Fatalf("get template: %v", err)
	}
	masterObs, err := service.GetProxyOutboundsFromTemplate(storedTmpl)
	if err != nil {
		t.Fatalf("get proxy outbounds from template: %v", err)
	}
	if len(masterObs) != 2 {
		t.Fatalf("expected 2 outbounds adopted concurrently, got %d: %+v", len(masterObs), masterObs)
	}
}
