package job

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/trafficshaper"
)

type mockJobCmdExecutor struct {
	commands []string
}

func (m *mockJobCmdExecutor) Execute(ctx context.Context, cmd string, args ...string) error {
	line := cmd
	for _, a := range args {
		line += " " + a
	}
	m.commands = append(m.commands, line)
	return nil
}

func TestCheckClientIpJobTrafficShaperSync(t *testing.T) {
	dbDir := t.TempDir()
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	mockExec := &mockJobCmdExecutor{}
	eng := trafficshaper.NewEngineWithExecutor("eth0", mockExec)
	rec := trafficshaper.NewReconciler(eng)
	trafficshaper.SetReconciler(rec)
	t.Cleanup(func() { trafficshaper.SetReconciler(nil) })

	inbound := &model.Inbound{
		Port:            3443,
		Protocol:        model.VLESS,
		ClientDownLimit: 20,
		Tag:             "in-job-rl",
		Enable:          true,
		Settings:        `{"clients":[{"id":"c1","email":"alice@test.com","enable":true}]}`,
	}
	if err := database.GetDB().Create(inbound).Error; err != nil {
		t.Fatalf("Create inbound failed: %v", err)
	}
	clientRecord := &model.ClientRecord{
		Email: "alice@test.com",
	}
	if err := database.GetDB().Create(clientRecord).Error; err != nil {
		t.Fatalf("Create clientRecord failed: %v", err)
	}
	clientInbound := &model.ClientInbound{
		ClientId:  clientRecord.Id,
		InboundId: inbound.Id,
	}
	if err := database.GetDB().Create(clientInbound).Error; err != nil {
		t.Fatalf("Create clientInbound failed: %v", err)
	}

	ctx := context.Background()
	_ = rec.ApplyInbound(ctx, trafficshaper.InboundRule{
		InboundID:       inbound.Id,
		Port:            inbound.Port,
		ClientDownLimit: inbound.ClientDownLimit,
		Clients:         []string{"alice@test.com"},
	})

	j := NewCheckClientIpJob()

	observed := map[string]map[string]int64{
		"alice@test.com": {"1.2.3.4": 12345},
	}

	emails := make([]string, 0, len(observed))
	for email := range observed {
		emails = append(emails, email)
	}
	inboundByEmail := j.loadInboundsByEmails(emails)
	for email, ib := range inboundByEmail {
		if ib != nil {
			rec.RegisterClientInbound(email, ib.Id)
		}
	}
	rec.SyncAllObserved(observed)

	hasClientFilter := false
	for _, cmd := range mockExec.commands {
		if strings.Contains(cmd, "match ip dst 1.2.3.4/32") {
			hasClientFilter = true
			break
		}
	}
	if !hasClientFilter {
		t.Fatalf("expected client filter for 1.2.3.4, got: %v", mockExec.commands)
	}
}
