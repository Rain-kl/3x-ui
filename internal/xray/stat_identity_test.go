package xray

import (
	"encoding/json"
	"testing"

	statsService "github.com/xtls/xray-core/app/stats/command"
)

func TestGetTrafficSeparatesScopedUsers(t *testing.T) {
	a, b := ScopedEmail("in-a", "alice"), ScopedEmail("in-b", "alice")
	api := startFakeStats(t, [][]*statsService.Stat{
		{stat("user>>>"+a+">>>traffic>>>uplink", 0), stat("user>>>"+b+">>>traffic>>>uplink", 0)},
		{stat("user>>>"+a+">>>traffic>>>uplink", 100), stat("user>>>"+b+">>>traffic>>>uplink", 300)},
	})
	if _, _, err := api.GetTraffic(); err != nil {
		t.Fatal(err)
	}
	_, rows, err := api.GetTraffic()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, row := range rows {
		if row.Email != "alice" {
			t.Fatalf("email=%q", row.Email)
		}
		got[row.InboundTag] = row.Up
	}
	if len(rows) != 2 || got["in-a"] != 100 || got["in-b"] != 300 {
		t.Fatalf("scoped traffic=%v", got)
	}
}

func TestScopedRoutingMatchesDynamicallyAddedClient(t *testing.T) {
	cfg := &Config{InboundConfigs: []InboundConfig{{Tag: "a"}, {Tag: "b"}}}
	cfg.RouterConfig = []byte(`{"rules":[{"type":"field","user":["future"],"outboundTag":"direct"}]}`)
	ScopeRoutingUsers(cfg)
	var routing struct {
		Rules []struct {
			User []string `json:"user"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(cfg.RouterConfig, &routing); err != nil {
		t.Fatal(err)
	}
	users := routing.Rules[0].User
	if len(users) != 3 || users[0] != "future" || users[1] != ScopedEmail("a", "future") || users[2] != ScopedEmail("b", "future") {
		t.Fatalf("routing users=%v", users)
	}
}
