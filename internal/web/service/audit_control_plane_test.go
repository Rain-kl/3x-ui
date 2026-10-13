package service

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/audit"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestAuditExcludesAPIButPreservesClientLoopbackTraffic(t *testing.T) {
	db := initTrafficTestDB(t)
	spool := audit.NewSpool(filepath.Join(t.TempDir(), "events.jsonl"))
	api := "from 127.0.0.1:59428 accepted tcp:127.0.0.1:62789 [api -> api] uplink=264 downlink=258"
	client := "from 127.0.0.1:59428 accepted tcp:127.0.0.1:62789 [client-in >> direct] email: alice uplink=10 downlink=20"
	if !spool.Capture(api) || len(spool.Snapshot(100)) != 0 {
		t.Fatal("API access must be consumed without filling the spool")
	}
	if !spool.Capture(client) || len(spool.Snapshot(100)) != 1 {
		t.Fatal("real client loopback traffic must remain auditable")
	}
	svc := AuditService{}
	if err := svc.Ingest(7, spool.Snapshot(100)); err != nil {
		t.Fatal(err)
	}
	if err := svc.Ingest(7, []audit.Event{{ID: "ignored", Time: time.Now(), Inbound: "api", Destination: "tcp:127.0.0.1:62789"}}); err != nil {
		t.Fatal(err)
	}
	legacy := model.AuditEvent{ID: "legacy-api", NodeID: 7, Time: time.Now().UTC(), Inbound: "api", Destination: "tcp:127.0.0.1:62789"}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	summary, err := svc.Query(AuditFilter{Hours: 3})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Connections != 1 || summary.Uplink != 10 || summary.Downlink != 20 || len(summary.Events) != 1 || summary.Events[0].Client != "alice" {
		t.Fatalf("control-plane traffic polluted the summary: %+v", summary)
	}
	if err := svc.Prune(); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.AuditEvent{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("prune left %d records, want one real client: %v", count, err)
	}
}
