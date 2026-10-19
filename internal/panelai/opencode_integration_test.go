package panelai

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestLocalOpenCodeAuditTools(t *testing.T) {
	binary := os.Getenv("XUI_TEST_OPENCODE_BIN")
	if binary == "" {
		t.Skip("set XUI_TEST_OPENCODE_BIN to verify against a real OpenCode CLI")
	}
	configPath := os.Getenv("XUI_TEST_OPENCODE_CONFIG")
	if configPath == "" {
		t.Skip("set XUI_TEST_OPENCODE_CONFIG to a native provider/model JSON file for CLI integration")
	}
	providerJSON, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(t.TempDir(), "opencode")
	if err := database.InitDB(filepath.Join(t.TempDir(), "panel.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	if err := SaveSettings(AISettings{Enabled: true, Directory: workspace, Binary: binary, Model: os.Getenv("XUI_TEST_OPENCODE_MODEL"), RetentionDays: 7}); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveProviderConfig(AIProviderConfig{Config: string(providerJSON)}); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	manager := &Manager{}
	if err := manager.Start(context.Background(), Hooks{Audit: func(_ context.Context, query AuditQuery) (any, error) {
		calls.Add(1)
		return map[string]any{"client": "synthetic-client", "connectionsPerSecond": 12, "observation": "Synthetic fixture, not real user data", "mode": query.Mode}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Stop)
	models, err := manager.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) == 0 {
		t.Fatal("OpenCode returned no connected models")
	}
	conversation, err := NewConversation("Synthetic audit integration")
	if err != nil {
		t.Fatal(err)
	}
	run, err := manager.Begin(conversation.ID, "", "Call panel_audit_query with mode metadata. Report the exact synthetic client identifier from the tool result. This is a connection test, do not infer abuse or create a task.", "", false)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(3 * time.Minute)
	defer deadline.Stop()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-deadline.C:
			_ = manager.Cancel(run.ID)
			t.Fatal("real OpenCode turn timed out")
		case <-tick.C:
			result, err := Run(run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if result.State == "running" {
				continue
			}
			if result.State != "completed" {
				t.Fatalf("OpenCode turn %s: %s", result.State, result.Error)
			}
			if calls.Load() == 0 || !strings.Contains(result.Result, "synthetic-client") {
				t.Fatalf("missing real MCP tool evidence: calls=%d result=%s trace=%s", calls.Load(), result.Result, result.Tools)
			}
			t.Logf("OpenCode models=%d tool calls=%d result=%s trace=%s", len(models), calls.Load(), result.Result, result.Tools)
			taskConversation, taskErr := NewConversation("Synthetic scheduling integration")
			if taskErr != nil {
				t.Fatal(taskErr)
			}
			taskRun, taskErr := manager.Begin(taskConversation.ID, "", "Call panel_audit_create_task now with these exact arguments: {\"name\":\"Synthetic scheduled review\",\"prompt\":\"Call the audit metadata tool and summarize its coverage.\",\"intervalHours\":12,\"telegram\":true}. You must actually call the tool before reporting that the task exists. Do not run the task now.", "", false)
			if taskErr != nil {
				t.Fatal(taskErr)
			}
			for until := time.Now().Add(3 * time.Minute); ; {
				if time.Now().After(until) {
					_ = manager.Cancel(taskRun.ID)
					t.Fatal("real OpenCode task creation timed out")
				}
				current, taskErr := Run(taskRun.ID)
				if taskErr != nil {
					t.Fatal(taskErr)
				}
				if current.State == "running" {
					time.Sleep(time.Second)
					continue
				}
				if current.State != "completed" {
					t.Fatalf("task creation failed: %s", current.Error)
				}
				var task model.AITask
				if taskErr = database.GetDB().First(&task, "name = ?", "Synthetic scheduled review").Error; taskErr != nil {
					t.Fatalf("OpenCode did not create the task: %v; result=%s", taskErr, current.Result)
				}
				if task.IntervalHours != 12 || !task.Enabled || !task.Telegram {
					t.Fatalf("incorrect real task: %+v", task)
				}
				t.Logf("OpenCode created task %s: interval=%d telegram=%t trace=%s", task.Name, task.IntervalHours, task.Telegram, current.Tools)
				break
			}
			if err = manager.DeleteConversation(taskConversation.ID); err != nil {
				t.Fatal(err)
			}
			if err = manager.DeleteConversation(conversation.ID); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
}
