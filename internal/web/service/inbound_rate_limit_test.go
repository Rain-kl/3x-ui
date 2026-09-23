package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/trafficshaper"
)

type mockCmdExecutor struct {
	commands []string
}

func (m *mockCmdExecutor) Execute(ctx context.Context, cmd string, args ...string) error {
	line := cmd
	for _, a := range args {
		line += " " + a
	}
	m.commands = append(m.commands, line)
	return nil
}

func TestInboundServiceRateLimitIntegration(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_svc_rl.db")
	_ = database.InitDB(dbPath)
	t.Cleanup(func() { _ = database.CloseDB() })

	svc := &InboundService{}
	inbound := &model.Inbound{
		Remark:           "svc-test-rl",
		Port:             28443,
		Protocol:         model.VLESS,
		Settings:         `{"clients":[{"id":"u1","email":"user1@example.com","enable":true}]}`,
		InboundDownLimit: 50,
		ClientDownLimit:  5,
		Tag:              "in-svc-rl",
		Enable:           true,
	}

	_, _, err := svc.AddInbound(inbound)
	if err != nil {
		t.Fatalf("AddInbound failed: %v", err)
	}

	got, err := svc.GetInbound(inbound.Id)
	if err != nil || got == nil {
		t.Fatalf("GetInbound failed: %v", err)
	}
	if got.InboundDownLimit != 50 || got.ClientDownLimit != 5 {
		t.Errorf("Rate limit not stored correctly in service: %+v", got)
	}
}

func TestInboundServiceRateLimitReconcilerLifecycle(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_svc_rl_lifecycle.db")
	_ = database.InitDB(dbPath)
	t.Cleanup(func() { _ = database.CloseDB() })

	mockExec := &mockCmdExecutor{}
	eng := trafficshaper.NewEngineWithExecutor("eth0", mockExec)
	rec := trafficshaper.NewReconciler(eng)
	trafficshaper.SetReconciler(rec)
	t.Cleanup(func() { trafficshaper.SetReconciler(nil) })

	svc := &InboundService{}
	inbound := &model.Inbound{
		Remark:           "svc-test-rl-reconcile",
		Port:             28444,
		Protocol:         model.VLESS,
		Settings:         `{"clients":[{"id":"u2","email":"user2@example.com","enable":true}]}`,
		InboundDownLimit: 60,
		ClientDownLimit:  6,
		Tag:              "in-svc-rl-rec",
		Enable:           true,
	}

	_, _, err := svc.AddInbound(inbound)
	if err != nil {
		t.Fatalf("AddInbound failed: %v", err)
	}

	// Verify reconciler received AddInbound and created 60mbit class.
	hasInboundClass := false
	for _, cmd := range mockExec.commands {
		if strings.Contains(cmd, "class replace dev eth0") && strings.Contains(cmd, "rate 60mbit") {
			hasInboundClass = true
			break
		}
	}
	if !hasInboundClass {
		t.Fatalf("reconciler did not receive AddInbound rule; commands: %v", mockExec.commands)
	}

	// Update rate limits and verify DB update + reconciler update.
	inbound.InboundDownLimit = 80
	inbound.ClientDownLimit = 8
	_, _, err = svc.UpdateInbound(inbound)
	if err != nil {
		t.Fatalf("UpdateInbound failed: %v", err)
	}

	updated, err := svc.GetInbound(inbound.Id)
	if err != nil || updated == nil {
		t.Fatalf("GetInbound failed: %v", err)
	}
	if updated.InboundDownLimit != 80 || updated.ClientDownLimit != 8 {
		t.Fatalf("UpdateInbound did not persist new limits: %+v", updated)
	}

	hasUpdatedClass := false
	for _, cmd := range mockExec.commands {
		if strings.Contains(cmd, "class replace dev eth0") && strings.Contains(cmd, "rate 80mbit") {
			hasUpdatedClass = true
			break
		}
	}
	if !hasUpdatedClass {
		t.Fatalf("reconciler did not receive UpdateInbound rule; commands: %v", mockExec.commands)
	}

	// Disable inbound and verify reconciler deletes class.
	mockExec.commands = nil
	_, err = svc.SetInboundEnable(inbound.Id, false)
	if err != nil {
		t.Fatalf("SetInboundEnable(false) failed: %v", err)
	}
	hasDisableClassDel := false
	for _, cmd := range mockExec.commands {
		if strings.Contains(cmd, "class del dev eth0") {
			hasDisableClassDel = true
			break
		}
	}
	if !hasDisableClassDel {
		t.Fatalf("reconciler did not receive disable cleanup; commands: %v", mockExec.commands)
	}

	// Re-enable inbound and verify reconciler restores class.
	mockExec.commands = nil
	_, err = svc.SetInboundEnable(inbound.Id, true)
	if err != nil {
		t.Fatalf("SetInboundEnable(true) failed: %v", err)
	}
	hasReEnableClass := false
	for _, cmd := range mockExec.commands {
		if strings.Contains(cmd, "class replace dev eth0") && strings.Contains(cmd, "rate 80mbit") {
			hasReEnableClass = true
			break
		}
	}
	if !hasReEnableClass {
		t.Fatalf("reconciler did not receive enable restore; commands: %v", mockExec.commands)
	}

	// Delete inbound and verify reconciler deletes class.
	mockExec.commands = nil
	_, err = svc.DelInbound(inbound.Id)
	if err != nil {
		t.Fatalf("DelInbound failed: %v", err)
	}

	hasDelClass := false
	for _, cmd := range mockExec.commands {
		if strings.Contains(cmd, "class del dev eth0") {
			hasDelClass = true
			break
		}
	}
	if !hasDelClass {
		t.Fatalf("reconciler did not receive DelInbound; commands: %v", mockExec.commands)
	}
}
