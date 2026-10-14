package audit

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSpoolCapturesMeasuredAndLegacyAccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	s := NewSpool(path)
	measured := "2026/10/13 01:02:03.123456 from tcp:192.0.2.1:4567 accepted tcp:192.0.2.10:443 [in >> direct] email: alice sniffed_domain=www.example.com uplink=1024 downlink=2048"
	legacy := "2026/10/13 01:02:04.123456 from tcp:192.0.2.2:4568 accepted tcp:old.example:443 [in >> direct] email: bob"
	if !s.Capture(measured) || !s.Capture(legacy) {
		t.Fatal("access lines were not captured")
	}
	events := s.Snapshot(100)
	if len(events) != 2 || events[0].Destination != "tcp:192.0.2.10:443" || events[0].SniffedDomain != "www.example.com" || events[0].Client != "alice" || events[0].Inbound != "in" || events[0].Uplink == nil || *events[0].Uplink != 1024 || events[1].Uplink != nil {
		t.Fatalf("unexpected events: %+v", events)
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	reloaded := NewSpool(path)
	if got := reloaded.Snapshot(100); len(got) != 2 || got[0].SniffedDomain != "www.example.com" {
		t.Fatalf("domain lost during spool reload: %+v", got)
	}
	if err := reloaded.Ack([]string{events[0].ID}); err != nil {
		t.Fatal(err)
	}
	if got := len(NewSpool(path).Snapshot(100)); got != 1 {
		t.Fatalf("ack left %d events, want 1", got)
	}
}

func TestOutputWriterKeepsSplitAccessLineOutOfFallback(t *testing.T) {
	spool := NewSpool(filepath.Join(t.TempDir(), "audit.jsonl"))
	var fallback bytes.Buffer
	writer := &OutputWriter{fallback: &fallback, spool: spool}
	line := "2026/10/13 01:02:03.123456 from tcp:192.0.2.1:4567 accepted tcp:example.com:443 [in >> direct] email: alice uplink=10 downlink=20\n"
	if _, err := writer.Write([]byte(line[:49])); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(line[49:])); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("core ready\n")); err != nil {
		t.Fatal(err)
	}
	if got := len(spool.Snapshot(10)); got != 1 {
		t.Fatalf("captured %d events, want 1", got)
	}
	if fallback.String() != "core ready\n" {
		t.Fatalf("unexpected fallback: %q", fallback.String())
	}
}

func TestOutputWriterForwardsOversizedLineAndRecovers(t *testing.T) {
	spool := NewSpool(filepath.Join(t.TempDir(), "audit.jsonl"))
	var fallback bytes.Buffer
	writer := &OutputWriter{fallback: &fallback, spool: spool}
	longLine := strings.Repeat("x", 70<<10) + "\n"
	accessLine := "2026/10/13 01:02:03.123456 from tcp:192.0.2.1:4567 accepted tcp:example.com:443 [in >> direct] email: alice uplink=10 downlink=20\n"
	for _, part := range []string{longLine[:66<<10], longLine[66<<10:] + accessLine} {
		if _, err := writer.Write([]byte(part)); err != nil {
			t.Fatal(err)
		}
	}
	if got := fallback.String(); got != longLine {
		t.Fatalf("oversized output was lost: got %d bytes, want %d", len(got), len(longLine))
	}
	if got := len(spool.Snapshot(10)); got != 1 {
		t.Fatalf("captured %d events after oversized line, want 1", got)
	}
}

func TestSpoolBoundsOfflineCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	s := NewSpool(path)
	line := "2026/10/13 01:02:03.123456 from tcp:192.0.2.1:4567 accepted tcp:" + strings.Repeat("a", 200) + ".example:443 [in >> direct] email: alice uplink=1 downlink=2"
	for range 35000 {
		s.Capture(line)
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if stat.Size() > MaxSpoolBytes {
		t.Fatalf("spool size %d exceeds %d", stat.Size(), MaxSpoolBytes)
	}
}

func TestSpoolPrunePersistsAndRejectsClearedEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spool.jsonl")
	spool := NewSpool(path)
	cutoff := time.Now().UTC().Add(-time.Minute)
	spool.Add(Event{ID: "old", Time: cutoff.Add(-time.Second), Destination: "old.example"})
	spool.Add(Event{ID: "fresh", Time: cutoff, Destination: "fresh.example"})
	if err := spool.Prune(time.Time{}, cutoff); err != nil {
		t.Fatal(err)
	}
	if spool.Add(Event{ID: "late", Time: cutoff.Add(-time.Second), Destination: "late.example"}) {
		t.Fatal("late cleared event was accepted")
	}
	rows := NewSpool(path).Snapshot(1000)
	if len(rows) != 1 || rows[0].ID != "fresh" {
		t.Fatalf("persistent cleanup = %+v", rows)
	}
}

func TestSpoolCaptureContinuesWhileDiskFlushIsSerialized(t *testing.T) {
	spool := NewSpool(filepath.Join(t.TempDir(), "spool.jsonl"))
	spool.flushMu.Lock()
	done := make(chan error, 1)
	go func() { done <- spool.Flush() }()
	added := make(chan bool, 1)
	go func() { added <- spool.Add(Event{ID: "new", Time: time.Now().UTC(), Destination: "example.com"}) }()
	select {
	case ok := <-added:
		if !ok {
			t.Error("capture failed")
		}
	case <-time.After(time.Second):
		t.Error("disk serialization blocked capture")
	}
	spool.flushMu.Unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(NewSpool(spool.path).Snapshot(1000)) != 1 {
		t.Fatal("concurrent event was lost")
	}
}
