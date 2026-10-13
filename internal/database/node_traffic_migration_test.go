package database

import (
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestNodeTrafficInboundBaselineMigrationPreservesHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x-ui.db")
	legacy, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := legacy.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := legacy.Exec(`CREATE TABLE node_client_traffics (id INTEGER PRIMARY KEY, node_id INTEGER NOT NULL, email TEXT NOT NULL, up BIGINT, down BIGINT)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := legacy.Exec(`INSERT INTO node_client_traffics (id, node_id, email, up, down) VALUES (1, 2, 'legacy', 1234, 5678)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XUI_DB_FOLDER", filepath.Dir(path))
	if err := InitDB(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = CloseDB() })
	var row model.NodeClientTraffic
	if err := GetDB().First(&row, 1).Error; err != nil {
		t.Fatal(err)
	}
	if row.NodeId != 2 || row.Email != "legacy" || row.Up != 1234 || row.Down != 5678 || row.InboundTotals != "" {
		t.Fatalf("legacy baseline changed: %+v", row)
	}
	if !GetDB().Migrator().HasColumn(&model.NodeClientTraffic{}, "inbound_totals") {
		t.Fatal("per-inbound baseline column missing")
	}
}
