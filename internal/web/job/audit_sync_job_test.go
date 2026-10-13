package job

import (
	"path/filepath"
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
