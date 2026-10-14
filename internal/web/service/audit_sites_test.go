package service

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestAuditSitesPaginationAndDashboardOrdering(t *testing.T) {
	db := initTrafficTestDB(t)
	now := time.Now().UTC().Add(-time.Minute)
	rows := make([]model.AuditEvent, 120)
	for i := range rows {
		rows[i] = model.AuditEvent{ID: fmt.Sprint(i), NodeID: 7, Time: now, Inbound: "client-in", Client: "alice", Site: fmt.Sprintf("site-%03d.example", i), Uplink: auditMetric(1), Downlink: auditMetric(9)}
	}
	rows = append(rows, model.AuditEvent{ID: "control", NodeID: 7, Time: now, Inbound: "api", Client: "alice", Site: "control.example", Uplink: auditMetric(10000)})
	if err := db.CreateInBatches(rows, 200).Error; err != nil {
		t.Fatal(err)
	}
	svc := AuditService{}
	filter := AuditFilter{Hours: 24, NodeID: 7, Client: "alice"}
	for _, size := range []int{20, 50, 100} {
		seen := map[string]bool{}
		for page := 1; page <= (120+size-1)/size; page++ {
			result, err := svc.QuerySites(filter, page, size)
			if err != nil || result.Total != 120 || result.Page != page || result.PageSize != size {
				t.Fatalf("page=%d size=%d result=%+v err=%v", page, size, result, err)
			}
			want := min(size, 120-(page-1)*size)
			if len(result.Items) != want {
				t.Fatalf("page length=%d want=%d", len(result.Items), want)
			}
			for i, item := range result.Items {
				expected := fmt.Sprintf("site-%03d.example", (page-1)*size+i)
				if item.Name != expected || seen[item.Name] {
					t.Fatalf("unstable/duplicate site: %+v expected=%s", item, expected)
				}
				seen[item.Name] = true
			}
		}
		if len(seen) != 120 {
			t.Fatalf("incomplete ranking with size %d: %d", size, len(seen))
		}
	}
	dashboard, err := svc.Query(filter)
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.QuerySites(filter, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	for i, site := range dashboard.Sites {
		if site != first.Items[i] {
			t.Fatalf("dashboard differs at %d: %+v vs %+v", i, site, first.Items[i])
		}
	}
	beyond, err := svc.QuerySites(filter, 99, 20)
	if err != nil || beyond.Total != 120 || len(beyond.Items) != 0 {
		t.Fatalf("beyond last page: %+v %v", beyond, err)
	}
}

func TestAuditSitesHonorsNodeClientSiteTimeAndHistoricalFilters(t *testing.T) {
	db := initTrafficTestDB(t)
	now := time.Now().UTC()
	day := now.AddDate(0, 0, -3).Format("2006-01-02")
	rows := []model.AuditEvent{
		{ID: "local", NodeID: 0, Time: now.Add(-time.Minute), Inbound: "in", Client: "alice", Site: "local.example", Uplink: auditMetric(1)},
		{ID: "alice", NodeID: 7, Time: now.Add(-time.Minute), Inbound: "in", Client: "alice", Site: "shared.example", Uplink: auditMetric(3), Downlink: auditMetric(7)},
		{ID: "bob", NodeID: 7, Time: now.Add(-time.Minute), Inbound: "in", Client: "bob", Site: "shared.example", Uplink: auditMetric(5)},
		{ID: "other-node", NodeID: 8, Time: now.Add(-time.Minute), Inbound: "in", Client: "alice", Site: "other.example", Uplink: auditMetric(99)},
		{ID: "old", NodeID: 7, Time: now.Add(-5 * time.Hour), Inbound: "in", Client: "alice", Site: "older.example"},
		{ID: "future", NodeID: 7, Time: now.Add(time.Hour), Inbound: "in", Client: "alice", Site: "future.example"},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AuditDailyStat{Day: day, NodeID: 7, Client: "alice", Site: "history.example", Connections: 5, Uplink: 100, Downlink: 200, Measured: 4}).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		filter      AuditFilter
		total       int64
		site        string
		connections int64
		traffic     int64
	}{
		{AuditFilter{Hours: 3, NodeID: 7, Client: "alice", Site: "SHARED.EXAMPLE"}, 1, "shared.example", 1, 10},
		{AuditFilter{Hours: 3, NodeID: -1}, 1, "local.example", 1, 1},
		{AuditFilter{Hours: 3, NodeID: 7, Site: "shared.example"}, 1, "shared.example", 2, 15},
		{AuditFilter{Hours: 168, Day: day, NodeID: 7, Client: "alice"}, 1, "history.example", 5, 300},
	} {
		result, err := (AuditService{}).QuerySites(tc.filter, 1, 20)
		if err != nil || result.Total != tc.total || len(result.Items) != 1 {
			t.Fatalf("filter=%+v result=%+v err=%v", tc.filter, result, err)
		}
		item := result.Items[0]
		if item.Name != tc.site || item.Connections != tc.connections || item.Uplink+item.Downlink != tc.traffic {
			t.Fatalf("filtered metrics=%+v", item)
		}
	}
	long, err := (AuditService{}).QuerySites(AuditFilter{Hours: 168, NodeID: 7, Client: "alice"}, 1, 20)
	if err != nil || long.Total != 3 || long.Items[0].Name != "history.example" {
		t.Fatalf("history ranking=%+v err=%v", long, err)
	}
}

func TestAuditSitesValidationAndChildMode(t *testing.T) {
	initTrafficTestDB(t)
	for _, tc := range []struct {
		f          AuditFilter
		page, size int
	}{
		{AuditFilter{}, 0, 20},
		{AuditFilter{}, -1, 20},
		{AuditFilter{}, 1, 30},
		{AuditFilter{}, int(^uint(0) >> 1), 100},
		{AuditFilter{Day: "invalid"}, 1, 20},
		{AuditFilter{Client: strings.Repeat("x", 257)}, 1, 20},
	} {
		if _, err := (AuditService{}).QuerySites(tc.f, tc.page, tc.size); err == nil {
			t.Fatalf("invalid page accepted: %+v", tc)
		}
	}
	t.Setenv("XUI_AUDIT_CHILD", "1")
	result, err := (AuditService{}).QuerySites(AuditFilter{}, 1, 20)
	if err != nil || !result.Centralized || result.Total != 0 || len(result.Items) != 0 {
		t.Fatalf("child ranking=%+v err=%v", result, err)
	}
}
