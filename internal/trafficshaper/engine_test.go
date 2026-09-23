package trafficshaper

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type mockExecutor struct {
	mu        sync.Mutex
	commands  []string
	errOnCmd  string
	customErr error
}

func (m *mockExecutor) Execute(ctx context.Context, cmd string, args ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	full := cmd + " " + strings.Join(args, " ")
	m.commands = append(m.commands, full)
	if m.errOnCmd != "" && strings.Contains(full, m.errOnCmd) {
		if m.customErr != nil {
			return m.customErr
		}
		return errors.New("simulated error")
	}
	return nil
}

func TestEngineInitAndTeardown(t *testing.T) {
	mock := &mockExecutor{}
	engine := NewEngineWithExecutor("eth0", mock)

	ctx := context.Background()
	if err := engine.Init(ctx); err != nil {
		t.Fatalf("engine.Init failed: %v", err)
	}

	if len(mock.commands) == 0 {
		t.Fatalf("expected tc commands executed on init, got none")
	}

	// Verify root HTB qdisc and default 9999 class created.
	hasRoot := false
	hasDefaultClass := false
	hasTotalClass := false
	for _, cmd := range mock.commands {
		if strings.Contains(cmd, "qdisc replace dev eth0 root handle 1: htb default 9999") {
			hasRoot = true
		}
		if strings.Contains(cmd, "class replace dev eth0 parent 1: classid 1:1") {
			hasTotalClass = true
		}
		if strings.Contains(cmd, "class replace dev eth0 parent 1: classid 1:9999") {
			hasDefaultClass = true
		}
	}
	if !hasRoot || !hasDefaultClass || !hasTotalClass {
		t.Errorf("missing root or default class in executed commands: %v", mock.commands)
	}

	if err := engine.Teardown(ctx); err != nil {
		t.Fatalf("engine.Teardown failed: %v", err)
	}

	hasTeardown := false
	for _, cmd := range mock.commands {
		if strings.Contains(cmd, "qdisc del dev eth0 root") {
			hasTeardown = true
		}
	}
	if !hasTeardown {
		t.Errorf("missing qdisc del in executed commands: %v", mock.commands)
	}
}

func TestEngineInitError(t *testing.T) {
	mock := &mockExecutor{errOnCmd: "qdisc replace"}
	engine := NewEngineWithExecutor("eth0", mock)

	ctx := context.Background()
	if err := engine.Init(ctx); err == nil {
		t.Fatal("expected error on qdisc replace failure, got nil")
	}
}

func TestEngineTeardownToleratesMissingQdisc(t *testing.T) {
	mock := &mockExecutor{
		errOnCmd:  "qdisc del",
		customErr: errors.New("RTNETLINK answers: No such file or directory"),
	}
	engine := NewEngineWithExecutor("eth0", mock)

	ctx := context.Background()
	if err := engine.Teardown(ctx); err != nil {
		t.Fatalf("expected nil for missing qdisc, got: %v", err)
	}
}

func TestEngineTeardownFailsOnUnexpectedError(t *testing.T) {
	mock := &mockExecutor{
		errOnCmd:  "qdisc del",
		customErr: errors.New("permission denied"),
	}
	engine := NewEngineWithExecutor("eth0", mock)

	ctx := context.Background()
	if err := engine.Teardown(ctx); err == nil {
		t.Fatal("expected error on permission denied during teardown, got nil")
	}
}

func TestEngineProperties(t *testing.T) {
	mock := &mockExecutor{}
	engine := NewEngineWithExecutor("eth0", mock)

	if got := engine.Interface(); got != "eth0" {
		t.Fatalf("unexpected interface: got %s, want eth0", got)
	}
	if engine.Executor() != mock {
		t.Fatal("unexpected executor returned")
	}

	ctx := context.Background()
	if err := engine.Execute(ctx, "echo", "test"); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestEngineAutoDetectInterface(t *testing.T) {
	engine := NewEngine("")
	if engine.Interface() == "" {
		t.Fatal("expected non-empty interface from auto-detection")
	}
}

func TestEngineIsSupported(t *testing.T) {
	engine := NewEngine("eth0")
	supported := engine.IsSupported()
	if runtime.GOOS != "linux" && supported {
		t.Fatal("IsSupported should return false on non-Linux")
	}
}

func TestSysExecutor(t *testing.T) {
	executor := NewSysExecutorWithTimeout(2 * time.Second)
	ctx := context.Background()

	// Use standard command available on all POSIX platforms.
	if err := executor.Execute(ctx, "echo", "hello"); err != nil {
		t.Fatalf("SysExecutor echo failed: %v", err)
	}

	if err := executor.Execute(ctx, "nonexistent-command-12345"); err == nil {
		t.Fatal("expected error for nonexistent command, got nil")
	}

	//nolint:staticcheck // explicitly testing nil context guard in SysExecutor
	if err := executor.Execute(nil, "echo", "nil-context"); err != nil {
		t.Fatalf("SysExecutor with nil context failed: %v", err)
	}
}

func TestInboundRuleHelpers(t *testing.T) {
	rule := InboundRule{
		InboundID:        1,
		Port:             443,
		InboundDownLimit: 100,
		ClientDownLimit:  10,
		ActiveIPs:        []string{"192.168.1.1"},
	}

	if rule.ID() != 1 {
		t.Errorf("ID() = %d, want 1", rule.ID())
	}
	if rule.InboundLimitMbps() != 100 {
		t.Errorf("InboundLimitMbps() = %d, want 100", rule.InboundLimitMbps())
	}
	if rule.ClientLimitMbps() != 10 {
		t.Errorf("ClientLimitMbps() = %d, want 10", rule.ClientLimitMbps())
	}
}
