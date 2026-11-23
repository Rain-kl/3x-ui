package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestTelegramMessageDeleteAuthorization(t *testing.T) {
	node := startContractNode(t)
	_ = node.masterWithToken(t, model.ApiScopeAdmin)
	_ = node.masterWithToken(t, model.ApiScopeNodeSync)

	db := database.GetDB()
	const tgID int64 = 265758444
	if err := db.Create(&model.TelegramConversation{TgID: tgID, Name: "Castaway", LastMessageAt: 2000}).Error; err != nil {
		t.Fatal(err)
	}
	msg1 := model.TelegramMessage{TgID: tgID, Direction: "incoming", Kind: "text", Text: "one", Role: "chat", DeliveryBot: "user", Status: "received", CreatedAt: 1000}
	msg2 := model.TelegramMessage{TgID: tgID, Direction: "incoming", Kind: "text", Text: "two", Role: "chat", DeliveryBot: "user", Status: "received", CreatedAt: 2000}
	if err := db.Create(&msg1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&msg2).Error; err != nil {
		t.Fatal(err)
	}

	post := func(path, scope string) (int, bool) {
		req, err := http.NewRequest(http.MethodPost, node.srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if scope != "" {
			req.Header.Set("Authorization", "Bearer contract-"+scope)
		}
		resp, err := node.srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var body struct {
			Success bool `json:"success"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return resp.StatusCode, body.Success
	}

	for _, scope := range []string{"", model.ApiScopeNodeSync} {
		if status, ok := post("/panel/api/telegram/messages/1/delete", scope); ok || status == http.StatusOK && ok {
			t.Fatalf("unauthorized single message delete: scope=%q status=%d ok=%v", scope, status, ok)
		}
		if status, ok := post("/panel/api/telegram/conversations/265758444/delete", scope); ok || status == http.StatusOK && ok {
			t.Fatalf("unauthorized conversation delete: scope=%q status=%d ok=%v", scope, status, ok)
		}
	}

	if status, ok := post("/panel/api/telegram/messages/"+jsonNumber(msg1.Id)+"/delete", model.ApiScopeAdmin); status != http.StatusOK || !ok {
		t.Fatalf("admin single message delete failed: status=%d ok=%v", status, ok)
	}
	if status, ok := post("/panel/api/telegram/conversations/265758444/delete", model.ApiScopeAdmin); status != http.StatusOK || !ok {
		t.Fatalf("admin conversation delete failed: status=%d ok=%v", status, ok)
	}
	var remaining int64
	db.Model(&model.TelegramConversation{}).Where("tg_id = ?", tgID).Count(&remaining)
	if remaining != 0 {
		t.Fatalf("expected conversation deleted, remaining=%d", remaining)
	}
}

func jsonNumber(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}
