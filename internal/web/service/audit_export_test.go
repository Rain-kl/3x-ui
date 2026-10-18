package service

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestAuditExportSnapshotAndClientScope(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "audit.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	db := database.GetDB()
	up, down := int64(10), int64(20)
	now := time.Now().UTC().Add(-time.Minute)
	for _, row := range []model.AuditEvent{
		{ID: "alice", Time: now, Inbound: "in", Client: "alice", Site: "example.com", Uplink: &up, Downlink: &down},
		{ID: "bob", Time: now, Inbound: "in", Client: "bob", Site: "example.com", Uplink: &up, Downlink: &down},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	archived := model.AuditDailyStat{Day: now.Add(-48 * time.Hour).Format("2006-01-02"), Client: "alice", Site: "example.com", Connections: 3, Uplink: 40, Downlink: 50, Measured: 3}
	if err := db.Create(&archived).Error; err != nil {
		t.Fatal(err)
	}
	read := func(kind, client string) map[string][]byte {
		t.Helper()
		f, err := BuildAuditExport(context.Background(), kind, client)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
		info, err := f.Stat()
		if err != nil {
			t.Fatal(err)
		}
		zr, err := zip.NewReader(f, info.Size())
		if err != nil {
			t.Fatal(err)
		}
		result := map[string][]byte{}
		for _, entry := range zr.File {
			r, err := entry.Open()
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(r)
			_ = r.Close()
			if err != nil {
				t.Fatal(err)
			}
			result[entry.Name] = data
		}
		return result
	}
	for _, tc := range []struct{ kind, client, want string }{{"sites", "", "example.com,5,60,90,150,5"}, {"client", "alice", "example.com,4,50,70,120,4"}} {
		data := read(tc.kind, tc.client)
		records, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data["sites.csv"]), "\xef\xbb\xbf"))).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		if len(records) != 2 || strings.Join(records[1], ",") != tc.want {
			t.Fatalf("%s got %v want %s", tc.kind, records, tc.want)
		}
	}
	data := read("database", "")
	if len(data) != 6 {
		t.Fatalf("archive entries = %d, want 6", len(data))
	}
	if !strings.Contains(string(data["audit_events.jsonl"]), `"client":"bob"`) {
		t.Fatalf("raw export missing bob: %s", data["audit_events.jsonl"])
	}
	var manifest map[string]any
	if err := json.Unmarshal(data["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest["kind"] != "database" {
		t.Fatalf("wrong manifest %v", manifest)
	}
	if _, err := BuildAuditExport(context.Background(), "client", ""); err == nil {
		t.Fatal("empty client exported everyone")
	}
}
