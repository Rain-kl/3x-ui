package controller

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/crypto"
)

func TestAuditSyncRequiresAuthenticatedParentChannel(t *testing.T) {
	for _, tc := range []struct {
		name, scope string
		mtls        bool
		want        int
	}{
		{"session", "", false, 403}, {"monitor", model.ApiScopeMonitor, false, 403},
		{"node-sync", model.ApiScopeNodeSync, false, 200}, {"admin", model.ApiScopeAdmin, false, 200}, {"mtls", "", true, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine, auth := newAPIAuthTestEngine(t)
			group := engine.Group("/panel/api/audit", auth.checkAPIAuth, auth.enforceTokenScope)
			newAuditController(group)
			body := bytes.NewBufferString(`{"detailHours":48,"summaryDays":12,"revision":1,"clearBefore":"0001-01-01T00:00:00Z"}`)
			req := httptest.NewRequest(http.MethodPost, "/panel/api/audit/sync", body)
			req.Header.Set("Content-Type", "application/json")
			if tc.scope != "" {
				token := model.ApiToken{Name: "parent", Scope: tc.scope, Enabled: true, Token: crypto.HashTokenSHA256("audit-parent")}
				if err := database.GetDB().Create(&token).Error; err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Authorization", "Bearer audit-parent")
			} else if tc.mtls {
				req.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{{}}}}
			} else {
				login := httptest.NewRecorder()
				engine.ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/test-login", nil))
				for _, cookie := range login.Result().Cookies() {
					req.AddCookie(cookie)
				}
			}
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, req)
			if recorder.Code != tc.want {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if tc.want == 200 && !bytes.Contains(recorder.Body.Bytes(), []byte(`"success":true`)) {
				t.Fatalf("apply failed: %s", recorder.Body.String())
			}
		})
	}
}
