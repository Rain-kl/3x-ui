package database

import (
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestClientInboundTrafficColumns(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "test-traffic.db")
	if err := InitDB(dbPath); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = CloseDB() })

	ci := &model.ClientInbound{
		ClientId:  1,
		InboundId: 10,
		TotalGB:   50 * 1024 * 1024 * 1024,
		Up:        1024,
		Down:      2048,
	}
	if err := GetDB().Create(ci).Error; err != nil {
		t.Fatalf("Create ClientInbound failed: %v", err)
	}

	var found model.ClientInbound
	if err := GetDB().Where("client_id = ? AND inbound_id = ?", 1, 10).First(&found).Error; err != nil {
		t.Fatalf("Query ClientInbound failed: %v", err)
	}
	if found.TotalGB != 50*1024*1024*1024 {
		t.Errorf("TotalGB = %d, want %d", found.TotalGB, 50*1024*1024*1024)
	}
	if found.Up != 1024 || found.Down != 2048 {
		t.Errorf("Up/Down = %d/%d, want 1024/2048", found.Up, found.Down)
	}
}

func TestMigrateClientInboundTrafficColumns(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "test-migration.db")
	if err := InitDB(dbPath); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = CloseDB() })

	migrator := GetDB().Migrator()
	if !migrator.HasColumn(&model.ClientInbound{}, "total_gb") {
		t.Error("expected total_gb column in client_inbounds table")
	}
	if !migrator.HasColumn(&model.ClientInbound{}, "up") {
		t.Error("expected up column in client_inbounds table")
	}
	if !migrator.HasColumn(&model.ClientInbound{}, "down") {
		t.Error("expected down column in client_inbounds table")
	}
}

func TestMigrateClientInboundTrafficColumns_ExistingTable(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "test-existing.db")
	if err := InitDB(dbPath); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = CloseDB() })

	// Simulate pre-migration schema by dropping the new columns.
	migrator := GetDB().Migrator()
	if err := migrator.DropColumn(&model.ClientInbound{}, "total_gb"); err != nil {
		t.Fatalf("DropColumn total_gb failed: %v", err)
	}
	if err := migrator.DropColumn(&model.ClientInbound{}, "up"); err != nil {
		t.Fatalf("DropColumn up failed: %v", err)
	}
	if err := migrator.DropColumn(&model.ClientInbound{}, "down"); err != nil {
		t.Fatalf("DropColumn down failed: %v", err)
	}

	if err := migrateClientInboundTrafficColumns(); err != nil {
		t.Fatalf("migrateClientInboundTrafficColumns failed: %v", err)
	}

	if !migrator.HasColumn(&model.ClientInbound{}, "total_gb") {
		t.Error("expected total_gb column after migration")
	}
	if !migrator.HasColumn(&model.ClientInbound{}, "up") {
		t.Error("expected up column after migration")
	}
	if !migrator.HasColumn(&model.ClientInbound{}, "down") {
		t.Error("expected down column after migration")
	}
}

func TestClientStructInboundTrafficFields(t *testing.T) {
	c := model.Client{
		Email:   "test@example.com",
		TotalGB: 100 * 1024 * 1024 * 1024,
		TotalGBByInbound: map[int]int64{
			1: 10 * 1024 * 1024 * 1024,
			2: 20 * 1024 * 1024 * 1024,
		},
		InboundTraffics: map[int]model.ClientInboundTraffic{
			1: {
				InboundID: 1,
				Up:        500,
				Down:      1500,
				Total:     10 * 1024 * 1024 * 1024,
				Used:      2000,
				Remained:  10*1024*1024*1024 - 2000,
				Depleted:  false,
			},
		},
	}

	if c.TotalGBByInbound[1] != 10*1024*1024*1024 {
		t.Errorf("TotalGBByInbound[1] = %d, want %d", c.TotalGBByInbound[1], 10*1024*1024*1024)
	}
	if tr, ok := c.InboundTraffics[1]; !ok || tr.Used != 2000 || tr.Depleted {
		t.Errorf("InboundTraffics[1] unexpected: %+v", tr)
	}
}
