package panelai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestScheduledOpenCodeRunAndCancellation(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "panel.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	t.Setenv("XUI_OPENCODE_DIRECTORY", t.TempDir())
	var mu sync.Mutex
	var mcpURL, authorization string
	var prompts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/global/health":
			fmt.Fprint(w, `{"healthy":true,"version":"test"}`)
		case r.URL.Path == "/mcp":
			var body struct {
				Config struct {
					URL     string            `json:"url"`
					Headers map[string]string `json:"headers"`
				} `json:"config"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			mu.Lock()
			mcpURL, authorization = body.Config.URL, body.Config.Headers["Authorization"]
			mu.Unlock()
			fmt.Fprint(w, `{"panel_audit":{"status":"connected"}}`)
		case r.URL.Path == "/experimental/tool/ids":
			fmt.Fprint(w, `["bash","read","task"]`)
		case r.URL.Path == "/session":
			var body struct {
				Permission []map[string]string `json:"permission"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(body.Permission) < 2 || body.Permission[0]["action"] != "deny" {
				t.Error("session was not restricted")
			}
			fmt.Fprintf(w, `{"id":"ses_%s"}`, randomID())
		case strings.HasSuffix(r.URL.Path, "/abort"):
			fmt.Fprint(w, `true`)
		case strings.HasSuffix(r.URL.Path, "/message") && r.Method == http.MethodPost:
			var body struct {
				Tools map[string]bool `json:"tools"`
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Tools["bash"] || body.Tools["read"] || body.Tools["task"] || !body.Tools["panel_audit_query"] {
				t.Error("unrelated native tools were allowed")
			}
			prompts.Add(1)
			if len(body.Parts) > 0 && strings.Contains(body.Parts[0].Text, "BLOCK_UNTIL_CANCELLED") {
				<-r.Context().Done()
				return
			}
			if body.Tools["panel_audit_create_task"] {
				t.Error("scheduled execution can create tasks")
			}
			mu.Lock()
			endpoint, auth := mcpURL, authorization
			mu.Unlock()
			req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint, bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"query","arguments":{"mode":"metadata"}}}`))
			req.Header.Set("Authorization", auth)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Error(err)
			} else {
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					t.Errorf("MCP status=%d", resp.StatusCode)
				}
			}
			fmt.Fprint(w, `{"info":{},"parts":[{"type":"text","text":"Synthetic audit report"}]}`)
		case strings.HasSuffix(r.URL.Path, "/message"):
			fmt.Fprint(w, `[{"info":{"time":{"created":9999999999999}},"parts":[{"type":"tool","tool":"panel_audit_query","state":{"status":"completed"}}]}]`)
		case r.Method == http.MethodDelete:
			fmt.Fprint(w, `true`)
		default:
			http.Error(w, "unknown path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer server.Close()
	if err := SaveSettings(AISettings{Enabled: true, URL: server.URL, RetentionDays: 7}); err != nil {
		t.Fatal(err)
	}
	notifications := make(chan string, 2)
	var calls atomic.Int32
	manager := &Manager{}
	if err := manager.Start(context.Background(), Hooks{Audit: func(context.Context, AuditQuery) (any, error) {
		calls.Add(1)
		return map[string]string{"source": "fixture"}, nil
	}, Notify: func(_ context.Context, text string) error {
		notifications <- text
		return fmt.Errorf("fixture delivery failure")
	}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Stop)
	task, err := SaveTask(AITaskInput{Name: "Fixture", Prompt: "Review synthetic records", IntervalHours: 12, Enabled: true, Telegram: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = database.GetDB().Model(task).Update("next_run", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = manager.callTool(context.Background(), "create_task", json.RawMessage(`{"id":"`+task.ID+`","name":"malicious overwrite","prompt":"changed","intervalHours":1,"telegram":false}`)); err == nil {
		t.Fatal("create-only MCP tool accepted an existing task ID")
	}
	toolArgs := json.RawMessage(`{"name":"Tool fixture","prompt":"Review fixture only","intervalHours":24,"telegram":false}`)
	created, err := manager.callTool(context.Background(), "create_task", toolArgs)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := manager.callTool(context.Background(), "create_task", toolArgs)
	if err != nil {
		t.Fatal(err)
	}
	if created.(*model.AITask).ID != repeated.(*model.AITask).ID {
		t.Fatal("retried MCP call created duplicate schedules")
	}
	manager.Tick()
	manager.Tick()
	wait := func(id string, want string) *model.AIRun {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			value, err := Run(id)
			if err != nil {
				t.Fatal(err)
			}
			if value.State != "running" {
				if value.State != want {
					t.Fatalf("state=%s error=%s", value.State, value.Error)
				}
				return value
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("run did not finish")
		return nil
	}
	var scheduled model.AIRun
	if err = database.GetDB().First(&scheduled, "task_id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	result := wait(scheduled.ID, "completed")
	if result.Result != "Synthetic audit report" || result.NotifyError != "fixture delivery failure" || calls.Load() != 1 || prompts.Load() != 1 || !strings.Contains(result.Tools, "panel_audit_query") {
		t.Fatalf("incorrect task result %+v calls=%d prompts=%d", result, calls.Load(), prompts.Load())
	}
	select {
	case text := <-notifications:
		if !strings.Contains(text, "Synthetic audit report") {
			t.Fatalf("notification %s", text)
		}
	default:
		t.Fatal("no administrator report")
	}
	conversation, err := NewConversation("Cancellation")
	if err != nil {
		t.Fatal(err)
	}
	run, err := manager.Begin(conversation.ID, "", "BLOCK_UNTIL_CANCELLED", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Begin(conversation.ID, "", "second turn", "", false); err == nil {
		t.Fatal("overlapping conversation accepted")
	}
	if err = manager.Cancel(run.ID); err != nil {
		t.Fatal(err)
	}
	wait(run.ID, "cancelled")
}
