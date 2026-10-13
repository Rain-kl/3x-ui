package service

import (
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

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
