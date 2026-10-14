package controller

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func TestRemoteRoutingSaveImmediatelyAppliesToRunningCore(t *testing.T) {
	binary := os.Getenv("XRAY_E2E_BINARY")
	if binary == "" {
		t.Skip("set XRAY_E2E_BINARY to the custom Xray binary for live route verification")
	}
	dir := t.TempDir()
	t.Setenv("XUI_BIN_FOLDER", dir)
	t.Setenv("XUI_LOG_FOLDER", dir)
	if err := os.Symlink(binary, filepath.Join(dir, xray.GetBinaryName())); err != nil {
		t.Fatal(err)
	}
	if err := database.InitDB(filepath.Join(dir, "panel.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	t.Cleanup(service.SetXrayProcessForTest(nil))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	apiPort := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	template := map[string]any{
		"log":       map[string]any{"access": "", "error": "none", "loglevel": "warning"},
		"api":       map[string]any{"tag": "api", "services": []string{"HandlerService", "StatsService", "RoutingService"}},
		"stats":     map[string]any{},
		"inbounds":  []any{map[string]any{"tag": "api", "listen": "127.0.0.1", "port": apiPort, "protocol": "dokodemo-door", "settings": map[string]any{"address": "127.0.0.1"}}},
		"outbounds": []any{map[string]any{"tag": "direct", "protocol": "freedom"}, map[string]any{"tag": "blocked", "protocol": "blackhole"}},
		"routing":   map[string]any{"domainStrategy": "AsIs", "rules": []any{map[string]any{"type": "field", "domain": []string{"domain:example.com"}, "outboundTag": "direct"}}},
	}
	raw, _ := json.Marshal(template)
	if err := (&service.XraySettingService{}).SaveXraySetting(string(raw)); err != nil {
		t.Fatal(err)
	}
	config, err := (&service.XrayService{}).GetXrayConfig()
	if err != nil {
		t.Fatal(err)
	}
	process := xray.NewTestProcess(config, filepath.Join(dir, "config.json"))
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if current := service.XrayProcess(); current != nil {
			_ = current.Stop()
		}
		_ = process.Stop()
	})
	_ = service.SetXrayProcessForTest(process)
	route := func() (string, error) {
		api := &xray.XrayAPI{}
		if err := api.Init(apiPort); err != nil {
			return "", err
		}
		defer api.Close()
		result, err := api.TestRoute(xray.RouteTestRequest{Domain: "example.com", Port: 443, Network: "tcp"})
		if err != nil {
			return "", err
		}
		return result.OutboundTag, nil
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		tag, err := route()
		if err == nil && tag == "direct" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("initial routing unavailable: tag=%s err=%v", tag, err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	controller := &ServerController{}
	engine.POST("/panel/api/server/routing", controller.setRouting)
	child := httptest.NewServer(engine)
	defer child.Close()
	target, _ := url.Parse(child.URL)
	port, _ := strconv.Atoi(target.Port())
	remote := runtime.NewRemote(&model.Node{Name: "child", Scheme: "http", Address: target.Hostname(), Port: port, BasePath: "/", ApiToken: "token", AllowPrivateAddress: true}, nil)
	rules := []map[string]any{{"type": "field", "domain": []string{"domain:example.com"}, "outboundTag": "blocked"}}
	if err := remote.PushRoutingRules(context.Background(), rules); err != nil {
		t.Fatal(err)
	}
	tag, err := route()
	if err != nil || tag != "blocked" {
		t.Fatalf("saved remote rule not immediately active: tag=%s err=%v", tag, err)
	}
	if service.XrayProcess() != process || !process.IsRunning() {
		t.Fatal("routing-only save restarted the core instead of hot applying")
	}
	// Deliberately make runtime config stale, then submit the already-saved rules.
	api := &xray.XrayAPI{}
	if err := api.Init(apiPort); err != nil {
		t.Fatal(err)
	}
	var staleRouting map[string]any
	if err := json.Unmarshal(process.GetConfig().RouterConfig, &staleRouting); err != nil {
		t.Fatal(err)
	}
	staleRouting["rules"] = []map[string]any{
		{"type": "field", "inboundTag": []string{"api"}, "outboundTag": "api"},
		{"type": "field", "domain": []string{"domain:example.com"}, "outboundTag": "direct"},
	}
	stale, _ := json.Marshal(staleRouting)
	if err := api.ApplyRoutingConfig(stale); err != nil {
		api.Close()
		t.Fatal(err)
	}
	api.Close()
	// Process's cached config must represent the stale runtime too, as after an earlier failed save.
	old := *process.GetConfig()
	old.RouterConfig = stale
	process.SetConfig(&old)
	if err := remote.PushRoutingRules(context.Background(), rules); err != nil {
		t.Fatal(err)
	}
	if tag, err := route(); err != nil || tag != "blocked" {
		t.Fatalf("unchanged desired rule not reconciled: %s %v", tag, err)
	}
	// A pending listener conflict must be visible to the master, including on retry.
	saved, err := (&service.SettingService{}).GetXrayConfigTemplate()
	if err != nil {
		t.Fatal(err)
	}
	var desired map[string]any
	json.Unmarshal([]byte(saved), &desired)
	inbounds := desired["inbounds"].([]any)
	desired["inbounds"] = append(inbounds, map[string]any{"tag": "collision", "listen": "127.0.0.1", "port": apiPort, "protocol": "dokodemo-door", "settings": map[string]any{"address": "127.0.0.1"}})
	raw, _ = json.Marshal(desired)
	if err := database.GetDB().Model(&model.Setting{}).Where("key = ?", "xrayTemplateConfig").Update("value", string(raw)).Error; err != nil {
		t.Fatal(err)
	}
	rules[0]["domain"] = []string{"domain:example.net"}
	for range 2 {
		err := remote.PushRoutingRules(context.Background(), rules)
		if err == nil || !strings.Contains(err.Error(), "applying to running Xray failed") {
			t.Fatalf("runtime failure masked: %v", err)
		}
	}
}

func TestRemoteRoutingSaveKeepsStoppedCoreStopped(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "panel.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	t.Cleanup(service.SetXrayProcessForTest(nil))
	if err := (&service.XraySettingService{}).SaveXraySetting(`{"outbounds":[],"routing":{"rules":[]}}`); err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	controller := &ServerController{}
	engine.POST("/routing", controller.setRouting)
	req := httptest.NewRequest(http.MethodPost, "/routing", strings.NewReader(`[{"type":"field","domain":["domain:example.com"],"outboundTag":"blocked"}]`))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, req)
	if !strings.Contains(response.Body.String(), `"success":true`) {
		t.Fatal(response.Body.String())
	}
	if service.XrayProcess() != nil || (&service.XrayService{}).IsXrayRunning() {
		t.Fatal("saving rules started a stopped core")
	}
}
