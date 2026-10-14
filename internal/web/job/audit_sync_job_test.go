package job

import (
	"encoding/json"
	"fmt"
	"github.com/mhsanaei/3x-ui/v3/internal/audit"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestAuditChildModeKeepsNodeSpoolSeparateFromStandalone(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "audit.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	t.Setenv("XUI_AUDIT_CHILD", "")
	if auditChildMode() {
		t.Fatal("standalone panel classified as child")
	}
	token := model.ApiToken{Name: "audit-parent", Scope: model.ApiScopeNodeSync, Enabled: true, ExpiresAt: time.Now().Add(time.Hour).Unix()}
	if err := database.GetDB().Create(&token).Error; err != nil {
		t.Fatal(err)
	}
	if !auditChildMode() {
		t.Fatal("panel with active parent credential classified as standalone")
	}
	if err := database.GetDB().Model(&token).Update("expires_at", time.Now().Add(-time.Hour).Unix()).Error; err != nil {
		t.Fatal(err)
	}
	if auditChildMode() {
		t.Fatal("expired parent credential still marks child")
	}
	t.Setenv("XUI_AUDIT_CHILD", "1")
	if !auditChildMode() {
		t.Fatal("explicit child role was ignored")
	}
}

func TestAuditSyncJobSendsPolicyBeforeExportAndACK(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprint("legacy=", legacy), func(t *testing.T) {
			if err := database.InitDB(filepath.Join(t.TempDir(), "audit.db")); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = database.CloseDB() })
			t.Setenv("XUI_AUDIT_CHILD", "")
			var paths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				if legacy && r.URL.Path == "/panel/api/audit/sync" {
					w.WriteHeader(404)
					return
				}
				if r.URL.Path == "/panel/api/audit/pending" {
					json.NewEncoder(w).Encode(map[string]any{"success": true, "obj": []audit.Event{{ID: "from-child", Time: time.Now().UTC(), Inbound: "in", Destination: "example.com"}}})
					return
				}
				_, _ = w.Write([]byte(`{"success":true}`))
			}))
			defer server.Close()
			host, portText, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
			port, _ := strconv.Atoi(portText)
			node := model.Node{Name: "edge", Scheme: "http", Address: host, Port: port, BasePath: "/", ApiToken: "tok", Enable: true, Status: "online", AllowPrivateAddress: true}
			if err := database.GetDB().Create(&node).Error; err != nil {
				t.Fatal(err)
			}
			previous := runtime.GetManager()
			runtime.SetManager(runtime.NewManager(runtime.LocalDeps{}))
			t.Cleanup(func() { runtime.SetManager(previous) })
			NewAuditSyncJob().Run()
			want := []string{"/panel/api/audit/sync", "/panel/api/audit/pending", "/panel/api/audit/ack"}
			if !reflect.DeepEqual(paths, want) {
				t.Fatalf("RPC order=%v want=%v", paths, want)
			}
			var count int64
			if err := database.GetDB().Model(&model.AuditEvent{}).Where("id = ? AND node_id = ? AND delivered = ?", "from-child", node.Id, true).Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("delivery count=%d err=%v", count, err)
			}

		})
	}
}

func TestAuditChildWithDisabledDownstreamKeepsPendingDetails(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "audit.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	t.Setenv("XUI_AUDIT_CHILD", "1")
	db := database.GetDB()
	node := model.Node{Name: "disabled", Enable: true}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&node).Update("enable", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AuditEvent{ID: "pending", NodeID: 0, Time: time.Now().UTC().Add(-48 * time.Hour), Destination: "example.com", Site: "example.com"}).Error; err != nil {
		t.Fatal(err)
	}
	NewAuditSyncJob().Run()
	var count int64
	if err := db.Model(&model.AuditEvent{}).Where("id = ?", "pending").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("child prematurely archived pending detail: count=%d err=%v", count, err)
	}
}
