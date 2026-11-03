package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

func TestCheckedRoutingRejectsStaleRemoteEditor(t *testing.T) {
	t.Setenv("XRAY_LOCATION_ASSET", t.TempDir())
	if err := database.InitDB(filepath.Join(t.TempDir(), "routing.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	t.Cleanup(service.SetXrayProcessForTest(nil))
	settings := &service.XraySettingService{}
	if err := settings.SaveXraySetting(`{"outbounds":[{"tag":"direct","protocol":"freedom"}],"routing":{"rules":[{"domain":["domain:local.example"],"outboundTag":"direct"}]}}`); err != nil {
		t.Fatal(err)
	}
	baseline, _ := settings.GetXrayConfigTemplate()
	rules, _ := service.GetRoutingRulesFromTemplate(baseline)
	rawRules, _ := json.Marshal(rules)
	call := func(expected string) bool {
		values := url.Values{"rules": {"[]"}, "expectedRules": {expected}}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/panel/api/server/routing/checked", strings.NewReader(values.Encode()))
		c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		(&ServerController{}).setRouting(c)
		var response struct {
			Success bool `json:"success"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		return response.Success
	}
	for _, expected := range []string{"", "[]"} {
		if call(expected) {
			t.Fatal("missing or stale baseline accepted")
		}
	}
	after, _ := settings.GetXrayConfigTemplate()
	if after != baseline {
		t.Fatal("rejected save modified local rules")
	}
	if !call(string(rawRules)) {
		t.Fatal("current baseline rejected")
	}
}
