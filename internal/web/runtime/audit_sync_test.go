package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/audit"
)

func TestRemoteAuditSyncCachesSuccessAndRetriesFailure(t *testing.T) {
	var calls atomic.Int64
	var failing atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/panel/api/audit/sync" || r.Method != http.MethodPost {
			t.Errorf("wrong endpoint: %s %s", r.Method, r.URL.Path)
		}
		if failing.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()
	remote := NewRemote(nodeForServer(t, server, "skip", ""), nil)
	state := audit.Control{DetailHours: 24, SummaryDays: 30, Revision: 1}
	for range 3 {
		if err := remote.SyncAudit(context.Background(), state); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("unchanged policy sent %d times", calls.Load())
	}
	state.Revision++
	failing.Store(true)
	if err := remote.SyncAudit(context.Background(), state); err == nil {
		t.Fatal("HTTP failure masked")
	}
	failing.Store(false)
	if err := remote.SyncAudit(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatal("failure was cached rather than retried")
	}
	remote.auditSyncedAt = time.Now().Add(-2 * time.Minute)
	if err := remote.SyncAudit(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 4 {
		t.Fatal("source restart reconciliation suppressed")
	}
}
