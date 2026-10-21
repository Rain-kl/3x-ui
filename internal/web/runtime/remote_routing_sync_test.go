package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/routingsync"
)

func TestRemoteRoutingSyncRequiresBalancingAcknowledgment(t *testing.T) {
	for _, tc := range []struct {
		name, response       string
		balancing, wantError bool
	}{
		{"legacy rules", `{"success":true}`, false, false},
		{"old child ignores balancing", `{"success":true}`, true, true},
		{"child did not apply balancing", `{"success":true,"obj":{"balancingApplied":false}}`, true, true},
		{"balancing applied", `{"success":true,"obj":{"balancingApplied":true}}`, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/panel/api/server/routing/sync" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()
			remote := NewRemote(nodeForPlainServer(t, server, "verify", "tok"), nil)
			state := routingsync.Control{Source: "master", Revision: 1, Rules: []map[string]any{}}
			if tc.balancing {
				state.Balancing = &routingsync.Balancing{Balancers: []map[string]any{}}
			}
			err := remote.SyncRouting(context.Background(), state)
			if tc.wantError {
				if err == nil || !strings.Contains(err.Error(), "upgrade the child panel") {
					t.Fatalf("expected explicit upgrade failure, got %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
