package service

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/audit"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
)

func BenchmarkAuditDelayedBatch(b *testing.B) {
	b.Setenv("XUI_AUDIT_CHILD", "")
	if err := database.InitDB(filepath.Join(b.TempDir(), "audit.db")); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = database.CloseDB() })
	events := make([]audit.Event, 1000)
	for i := range events {
		events[i] = audit.Event{ID: fmt.Sprint(i), Time: time.Now().UTC().Add(-48 * time.Hour), Inbound: "in", Client: "alice", Destination: "example.com"}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		for i := range events {
			events[i].ID = fmt.Sprintf("%d-%d", n, i)
		}
		if err := (AuditService{}).Ingest(7, events); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAuditQuery20000(b *testing.B) {
	b.Setenv("XUI_AUDIT_CHILD", "")
	if err := database.InitDB(filepath.Join(b.TempDir(), "audit.db")); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = database.CloseDB() })
	for start := 0; start < 20000; start += 1000 {
		events := make([]audit.Event, 1000)
		for i := range events {
			n := start + i
			events[i] = audit.Event{ID: fmt.Sprint(n), Time: time.Now().UTC().Add(-time.Duration(n) * time.Second), Inbound: "in", Client: fmt.Sprintf("client-%d", n%100), Destination: fmt.Sprintf("site-%d.example", n%50)}
		}
		if err := (AuditService{}).Ingest(7, events); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if _, err := (AuditService{}).Query(AuditFilter{Hours: 24}); err != nil {
			b.Fatal(err)
		}
	}
}
