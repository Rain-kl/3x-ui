package controller

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/locale"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
)

func newNodeCredentialTestEngine(t *testing.T) *gin.Engine {
	t.Helper()
	prev := runtime.GetManager()
	mgr := runtime.NewManager(runtime.LocalDeps{APIPort: func() int { return 0 }, SetNeedRestart: func() {}})
	runtime.SetManager(mgr)
	t.Cleanup(func() { runtime.SetManager(prev) })
	gin.SetMode(gin.TestMode)
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("I18n", func(_ locale.I18nType, key string, _ ...string) string { return key })
		c.Next()
	})
	NewNodeController(engine.Group("/panel/api/nodes"))
	return engine
}

func TestNodeControllerResponsesDoNotLeakApiToken(t *testing.T) {
	engine := newNodeCredentialTestEngine(t)
	if err := database.GetDB().Create(&model.Node{
		Name:     "stored-node",
		Scheme:   "https",
		Address:  "example.com",
		Port:     2053,
		BasePath: "/",
		ApiToken: "stored-secret-token",
		Enable:   true,
	}).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}

	for _, path := range []string{"/panel/api/nodes/list", "/panel/api/nodes/get/1"} {
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", path, w.Code, w.Body.String())
		}
		body := w.Body.String()
		if strings.Contains(body, "stored-secret-token") || strings.Contains(body, "apiToken") {
			t.Fatalf("%s leaked api token: %s", path, body)
		}
		if !strings.Contains(body, `"hasApiToken":true`) {
			t.Fatalf("%s did not expose credential presence: %s", path, body)
		}
	}
}

func TestNodeControllerProbeReportsHeartbeatPersistenceFailure(t *testing.T) {
	engine := newNodeCredentialTestEngine(t)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"obj":{"cpu":1,"mem":{"current":1,"total":2},"xray":{"version":"1","state":"running"},"panelVersion":"v3.6.0","panelGuid":"guid","uptime":7,"netIO":{"up":3,"down":4}}}`))
	}))
	defer remote.Close()
	host, portString, err := net.SplitHostPort(strings.TrimPrefix(remote.URL, "http://"))
	if err != nil {
		t.Fatalf("split remote addr: %v", err)
	}
	port, err := strconv.Atoi(portString)
	if err != nil {
		t.Fatalf("parse remote port: %v", err)
	}
	node := &model.Node{Scheme: "http", Address: host, Port: port, BasePath: "/", Enable: true, AllowPrivateAddress: true}
	if err := database.GetDB().Create(node).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}

	db := database.GetDB()
	const callback = "test:fail_node_heartbeat_update"
	errInjected := errors.New("injected heartbeat persistence failure")
	if err := db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "nodes" {
			tx.AddError(errInjected)
		}
	}); err != nil {
		t.Fatalf("register update callback: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Update().Remove(callback); err != nil {
			t.Errorf("remove update callback: %v", err)
		}
	})

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/panel/api/nodes/probe/"+strconv.Itoa(node.Id), nil))
	if !strings.Contains(w.Body.String(), `"success":false`) {
		t.Fatalf("probe reported success despite heartbeat persistence failure: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), errInjected.Error()) {
		t.Fatalf("probe response omitted persistence error: %s", w.Body.String())
	}
}

func TestNodeControllerAddAcceptsTokenButReturnsView(t *testing.T) {
	engine := newNodeCredentialTestEngine(t)

	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/panel/api/server/status" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer input-secret-token" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"obj":{"cpu":1,"mem":{"current":1,"total":2},"xray":{"version":"1","state":"running"},"panelVersion":"v3.4.1","panelGuid":"guid","uptime":7,"netIO":{"up":3,"down":4}}}`))
	}))
	defer remote.Close()
	host, portString, err := net.SplitHostPort(strings.TrimPrefix(remote.URL, "http://"))
	if err != nil {
		t.Fatalf("split remote addr: %v", err)
	}
	port, err := strconv.Atoi(portString)
	if err != nil {
		t.Fatalf("parse remote port: %v", err)
	}

	payload := map[string]any{
		"name":                "added-node",
		"scheme":              "http",
		"address":             host,
		"port":                port,
		"basePath":            "/",
		"apiToken":            "input-secret-token",
		"enable":              true,
		"allowPrivateAddress": true,
	}
	raw, _ := json.Marshal(payload)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/panel/api/nodes/add", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("add status = %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, "input-secret-token") || strings.Contains(body, "apiToken") {
		t.Fatalf("add response leaked api token: %s", body)
	}
	if !strings.Contains(body, `"hasApiToken":true`) {
		t.Fatalf("add response did not expose credential presence: %s", body)
	}

	var stored model.Node
	if err := database.GetDB().Where("name = ?", "added-node").First(&stored).Error; err != nil {
		t.Fatalf("load stored node: %v", err)
	}
	if stored.ApiToken != "input-secret-token" {
		t.Fatalf("stored token = %q, want input-secret-token", stored.ApiToken)
	}
}

func TestNodeControllerUpdateBlankApiTokenKeepsStoredToken(t *testing.T) {
	engine := newNodeCredentialTestEngine(t)

	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/panel/api/server/status" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer stored-secret-token" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"obj":{"cpu":1,"mem":{"current":1,"total":2},"xray":{"version":"1","state":"running"},"panelVersion":"v3.4.1","panelGuid":"guid","uptime":7,"netIO":{"up":3,"down":4}}}`))
	}))
	defer remote.Close()
	host, portString, err := net.SplitHostPort(strings.TrimPrefix(remote.URL, "http://"))
	if err != nil {
		t.Fatalf("split remote addr: %v", err)
	}
	port, err := strconv.Atoi(portString)
	if err != nil {
		t.Fatalf("parse remote port: %v", err)
	}
	node := &model.Node{
		Name:                "stored-node",
		Scheme:              "http",
		Address:             host,
		Port:                port,
		BasePath:            "/",
		ApiToken:            "stored-secret-token",
		Enable:              true,
		AllowPrivateAddress: true,
	}
	if err := database.GetDB().Create(node).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}

	payload := map[string]any{
		"name":                "stored-node-renamed",
		"scheme":              "http",
		"address":             host,
		"port":                port,
		"basePath":            "/",
		"apiToken":            "",
		"enable":              true,
		"allowPrivateAddress": true,
	}
	raw, _ := json.Marshal(payload)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/panel/api/nodes/update/"+strconv.Itoa(node.Id), strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", w.Code, w.Body.String())
	}

	var stored model.Node
	if err := database.GetDB().Where("id = ?", node.Id).First(&stored).Error; err != nil {
		t.Fatalf("load stored node: %v", err)
	}
	if stored.ApiToken != "stored-secret-token" {
		t.Fatalf("blank update changed token to %q", stored.ApiToken)
	}
	if stored.Name != "stored-node-renamed" {
		t.Fatalf("stored name = %q, want stored-node-renamed", stored.Name)
	}
}

func TestNodeControllerLoginUrl(t *testing.T) {
	engine := newNodeCredentialTestEngine(t)
	db := database.GetDB()
	if err := db.Create(&model.Node{
		Name:     "target-node",
		Scheme:   "https",
		Address:  "target.example.com",
		Port:     2053,
		BasePath: "/custom/",
		ApiToken: "secret-token-123",
		Enable:   true,
	}).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/panel/api/nodes/loginUrl/1", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("loginUrl status = %d, body = %s", w.Code, w.Body.String())
	}
	expected := "https://target.example.com:2053/custom/?apiToken=secret-token-123"
	if !strings.Contains(w.Body.String(), expected) {
		t.Fatalf("loginUrl = %s, want to contain %s", w.Body.String(), expected)
	}

	// Test IPv6 address formatting
	if err := db.Create(&model.Node{
		Name:     "ipv6-node",
		Scheme:   "http",
		Address:  "2001:db8::1",
		Port:     8080,
		BasePath: "api",
		ApiToken: "token-ipv6",
		Enable:   true,
	}).Error; err != nil {
		t.Fatalf("seed ipv6 node: %v", err)
	}

	w2 := httptest.NewRecorder()
	engine.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/panel/api/nodes/loginUrl/2", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("loginUrl status = %d, body = %s", w2.Code, w2.Body.String())
	}
	expectedIpv6 := "http://[2001:db8::1]:8080/api?apiToken=token-ipv6"
	if !strings.Contains(w2.Body.String(), expectedIpv6) {
		t.Fatalf("loginUrl = %s, want to contain %s", w2.Body.String(), expectedIpv6)
	}
}

func TestNodeControllerRestart(t *testing.T) {
	fakeRestartCalled := false
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/setting/restartPanel") {
			fakeRestartCalled = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"msg":"restarted"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer fakeServer.Close()

	engine := newNodeCredentialTestEngine(t)
	db := database.GetDB()

	host, portStr, _ := net.SplitHostPort(strings.TrimPrefix(fakeServer.URL, "http://"))
	port, _ := strconv.Atoi(portStr)

	node := &model.Node{
		Name:                "restart-node",
		Scheme:              "http",
		Address:             host,
		Port:                port,
		BasePath:            "/",
		ApiToken:            "test-token",
		Enable:              true,
		Status:              "online",
		AllowPrivateAddress: true,
	}
	if err := db.Create(node).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/panel/api/nodes/restart/"+strconv.Itoa(node.Id), nil))
	if w.Code != http.StatusOK {
		t.Fatalf("restart status = %d, body = %s", w.Code, w.Body.String())
	}
	if !fakeRestartCalled {
		t.Fatal("expected remote restartPanel endpoint to be called")
	}

	// Offline node should fail
	nodeOffline := &model.Node{
		Name:     "offline-node",
		Scheme:   "http",
		Address:  host,
		Port:     port,
		BasePath: "/",
		ApiToken: "test-token",
		Enable:   true,
		Status:   "offline",
	}
	if err := db.Create(nodeOffline).Error; err != nil {
		t.Fatalf("seed offline node: %v", err)
	}
	w2 := httptest.NewRecorder()
	engine.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/panel/api/nodes/restart/"+strconv.Itoa(nodeOffline.Id), nil))
	var res struct {
		Success bool   `json:"success"`
		Msg     string `json:"msg"`
	}
	_ = json.Unmarshal(w2.Body.Bytes(), &res)
	if res.Success {
		t.Fatal("expected restart to fail for offline node")
	}
}
