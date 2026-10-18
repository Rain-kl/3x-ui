package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/panelai"
)

func TestAIAuditQueryClientScopeAndPaging(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "audit.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	now := time.Now().UTC().Add(-time.Minute)
	for _, row := range []model.AuditEvent{{ID: "a", Time: now, Inbound: "in", Client: "alice", Site: "first.example"}, {ID: "b", Time: now, Inbound: "in", Client: "bob", Site: "second.example"}} {
		if err := database.GetDB().Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	result, err := queryAIAudit(context.Background(), panelai.AuditQuery{Mode: "client_sites", Hours: 24, Page: 1, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	page := result.(map[string]any)
	if page["hasMore"] != true || len(page["items"].([]map[string]any)) != 1 {
		t.Fatalf("unbounded or unpaged result: %v", page)
	}
	result, err = queryAIAudit(context.Background(), panelai.AuditQuery{Mode: "details", Client: "bob", Hours: 24, Page: 1, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	details := result.(map[string]any)["items"].([]model.AuditEvent)
	if len(details) != 1 || details[0].Client != "bob" {
		t.Fatalf("client scope leaked: %+v", details)
	}
	if _, err = queryAIAudit(context.Background(), panelai.AuditQuery{Mode: "DELETE FROM audit_events", Hours: 24, Page: 1, Limit: 50}); err == nil {
		t.Fatal("arbitrary query mode accepted")
	}
}
