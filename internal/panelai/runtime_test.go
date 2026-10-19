package panelai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	workspace := t.TempDir()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveSettings(AISettings{Enabled: true, Directory: workspace, Binary: binary, RetentionDays: 7}); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateFile(workspace, "opencode.json", []byte(`{"provider":{"fixture":{"models":{"model":{}}}}}`)); err != nil {
		t.Fatal(err)
	}
	notifications := make(chan string, 2)
	var calls atomic.Int32
	manager := &Manager{}
	if err := manager.Start(context.Background(), Hooks{SQL: func(context.Context, string) (any, error) {
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
	if result.Result != "Synthetic audit report" || result.NotifyError != "fixture delivery failure" || calls.Load() != 1 || !strings.Contains(result.Tools, "panel_audit_execute_sql") {
		t.Fatalf("incorrect task result %+v calls=%d", result, calls.Load())
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

// TestMain's subprocess mode replaces the old HTTP fixture with native JSON-RPC transport.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version":
			fmt.Println("fixture-cli")
			os.Exit(0)
		case "session":
			os.Exit(0)
		case "acp":
			runACPFixture()
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}
