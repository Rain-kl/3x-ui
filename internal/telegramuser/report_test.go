package telegramuser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestAdminReportUnicodeAndDeliveryErrors(t *testing.T) {
	var mu sync.Mutex
	var chunks []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ChatID    int64  `json:"chat_id"`
			Text      string `json:"text"`
			ParseMode string `json:"parse_mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		if body.ChatID == 2 {
			fmt.Fprint(w, `{"ok":false,"error_code":403,"description":"fixture blocked recipient"}`)
			return
		}
		if body.ChatID != 1 || body.ParseMode != "" {
			t.Errorf("unexpected destination or formatting: %+v", body)
		}
		if len([]rune(body.Text)) > 3500 {
			t.Errorf("oversized Telegram chunk")
		}
		mu.Lock()
		chunks = append(chunks, body.Text)
		mu.Unlock()
		fmt.Fprint(w, `{"ok":true,"result":{}}`)
	}))
	defer server.Close()
	text := strings.Repeat("中<&", 1500)
	err := SendAdminReport(context.Background(), Network{AdminToken: "test-token", AdminIDs: []int64{1, 2}, APIServer: server.URL}, text)
	if err == nil || !strings.Contains(err.Error(), "administrator chat 2") || !strings.Contains(err.Error(), "fixture blocked recipient") {
		t.Fatalf("delivery error=%v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(chunks) != 2 || strings.Join(chunks, "") != text {
		t.Fatalf("Unicode report was changed: chunks=%d", len(chunks))
	}
}
