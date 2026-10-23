package telegramuser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mymmrac/telego"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestReissuedCodeBindsThroughStartDeepLink(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "deeplink.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	client := model.ClientRecord{Email: "deeplink-client", SubID: "deeplink-sub"}
	if err := database.GetDB().Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	cfg := model.TelegramUserConfig{Id: 1, BotUsername: "local_check_bot"}
	if err := database.GetDB().Create(&cfg).Error; err != nil {
		t.Fatal(err)
	}
	first, err := CreateCode(client.Id)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateCode(client.Id)
	if err != nil {
		t.Fatal(err)
	}
	link, err := url.Parse(second.URL)
	if err != nil {
		t.Fatal(err)
	}
	payload := link.Query().Get("start")
	if payload != second.Code || first.Code == second.Code {
		t.Fatal("reissued link did not carry the new code")
	}
	var replies []string
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		replies = append(replies, body.Text)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":42,"chat":{"id":99900001,"type":"private"}}}`))
	}))
	defer apiServer.Close()
	api, err := newBotAPI("test", Network{APIServer: apiServer.URL})
	if err != nil {
		t.Fatal(err)
	}
	receiver := receiver{ctx: context.Background(), cfg: cfg, user: api, await: map[int64]awaiting{}}
	receive := func(id int, text string) {
		t.Helper()
		err := receiver.handle(telego.Update{UpdateID: id, Message: &telego.Message{MessageID: id, From: &telego.User{ID: 99900001}, Chat: telego.Chat{ID: 99900001, Type: "private"}, Text: text}})
		if err != nil {
			t.Fatal(err)
		}
	}
	receive(1, "/start")
	if _, err := BoundClient(99900001); err == nil {
		t.Fatal("bare /start unexpectedly selected a client")
	}
	if len(replies) != 1 || !strings.Contains(replies[0], "/bind") {
		t.Fatal("bare start did not explain how to bind")
	}
	receive(2, "/start "+first.Code)
	if _, err := BoundClient(99900001); err == nil {
		t.Fatal("superseded code still bound the client")
	}
	receive(3, "/start "+payload)
	bound, err := BoundClient(99900001)
	if err != nil || bound.Id != client.Id {
		t.Fatalf("new /start deep link failed to bind expected client: %v", err)
	}
	if len(replies) != 3 || !strings.Contains(replies[2], "绑定成功") {
		t.Fatal("successful deep link did not return binding confirmation")
	}
}
