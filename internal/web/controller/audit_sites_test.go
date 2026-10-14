package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/crypto"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

func TestAuditSitesEndpointPaginationDefaultsAndValidation(t *testing.T) {
	engine, auth := newAPIAuthTestEngine(t)
	newAuditController(engine.Group("/panel/api/audit", auth.checkAPIAuth, auth.enforceTokenScope))
	if err := database.GetDB().Create(&model.ApiToken{Name: "sites-admin", Token: crypto.HashTokenSHA256("sites-token"), Enabled: true, Scope: model.ApiScopeAdmin}).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		query   string
		success bool
		size    int
	}{
		{"", true, 20}, {"?page=2&pageSize=50", true, 50}, {"?pageSize=100", true, 100},
		{"?page=bad", false, 0}, {"?page=0", false, 0}, {"?pageSize=40", false, 0}, {"?day=bad", false, 0},
	} {
		req := httptest.NewRequest(http.MethodGet, "/panel/api/audit/sites"+tc.query, nil)
		req.Header.Set("Authorization", "Bearer sites-token")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		var result struct {
			Success bool                   `json:"success"`
			Obj     service.AuditSitesPage `json:"obj"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Success != tc.success || (tc.success && result.Obj.PageSize != tc.size) {
			t.Fatalf("query=%s response=%s", tc.query, response.Body.String())
		}
	}
}
