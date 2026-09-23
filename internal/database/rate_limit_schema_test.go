package database

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestInboundRateLimitSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_rate_limit.db")
	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = CloseDB() })

	inbound := &model.Inbound{
		Remark:           "test-rate-limit",
		Port:             18443,
		Protocol:         model.VLESS,
		InboundDownLimit: 100,
		ClientDownLimit:  10,
		Tag:              "in-test-rl",
		Enable:           true,
	}

	if err := GetDB().Create(inbound).Error; err != nil {
		t.Fatalf("Create inbound with rate limits failed: %v", err)
	}

	var loaded model.Inbound
	if err := GetDB().First(&loaded, inbound.Id).Error; err != nil {
		t.Fatalf("Query inbound failed: %v", err)
	}

	if loaded.InboundDownLimit != 100 || loaded.ClientDownLimit != 10 {
		t.Fatalf("Unexpected rate limits: got inbound=%d client=%d, want 100 and 10",
			loaded.InboundDownLimit, loaded.ClientDownLimit)
	}
}

func TestInboundRateLimitMigration(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_migration.db")
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
		sniffing TEXT
	);`
	if _, err := rawDB.Exec(createTableSQL); err != nil {
		_ = rawDB.Close()
		t.Fatalf("Create legacy table failed: %v", err)
	}
	if _, err := rawDB.Exec(`INSERT INTO inbounds (remark, port, protocol, tag, enable) VALUES ('legacy', 8080, 'vless', 'in-legacy', 1)`); err != nil {
		_ = rawDB.Close()
		t.Fatalf("Insert legacy inbound failed: %v", err)
	}
	_ = rawDB.Close()

	if err := InitDB(dbPath); err != nil {
		t.Fatalf("InitDB failed on legacy db: %v", err)
	}
	t.Cleanup(func() { _ = CloseDB() })

	var loaded model.Inbound
	if err := GetDB().Where("tag = ?", "in-legacy").First(&loaded).Error; err != nil {
		t.Fatalf("Query migrated inbound failed: %v", err)
	}
	if loaded.InboundDownLimit != 0 || loaded.ClientDownLimit != 0 {
		t.Fatalf("Expected default 0 for rate limits, got inbound=%d client=%d",
			loaded.InboundDownLimit, loaded.ClientDownLimit)
	}

	loaded.InboundDownLimit = 50
	loaded.ClientDownLimit = 5
	if err := GetDB().Save(&loaded).Error; err != nil {
		t.Fatalf("Save migrated inbound failed: %v", err)
	}

	var updated model.Inbound
	if err := GetDB().First(&updated, loaded.Id).Error; err != nil {
		t.Fatalf("Query updated inbound failed: %v", err)
	}
	if updated.InboundDownLimit != 50 || updated.ClientDownLimit != 5 {
		t.Fatalf("Unexpected updated rate limits: got inbound=%d client=%d",
			updated.InboundDownLimit, updated.ClientDownLimit)
	}
}
