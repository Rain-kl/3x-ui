package controller

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/crypto"
	"github.com/mhsanaei/3x-ui/v3/internal/web/locale"
)

func newIndexTestEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	engine := gin.New()
	SetDistFS(fstest.MapFS{
		"dist/login.html": &fstest.MapFile{Data: []byte("<html>login</html>")},
	})
	store := cookie.NewStore([]byte("test-secret"))
	engine.Use(sessions.Sessions("3x-ui", store))
	engine.Use(func(c *gin.Context) {
		c.Set("base_path", "/")
		c.Set("I18n", func(_ locale.I18nType, key string, _ ...string) string { return key })
		c.Next()
	})
	NewIndexController(engine.Group("/"))
	return engine
}

func TestIndexControllerApiTokenAutoLogin(t *testing.T) {
	engine := newIndexTestEngine(t)
	db := database.GetDB()

	user := &model.User{
		Username: "admin",
		Password: "hashed-password",
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	rawToken := "valid-secret-token-abcdef123456"
	token := &model.ApiToken{
		Name:    "test-admin-token",
		Token:   crypto.HashTokenSHA256(rawToken),
		Enabled: true,
		Scope:   model.ApiScopeAdmin,
	}
	if err := db.Create(token).Error; err != nil {
		t.Fatalf("seed api token: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/?apiToken="+rawToken, nil)
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want 307; body=%s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/panel/" {
		t.Fatalf("redirect Location = %q, want /panel/", loc)
	}
	cookies := w.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "3x-ui" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("session cookie '3x-ui' was not set on successful auto-login")
	}

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/?apiToken=wrong-token", nil)
	engine.ServeHTTP(w2, req2)
	if w2.Code == http.StatusTemporaryRedirect {
		t.Fatalf("invalid token should not redirect, got %d", w2.Code)
	}

	monitorToken := &model.ApiToken{
		Name:    "monitor-token",
		Token:   crypto.HashTokenSHA256("monitor-secret-token"),
		Enabled: true,
		Scope:   model.ApiScopeMonitor,
	}
	if err := db.Create(monitorToken).Error; err != nil {
		t.Fatalf("seed monitor token: %v", err)
	}
	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/?apiToken=monitor-secret-token", nil)
	engine.ServeHTTP(w3, req3)
	if w3.Code == http.StatusTemporaryRedirect {
		t.Fatalf("monitor scope token must not grant panel session, got status %d", w3.Code)
	}

	setting := &model.Setting{
		Key:   "twoFactorEnable",
		Value: "true",
	}
	if err := db.Save(setting).Error; err != nil {
		t.Fatalf("enable 2fa: %v", err)
	}
	w4 := httptest.NewRecorder()
	req4 := httptest.NewRequest(http.MethodGet, "/?apiToken="+rawToken, nil)
	engine.ServeHTTP(w4, req4)
	if w4.Code == http.StatusTemporaryRedirect {
		t.Fatalf("admin token must not bypass 2FA when 2FA is enabled, got status %d", w4.Code)
	}
}
