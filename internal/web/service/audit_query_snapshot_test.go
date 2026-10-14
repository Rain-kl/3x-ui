package service

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm/logger"

	"github.com/mhsanaei/3x-ui/v3/internal/audit"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
)

type auditReadGate struct {
	logger.Interface
	once    sync.Once
	started chan struct{}
	release chan struct{}
}

func (g *auditReadGate) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	query, _ := fc()
	if strings.HasPrefix(query, "SELECT COALESCE(SUM(connections)") {
		g.once.Do(func() { close(g.started); <-g.release })
	}
	g.Interface.Trace(ctx, begin, fc, err)
}

func TestAuditQuerySnapshotAllowsWritesAndKeepsMetricsConsistent(t *testing.T) {
	if database.IsPostgres() {
		t.Skip("requires SQLite WAL test fixture")
	}
	db := initTrafficTestDB(t)
	svc := AuditService{}
	event := audit.Event{ID: "one", Time: time.Now().UTC(), Inbound: "in", Client: "alice", Destination: "example.com"}
	if err := svc.Ingest(7, []audit.Event{event}); err != nil {
		t.Fatal(err)
	}
	gate := &auditReadGate{Interface: db.Logger, started: make(chan struct{}), release: make(chan struct{})}
	original := db.Logger
	db.Logger = gate
	var result *AuditSummary
	queryDone := make(chan error, 1)
	go func() { var err error; result, err = svc.Query(AuditFilter{Hours: 24}); queryDone <- err }()
	select {
	case <-gate.started:
	case <-time.After(3 * time.Second):
		close(gate.release)
		<-queryDone
		db.Logger = original
		t.Fatal("query did not enter snapshot")
	}
	writeDone := make(chan error, 1)
	event.ID = "two"
	go func() { writeDone <- svc.Ingest(7, []audit.Event{event}) }()
	var writeErr error
	select {
	case writeErr = <-writeDone:
	case <-time.After(3 * time.Second):
		close(gate.release)
		<-queryDone
		<-writeDone
		db.Logger = original
		t.Fatal("analytical snapshot blocked source ingestion")
	}
	close(gate.release)
	queryErr := <-queryDone
	db.Logger = original
	if writeErr != nil || queryErr != nil {
		t.Fatalf("write=%v query=%v", writeErr, queryErr)
	}
	if result.Connections != 1 || result.Total != 1 || len(result.Events) != 1 {
		t.Fatalf("mixed snapshot: %+v", result)
	}
	next, err := svc.Query(AuditFilter{Hours: 24})
	if err != nil || next.Connections != 2 {
		t.Fatalf("next snapshot=%+v err=%v", next, err)
	}
}

func TestAuditReadSnapshotRollbackReleasesConnection(t *testing.T) {
	initTrafficTestDB(t)
	if _, err := (AuditService{}).Query(AuditFilter{Day: "invalid"}); err == nil {
		t.Fatal("invalid date accepted")
	}
	if err := (AuditService{}).Ingest(0, []audit.Event{{ID: "after-failure", Time: time.Now().UTC(), Destination: "example.com"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := (AuditService{}).Query(AuditFilter{Hours: 24}); err != nil {
		t.Fatal("snapshot connection left in transaction:", err)
	}
}
