package sub

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestMixedOnlySubscriptionManualCredentials(t *testing.T) {
	seedSubDB(t)
	ib := seedSubInbound(t, "mixed-sub", "mixed", 10801, 1, `{"network":"tcp","security":"none"}`)
	db := database.GetDB()
	if err := db.Model(ib).Updates(map[string]any{"protocol": model.Mixed, "settings": `{"auth":"password","clients":[]}`, "listen": "2001:db8::1"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ClientRecord{}).Where("sub_id = ?", "mixed-sub").Updates(map[string]any{"username": "user@example.test", "password": "p@ss/:word"}).Error; err != nil {
		t.Fatal(err)
	}
	req := NewSubService("").ForRequest("sub.example.test")
	links, _, _, traffic, err := req.getSubs("mixed-sub")
	if err != nil || links == nil || len(links) != 0 || !traffic.Enable {
		t.Fatalf("mixed-only page rejected: links=%v enabled=%t err=%v", links, traffic.Enable, err)
	}
	nodes, err := req.manualMixedNodes("mixed-sub")
	if err != nil || len(nodes) != 1 {
		t.Fatalf("manual mixed: len=%d err=%v", len(nodes), err)
	}
	u, err := url.Parse(nodes[0].HTTP)
	if err != nil {
		t.Fatal(err)
	}
	pass, _ := u.User.Password()
	if u.Host != "[2001:db8::1]:10801" || u.User.Username() != "user@example.test" || pass != "p@ss/:word" {
		t.Fatalf("invalid credential URL: %s", nodes[0].HTTP)
	}
	if got, err := req.manualMixedNodes("someone-else"); err != nil || len(got) != 0 {
		t.Fatalf("leaked other subscriber: %v %v", got, err)
	}
	for _, kind := range []string{"json", "clash"} {
		var body string
		if kind == "json" {
			body, _, err = NewSubJsonService("", "", "", "", req).GetJson("mixed-sub", "sub.example.test", false)
		} else {
			body, _, err = NewSubClashService(false, "", req).GetClash("mixed-sub", "sub.example.test")
		}
		if err != nil || strings.Contains(body, "2001:db8::1") || strings.Contains(body, "p@ss") || strings.Contains(body, "10801") {
			t.Fatalf("%s contains mixed proxy or failed: %q %v", kind, body, err)
		}
	}
	for _, updates := range []map[string]any{{"enable": false}, {"enable": true, "expiry_time": time.Now().Add(-time.Hour).UnixMilli()}} {
		if err := db.Model(&model.ClientRecord{}).Where("sub_id = ?", "mixed-sub").Updates(updates).Error; err != nil {
			t.Fatal(err)
		}
		if got, err := req.manualMixedNodes("mixed-sub"); err != nil || len(got) != 0 {
			t.Fatalf("inactive credentials exposed: %d %v", len(got), err)
		}
	}
}
