package database

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestModelRateLimitAndTrafficRatioFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_fields.db")
	if err := InitDB(dbPath); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = CloseDB() })

	db := GetDB()
	ib := &model.Inbound{
		Remark:       "test-ratio",
		Port:         54321,
		Protocol:     model.VLESS,
		TrafficRatio: 1.5,
	}
	if err := db.Create(ib).Error; err != nil {
		t.Fatalf("Create Inbound failed: %v", err)
	}

	cr := &model.ClientRecord{
		Email:     "user@test.com",
		DownLimit: 100,
	}
	if err := db.Create(cr).Error; err != nil {
		t.Fatalf("Create ClientRecord failed: %v", err)
	}

	ci := &model.ClientInbound{
		ClientId:  cr.Id,
		InboundId: ib.Id,
		DownLimit: 50,
	}
	if err := db.Create(ci).Error; err != nil {
		t.Fatalf("Create ClientInbound failed: %v", err)
	}

	var loadedIb model.Inbound
	if err := db.First(&loadedIb, ib.Id).Error; err != nil || loadedIb.TrafficRatio != 1.5 {
		t.Fatalf("TrafficRatio = %v, want 1.5", loadedIb.TrafficRatio)
	}

	var loadedCi model.ClientInbound
	if err := db.Where("client_id = ? AND inbound_id = ?", cr.Id, ib.Id).First(&loadedCi).Error; err != nil || loadedCi.DownLimit != 50 {
		t.Fatalf("ClientInbound.DownLimit = %v, want 50", loadedCi.DownLimit)
	}
}

func TestInboundTrafficRatioMigrationBackfill(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_ratio_migration.db")
	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Open raw sqlite failed: %v", err)
	}

	createTableSQL := `CREATE TABLE inbounds (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER,
		up INTEGER,
		down INTEGER,
		total INTEGER,
		remark TEXT,
		enable NUMERIC,
		port INTEGER,
		protocol TEXT,
		settings TEXT,
		stream_settings TEXT,
		tag TEXT UNIQUE,
		sniffing TEXT,
		traffic_ratio REAL DEFAULT 0.0
	);`
	if _, err := rawDB.Exec(createTableSQL); err != nil {
		_ = rawDB.Close()
		t.Fatalf("Create legacy table failed: %v", err)
	}
	if _, err := rawDB.Exec(`INSERT INTO inbounds (remark, port, protocol, tag, enable, traffic_ratio) VALUES ('legacy-zero', 8081, 'vless', 'in-legacy-zero', 1, 0.0)`); err != nil {
		_ = rawDB.Close()
		t.Fatalf("Insert legacy zero ratio inbound failed: %v", err)
	}
	if _, err := rawDB.Exec(`INSERT INTO inbounds (remark, port, protocol, tag, enable, traffic_ratio) VALUES ('legacy-custom', 8082, 'vless', 'in-legacy-custom', 1, 2.5)`); err != nil {
		_ = rawDB.Close()
		t.Fatalf("Insert legacy custom ratio inbound failed: %v", err)
	}
	_ = rawDB.Close()

	if err := InitDB(dbPath); err != nil {
		t.Fatalf("InitDB failed on legacy db: %v", err)
	}
	t.Cleanup(func() { _ = CloseDB() })

	var zeroIb model.Inbound
	if err := GetDB().Where("tag = ?", "in-legacy-zero").First(&zeroIb).Error; err != nil {
		t.Fatalf("Query legacy-zero inbound failed: %v", err)
	}
	if zeroIb.TrafficRatio != 1.0 {
		t.Fatalf("Expected backfilled TrafficRatio = 1.0, got %v", zeroIb.TrafficRatio)
	}

	var customIb model.Inbound
	if err := GetDB().Where("tag = ?", "in-legacy-custom").First(&customIb).Error; err != nil {
		t.Fatalf("Query legacy-custom inbound failed: %v", err)
	}
	if customIb.TrafficRatio != 2.5 {
		t.Fatalf("Expected preserved TrafficRatio = 2.5, got %v", customIb.TrafficRatio)
	}
}

func TestClientModelDownLimitFieldsAndConversion(t *testing.T) {
	c := &model.Client{
		Email:              "test@example.com",
		DownLimit:          100,
		DownLimitByInbound: map[int]int{1: 50, 2: 80},
	}
	if c.DownLimit != 100 {
		t.Fatalf("Client.DownLimit = %d, want 100", c.DownLimit)
	}
	if c.DownLimitByInbound[1] != 50 || c.DownLimitByInbound[2] != 80 {
		t.Fatalf("Client.DownLimitByInbound mismatch: %v", c.DownLimitByInbound)
	}

	rec := c.ToRecord()
	if rec.DownLimit != 100 {
		t.Fatalf("rec.DownLimit = %d, want 100", rec.DownLimit)
	}

	c2 := rec.ToClient()
	if c2.DownLimit != 100 {
		t.Fatalf("c2.DownLimit = %d, want 100", c2.DownLimit)
	}
}
