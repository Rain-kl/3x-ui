package service

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/audit"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
)

func TestAuditMasterBroadcastPersistsFailuresAndRetries(t *testing.T) {
	db := initTrafficTestDB(t)
	useTestRuntimeManager(t)
	var mu sync.Mutex
	var states []audit.Control
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/panel/api/audit/sync" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("unexpected audit RPC: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var state audit.Control
		if err := json.NewDecoder(r.Body).Decode(&state); err != nil {
			t.Error(err)
		}
		mu.Lock()
		states = append(states, state)
		failing := fail
		mu.Unlock()
		if failing {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()
	host, portText, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	port, _ := strconv.Atoi(portText)
	node := model.Node{Name: "edge", Scheme: "http", Address: host, Port: port, BasePath: "/", ApiToken: "tok", Enable: true, Status: "offline", AllowPrivateAddress: true}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	disabled := node
	disabled.Id = 0
	disabled.Name = "disabled"
	disabled.Enable = false
	if err := db.Create(&disabled).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&disabled).Update("enable", false).Error; err != nil {
		t.Fatal(err)
	}
	svc := AuditService{}
	if err := svc.SetRetention(AuditRetention{DetailHours: 48, SummaryDays: 12}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	first := states[0]
	count := len(states)
	mu.Unlock()
	if count != 1 || first.DetailHours != 48 || first.SummaryDays != 12 || first.Revision != 1 {
		t.Fatalf("initial broadcast: count=%d state=%+v", count, first)
	}
	mu.Lock()
	fail = true
	mu.Unlock()
	if err := svc.Clear(0); err == nil || !strings.Contains(err.Error(), "node edge") {
		t.Fatalf("missing partial failure: %v", err)
	}
	state, err := svc.Control()
	if err != nil || state.ClearBefore.IsZero() || state.Revision != 2 {
		t.Fatalf("failed broadcast lost local policy: %+v %v", state, err)
	}
	mu.Lock()
	fail = false
	mu.Unlock()
	// Recreate the manager to represent a restart, with no in-memory retry cache.
	runtime.SetManager(runtime.NewManager(runtime.LocalDeps{}))
	if err := svc.SyncNodes(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	retried := states[len(states)-1]
	mu.Unlock()
	if retried != state {
		t.Fatalf("retry changed absolute cleanup time: %+v != %+v", retried, state)
	}
}

func TestAuditChildControlOrderingAndCompleteCleanup(t *testing.T) {
	db := initTrafficTestDB(t)
	spool := audit.NewSpool(filepath.Join(t.TempDir(), "spool.jsonl"))
	svc := AuditService{}
	cutoff := time.Now().UTC().Add(-time.Hour)
	old := cutoff.Add(-time.Minute)
	fresh := cutoff.Add(time.Minute)
	for _, e := range []audit.Event{{ID: "old", Time: old, Destination: "old.example"}, {ID: "fresh", Time: fresh, Destination: "new.example"}} {
		spool.Add(e)
		if err := db.Create(&model.AuditEvent{ID: e.ID, Time: e.Time, NodeID: 0, Destination: e.Destination}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&model.AuditReceipt{ID: "receipt", Time: old}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AuditDailyStat{Day: old.Format("2006-01-02"), NodeID: 0, Site: "old.example", Connections: 10}).Error; err != nil {
		t.Fatal(err)
	}
	state := audit.Control{DetailHours: 48, SummaryDays: 12, Revision: 2, ClearBefore: cutoff}
	if err := svc.ApplyControl(spool, state); err != nil {
		t.Fatal(err)
	}
	if !svc.IsChild() {
		t.Fatal("authenticated parent did not persist child role")
	}
	for range 2 {
		if err := svc.ApplyControl(spool, state); err != nil {
			t.Fatal(err)
		}
	}
	stale := audit.Control{DetailHours: 1, SummaryDays: 1, Revision: 1, ClearBefore: cutoff.Add(-time.Hour)}
	if err := svc.ApplyControl(spool, stale); err != nil {
		t.Fatal(err)
	}
	policy, err := svc.Retention()
	if err != nil || policy.DetailHours != 48 || policy.SummaryDays != 12 || !policy.ClearBefore.Equal(cutoff) {
		t.Fatalf("stale policy replaced current: %+v %v", policy, err)
	}
	events, err := svc.FetchPending(spool)
	if err != nil || len(events) != 1 || events[0].ID != "fresh" {
		t.Fatalf("cleanup removed fresh or retained old: %+v %v", events, err)
	}
	if spool.Add(audit.Event{ID: "late", Time: old, Destination: "late.example"}) {
		t.Fatal("cleared event reentered spool")
	}
	for _, m := range []any{&model.AuditReceipt{}, &model.AuditDailyStat{}} {
		var count int64
		if err := db.Model(m).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("uncleared %T: %d %v", m, count, err)
		}
	}
	if err := svc.SetRetention(AuditRetention{DetailHours: 24, SummaryDays: 30}); err == nil {
		t.Fatal("child local settings allowed")
	}
	if err := svc.Clear(0); err == nil {
		t.Fatal("child local clear allowed")
	}
}

func TestAuditChildControlRejectsNestedMasterWithoutChanges(t *testing.T) {
	db := initTrafficTestDB(t)
	if err := db.Create(&model.Node{Name: "downstream", Enable: true}).Error; err != nil {
		t.Fatal(err)
	}
	spool := audit.NewSpool(filepath.Join(t.TempDir(), "spool.jsonl"))
	err := (AuditService{}).ApplyControl(spool, audit.Control{DetailHours: 1, SummaryDays: 1, Revision: 1})
	if err == nil {
		t.Fatal("nested master accepted parent policy")
	}
	var count int64
	db.Model(&model.Setting{}).Where("key = ?", "auditChildMode").Count(&count)
	if count != 0 {
		t.Fatal("rejected sync persisted child role")
	}
}
