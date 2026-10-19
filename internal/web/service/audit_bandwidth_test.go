package service

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestBandwidthIntervalsResetRetryAndCleanup(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "bandwidth.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	old := auditBandwidth
	auditBandwidth = newBandwidthCapture()
	t.Cleanup(func() { auditBandwidth = old })
	s := auditBandwidth
	start := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Minute)
	sample := func(up, down int64) []bandwidthCounter {
		return []bandwidthCounter{{ClientID: 1, InboundID: 1, CreatedAt: 1, Client: "alice", Up: up, Down: down}}
	}
	s.observe(7, start, sample(100000, 100000))
	s.observe(7, start.Add(5*time.Second), sample(105000, 110000))
	if got := s.nodes[7].Rates["alice"]; got != [2]float64{1000, 2000} {
		t.Fatalf("rate=%v want 1000/2000 B/s", got)
	}
	s.observe(7, start.Add(10*time.Second), sample(0, 0))
	if got := s.nodes[7].Rates["alice"]; got != [2]float64{} {
		t.Fatalf("reset generated spike: %v", got)
	}
	s.observe(7, start.Add(50*time.Second), sample(10000000, 10000000))
	if got := s.nodes[7].Rates["alice"]; got != [2]float64{} {
		t.Fatalf("gap generated spike: %v", got)
	}
	s.observe(7, start.Add(55*time.Second), sample(10005000, 10010000))
	for range 2 {
		if err := s.flush(start.Add(56 * time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	var rows []model.AuditBandwidthMinute
	if err := database.GetDB().Where("client = ?", "alice").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Uplink != 10000 || rows[0].Downlink != 20000 {
		t.Fatalf("retry duplicated bytes: %+v", rows)
	}
	history, err := (AuditService{}).QueryBandwidthHistory(AuditFilter{NodeID: 7, Client: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	var bytes float64
	for _, point := range history.Points {
		bytes += point.Upload * 60
	}
	if bytes != 10000 {
		t.Fatalf("history upload=%v want 10000", bytes)
	}
	other, err := (AuditService{}).QueryBandwidthHistory(AuditFilter{Client: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	for _, point := range other.Points {
		if point.Upload != 0 || point.Download != 0 {
			t.Fatal("client data leaked")
		}
	}
	if err := (AuditService{}).Clear(0); err != nil {
		t.Fatal(err)
	}
	if err := s.flush(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := database.GetDB().Model(&model.AuditBandwidthMinute{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("cleanup resurrected %d rows", count)
	}
}

func TestAuditBandwidthCapturesRawCounters(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "raw-bandwidth.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	old := auditBandwidth
	auditBandwidth = newBandwidthCapture()
	t.Cleanup(func() { auditBandwidth = old })
	db := database.GetDB()
	inbound := model.Inbound{Tag: "bandwidth-test", TrafficRatio: 10}
	client := model.ClientRecord{Email: "raw-client"}
	for _, value := range []any{&inbound, &client} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	link := model.ClientInbound{ClientId: client.Id, InboundId: inbound.Id, Up: 100000, Down: 200000}
	if err := db.Create(&link).Error; err != nil {
		t.Fatal(err)
	}
	captureAuditBandwidth(0)
	baseline := auditBandwidth.nodes[0]
	if len(baseline.Counters) != 1 {
		t.Fatalf("batch query returned %d counters", len(baseline.Counters))
	}
	baseline.At = time.Now().UTC().Add(-5 * time.Second)
	auditBandwidth.nodes[0] = baseline
	delete(auditBandwidth.attempts, 0)
	if err := db.Model(&model.ClientInbound{}).Where("client_id = ? AND inbound_id = ?", client.Id, inbound.Id).Updates(map[string]any{"up": 105000, "down": 210000}).Error; err != nil {
		t.Fatal(err)
	}
	captureAuditBandwidth(0)
	value, err := (AuditService{}).QueryBandwidth(AuditFilter{NodeID: -1, Client: client.Email})
	if err != nil {
		t.Fatal(err)
	}
	elapsed := auditBandwidth.nodes[0].At.Sub(baseline.At).Seconds()
	if elapsed <= 0 || elapsed > 30 {
		t.Fatalf("unexpected sampling interval: %v seconds", elapsed)
	}
	if !value.Fresh || math.Abs(value.Upload*elapsed-5000) > 0.001 || math.Abs(value.Download*elapsed-10000) > 0.001 {
		t.Fatalf("raw counters with 10x billing ratio over %v seconds: %+v; want 5000/10000 bytes", elapsed, value)
	}
	other, err := (AuditService{}).QueryBandwidth(AuditFilter{NodeID: 999, Client: client.Email})
	if err != nil || other.Fresh || other.Upload != 0 {
		t.Fatalf("node isolation failed: %+v %v", other, err)
	}
}
