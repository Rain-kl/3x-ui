package trafficshaper

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestReconcilerInboundAndClientSync(t *testing.T) {
	mock := &mockExecutor{}
	engine := NewEngineWithExecutor("eth0", mock)
	reconciler := NewReconciler(engine)

	ctx := context.Background()
	rule := InboundRule{
		InboundID:        1,
		Port:             443,
		InboundDownLimit: 100,
		ClientDownLimit:  10,
	}

	// Apply inbound rule
	reconciler.ApplyInbound(ctx, rule)

	// Verify inbound class created with 100mbit
	hasInboundClass := false
	for _, cmd := range mock.commands {
		if strings.Contains(cmd, "class replace dev eth0 parent 1:1 classid 1:10 htb rate 100mbit") {
			hasInboundClass = true
		}
	}
	if !hasInboundClass {
		t.Fatalf("inbound class not found in commands: %v", mock.commands)
	}

	// Sync active client IP
	reconciler.SyncClientIPs(ctx, 1, "user1@example.com", []string{"192.168.1.100"})

	// Verify client class and filter created with 10mbit and fq_codel
	hasClientClass := false
	hasClientFilter := false
	for _, cmd := range mock.commands {
		if strings.Contains(cmd, "class replace dev eth0 parent 1:10") && strings.Contains(cmd, "rate 10mbit") {
			hasClientClass = true
		}
		if strings.Contains(cmd, "filter replace dev eth0") && strings.Contains(cmd, "match ip dst 192.168.1.100/32") {
			hasClientFilter = true
		}
	}
	if !hasClientClass || !hasClientFilter {
		t.Fatalf("client class/filter not created properly: %v", mock.commands)
	}
}

func TestReconcilerUpdateInboundAndRateChange(t *testing.T) {
	mock := &mockExecutor{}
	engine := NewEngineWithExecutor("eth0", mock)
	reconciler := NewReconciler(engine)

	ctx := context.Background()
	rule := InboundRule{
		InboundID:        2,
		Port:             8443,
		InboundDownLimit: 50,
		ClientDownLimit:  5,
	}

	if err := reconciler.ApplyInbound(ctx, rule); err != nil {
		t.Fatalf("ApplyInbound failed: %v", err)
	}
	if err := reconciler.SyncClientIPs(ctx, 2, "user2@example.com", []string{"192.168.1.102"}); err != nil {
		t.Fatalf("SyncClientIPs failed: %v", err)
	}

	// Update inbound limits
	rule.InboundDownLimit = 200
	rule.ClientDownLimit = 20
	if err := reconciler.UpdateInbound(rule); err != nil {
		t.Fatalf("UpdateInbound failed: %v", err)
	}

	hasUpdatedInboundClass := false
	hasUpdatedClientClass := false
	for _, cmd := range mock.commands {
		if strings.Contains(cmd, "class replace dev eth0 parent 1:1 classid 1:20 htb rate 200mbit") {
			hasUpdatedInboundClass = true
		}
		if strings.Contains(cmd, "class replace dev eth0 parent 1:20") && strings.Contains(cmd, "rate 20mbit") {
			hasUpdatedClientClass = true
		}
	}
	if !hasUpdatedInboundClass || !hasUpdatedClientClass {
		t.Fatalf("updated classes not found in commands: %v", mock.commands)
	}
}

func TestReconcilerRemoveInbound(t *testing.T) {
	mock := &mockExecutor{}
	engine := NewEngineWithExecutor("eth0", mock)
	reconciler := NewReconciler(engine)

	ctx := context.Background()
	rule := InboundRule{
		InboundID:        3,
		Port:             9443,
		InboundDownLimit: 30,
		ClientDownLimit:  3,
	}

	_ = reconciler.ApplyInbound(ctx, rule)
	_ = reconciler.SyncClientIPs(ctx, 3, "user3@example.com", []string{"192.168.1.103"})

	if err := reconciler.RemoveInbound(3); err != nil {
		t.Fatalf("RemoveInbound failed: %v", err)
	}

	hasFilterDel := false
	hasClassDel := false
	for _, cmd := range mock.commands {
		if strings.Contains(cmd, "filter del dev eth0") && strings.Contains(cmd, "192.168.1.103/32") {
			hasFilterDel = true
		}
		if strings.Contains(cmd, "class del dev eth0 classid 1:30") {
			hasClassDel = true
		}
	}
	if !hasFilterDel || !hasClassDel {
		t.Fatalf("expected deletion commands not found: %v", mock.commands)
	}
}

func TestReconcilerClientIPIncrementalSync(t *testing.T) {
	mock := &mockExecutor{}
	engine := NewEngineWithExecutor("eth0", mock)
	reconciler := NewReconciler(engine)

	ctx := context.Background()
	rule := InboundRule{
		InboundID:        1,
		Port:             443,
		InboundDownLimit: 100,
		ClientDownLimit:  10,
	}
	_ = reconciler.ApplyInbound(ctx, rule)

	// Step 1: Initial IP
	_ = reconciler.SyncClientIPs(ctx, 1, "client@test.com", []string{"10.0.0.1"})

	// Step 2: Add second IP
	mock.commands = nil
	_ = reconciler.SyncClientIPs(ctx, 1, "client@test.com", []string{"10.0.0.1", "10.0.0.2"})

	hasNewFilter := false
	hasOldFilterAgain := false
	for _, cmd := range mock.commands {
		if strings.Contains(cmd, "match ip dst 10.0.0.2/32") {
			hasNewFilter = true
		}
		if strings.Contains(cmd, "match ip dst 10.0.0.1/32") {
			hasOldFilterAgain = true
		}
	}
	if !hasNewFilter || hasOldFilterAgain {
		t.Fatalf("expected only 10.0.0.2 added incrementally, got: %v", mock.commands)
	}

	// Step 3: Remove second IP
	mock.commands = nil
	_ = reconciler.SyncClientIPs(ctx, 1, "client@test.com", []string{"10.0.0.1"})

	hasDelFilter := false
	for _, cmd := range mock.commands {
		if strings.Contains(cmd, "filter del") && strings.Contains(cmd, "10.0.0.2/32") {
			hasDelFilter = true
		}
	}
	if !hasDelFilter {
		t.Fatalf("expected filter del for 10.0.0.2, got: %v", mock.commands)
	}

	// Step 4: Disconnect all IPs
	mock.commands = nil
	_ = reconciler.SyncClientIPs(ctx, 1, "client@test.com", []string{})

	hasDelOld := false
	hasDelClass := false
	for _, cmd := range mock.commands {
		if strings.Contains(cmd, "filter del") && strings.Contains(cmd, "10.0.0.1/32") {
			hasDelOld = true
		}
		if strings.Contains(cmd, "class del") && strings.Contains(cmd, "classid 1:101") {
			hasDelClass = true
		}
	}
	if !hasDelOld || !hasDelClass {
		t.Fatalf("expected complete cleanup on client disconnect, got: %v", mock.commands)
	}
}

func TestReconcilerDebounce(t *testing.T) {
	mock := &mockExecutor{}
	engine := NewEngineWithExecutor("eth0", mock)
	reconciler := NewReconciler(engine)
	reconciler.SetDebounceDelay(50 * time.Millisecond)
	defer reconciler.Stop()

	ctx := context.Background()
	rule := InboundRule{
		InboundID:        1,
		Port:             443,
		InboundDownLimit: 100,
		ClientDownLimit:  10,
	}
	_ = reconciler.ApplyInbound(ctx, rule)
	mock.commands = nil

	// Rapidly queue multiple IP changes within debounce window
	reconciler.QueueClientIPs(1, "debounce@test.com", []string{"10.1.1.1"})
	reconciler.QueueClientIPs(1, "debounce@test.com", []string{"10.1.1.1", "10.1.1.2"})
	reconciler.QueueClientIPs(1, "debounce@test.com", []string{"10.1.1.3"})

	// Immediately check: commands should NOT yet be executed due to debounce delay
	if len(mock.commands) > 0 {
		t.Fatalf("expected 0 commands before debounce fires, got %d", len(mock.commands))
	}

	// Flush immediately to apply pending queue
	if err := reconciler.Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	hasFinalFilter := false
	for _, cmd := range mock.commands {
		if strings.Contains(cmd, "match ip dst 10.1.1.3/32") {
			hasFinalFilter = true
		}
	}
	if !hasFinalFilter {
		t.Fatalf("expected final state 10.1.1.3 after flush, got: %v", mock.commands)
	}
}

func TestReconcilerInvalidInputs(t *testing.T) {
	mock := &mockExecutor{}
	engine := NewEngineWithExecutor("eth0", mock)
	reconciler := NewReconciler(engine)

	// Zero / negative inbound ID
	if err := reconciler.ApplyInbound(context.Background(), InboundRule{InboundID: 0, Port: 80}); err == nil {
		t.Fatal("expected error for InboundID=0, got nil")
	}
	if err := reconciler.ApplyInbound(context.Background(), InboundRule{InboundID: 1, Port: 0}); err == nil {
		t.Fatal("expected error for Port=0, got nil")
	}
	if err := reconciler.RemoveInbound(0); err == nil {
		t.Fatal("expected error for RemoveInbound(0), got nil")
	}

	//nolint:staticcheck // explicitly testing nil context fallback
	if err := reconciler.ApplyInbound(nil, InboundRule{InboundID: 99, Port: 999, InboundDownLimit: 10}); err != nil {
		t.Fatalf("expected nil context to be tolerated, got: %v", err)
	}
}

func TestReconcilerZeroLimitsRemovesRules(t *testing.T) {
	mock := &mockExecutor{}
	engine := NewEngineWithExecutor("eth0", mock)
	reconciler := NewReconciler(engine)

	ctx := context.Background()
	rule := InboundRule{
		InboundID:        1,
		Port:             443,
		InboundDownLimit: 100,
		ClientDownLimit:  10,
	}
	_ = reconciler.ApplyInbound(ctx, rule)

	mock.commands = nil
	// Disable rate limits by setting 0/0
	rule.InboundDownLimit = 0
	rule.ClientDownLimit = 0
	if err := reconciler.ApplyInbound(ctx, rule); err != nil {
		t.Fatalf("ApplyInbound zero limits failed: %v", err)
	}

	hasClassDel := false
	for _, cmd := range mock.commands {
		if strings.Contains(cmd, "class del dev eth0 classid 1:10") {
			hasClassDel = true
		}
	}
	if !hasClassDel {
		t.Fatalf("expected class del when limits reset to 0, got: %v", mock.commands)
	}
}

func TestReconcilerSingleton(t *testing.T) {
	mock := &mockExecutor{}
	engine := NewEngineWithExecutor("eth0", mock)
	r := NewReconciler(engine)
	SetReconciler(r)

	if got := GetReconciler(); got != r {
		t.Fatalf("GetReconciler got %v, want %v", got, r)
	}
}
