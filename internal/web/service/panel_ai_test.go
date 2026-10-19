package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestAIAuditSQLClientScopeAndAggregation(t *testing.T) {
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

	result, err := queryAISQL(context.Background(), "SELECT client, COUNT(*) AS connections FROM audit_events GROUP BY client ORDER BY client LIMIT 1")
	if err != nil {
		t.Fatal(err)
	}
	ranked := result.(aiSQLResult)
	if len(ranked.Rows) != 1 || ranked.Rows[0][0] != "alice" {
		t.Fatalf("unexpected SQL ranking: %+v", ranked)
	}
	result, err = queryAISQL(context.Background(), "SELECT client,site FROM audit_events WHERE client = 'bob'")
	if err != nil {
		t.Fatal(err)
	}
	details := result.(aiSQLResult)
	if len(details.Rows) != 1 || details.Rows[0][0] != "bob" {
		t.Fatalf("client scope leaked: %+v", details)
	}
	if _, err = queryAISQL(context.Background(), "DELETE FROM audit_events"); err == nil {
		t.Fatal("write SQL accepted")
	}
}
