package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/routingsync"
)

func testBalancer(tag, selector string) map[string]any {
	return map[string]any{"tag": tag, "selector": []string{selector}, "strategy": map[string]any{"type": "roundRobin"}}
}

func TestRoutingSyncBalancerOnlySnapshotLifecycle(t *testing.T) {
	initTrafficTestDB(t)
	settings := &XraySettingService{}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(syncTemplate), &cfg); err != nil {
		t.Fatal(err)
	}
	routing := cfg["routing"].(map[string]any)
	balancer := testBalancer("pool", "direct")
	routing["balancers"] = []map[string]any{balancer}
	save := func(wantRevision int64, wantCount int) routingsync.Control {
		t.Helper()
		raw, _ := json.Marshal(cfg)
		if err := settings.SaveXraySetting(string(raw)); err != nil {
			t.Fatal(err)
		}
		var state routingsync.Control
		found, err := readRoutingValue(database.GetDB(), routingSyncDesiredKey, &state)
		if err != nil || !found || state.Source == "" || state.Revision != wantRevision || len(state.Rules) != 0 || state.Balancing == nil || len(state.Balancing.Balancers) != wantCount {
			t.Fatalf("snapshot=%+v err=%v", state, err)
		}
		return state
	}
	save(1, 1)
	save(1, 1)
	balancer["selector"] = []string{"blocked"}
	save(2, 1)
	cfg["observatory"] = map[string]any{"subjectSelector": []string{"blocked"}, "probeURL": "https://probe.example/", "probeInterval": "10s"}
	if state := save(3, 1); state.Balancing.Observatory["probeInterval"] != "10s" {
		t.Fatalf("lost observer: %+v", state.Balancing)
	}
	delete(cfg, "observatory")
	cfg["burstObservatory"] = map[string]any{"subjectSelector": []string{"blocked"}, "pingConfig": map[string]any{"interval": "30s"}}
	if state := save(4, 1); state.Balancing.Observatory != nil || state.Balancing.BurstObservatory == nil {
		t.Fatalf("lost observer transition: %+v", state.Balancing)
	}
	routing["balancers"] = []map[string]any{}
	delete(cfg, "burstObservatory")
	save(5, 0)
	save(5, 0)
}

func TestRoutingSyncBalancerMergeProtectionAndWithdrawal(t *testing.T) {
	initTrafficTestDB(t)
	t.Cleanup(SetXrayProcessForTest(nil))
	settings := &XraySettingService{}
	if err := settings.SaveXraySetting(syncTemplate); err != nil {
		t.Fatal(err)
	}
	primary := testBalancer("primary", "direct")
	primary["fallbackTag"] = "_bl_backup"
	state := routingsync.Control{Source: "master", Revision: 1, Rules: []map[string]any{}, Balancing: &routingsync.Balancing{
		Balancers:        []map[string]any{primary, testBalancer("backup", "blocked")},
		BurstObservatory: map[string]any{"subjectSelector": []string{"direct", "blocked"}, "pingConfig": map[string]any{"interval": "1m"}},
	}}
	for range 2 {
		if err := (RoutingSyncService{}).Apply(state); err != nil {
			t.Fatal(err)
		}
	}
	read := func() (string, *routingsync.Balancing) {
		t.Helper()
		raw, err := settings.GetXrayConfigTemplate()
		if err != nil {
			t.Fatal(err)
		}
		balance, err := balancingFromTemplate(raw)
		if err != nil {
			t.Fatal(err)
		}
		return raw, balance
	}
	raw, balance := read()
	if len(balance.Balancers) != 2 || balance.BurstObservatory == nil || !strings.Contains(raw, `"protocol": "loopback"`) {
		t.Fatalf("incomplete balancing: %s", raw)
	}
	rules := savedSyncRules(t)
	if len(rules) != 3 || !isApiRule(rules[0]) || rules[1]["balancerTag"] != "backup" || rules[2]["outboundTag"] != "direct" {
		t.Fatalf("lost control priority/local routing: %+v", rules)
	}
	if err := settings.SaveXraySetting(syncTemplate); err != nil {
		t.Fatal(err)
	}
	_, protected := read()
	if !reflect.DeepEqual(balance, protected) {
		t.Fatal("child local save overwrote parent balancing")
	}
	var desired routingsync.Control
	if found, err := readRoutingValue(database.GetDB(), routingSyncDesiredKey, &desired); err != nil || found {
		t.Fatalf("child rebroadcast managed balancing: %+v %v", desired, err)
	}
	state.Revision++
	state.Balancing = nil
	if err := (RoutingSyncService{}).Apply(state); err != nil {
		t.Fatal(err)
	}
	_, legacy := read()
	if !reflect.DeepEqual(balance, legacy) || len(savedSyncRules(t)) != 3 {
		t.Fatal("legacy snapshot removed managed balancing or fallback controls")
	}
	state.Revision++
	state.Balancing = &routingsync.Balancing{Balancers: []map[string]any{}}
	if err := (RoutingSyncService{}).Apply(state); err != nil {
		t.Fatal(err)
	}
	raw, balance = read()
	if len(balance.Balancers) != 0 || balance.BurstObservatory != nil || strings.Contains(raw, "_bl_backup") || len(savedSyncRules(t)) != 2 {
		t.Fatalf("withdrawal incomplete: %s", raw)
	}
}

func TestRoutingSyncBalancerInvalidSnapshotsAreAtomic(t *testing.T) {
	initTrafficTestDB(t)
	t.Cleanup(SetXrayProcessForTest(nil))
	settings := &XraySettingService{}
	if err := settings.SaveXraySetting(syncTemplate); err != nil {
		t.Fatal(err)
	}
	before, _ := settings.GetXrayConfigTemplate()
	cases := []struct {
		name      string
		balancers []map[string]any
		message   string
	}{
		{"missing array", nil, "explicit balancers array"},
		{"missing selector", []map[string]any{testBalancer("pool", "absent")}, "no node outbound matches"},
		{"empty selector", []map[string]any{testBalancer("pool", "")}, "empty selector"},
		{"duplicate tag", []map[string]any{testBalancer("pool", "direct"), testBalancer("pool", "blocked")}, "duplicated"},
		{"missing fallback", []map[string]any{{"tag": "pool", "selector": []string{"direct"}, "fallbackTag": "absent"}}, "fallback outbound"},
		{"missing backup", []map[string]any{{"tag": "pool", "selector": []string{"direct"}, "fallbackTag": "_bl_absent"}}, "references missing balancer"},
		{"cycle", []map[string]any{{"tag": "pool", "selector": []string{"direct"}, "fallbackTag": "_bl_pool"}}, "fallback cycle"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := routingsync.Control{Source: "master", Revision: 1, Rules: []map[string]any{}, Balancing: &routingsync.Balancing{Balancers: tc.balancers}}
			err := (RoutingSyncService{}).Apply(state)
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("expected %q, got %v", tc.message, err)
			}
			after, _ := settings.GetXrayConfigTemplate()
			var receipt routingReceipt
			found, readErr := readRoutingValue(database.GetDB(), routingSyncReceivedKey, &receipt)
			if after != before || found || readErr != nil {
				t.Fatalf("invalid snapshot modified template/receipt: found=%t err=%v", found, readErr)
			}
		})
	}
}

func TestRoutingSyncBalancerChangesParticipateInReceiptHash(t *testing.T) {
	initTrafficTestDB(t)
	t.Cleanup(SetXrayProcessForTest(nil))
	if err := (&XraySettingService{}).SaveXraySetting(syncTemplate); err != nil {
		t.Fatal(err)
	}
	state := routingsync.Control{Source: "master", Revision: 1, Rules: []map[string]any{}, Balancing: &routingsync.Balancing{Balancers: []map[string]any{testBalancer("pool", "direct")}}}
	if err := (RoutingSyncService{}).Apply(state); err != nil {
		t.Fatal(err)
	}
	state.Balancing.Balancers[0]["selector"] = []string{"blocked"}
	if err := (RoutingSyncService{}).Apply(state); err == nil || !strings.Contains(err.Error(), "conflicting content") {
		t.Fatalf("unchanged revision accepted changed balancing: %v", err)
	}
	state.Revision++
	if err := (RoutingSyncService{}).Apply(state); err != nil {
		t.Fatal(err)
	}
}
