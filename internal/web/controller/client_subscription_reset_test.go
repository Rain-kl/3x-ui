package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/crypto"
	"github.com/mhsanaei/3x-ui/v3/internal/web/entity"
	"github.com/mhsanaei/3x-ui/v3/internal/web/locale"
)

func TestClientResetSubscriptionHTTP(t *testing.T) {
	router, api := newAPIAuthTestEngine(t)
	group := router.Group("/base/panel/api/clients", api.checkAPIAuth, api.enforceTokenScope)
	group.Use(func(c *gin.Context) {
		c.Set("I18n", func(_ locale.I18nType, key string, _ ...string) string { return key })
		c.Next()
	})
	NewClientController(group)
	db := database.GetDB()
	before := model.ClientRecord{
		Email: "reset+device@example.com", SubID: "old-subscription", UUID: "11111111-2222-3333-4444-555555555555",
		Enable: true, Group: "staff", TotalGB: 12 << 30, ExpiryTime: 1893456000000,
	}
	if err := db.Create(&before).Error; err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{model.ApiScopeAdmin, model.ApiScopeMonitor, model.ApiScopeNodeSync, model.ApiScopeNodeAdmin} {
		token := model.ApiToken{Name: scope, Token: crypto.HashTokenSHA256(scope), Enabled: true, Scope: scope}
		if err := db.Create(&token).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		scope string
		code  int
	}{
		{"", http.StatusUnauthorized},
		{model.ApiScopeMonitor, http.StatusForbidden},
		{model.ApiScopeNodeSync, http.StatusForbidden},
		{model.ApiScopeNodeAdmin, http.StatusForbidden},
		{model.ApiScopeAdmin, http.StatusOK},
	} {
		t.Run("scope="+tc.scope, func(t *testing.T) {
			path := "/base/panel/api/clients/resetSubscription?email=" + url.QueryEscape(before.Email)
			req := httptest.NewRequest(http.MethodPost, path, nil)
			req.Header.Set("X-Requested-With", "XMLHttpRequest")
			if tc.scope != "" {
				req.Header.Set("Authorization", "Bearer "+tc.scope)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tc.code {
				t.Fatalf("status = %d, want %d, body = %s", w.Code, tc.code, w.Body.String())
			}
			var after model.ClientRecord
			if err := db.First(&after, before.Id).Error; err != nil {
				t.Fatal(err)
			}
			if tc.code != http.StatusOK {
				if after.SubID != before.SubID || after.UUID != before.UUID {
					t.Fatal("denied request changed credentials")
				}
				return
			}
			var response entity.Msg
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if !response.Success || response.Obj != nil || after.SubID == "" || after.SubID == before.SubID || after.UUID == "" || after.UUID == before.UUID {
				t.Fatalf("request did not rotate both credentials: %s", w.Body.String())
			}
			if after.Email != before.Email || after.Enable != before.Enable || after.Group != before.Group || after.TotalGB != before.TotalGB || after.ExpiryTime != before.ExpiryTime {
				t.Fatalf("reset changed client settings: %+v", after)
			}
		})
	}
}
