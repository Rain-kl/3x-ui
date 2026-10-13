package service

import (
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/audit"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func auditMetric(value int64) *int64  { return &value }
func auditDelivered(value bool) *bool { return &value }

func TestAuditRetentionIngestRetryAndExpiredEvents(t *testing.T) {
	db := initTrafficTestDB(t)
	now := time.Now().UTC()
	svc := AuditService{}
	events := []audit.Event{
		{ID: "fresh", Time: now, Inbound: "client-in", Client: "alice", Destination: "tcp:one.example.com:443", Uplink: auditMetric(11), Downlink: auditMetric(17)},
		{ID: "delayed", Time: now.Add(-25 * time.Hour), Inbound: "client-in", Client: "alice", Destination: "tcp:old.example.com:443", Uplink: auditMetric(100), Downlink: auditMetric(200)},
		{ID: "control", Time: now, Inbound: "api", Client: "api", Destination: "tcp:127.0.0.1:62789", Uplink: auditMetric(300), Downlink: auditMetric(400)},
	}
	if err := svc.Ingest(7, events); err != nil {
		t.Fatal(err)
	}
	if err := svc.Ingest(7, events); err != nil {
		t.Fatal(err)
	}
	var rows int64
	if err := db.Model(&model.AuditEvent{}).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("stored %d detail rows, want only the fresh event", rows)
	}
	summary, err := svc.Query(AuditFilter{Hours: 168})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Connections != 2 || summary.Uplink != 111 || summary.Downlink != 217 || summary.Measured != 2 || summary.Total != 0 || len(summary.Events) != 0 {
		t.Fatalf("delayed retry or API events changed metrics: %+v", summary)
	}
	var receipts int64
	if err := db.Model(&model.AuditReceipt{}).Count(&receipts).Error; err != nil {
		t.Fatal(err)
	}
	if receipts != 1 {
		t.Fatalf("delayed event created %d receipts, want one", receipts)
	}
}

func TestAuditRetentionArchivedUnacknowledgedRetryIsDeduplicated(t *testing.T) {
	db := initTrafficTestDB(t)
	svc := AuditService{}
	when := time.Now().UTC().Add(-30 * time.Hour)
	archived := model.AuditEvent{Delivered: auditDelivered(false), ID: "unacked-archive", NodeID: 7, Time: when, Inbound: "client-in", Client: "alice", Site: "example.com", Uplink: auditMetric(23), Downlink: auditMetric(29)}
	if err := db.Create(&archived).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.Prune(); err != nil {
		t.Fatal(err)
	}
	var receiptCount int64
	if err := db.Model(&model.AuditReceipt{}).Where("id = ?", archived.ID).Count(&receiptCount).Error; err != nil {
		t.Fatal(err)
	}
	if receiptCount != 1 {
		t.Fatalf("archive did not preserve an unacknowledged event receipt: count=%d", receiptCount)
	}
	delayed := audit.Event{ID: archived.ID, Time: when, Inbound: archived.Inbound, Client: archived.Client, Destination: "tcp:example.com:443", Uplink: archived.Uplink, Downlink: archived.Downlink}
	if err := svc.Ingest(7, []audit.Event{delayed}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Query(AuditFilter{Hours: 168, NodeID: 7, Client: "alice", Site: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Connections != 1 || got.Uplink != 23 || got.Downlink != 29 || got.Measured != 1 || len(got.Events) != 0 {
		t.Fatalf("retry after archive changed historical totals: %+v", got)
	}
}

func TestAuditRetentionConfiguredDetailAndSummaryBoundaries(t *testing.T) {
	db := initTrafficTestDB(t)
	svc := AuditService{}
	if err := svc.SetRetention(AuditRetention{DetailHours: 2, SummaryDays: 3}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	acceptedOldDay := now.AddDate(0, 0, -2)
	events := []audit.Event{
		{ID: "inside-detail-window", Time: now.Add(-time.Hour), Inbound: "client-in", Client: "alice", Destination: "tcp:example.com:443", Uplink: auditMetric(1), Downlink: auditMetric(2)},
		{ID: "past-detail-window", Time: now.Add(-3 * time.Hour), Inbound: "client-in", Client: "alice", Destination: "tcp:example.com:443", Uplink: auditMetric(3), Downlink: auditMetric(4)},
		{ID: "summary-oldest-day", Time: acceptedOldDay, Inbound: "client-in", Client: "alice", Destination: "tcp:example.com:443", Uplink: auditMetric(5), Downlink: auditMetric(6)},
		{ID: "outside-summary-window", Time: now.AddDate(0, 0, -3), Inbound: "client-in", Client: "alice", Destination: "tcp:example.com:443", Uplink: auditMetric(100), Downlink: auditMetric(200)},
	}
	if err := svc.Ingest(7, events); err != nil {
		t.Fatal(err)
	}
	if err := svc.Ingest(7, events); err != nil {
		t.Fatal(err)
	}
	var details int64
	if err := db.Model(&model.AuditEvent{}).Count(&details).Error; err != nil {
		t.Fatal(err)
	}
	if details != 1 {
		t.Fatalf("configured detail window retained %d rows, want only the one-hour event", details)
	}
	var oldReceipts int64
	if err := db.Model(&model.AuditReceipt{}).Where("id = ?", "outside-summary-window").Count(&oldReceipts).Error; err != nil {
		t.Fatal(err)
	}
	if oldReceipts != 0 {
		t.Fatal("event beyond the configured summary window created a receipt")
	}
	got, err := svc.Query(AuditFilter{Hours: 720, NodeID: 7, Client: "alice", Site: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Connections != 3 || got.Uplink != 9 || got.Downlink != 12 || got.Measured != 3 {
		t.Fatalf("configured detail/summary boundaries yielded %+v, want 3/9/12/3", got)
	}
	oldest, err := svc.Query(AuditFilter{Day: acceptedOldDay.Format("2006-01-02"), NodeID: 7, Client: "alice", Site: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if oldest.Connections != 1 || oldest.Uplink != 5 || oldest.Downlink != 6 || oldest.Measured != 1 || len(oldest.Days) != 1 {
		t.Fatalf("summary boundary date was not retained: %+v", oldest)
	}
}

func TestAuditRetentionOneUtcSummaryDayKeepsRollingDetailsAcrossMidnight(t *testing.T) {
	db := initTrafficTestDB(t)
	svc := AuditService{}
	if err := svc.SetRetention(AuditRetention{DetailHours: 24, SummaryDays: 1}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
	stats := []model.AuditDailyStat{
		{Day: today, NodeID: 7, Client: "today", Site: "today.example", Connections: 2, Uplink: 3, Downlink: 4, Measured: 1},
		{Day: yesterday, NodeID: 7, Client: "yesterday", Site: "yesterday.example", Connections: 5, Uplink: 6, Downlink: 7, Measured: 1},
	}
	if err := db.Create(&stats).Error; err != nil {
		t.Fatal(err)
	}
	visit := audit.Event{ID: "detail-crossing-midnight", Time: now.Add(-24*time.Hour + time.Minute), Inbound: "client-in", Client: "alice", Destination: "tcp:example.com:443", Uplink: auditMetric(11), Downlink: auditMetric(13)}
	if visit.Time.UTC().Format("2006-01-02") != yesterday {
		t.Fatalf("test setup did not cross a UTC date boundary: event=%s now=%s", visit.Time.UTC(), now)
	}
	if err := svc.Ingest(7, []audit.Event{visit}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Prune(); err != nil {
		t.Fatal(err)
	}
	var details []model.AuditEvent
	if err := db.Find(&details).Error; err != nil {
		t.Fatal(err)
	}
	if len(details) != 1 || details[0].ID != visit.ID {
		t.Fatalf("rolling 24-hour detail was removed at UTC midnight: %+v", details)
	}
	var retained []model.AuditDailyStat
	if err := db.Order("day").Find(&retained).Error; err != nil {
		t.Fatal(err)
	}
	if len(retained) != 1 || retained[0].Day != today {
		t.Fatalf("summaryDays=1 retained %d UTC dates: %+v, want only %s", len(retained), retained, today)
	}
	current, err := svc.Query(AuditFilter{Hours: 24, NodeID: 7, Client: "alice", Site: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if current.Connections != 1 || current.Uplink != 11 || current.Downlink != 13 || current.Total != 1 || len(current.Events) != 1 || current.Events[0].ID != visit.ID {
		t.Fatalf("rolling detail query lost prior-date event: %+v", current)
	}
	historical, err := svc.Query(AuditFilter{Day: yesterday, NodeID: 7, Client: "alice", Site: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if historical.Connections != 0 || historical.DailyTotal != 0 || historical.Total != 0 || len(historical.Days) != 0 || len(historical.Events) != 0 {
		t.Fatalf("one-day history included a day outside summary retention: %+v", historical)
	}
}

func TestAuditRetentionNodeMinusOneFiltersLocalDetailsAndRollups(t *testing.T) {
	db := initTrafficTestDB(t)
	svc := AuditService{}
	now := time.Now().UTC()
	rows := []model.AuditEvent{
		{ID: "local-detail", NodeID: 0, Time: now.Add(-time.Hour), Inbound: "client-in", Client: "local", Site: "local.example", Uplink: auditMetric(2), Downlink: auditMetric(3)},
		{ID: "remote-detail", NodeID: 7, Time: now.Add(-time.Hour), Inbound: "client-in", Client: "remote", Site: "remote.example", Uplink: auditMetric(5), Downlink: auditMetric(7)},
		{ID: "local-archived", NodeID: 0, Time: now.Add(-48 * time.Hour), Inbound: "client-in", Client: "local", Site: "local.example", Uplink: auditMetric(11), Downlink: auditMetric(13)},
		{ID: "remote-archived", NodeID: 7, Time: now.Add(-48 * time.Hour), Inbound: "client-in", Client: "remote", Site: "remote.example", Uplink: auditMetric(17), Downlink: auditMetric(19)},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.Prune(); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Query(AuditFilter{Hours: 168, NodeID: -1})
	if err != nil {
		t.Fatal(err)
	}
	if got.Connections != 2 || got.Uplink != 13 || got.Downlink != 16 || got.Measured != 2 {
		t.Fatalf("local node metrics = %+v, want 2/13/16/2", got)
	}
	if got.Total != 0 || len(got.Events) != 0 {
		t.Fatalf("historical local-node query exposed detail pagination: total=%d events=%+v", got.Total, got.Events)
	}
	if got.DailyTotal != 2 || len(got.Days) != 2 {
		t.Fatalf("local node daily rows = total %d, rows %+v; want current and archived local dates", got.DailyTotal, got.Days)
	}
	foundArchived := false
	for _, day := range got.Days {
		if day.NodeID != 0 || day.Client != "local" || day.Site != "local.example" {
			t.Fatalf("remote data leaked into local daily rows: %+v", got.Days)
		}
		if day.Connections == 1 && day.Uplink == 11 && day.Downlink == 13 {
			foundArchived = true
		}
	}
	if !foundArchived {
		t.Fatalf("archived local day missing from local results: %+v", got.Days)
	}
	current, err := svc.Query(AuditFilter{Hours: 3, NodeID: -1})
	if err != nil {
		t.Fatal(err)
	}
	if current.Total != 1 || len(current.Events) != 1 || current.Events[0].ID != "local-detail" {
		t.Fatalf("short local-node query should retain detail pagination: %+v", current)
	}
}

func TestAuditRetentionPrunesAndQueriesDailyStats(t *testing.T) {
	db := initTrafficTestDB(t)
	svc := AuditService{}
	now := time.Now().UTC()
	history := now.Add(-48 * time.Hour)
	otherHistoryDay := history.AddDate(0, 0, -1)
	events := []model.AuditEvent{
		{ID: "historical-a", NodeID: 7, Time: history, Inbound: "client-in", Client: "alice", Site: "example.com", Uplink: auditMetric(10), Downlink: auditMetric(20)},
		{ID: "historical-b", NodeID: 7, Time: history.Add(time.Minute), Inbound: "client-in", Client: "alice", Site: "example.com", Uplink: auditMetric(30), Downlink: auditMetric(40)},
		{ID: "other-client", NodeID: 7, Time: history.Add(2 * time.Minute), Inbound: "client-in", Client: "bob", Site: "elsewhere.net", Uplink: auditMetric(5), Downlink: auditMetric(6)},
		{ID: "other-node", NodeID: 8, Time: history.Add(3 * time.Minute), Inbound: "client-in", Client: "alice", Site: "example.com", Uplink: auditMetric(7), Downlink: auditMetric(8)},
		{ID: "unknown-bytes", NodeID: 7, Time: history.Add(4 * time.Minute), Inbound: "client-in", Client: "alice", Site: "example.com"},
		{ID: "same-client-site-next-day", NodeID: 7, Time: otherHistoryDay, Inbound: "client-in", Client: "alice", Site: "example.com", Uplink: auditMetric(9), Downlink: auditMetric(12)},
		{ID: "fresh", NodeID: 7, Time: now.Add(-time.Hour), Inbound: "client-in", Client: "alice", Site: "example.com", Uplink: auditMetric(2), Downlink: auditMetric(3)},
		{ID: "api-history", NodeID: 7, Time: history, Inbound: "api", Client: "api", Site: "localhost", Uplink: auditMetric(1000), Downlink: auditMetric(1000)},
		{ID: "expired-history", NodeID: 7, Time: now.Add(-35 * 24 * time.Hour), Inbound: "client-in", Client: "alice", Site: "expired.example", Uplink: auditMetric(90), Downlink: auditMetric(91)},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.Prune(); err != nil {
		t.Fatal(err)
	}

	var details []model.AuditEvent
	if err := db.Order("id").Find(&details).Error; err != nil {
		t.Fatal(err)
	}
	if len(details) != 1 || details[0].ID != "fresh" {
		t.Fatalf("remaining detailed events = %+v, want only fresh event", details)
	}
	var stats []model.AuditDailyStat
	if err := db.Order("node_id, client").Find(&stats).Error; err != nil {
		t.Fatal(err)
	}
	if len(stats) != 4 {
		t.Fatalf("daily stats = %+v, want four retained day/dimension groups", stats)
	}

	for _, tc := range []struct {
		name                                            string
		filter                                          AuditFilter
		wantConnections, wantUp, wantDown, wantMeasured int64
		wantDetails                                     int
	}{
		{"seven days", AuditFilter{Hours: 168}, 7, 63, 89, 6, 0},
		{"thirty days", AuditFilter{Hours: 720}, 7, 63, 89, 6, 0},
		{"client and site", AuditFilter{Hours: 168, NodeID: 7, Client: "alice", Site: "example.com"}, 5, 51, 75, 4, 0},
		{"node filter", AuditFilter{Hours: 720, NodeID: 8}, 1, 7, 8, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := svc.Query(tc.filter)
			if err != nil {
				t.Fatal(err)
			}
			if got.Connections != tc.wantConnections || got.Uplink != tc.wantUp || got.Downlink != tc.wantDown || got.Measured != tc.wantMeasured {
				t.Fatalf("metrics = connections:%d uplink:%d downlink:%d measured:%d, want %d/%d/%d/%d", got.Connections, got.Uplink, got.Downlink, got.Measured, tc.wantConnections, tc.wantUp, tc.wantDown, tc.wantMeasured)
			}
			if len(got.Events) != tc.wantDetails || got.Total != 0 {
				t.Fatalf("historical detail page = total %d events %+v, want no visit-level details", got.Total, got.Events)
			}
		})
	}

	dayFilter := AuditFilter{Day: history.Format("2006-01-02"), NodeID: 7, Client: "alice", Site: "example.com"}
	daySummary, err := svc.Query(dayFilter)
	if err != nil {
		t.Fatal(err)
	}
	if daySummary.Connections != 3 || daySummary.Uplink != 40 || daySummary.Downlink != 60 || daySummary.Measured != 2 {
		t.Fatalf("selected UTC day totals = %+v, want 3/40/60/2", daySummary)
	}
	if daySummary.DailyTotal != 1 || len(daySummary.Days) != 1 {
		t.Fatalf("selected UTC day groups = total %d, rows %+v; want one merged day/client/site row", daySummary.DailyTotal, daySummary.Days)
	}
	dayRow := daySummary.Days[0]
	if dayRow.Day != history.Format("2006-01-02") || dayRow.NodeID != 7 || dayRow.Client != "alice" || dayRow.Site != "example.com" || dayRow.Connections != 3 || dayRow.Uplink != 40 || dayRow.Downlink != 60 || dayRow.Measured != 2 {
		t.Fatalf("selected day row = %+v, want that UTC date and merged dimensions", dayRow)
	}
	if daySummary.Total != 0 || len(daySummary.Events) != 0 || len(daySummary.Hours) != 0 {
		t.Fatalf("historical selected day exposed visit-level data: total=%d events=%+v hours=%+v", daySummary.Total, daySummary.Events, daySummary.Hours)
	}

	otherDaySummary, err := svc.Query(AuditFilter{Day: otherHistoryDay.Format("2006-01-02"), NodeID: 7, Client: "alice", Site: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if otherDaySummary.Connections != 1 || otherDaySummary.Uplink != 9 || otherDaySummary.Downlink != 12 || otherDaySummary.Measured != 1 || len(otherDaySummary.Days) != 1 || otherDaySummary.Days[0].Day != otherHistoryDay.Format("2006-01-02") {
		t.Fatalf("same client/site on another date was not kept separate: %+v", otherDaySummary)
	}
	if _, err := svc.Query(AuditFilter{Day: "2026-02-30"}); err == nil {
		t.Fatal("invalid calendar date was accepted")
	}
	oldDaySummary, err := svc.Query(AuditFilter{Day: now.AddDate(0, 0, -35).Format("2006-01-02")})
	if err != nil {
		t.Fatal(err)
	}
	if oldDaySummary.Connections != 0 || oldDaySummary.DailyTotal != 0 || len(oldDaySummary.Days) != 0 || len(oldDaySummary.Events) != 0 {
		t.Fatalf("expired UTC day returned data: %+v", oldDaySummary)
	}

	if err := svc.Prune(); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Query(AuditFilter{Hours: 720})
	if err != nil {
		t.Fatal(err)
	}
	if got.Connections != 7 || got.Uplink != 63 || got.Downlink != 89 || got.Measured != 6 {
		t.Fatalf("repeated prune changed totals: %+v", got)
	}

	// Add another batch from the same UTC date/key to verify conflict upserts add.
	second := model.AuditEvent{ID: "same-day-later", NodeID: 7, Time: history.Add(time.Hour), Inbound: "client-in", Client: "alice", Site: "example.com", Uplink: auditMetric(13), Downlink: auditMetric(19)}
	if err := db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.Prune(); err != nil {
		t.Fatal(err)
	}
	got, err = svc.Query(AuditFilter{Hours: 720, NodeID: 7, Client: "alice", Site: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Connections != 6 || got.Uplink != 64 || got.Downlink != 94 || got.Measured != 5 {
		t.Fatalf("same-day archive changed overall metrics: %+v, want 6/64/94/5", got)
	}
	foundArchivedDay := false
	for _, row := range got.Days {
		if row.Day == history.Format("2006-01-02") {
			foundArchivedDay = row.Connections == 4 && row.Uplink == 53 && row.Downlink == 79 && row.Measured == 3
		}
	}
	if !foundArchivedDay {
		t.Fatalf("same-day archive did not accumulate in its daily row: %+v", got.Days)
	}
}
