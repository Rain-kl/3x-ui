package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/entity"
	"github.com/mhsanaei/3x-ui/v3/internal/web/locale"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

func TestGetXraySetting_NodeIdIncludesOutboundTags(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	runtime.SetManager(runtime.NewManager(runtime.LocalDeps{
		APIPort:        func() int { return 0 },
		SetNeedRestart: func() {},
	}))
	t.Cleanup(func() { runtime.SetManager(nil) })

	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "server/routing") && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"success":true,"obj":[{"type":"field","outboundTag":"worker-proxy"}]}`))
		case strings.HasSuffix(r.URL.Path, "server/outboundTags") && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"success":true,"obj":["direct","blocked","worker-proxy"]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer worker.Close()

	u, _ := url.Parse(worker.URL)
	port, _ := strconv.Atoi(u.Port())
	node := &model.Node{
		Name:                "worker",
		Scheme:              "http",
		Address:             u.Hostname(),
		Port:                port,
		BasePath:            "/",
		ApiToken:            "tok",
		Enable:              true,
		Status:              "online",
		AllowPrivateAddress: true,
	}
	if err := database.GetDB().Create(node).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("I18n", func(_ locale.I18nType, key string, _ ...string) string { return key })
		c.Next()
	})
	NewXraySettingController(engine.Group("/panel/api"))

	form := url.Values{"nodeId": {strconv.Itoa(node.Id)}}
	req := httptest.NewRequest(http.MethodPost, "/panel/api/xray/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	var env entity.Msg
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v body=%s", err, w.Body.String())
	}
	if !env.Success {
		t.Fatalf("success=false msg=%s body=%s", env.Msg, w.Body.String())
	}
	objStr, ok := env.Obj.(string)
	if !ok {
		t.Fatalf("obj type %T, want string", env.Obj)
	}
	var payload struct {
		OutboundTags []string `json:"outboundTags"`
	}
	if err := json.Unmarshal([]byte(objStr), &payload); err != nil {
		t.Fatalf("decode payload: %v obj=%s", err, objStr)
	}
	want := []string{"direct", "blocked", "worker-proxy"}
	if strings.Join(payload.OutboundTags, ",") != strings.Join(want, ",") {
		t.Fatalf("outboundTags = %v, want %v", payload.OutboundTags, want)
	}
}

func TestGetXraySetting_LocalIncludesOutboundTags(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	template := `{"outbounds":[{"protocol":"freedom","tag":"direct"},{"protocol":"blackhole","tag":"blocked"}]}`
	if err := (&service.XraySettingService{}).SaveXraySetting(template); err != nil {
		t.Fatalf("save template: %v", err)
	}

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("I18n", func(_ locale.I18nType, key string, _ ...string) string { return key })
		c.Next()
	})
	NewXraySettingController(engine.Group("/panel/api"))

	req := httptest.NewRequest(http.MethodPost, "/panel/api/xray/", nil)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	var env entity.Msg
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v body=%s", err, w.Body.String())
	}
	if !env.Success {
		t.Fatalf("success=false msg=%s body=%s", env.Msg, w.Body.String())
	}
	objStr, ok := env.Obj.(string)
	if !ok {
		t.Fatalf("obj type %T, want string", env.Obj)
	}
	var payload struct {
		OutboundTags []string `json:"outboundTags"`
	}
	if err := json.Unmarshal([]byte(objStr), &payload); err != nil {
		t.Fatalf("decode payload: %v obj=%s", err, objStr)
	}
	want := []string{"direct", "blocked"}
	if strings.Join(payload.OutboundTags, ",") != strings.Join(want, ",") {
		t.Fatalf("outboundTags = %v, want %v", payload.OutboundTags, want)
	}
}
