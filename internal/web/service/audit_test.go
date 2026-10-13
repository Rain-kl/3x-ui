package service

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/audit"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func TestAuditIngestAndSummary(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "audit.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	one, two := int64(1024), int64(4096)
	now := time.Now().UTC()
	service := AuditService{}
	events := []audit.Event{
		{ID: "one", Time: now, Inbound: "in", Client: xray.ScopedEmail("in", "alice"), Destination: "tcp:192.0.2.10:443", SniffedDomain: "www.example.com", Uplink: &one, Downlink: &two},
		{ID: "two", Time: now, Inbound: "in", Client: "bob", Destination: "//other.example.com:443"},
	}
	if err := service.Ingest(7, events); err != nil {
		t.Fatal(err)
	}
	if err := service.Ingest(7, events); err != nil {
		t.Fatal(err)
	}
	summary, err := service.Query(AuditFilter{Hours: 3})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Connections != 2 || summary.Measured != 1 || summary.Uplink != one || summary.Downlink != two {
		t.Fatalf("wrong totals: %+v", summary)
	}
	if len(summary.Sites) != 1 || summary.Sites[0].Name != "example.com" || len(summary.Hours) != 1 {
		t.Fatalf("wrong aggregation: sites=%+v hours=%+v", summary.Sites, summary.Hours)
	}
	filtered, err := service.Query(AuditFilter{Hours: 3, NodeID: 7, Client: "alice", Site: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Connections != 1 || len(filtered.Events) != 1 || filtered.Events[0].Client != "alice" || filtered.Events[0].Destination != "tcp:192.0.2.10:443" || filtered.Events[0].Host != "www.example.com" {
		t.Fatalf("wrong filtered rows: %+v", filtered)
	}
}
