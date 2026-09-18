package controller

import (
	"encoding/json"
	"fmt"
	"io"
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
)

func setupNodeXrayEngine(t *testing.T, worker http.HandlerFunc) (*gin.Engine, *model.Node) {
	t.Helper()
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

	srv := httptest.NewServer(worker)
	t.Cleanup(srv.Close)

	u, _ := url.Parse(srv.URL)
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
	return engine, node
}

func TestGetXraySetting_NodeIdStripsCentralInboundTagPrefix(t *testing.T) {
	engine, node := setupNodeXrayEngine(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "server/routing") && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"success":true,"obj":[]}`))
		case strings.HasSuffix(r.URL.Path, "server/outboundTags") && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"success":true,"obj":[]}`))
		default:
			http.NotFound(w, r)
		}
	})

	prefixed := fmt.Sprintf("n%d-in-443-tcp", node.Id)
	ib := &model.Inbound{
		UserId: 1, NodeID: &node.Id, Tag: prefixed, Enable: true, Port: 443,
		Protocol: model.VLESS, Settings: `{"clients":[]}`,
	}
	if err := database.GetDB().Create(ib).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}

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
		InboundTags []string `json:"inboundTags"`
	}
	if err := json.Unmarshal([]byte(objStr), &payload); err != nil {
		t.Fatalf("decode payload: %v obj=%s", err, objStr)
	}
	if len(payload.InboundTags) != 1 || payload.InboundTags[0] != "in-443-tcp" {
		t.Fatalf("inboundTags = %v, want [in-443-tcp] (central prefix stripped)", payload.InboundTags)
	}
}

func TestUpdateSetting_NodeIdStripsCentralInboundTagPrefixOnPush(t *testing.T) {
	var posted []map[string]any
	engine, node := setupNodeXrayEngine(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "server/routing") && r.Method == http.MethodPost {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read body: %v", err)
				return
			}
			if err := json.Unmarshal(body, &posted); err != nil {
				t.Errorf("decode pushed rules: %v body=%s", err, body)
			}
			_, _ = w.Write([]byte(`{"success":true}`))
			return
		}
		http.NotFound(w, r)
	})

	prefixed := fmt.Sprintf("n%d-in-443-tcp", node.Id)
	setting := fmt.Sprintf(`{"routing":{"rules":[{"type":"field","inboundTag":[%q],"outboundTag":"direct"}]}}`, prefixed)
	form := url.Values{"nodeId": {strconv.Itoa(node.Id)}, "xraySetting": {setting}}
	req := httptest.NewRequest(http.MethodPost, "/panel/api/xray/update", strings.NewReader(form.Encode()))
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
	if len(posted) != 1 {
		t.Fatalf("pushed %d rules, want 1: %+v", len(posted), posted)
	}
	tags, _ := posted[0]["inboundTag"].([]any)
	if len(tags) != 1 || tags[0] != "in-443-tcp" {
		t.Fatalf("pushed inboundTag = %#v, want [in-443-tcp]", posted[0]["inboundTag"])
	}
}
