package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestTelegramAdministratorBindingAuthorization(t *testing.T) {
	node := startContractNode(t)
	_ = node.masterWithToken(t, model.ApiScopeAdmin)
	_ = node.masterWithToken(t, model.ApiScopeNodeSync)
	client := model.ClientRecord{Email: "manual-auth-client", SubID: "manual-auth-sub"}
	if err := database.GetDB().Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"clientId": client.Id, "tgId": 12345})
	for _, scope := range []string{"", model.ApiScopeNodeSync, model.ApiScopeAdmin} {
		req, err := http.NewRequest(http.MethodPost, node.srv.URL+"/panel/api/telegram/bindings", strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if scope != "" {
			req.Header.Set("Authorization", "Bearer contract-"+scope)
		}
		resp, err := node.srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Success bool `json:"success"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&result)
		_ = resp.Body.Close()
		var count int64
		if err := database.GetDB().Model(&model.TelegramBinding{}).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if scope == model.ApiScopeAdmin {
			if resp.StatusCode != http.StatusOK || decodeErr != nil || !result.Success || count != 1 {
				t.Fatalf("admin binding failed: status=%d result=%+v count=%d err=%v", resp.StatusCode, result, count, decodeErr)
			}
		} else if result.Success || count != 0 {
			t.Fatalf("unauthorized binding: scope=%q status=%d result=%+v count=%d", scope, resp.StatusCode, result, count)
		}
	}
}
