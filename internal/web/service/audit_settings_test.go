package service

import (
	"strconv"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/audit"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestAuditRetentionSettingsDefaultsAndPersistence(t *testing.T) {
	db := initTrafficTestDB(t)
	svc := AuditService{}

	got, err := svc.Retention()
	if err != nil {
		t.Fatal(err)
	}
	if got != (AuditRetention{DetailHours: 24, SummaryDays: 30}) {
		t.Fatalf("default retention = %+v, want detailHours=24 summaryDays=30", got)
	}

	want := AuditRetention{DetailHours: 48, SummaryDays: 12}
	if err := svc.SetRetention(want); err != nil {
		t.Fatal(err)
	}
	got, err = svc.Retention()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("saved retention = %+v, want %+v", got, want)
	}
	for key, value := range map[string]string{"auditDetailHours": "48", "auditSummaryDays": "12"} {
		var setting model.Setting
		if err := db.Where("key = ?", key).First(&setting).Error; err != nil {
			t.Fatalf("read persisted setting %q: %v", key, err)
		}
		if setting.Value != value || setting.Value != strconv.Itoa(map[string]int{"auditDetailHours": want.DetailHours, "auditSummaryDays": want.SummaryDays}[key]) {
			t.Errorf("persisted %s = %q, want %q", key, setting.Value, value)
		}
	}
}

func TestSetAuditRetentionPrunesImmediatelyAndPreservesTotals(t *testing.T) {
	db := initTrafficTestDB(t)
	svc := AuditService{}
	now := time.Now().UTC()
	events := []model.AuditEvent{
		{ID: "fresh", NodeID: 7, Time: now.Add(-time.Hour), Inbound: "client-in", Client: "alice", Site: "example.com", Uplink: auditMetric(5), Downlink: auditMetric(6)},
		{ID: "compact-today", NodeID: 7, Time: now.Add(-3 * time.Hour), Inbound: "client-in", Client: "alice", Site: "example.com", Uplink: auditMetric(10), Downlink: auditMetric(20)},
		{ID: "compact-yesterday", NodeID: 7, Time: now.Add(-25 * time.Hour), Inbound: "client-in", Client: "alice", Site: "example.com", Uplink: auditMetric(30), Downlink: auditMetric(40)},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatal(err)
	}
	stale := model.AuditDailyStat{Day: now.AddDate(0, 0, -4).Format("2006-01-02"), NodeID: 7, Client: "alice", Site: "example.com", Connections: 99, Uplink: 99, Downlink: 99, Measured: 99}
	if err := db.Create(&stale).Error; err != nil {
		t.Fatal(err)
	}

	if err := svc.SetRetention(AuditRetention{DetailHours: 2, SummaryDays: 3}); err != nil {
		t.Fatal(err)
	}
	var details []model.AuditEvent
	if err := db.Order("id").Find(&details).Error; err != nil {
		t.Fatal(err)
	}
	if len(details) != 1 || details[0].ID != "fresh" {
		t.Fatalf("details after SetRetention = %+v, want only fresh", details)
	}
	var staleCount int64
	if err := db.Model(&model.AuditDailyStat{}).Where("day = ?", stale.Day).Count(&staleCount).Error; err != nil {
		t.Fatal(err)
	}
	if staleCount != 0 {
		t.Fatalf("expired summary day %s remains after SetRetention", stale.Day)
	}

	got, err := svc.Query(AuditFilter{Hours: 72, NodeID: 7, Client: "alice", Site: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Connections != 3 || got.Uplink != 45 || got.Downlink != 66 || got.Measured != 3 {
		t.Fatalf("metrics after immediate compaction = %+v, want 3/45/66/3", got)
	}
}

func TestSetAuditRetentionRejectsInvalidPolicyWithoutChangingDatabase(t *testing.T) {
	db := initTrafficTestDB(t)
	svc := AuditService{}
	if err := db.Create(&model.AuditEvent{ID: "untouched", NodeID: 7, Time: time.Now().UTC().Add(-48 * time.Hour), Inbound: "client-in", Client: "alice", Site: "example.com", Uplink: auditMetric(10), Downlink: auditMetric(20)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.SetRetention(AuditRetention{DetailHours: 24, SummaryDays: 30}); err != nil {
		t.Fatal(err)
	}
	var before []model.Setting
	if err := db.Order("key").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	var eventCount int64
	if err := db.Model(&model.AuditEvent{}).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}

	for _, policy := range []AuditRetention{
		{DetailHours: 0, SummaryDays: 30},
		{DetailHours: 721, SummaryDays: 31},
		{DetailHours: 24, SummaryDays: 0},
		{DetailHours: 24, SummaryDays: 366},
		{DetailHours: 49, SummaryDays: 2},
	} {
		if err := svc.SetRetention(policy); err == nil {
			t.Errorf("SetRetention(%+v) accepted invalid policy", policy)
		}
	}
	var after []model.Setting
	if err := db.Order("key").Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("invalid policy changed settings row count from %d to %d", len(before), len(after))
	}
	for i := range before {
		if after[i].Key != before[i].Key || after[i].Value != before[i].Value {
			t.Fatalf("invalid policy changed settings: before=%+v after=%+v", before, after)
		}
	}
	var afterEvents int64
	if err := db.Model(&model.AuditEvent{}).Count(&afterEvents).Error; err != nil {
		t.Fatal(err)
	}
	if afterEvents != eventCount {
		t.Fatalf("invalid policy changed event count from %d to %d", eventCount, afterEvents)
	}
}

func TestSetAuditRetentionRollsBackPolicyAndArchiveOnPruneFailure(t *testing.T) {
	if database.IsPostgres() {
		t.Skip("test uses a SQLite trigger to force a prune failure")
	}
	db := initTrafficTestDB(t)
	svc := AuditService{}
	oldPolicy, err := svc.Retention()
	if err != nil {
		t.Fatal(err)
	}
	oldEvent := model.AuditEvent{ID: "rollback-event", NodeID: 7, Time: time.Now().UTC().Add(-3 * time.Hour), Inbound: "client-in", Client: "alice", Site: "example.com", Uplink: auditMetric(31), Downlink: auditMetric(37)}
	if err := db.Create(&oldEvent).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TRIGGER fail_audit_event_delete BEFORE DELETE ON audit_events BEGIN SELECT RAISE(ABORT, 'forced prune failure'); END").Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.SetRetention(AuditRetention{DetailHours: 2, SummaryDays: 3}); err == nil {
		t.Fatal("SetRetention succeeded despite the forced prune failure")
	}
	currentPolicy, err := svc.Retention()
	if err != nil {
		t.Fatal(err)
	}
	if currentPolicy != oldPolicy {
		t.Fatalf("retention policy changed after failed transaction: got %+v, want %+v", currentPolicy, oldPolicy)
	}
	var eventCount int64
	if err := db.Model(&model.AuditEvent{}).Where("id = ?", oldEvent.ID).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("archive failure deleted the source detail row: count=%d", eventCount)
	}
	var summaryCount int64
	if err := db.Model(&model.AuditDailyStat{}).Count(&summaryCount).Error; err != nil {
		t.Fatal(err)
	}
	if summaryCount != 0 {
		t.Fatalf("archive from failed transaction remained in daily stats: count=%d", summaryCount)
	}
}

func TestAuditClearOlderThanKeepsCutoffDateAndFreshRows(t *testing.T) {
	db := initTrafficTestDB(t)
	svc := AuditService{}
	now := time.Now().UTC()
	cutoffDay := now.AddDate(0, 0, -2).Format("2006-01-02")
	rows := []model.AuditEvent{
		{ID: "old", NodeID: 7, Time: now.AddDate(0, 0, -3), Inbound: "client-in", Client: "alice"},
		{ID: "cutoff-date", NodeID: 7, Time: now.AddDate(0, 0, -2), Inbound: "client-in", Client: "alice"},
		{ID: "fresh", NodeID: 7, Time: now.Add(-time.Hour), Inbound: "client-in", Client: "alice"},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	stats := []model.AuditDailyStat{
		{Day: now.AddDate(0, 0, -3).Format("2006-01-02"), NodeID: 7, Client: "alice", Site: "old", Connections: 1},
		{Day: cutoffDay, NodeID: 7, Client: "alice", Site: "cutoff", Connections: 2},
		{Day: now.Format("2006-01-02"), NodeID: 7, Client: "alice", Site: "fresh", Connections: 3},
	}
	if err := db.Create(&stats).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.Clear(2); err != nil {
		t.Fatal(err)
	}
	var remainingEvents []model.AuditEvent
	if err := db.Order("id").Find(&remainingEvents).Error; err != nil {
		t.Fatal(err)
	}
	if len(remainingEvents) != 2 || remainingEvents[0].ID != "cutoff-date" || remainingEvents[1].ID != "fresh" {
		t.Fatalf("events after Clear(2) = %+v, want cutoff date and fresh event", remainingEvents)
	}
	var remainingStats []model.AuditDailyStat
	if err := db.Order("day").Find(&remainingStats).Error; err != nil {
		t.Fatal(err)
	}
	if len(remainingStats) != 2 || remainingStats[0].Day != cutoffDay {
		t.Fatalf("summaries after Clear(2) = %+v, want cutoff date and current date", remainingStats)
	}
}

func TestAuditClearZeroClearsDetailsAndSummaries(t *testing.T) {
	db := initTrafficTestDB(t)
	svc := AuditService{}
	if err := db.Create(&model.AuditEvent{ID: "event", NodeID: 7, Time: time.Now().UTC(), Inbound: "client-in", Client: "alice"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AuditDailyStat{Day: time.Now().UTC().Format("2006-01-02"), NodeID: 7, Client: "alice", Site: "example.com", Connections: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.Clear(0); err != nil {
		t.Fatal(err)
	}
	for _, modelValue := range []any{&model.AuditEvent{}, &model.AuditDailyStat{}} {
		var count int64
		if err := db.Model(modelValue).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("Clear(0) left %d rows in %T", count, modelValue)
		}
	}
}

func TestAuditClearRejectsNegativeDays(t *testing.T) {
	initTrafficTestDB(t)
	if err := (AuditService{}).Clear(-1); err == nil {
		t.Fatal("Clear(-1) accepted a negative cutoff")
	}
}

func TestAuditClearBlocksDelayedEventsAfterClear(t *testing.T) {
	db := initTrafficTestDB(t)
	svc := AuditService{}
	now := time.Now().UTC()
	if err := db.Create(&model.AuditEvent{ID: "cleared-event", NodeID: 7, Time: now.Add(-time.Hour), Inbound: "client-in", Client: "alice", Site: "example.com"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AuditDailyStat{Day: now.Format("2006-01-02"), NodeID: 7, Client: "alice", Site: "example.com", Connections: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AuditReceipt{ID: "receipt", NodeID: 7, Time: now.Add(-time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.Clear(0); err != nil {
		t.Fatal(err)
	}
	policy, err := svc.Retention()
	if err != nil {
		t.Fatal(err)
	}
	if policy.ClearBefore.IsZero() {
		t.Fatal("Clear(0) did not persist a clear-before watermark")
	}

	late := audit.Event{
		ID: "late-after-clear", Time: policy.ClearBefore.Add(-time.Second), Inbound: "client-in",
		Client: "alice", Destination: "tcp:late.example.com:443", Uplink: auditMetric(10), Downlink: auditMetric(20),
	}
	if err := svc.Ingest(7, []audit.Event{late}); err != nil {
		t.Fatal(err)
	}
	for _, modelValue := range []any{&model.AuditEvent{}, &model.AuditDailyStat{}, &model.AuditReceipt{}} {
		var count int64
		if err := db.Model(modelValue).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("late event revived cleared data: %d rows in %T", count, modelValue)
		}
	}
}

func TestSetAuditRetentionAcceptsSupportedBoundaries(t *testing.T) {
	svc := AuditService{}
	for _, policy := range []AuditRetention{
		{DetailHours: 1, SummaryDays: 1},
		{DetailHours: 720, SummaryDays: 30},
		{DetailHours: 720, SummaryDays: 365},
	} {
		t.Run(strconv.Itoa(policy.DetailHours)+"h_"+strconv.Itoa(policy.SummaryDays)+"d", func(t *testing.T) {
			initTrafficTestDB(t)
			if err := svc.SetRetention(policy); err != nil {
				t.Fatalf("SetRetention(%+v): %v", policy, err)
			}
			got, err := svc.Retention()
			if err != nil {
				t.Fatal(err)
			}
			if got.DetailHours != policy.DetailHours || got.SummaryDays != policy.SummaryDays {
				t.Fatalf("Retention() = %+v, want detailHours=%d summaryDays=%d", got, policy.DetailHours, policy.SummaryDays)
			}
		})
	}
}

func TestAuditCleanupOperationsAreRejectedOnChildWithoutDatabaseChanges(t *testing.T) {
	t.Setenv("XUI_AUDIT_CHILD", "")
	for _, tc := range []struct {
		name string
		call func(AuditService) error
	}{
		{
			name: "set retention",
			call: func(svc AuditService) error {
				return svc.SetRetention(AuditRetention{DetailHours: 1, SummaryDays: 30})
			},
		},
		{
			name: "clear all",
			call: func(svc AuditService) error { return svc.Clear(0) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := initTrafficTestDB(t)
			settings := []model.Setting{
				{Key: "auditChildMode", Value: "true"},
				{Key: "auditDetailHours", Value: "24"},
				{Key: "auditSummaryDays", Value: "30"},
				{Key: "auditClearBefore", Value: "2026-01-01T00:00:00Z"},
			}
			if err := db.Create(&settings).Error; err != nil {
				t.Fatal(err)
			}
			oldEvent := model.AuditEvent{ID: "child-history", NodeID: 0, Time: time.Now().UTC().Add(-72 * time.Hour), Inbound: "client-in", Client: "alice", Site: "example.com", Uplink: auditMetric(10), Downlink: auditMetric(20)}
			if err := db.Create(&oldEvent).Error; err != nil {
				t.Fatal(err)
			}
			stat := model.AuditDailyStat{Day: oldEvent.Time.Format("2006-01-02"), NodeID: 0, Client: "alice", Site: "example.com", Connections: 4, Uplink: 10, Downlink: 20, Measured: 1}
			if err := db.Create(&stat).Error; err != nil {
				t.Fatal(err)
			}
			receipt := model.AuditReceipt{ID: "child-receipt", NodeID: 0, Time: oldEvent.Time}
			if err := db.Create(&receipt).Error; err != nil {
				t.Fatal(err)
			}

			var settingsBefore []model.Setting
			if err := db.Order("key").Find(&settingsBefore).Error; err != nil {
				t.Fatal(err)
			}
			if err := tc.call(AuditService{}); err == nil {
				t.Fatal("cleanup operation succeeded on child panel")
			}

			var settingsAfter []model.Setting
			if err := db.Order("key").Find(&settingsAfter).Error; err != nil {
				t.Fatal(err)
			}
			if len(settingsAfter) != len(settingsBefore) {
				t.Fatalf("settings row count changed from %d to %d", len(settingsBefore), len(settingsAfter))
			}
			for i := range settingsBefore {
				if settingsAfter[i] != settingsBefore[i] {
					t.Fatalf("settings changed: before=%+v after=%+v", settingsBefore[i], settingsAfter[i])
				}
			}
			for _, row := range []struct {
				name  string
				model any
				want  int64
			}{
				{name: "audit events", model: &model.AuditEvent{}, want: 1},
				{name: "daily summaries", model: &model.AuditDailyStat{}, want: 1},
				{name: "receipts", model: &model.AuditReceipt{}, want: 1},
			} {
				var count int64
				if err := db.Model(row.model).Count(&count).Error; err != nil {
					t.Fatal(err)
				}
				if count != row.want {
					t.Errorf("%s count after rejected operation = %d, want %d", row.name, count, row.want)
				}
			}
		})
	}
}
