package service

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/audit"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestAuditLocalPendingStandaloneWithAdminToken(t *testing.T) {
	db := initTrafficTestDB(t)
	t.Setenv("XUI_AUDIT_CHILD", "")
	if err := db.Create(&model.ApiToken{Name: "admin", Token: "admin-hash", Enabled: true, Scope: model.ApiScopeAdmin}).Error; err != nil {
		t.Fatal(err)
	}
	svc := AuditService{}
	if svc.IsChild() {
		t.Fatal("an admin API token alone must not mark a standalone panel as a child")
	}

	spool := audit.NewSpool(filepath.Join(t.TempDir(), "events.jsonl"))
	uplink, downlink := int64(12), int64(34)
	if !spool.Add(audit.Event{
		ID: "standalone-event", Time: time.Now().UTC(), Inbound: "client-in", Client: "alice",
		Source: "192.0.2.1:1234", Destination: "tcp:192.0.2.10:443",
		SniffedDomain: "www.example.com", Uplink: &uplink, Downlink: &downlink,
	}) {
		t.Fatal("failed to add local event to spool")
	}
	if err := svc.IngestLocalPending(spool); err != nil {
		t.Fatalf("ingest local pending: %v", err)
	}
	if got := len(spool.Snapshot(100)); got != 0 {
		t.Fatalf("spool still has %d events after standalone ingest", got)
	}
	var row model.AuditEvent
	if err := db.Where("id = ?", "standalone-event").First(&row).Error; err != nil {
		t.Fatalf("read ingested event: %v", err)
	}
	if row.NodeID != 0 || row.Host != "www.example.com" || row.Destination != "tcp:192.0.2.10:443" || row.Uplink == nil || *row.Uplink != uplink || row.Downlink == nil || *row.Downlink != downlink {
		t.Fatalf("ingested row lost fields: %+v", row)
	}
}

func TestAuditChildFetchPendingAndAckPreserveExistingIDs(t *testing.T) {
	db := initTrafficTestDB(t)
	t.Setenv("XUI_AUDIT_CHILD", "")
	svc := AuditService{}
	now := time.Now().UTC()
	uplink, downlink := int64(1024), int64(4096)
	legacy := model.AuditEvent{
		ID: "legacy-audit-id", NodeID: 0, Time: now.Add(-time.Minute), Inbound: "client-in", Outbound: "direct",
		Client: "alice", Source: "192.0.2.3:1234", Destination: "tcp:203.0.113.8:443",
		Host: "www.example.com", Site: "example.com", Uplink: &uplink, Downlink: &downlink,
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	spool := audit.NewSpool(filepath.Join(t.TempDir(), "events.jsonl"))
	if !spool.Add(audit.Event{ID: legacy.ID, Time: legacy.Time, Inbound: legacy.Inbound, Destination: legacy.Destination}) {
		t.Fatal("failed to add duplicate child spool event")
	}
	if !spool.Add(audit.Event{
		ID: "new-spool-id", Time: now, Inbound: "client-in", Outbound: "direct", Client: "bob",
		Source: "192.0.2.4:4321", Destination: "tcp:192.0.2.5:443", SniffedDomain: "video.example.net",
	}) {
		t.Fatal("failed to add child spool event")
	}

	events, err := svc.FetchPending(spool)
	if err != nil {
		t.Fatalf("fetch pending: %v", err)
	}
	if !svc.IsChild() {
		t.Fatal("fetching pending events did not persist child role")
	}
	if len(events) != 2 || events[0].ID != legacy.ID || events[1].ID != "new-spool-id" {
		t.Fatalf("pending IDs = %+v, want stable existing ID followed by spool ID", auditIDsForTest(events))
	}
	if events[0].Destination != legacy.Destination || events[0].SniffedDomain != legacy.Host || events[0].Uplink == nil || *events[0].Uplink != uplink || events[0].Downlink == nil || *events[0].Downlink != downlink {
		t.Fatalf("existing row fields were not preserved: %+v", events[0])
	}
	if events[1].SniffedDomain != "video.example.net" {
		t.Fatalf("spool sniffed domain = %q, want video.example.net", events[1].SniffedDomain)
	}

	again, err := svc.FetchPending(spool)
	if err != nil {
		t.Fatalf("repeat fetch pending: %v", err)
	}
	if got, want := auditIDsForTest(again), auditIDsForTest(events); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("repeat fetch IDs = %v, want %v", got, want)
	}
	if err := svc.IngestLocalPending(spool); err != nil {
		t.Fatalf("child local ingest should be a no-op: %v", err)
	}
	if got := spool.Snapshot(100); len(got) != 2 || got[0].ID != legacy.ID || got[1].ID != "new-spool-id" {
		t.Fatalf("child local ingest consumed spool events: %+v", got)
	}
	var persisted int64
	if err := db.Model(&model.AuditEvent{}).Where("id = ?", legacy.ID).Count(&persisted).Error; err != nil || persisted != 1 {
		t.Fatalf("legacy DB event count = %d, err %v; want it retained until ACK", persisted, err)
	}

	summary, err := svc.Query(AuditFilter{Hours: 24})
	if err != nil {
		t.Fatalf("child summary query: %v", err)
	}
	if !summary.Centralized {
		t.Fatalf("child summary centralized = false, want true: %+v", summary)
	}

	if err := svc.AckPending(spool, []string{legacy.ID, "new-spool-id"}); err != nil {
		t.Fatalf("ack pending: %v", err)
	}
	if got := spool.Snapshot(100); len(got) != 0 {
		t.Fatalf("spool still has events after ACK: %+v", got)
	}
	if err := db.Model(&model.AuditEvent{}).Where("id = ?", legacy.ID).Count(&persisted).Error; err != nil || persisted != 0 {
		t.Fatalf("legacy DB event count after ACK = %d, err %v; want 0", persisted, err)
	}
}

func TestAuditMarkDeliveredMarksDetailedRows(t *testing.T) {
	db := initTrafficTestDB(t)
	t.Setenv("XUI_AUDIT_CHILD", "")
	svc := AuditService{}
	if err := svc.Ingest(7, []audit.Event{{
		ID: "delivered-detail", Time: time.Now().UTC(), Inbound: "client-in", Client: "alice", Destination: "tcp:example.com:443",
	}}); err != nil {
		t.Fatal(err)
	}
	var row model.AuditEvent
	if err := db.Where("id = ?", "delivered-detail").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Delivered == nil || *row.Delivered {
		t.Fatalf("new detail delivered = %v, want false", row.Delivered)
	}
	if err := svc.MarkDelivered(7, []string{"delivered-detail"}); err != nil {
		t.Fatalf("mark detailed event delivered: %v", err)
	}
	if err := db.Where("id = ?", "delivered-detail").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Delivered == nil || !*row.Delivered {
		t.Fatalf("delivered detail flag = %v, want true", row.Delivered)
	}
}

func TestAuditReceiptSurvivesDeliveryAndRetryUntilExpiry(t *testing.T) {
	db := initTrafficTestDB(t)
	t.Setenv("XUI_AUDIT_CHILD", "")
	svc := AuditService{}
	if err := svc.SetRetention(AuditRetention{DetailHours: 1, SummaryDays: 4}); err != nil {
		t.Fatal(err)
	}
	when := time.Now().UTC().Add(-50 * time.Hour)
	late := audit.Event{
		ID: "sparse-late-event", Time: when, Inbound: "client-in", Client: "alice",
		Destination: "tcp:example.com:443", Uplink: auditMetric(7), Downlink: auditMetric(11),
	}
	if err := svc.Ingest(7, []audit.Event{late}); err != nil {
		t.Fatalf("ingest sparse late event: %v", err)
	}
	var receipts int64
	if err := db.Model(&model.AuditReceipt{}).Where("id = ?", late.ID).Count(&receipts).Error; err != nil || receipts != 1 {
		t.Fatalf("late event receipt count = %d, err %v; want one", receipts, err)
	}

	// Delivery may be acknowledged after the detail has already compacted.
	if err := svc.MarkDelivered(7, []string{late.ID}); err != nil {
		t.Fatalf("mark compacted event delivered: %v", err)
	}
	if err := svc.Ingest(7, []audit.Event{late}); err != nil {
		t.Fatalf("retry acknowledged late event: %v", err)
	}
	got, err := svc.Query(AuditFilter{Hours: 720, NodeID: 7, Client: "alice", Site: "example.com"})
	if err != nil {
		t.Fatalf("query after retry: %v", err)
	}
	if got.Connections != 1 || got.Uplink != 7 || got.Downlink != 11 || got.Measured != 1 {
		t.Fatalf("acknowledged retry changed historical totals: %+v", got)
	}
	if err := db.Model(&model.AuditReceipt{}).Where("id = ?", late.ID).Count(&receipts).Error; err != nil || receipts != 1 {
		t.Fatalf("receipt after delivery and retry = %d, err %v; want one until expiry", receipts, err)
	}

	if err := svc.SetRetention(AuditRetention{DetailHours: 1, SummaryDays: 1}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Prune(); err != nil {
		t.Fatalf("prune expired receipt: %v", err)
	}
	if err := db.Model(&model.AuditReceipt{}).Where("id = ?", late.ID).Count(&receipts).Error; err != nil || receipts != 0 {
		t.Fatalf("expired receipt count = %d, err %v; want 0", receipts, err)
	}
}

func TestAuditFetchPendingClearsRedundantAndNumericLegacyDomains(t *testing.T) {
	db := initTrafficTestDB(t)
	t.Setenv("XUI_AUDIT_CHILD", "")
	now := time.Now().UTC().Add(-time.Minute)
	uplink, downlink := int64(13), int64(29)
	rows := []model.AuditEvent{
		{ID: "same-target-host", Time: now, Inbound: "client-in", Client: "alice", Destination: "tcp:example.com:443", Host: "example.com", Uplink: &uplink, Downlink: &downlink},
		{ID: "numeric-host", Time: now.Add(time.Second), Inbound: "client-in", Client: "alice", Destination: "tcp:other.example.net:443", Host: "12345", Uplink: &uplink, Downlink: &downlink},
		{ID: "observed-domain", Time: now.Add(2 * time.Second), Inbound: "client-in", Client: "alice", Destination: "tcp:192.0.2.8:443", Host: "www.example.org", Uplink: &uplink, Downlink: &downlink},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AuditEvent{
		ID: "node-scoped-row", NodeID: 41, Time: now, Inbound: "client-in", Client: "child-user",
		Destination: "tcp:child.example.net:443", Host: "child.example.net",
	}).Error; err != nil {
		t.Fatal(err)
	}
	events, err := (AuditService{}).FetchPending(audit.NewSpool(filepath.Join(t.TempDir(), "empty.jsonl")))
	if err != nil {
		t.Fatalf("fetch legacy pending rows: %v", err)
	}
	if len(events) != len(rows) {
		t.Fatalf("fetched %d rows, want %d: %+v", len(events), len(rows), events)
	}
	byID := make(map[string]audit.Event, len(events))
	for _, event := range events {
		byID[event.ID] = event
	}
	if _, ok := byID["node-scoped-row"]; ok {
		t.Fatal("FetchPending exported an event owned by a downstream node")
	}
	for _, id := range []string{"same-target-host", "numeric-host"} {
		if got := byID[id].SniffedDomain; got != "" {
			t.Errorf("%s sniffed domain = %q, want empty", id, got)
		}
	}
	observed := byID["observed-domain"]
	if observed.SniffedDomain != "www.example.org" || observed.Destination != "tcp:192.0.2.8:443" || observed.Uplink == nil || *observed.Uplink != uplink || observed.Downlink == nil || *observed.Downlink != downlink {
		t.Fatalf("legacy event lost observed domain, original target, or byte fields: %+v", observed)
	}
}

func TestAuditExportRefusesMasterWithDownstreamAndScopesLegacyRows(t *testing.T) {
	db := initTrafficTestDB(t)
	t.Setenv("XUI_AUDIT_CHILD", "")
	now := time.Now().UTC().Add(-time.Minute)
	if err := db.Create(&model.Node{
		Id: 41, Name: "downstream", Address: "127.0.0.1", Port: 2096,
		ApiToken: "node-token", Enable: true, Status: "online",
	}).Error; err != nil {
		t.Fatalf("create downstream node: %v", err)
	}
	rows := []model.AuditEvent{
		{ID: "master-local-row", NodeID: 0, Time: now, Inbound: "client-in", Destination: "tcp:master.example.com:443"},
		{ID: "downstream-owned-row", NodeID: 41, Time: now, Inbound: "client-in", Destination: "tcp:child.example.com:443"},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("create existing audit rows: %v", err)
	}
	spool := audit.NewSpool(filepath.Join(t.TempDir(), "events.jsonl"))
	if !spool.Add(audit.Event{ID: "spooled-event", Time: now, Inbound: "client-in", Destination: "tcp:spool.example.com:443"}) {
		t.Fatal("add spool event")
	}
	svc := AuditService{}
	if events, err := svc.FetchPending(spool); err == nil || !strings.Contains(err.Error(), "direct child") {
		t.Fatalf("FetchPending = %v, %v; want direct-child refusal", events, err)
	}
	if svc.IsChild() {
		t.Fatal("refused master export persisted child role")
	}
	var childRoleCount int64
	if err := db.Model(&model.Setting{}).Where("key = ? AND value = ?", "auditChildMode", "true").Count(&childRoleCount).Error; err != nil || childRoleCount != 0 {
		t.Fatalf("persisted child role count = %d, err %v; want 0", childRoleCount, err)
	}
	if got := spool.Snapshot(10); len(got) != 1 || got[0].ID != "spooled-event" {
		t.Fatalf("refused export changed spool: %+v", got)
	}

	if err := svc.AckPending(spool, []string{"master-local-row", "downstream-owned-row"}); err != nil {
		t.Fatalf("ack node-scoped audit rows: %v", err)
	}
	for _, tc := range []struct {
		id   string
		want int64
	}{{"master-local-row", 0}, {"downstream-owned-row", 1}} {
		var count int64
		if err := db.Model(&model.AuditEvent{}).Where("id = ?", tc.id).Count(&count).Error; err != nil || count != tc.want {
			t.Errorf("audit row %q count = %d, err %v; want %d", tc.id, count, err, tc.want)
		}
	}
}

func TestAuditStandaloneExportMigratesRetainedRawAndDailyBuckets(t *testing.T) {
	t.Setenv("XUI_AUDIT_CHILD", "")
	dir := t.TempDir()
	openDB := func(name string) {
		t.Helper()
		if err := database.InitDB(filepath.Join(dir, name)); err != nil {
			t.Fatalf("open %s database: %v", name, err)
		}
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	now := time.Now().UTC()
	rawTime := now.Add(-26 * time.Hour)
	day := rawTime.Format("2006-01-02")
	oldDay := now.AddDate(0, 0, -40).Format("2006-01-02")
	rawDownlink := int64(9)
	openDB("former-standalone-a.db")
	sourceA := database.GetDB()
	if err := sourceA.Create(&[]model.AuditEvent{
		{ID: "legacy-raw", NodeID: 0, Time: rawTime, Inbound: "client-in", Client: "alice", Destination: "tcp:api.example.com:443", Host: "api.example.com", Site: "example.com", Downlink: &rawDownlink},
		{ID: "expired-raw", NodeID: 0, Time: now.Add(-40 * 24 * time.Hour), Inbound: "client-in", Client: "old", Destination: "tcp:expired.example:443", Host: "expired.example", Site: "expired.example"},
	}).Error; err != nil {
		t.Fatalf("seed source A detail: %v", err)
	}
	if err := sourceA.Create(&[]model.AuditDailyStat{
		{Day: day, NodeID: 0, Client: "alice", Site: "example.com", Connections: 5, Uplink: 100, Downlink: 200, Measured: 2},
		{Day: day, NodeID: 8, Client: "alice", Site: "example.com", Connections: 99, Uplink: 900, Downlink: 900, Measured: 99},
		{Day: oldDay, NodeID: 0, Client: "old", Site: "expired.example", Connections: 7, Uplink: 70, Downlink: 80, Measured: 7},
	}).Error; err != nil {
		t.Fatalf("seed source A daily totals: %v", err)
	}
	spoolA := audit.NewSpool(filepath.Join(dir, "source-a-spool.jsonl"))
	svc := AuditService{}

	// An older master can receive and ACK raw detail without consuming compact buckets.
	legacyEvents, err := svc.FetchPending(spoolA, false)
	if err != nil {
		t.Fatalf("legacy-compatible fetch: %v", err)
	}
	if len(legacyEvents) != 1 || legacyEvents[0].ID != "legacy-raw" || legacyEvents[0].Time != rawTime {
		t.Fatalf("legacy-compatible export = %+v; want retained 26-hour raw visit", legacyEvents)
	}
	var count int64
	if err := sourceA.Model(&model.AuditEvent{}).Where("id = ?", "legacy-raw").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("raw crossing detail boundary was pruned before ACK: count=%d err=%v", count, err)
	}
	if err := sourceA.Model(&model.AuditEvent{}).Where("id = ?", "expired-raw").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("over-retention raw row count=%d err=%v; want 0", count, err)
	}
	if err := sourceA.Model(&model.AuditDailyStat{}).Where("day = ? AND node_id = 0", oldDay).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("over-retention daily row count=%d err=%v; want 0", count, err)
	}
	if err := svc.AckPending(spoolA, auditIDsForTest(legacyEvents)); err != nil {
		t.Fatalf("ACK raw event for older master: %v", err)
	}
	if err := sourceA.Model(&model.AuditDailyStat{}).Where("day = ? AND node_id = 0", day).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("legacy master ACK removed compact source bucket: count=%d err=%v", count, err)
	}
	if err := svc.SetRetention(AuditRetention{DetailHours: 24, SummaryDays: 30}); err == nil {
		t.Fatal("child panel accepted retention changes")
	}
	if err := svc.Clear(1); err == nil {
		t.Fatal("child panel accepted audit cleanup")
	}

	summaryEventsA, err := svc.FetchPending(spoolA, true)
	if err != nil {
		t.Fatalf("summary-capable fetch: %v", err)
	}
	if len(summaryEventsA) != 1 || summaryEventsA[0].Summary == nil {
		t.Fatalf("summary-capable export = %+v; want one compact summary", summaryEventsA)
	}
	compactA := summaryEventsA[0]
	if compactA.Summary.Connections != 5 || compactA.Summary.Measured != 2 || compactA.Client != "alice" || compactA.Destination != "example.com" || compactA.Uplink == nil || *compactA.Uplink != 100 || compactA.Downlink == nil || *compactA.Downlink != 200 {
		t.Fatalf("compact summary lost totals or unknown-byte count: %+v", compactA)
	}
	if compactA.ID == "" || len(compactA.ID) != 32 {
		t.Fatalf("summary export ID = %q; want one persisted 16-byte random ID", compactA.ID)
	}
	var persisted model.AuditDailyStat
	if err := sourceA.Where("day = ? AND node_id = 0 AND client = ? AND site = ?", day, "alice", "example.com").First(&persisted).Error; err != nil {
		t.Fatalf("read source A bucket after export: %v", err)
	}
	if persisted.ExportID != compactA.ID {
		t.Fatalf("source A export ID = %q, event ID = %q", persisted.ExportID, compactA.ID)
	}
	repeated, err := svc.FetchPending(spoolA, true)
	if err != nil {
		t.Fatalf("repeat summary-capable fetch: %v", err)
	}
	if len(repeated) != 1 || repeated[0].ID != compactA.ID {
		t.Fatalf("repeated summary fetch = %+v; want stable export ID %q", repeated, compactA.ID)
	}

	// A second former standalone has the same natural bucket key but its own export identity.
	openDB("former-standalone-b.db")
	sourceB := database.GetDB()
	if err := sourceB.Create(&model.AuditDailyStat{
		Day: day, NodeID: 0, Client: "alice", Site: "example.com", Connections: 5, Uplink: 100, Downlink: 200, Measured: 2,
	}).Error; err != nil {
		t.Fatalf("seed source B daily total: %v", err)
	}
	spoolB := audit.NewSpool(filepath.Join(dir, "source-b-spool.jsonl"))
	serviceB := AuditService{}
	summaryEventsB, err := serviceB.FetchPending(spoolB, true)
	if err != nil {
		t.Fatalf("fetch source B summary: %v", err)
	}
	if len(summaryEventsB) != 1 || summaryEventsB[0].Summary == nil || summaryEventsB[0].ID == compactA.ID {
		t.Fatalf("source B export ID conflicts with same-key source A: A=%q B=%+v", compactA.ID, summaryEventsB)
	}
	// The master turns the old raw visit and both compact buckets into node-scoped totals once.
	openDB("master.db")
	master := AuditService{}
	eventsA := append(append([]audit.Event(nil), legacyEvents...), summaryEventsA...)
	if err := master.Ingest(7, eventsA); err != nil {
		t.Fatalf("import source A events: %v", err)
	}
	if err := master.Ingest(8, summaryEventsB); err != nil {
		t.Fatalf("import source B summary: %v", err)
	}
	if err := master.Ingest(7, eventsA); err != nil {
		t.Fatalf("retry source A events: %v", err)
	}
	if err := master.Ingest(8, summaryEventsB); err != nil {
		t.Fatalf("retry source B summary: %v", err)
	}
	got, err := master.Query(AuditFilter{Hours: 720, Client: "alice", Site: "example.com"})
	if err != nil {
		t.Fatalf("query imported summaries: %v", err)
	}
	if got.Connections != 11 || got.Uplink != 200 || got.Downlink != 409 || got.Measured != 4 {
		t.Fatalf("imported metrics = %d/%d/%d/%d; want 11/200/409/4", got.Connections, got.Uplink, got.Downlink, got.Measured)
	}
	if err := database.GetDB().Model(&model.AuditReceipt{}).Count(&count).Error; err != nil || count != 3 {
		t.Fatalf("import receipts = %d, err %v; want raw plus two unique summaries", count, err)
	}

	// ACK of the new export removes only the matching node-zero source bucket.
	openDB("former-standalone-a.db")
	if err := svc.AckPending(spoolA, []string{compactA.ID}); err != nil {
		t.Fatalf("ACK compact source bucket: %v", err)
	}
	if err := database.GetDB().Model(&model.AuditDailyStat{}).Where("node_id = 0 AND export_id = ?", compactA.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("ACKed source A bucket count = %d, err %v; want 0", count, err)
	}
	if err := database.GetDB().Model(&model.AuditDailyStat{}).Where("day = ? AND node_id = 8", day).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("ACK removed unrelated node bucket: count=%d err=%v", count, err)
	}
	if err := database.GetDB().Model(&model.AuditEvent{}).Where("id = ?", "legacy-raw").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("ACKed raw event count=%d err=%v; want 0", count, err)
	}
}

func auditIDsForTest(events []audit.Event) []string {
	ids := make([]string, len(events))
	for i, event := range events {
		ids[i] = event.ID
	}
	return ids
}
