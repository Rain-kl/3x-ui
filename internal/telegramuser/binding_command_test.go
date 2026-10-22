package telegramuser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mymmrac/telego"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestBoundUserCallingBindCommandDirectlyAcknowledged(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "bind_cmd.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	client := model.ClientRecord{Email: "bound-client", SubID: "bound-sub"}
	if err := database.GetDB().Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	cfg := model.TelegramUserConfig{Id: 1, BotUsername: "test_bot"}
	if err := database.GetDB().Create(&cfg).Error; err != nil {
		t.Fatal(err)
	}

	boundTgID := int64(99900002)
	binding := model.TelegramBinding{ClientID: client.Id, TgID: boundTgID}
	if err := database.GetDB().Create(&binding).Error; err != nil {
		t.Fatal(err)
	}

	var replies []string
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		replies = append(replies, body.Text)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":42,"chat":{"id":99900002,"type":"private"}}}`))
	}))
	defer apiServer.Close()

	api, err := newBotAPI("test", Network{APIServer: apiServer.URL})
	if err != nil {
		t.Fatal(err)
	}
	rec := receiver{ctx: context.Background(), cfg: cfg, user: api, await: map[int64]awaiting{}}

	handleMsg := func(id int, tgID int64, text string) {
		t.Helper()
		err := rec.handle(telego.Update{
			UpdateID: id,
			Message: &telego.Message{
				MessageID: id,
				From:      &telego.User{ID: tgID},
				Chat:      telego.Chat{ID: tgID, Type: "private"},
				Text:      text,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	// 1. Bound user sends bare /bind
	handleMsg(1, boundTgID, "/bind")
	if len(replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(replies))
	}
	if !strings.Contains(replies[0], "你已经绑定成功") {
		t.Fatalf("expected already bound reminder, got: %q", replies[0])
	}
	if _, waiting := rec.await[boundTgID]; waiting {
		t.Fatal("bound user should not be placed into awaiting state")
	}

	// 2. Bound user sends /bind with arguments
	handleMsg(2, boundTgID, "/bind bind_somecode")
	if len(replies) != 2 {
		t.Fatalf("expected 2 replies, got %d", len(replies))
	}
	if !strings.Contains(replies[1], "你已经绑定成功") {
		t.Fatalf("expected already bound reminder for /bind with args, got: %q", replies[1])
	}

	// 3. Unbound user sends bare /bind
	unboundTgID := int64(99900003)
	handleMsg(3, unboundTgID, "/bind")
	if len(replies) != 3 {
		t.Fatalf("expected 3 replies, got %d", len(replies))
	}
	if !strings.Contains(replies[2], "请发送节点订阅链接或绑定码") {
		t.Fatalf("expected prompt to send code/link for unbound user, got: %q", replies[2])
	}
	if state, waiting := rec.await[unboundTgID]; !waiting || state.action != "bind" {
		t.Fatalf("unbound user should be in awaiting state, got: %+v (waiting=%v)", state, waiting)
	}
}
