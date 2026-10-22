package service

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/routingsync"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
)

const syncTemplate = `{"outbounds":[{"tag":"direct","protocol":"freedom"},{"tag":"blocked","protocol":"blackhole"}],"routing":{"domainStrategy":"AsIs","rules":[{"type":"field","domain":["domain:local.example"],"outboundTag":"direct"}]}}`

func syncRule(domain string) map[string]any {
	return map[string]any{"type": "field", "domain": []any{domain}, "outboundTag": "blocked", "sync": true}
}

func savedSyncRules(t *testing.T) []map[string]any {
	t.Helper()
	template, err := (&SettingService{}).GetXrayConfigTemplate()
	if err != nil {
		t.Fatal(err)
	}
	rules, err := GetRoutingRulesFromTemplate(template)
	if err != nil {
		t.Fatal(err)
	}
	return rules
}

func TestRoutingSyncMasterSnapshotAndWithdrawal(t *testing.T) {
	initTrafficTestDB(t)
	svc := &XraySettingService{}
	if err := svc.SaveXraySetting(syncTemplate); err != nil {
		t.Fatal(err)
	}
	var state routingsync.Control
	if found, err := readRoutingValue(database.GetDB(), routingSyncDesiredKey, &state); err != nil || found {
		t.Fatalf("untouched master broadcast: %t %v", found, err)
	}
	rule := syncRule("domain:shared.example")
	rules := append(savedSyncRules(t), rule)
	template, _, _ := ReplaceRoutingRulesInTemplate(syncTemplate, rules)
	for range 2 {
		if err := svc.SaveXraySetting(template); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := readRoutingValue(database.GetDB(), routingSyncDesiredKey, &state); err != nil {
		t.Fatal(err)
	}
	guid, err := svc.GetPanelGuid()
	if err != nil || state.Source != guid || state.Revision != 1 || len(state.Rules) != 1 {
		t.Fatalf("snapshot: %+v guid=%q err=%v", state, guid, err)
	}
	// Turning synchronization off withdraws the copy without deleting the local rule.
	rule["sync"] = false
	template, _, _ = ReplaceRoutingRulesInTemplate(syncTemplate, rules)
	if err := svc.SaveXraySetting(template); err != nil {
		t.Fatal(err)
	}
	if _, err := readRoutingValue(database.GetDB(), routingSyncDesiredKey, &state); err != nil {
		t.Fatal(err)
	}
	if state.Revision != 2 || state.Source != guid || state.Rules == nil || len(state.Rules) != 0 {
		t.Fatalf("withdrawal: %+v", state)
	}
	if len(savedSyncRules(t)) != 3 {
		t.Fatal("withdrawal removed a local or API rule")
	}
}

func TestRoutingSyncChildMergeOrderingAndProtection(t *testing.T) {
	initTrafficTestDB(t)
	t.Cleanup(SetXrayProcessForTest(nil))
	if err := (&XraySettingService{}).SaveXraySetting(syncTemplate); err != nil {
		t.Fatal(err)
	}
	svc := RoutingSyncService{}
	state := routingsync.Control{Source: "master", Revision: 2, Rules: []map[string]any{syncRule("domain:shared.example")}}
	for range 2 {
		if err := svc.Apply(state); err != nil {
			t.Fatal(err)
		}
	}
	rules := savedSyncRules(t)
	if len(rules) != 3 || !isApiRule(rules[0]) || rules[1][routingSyncSourceKey] != "master" || rules[2]["outboundTag"] != "direct" {
		t.Fatalf("wrong merge order: %+v", rules)
	}
	if _, exists := rules[1]["sync"]; exists {
		t.Fatal("received rule can rebroadcast")
	}
	if (&XrayService{}).IsXrayRunning() {
		t.Fatal("synchronization started a stopped core")
	}
	stale := routingsync.Control{Source: "master", Revision: 1, Rules: []map[string]any{}}
	if err := svc.Apply(stale); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(savedSyncRules(t), rules) {
		t.Fatal("stale snapshot replaced current")
	}
	// Normal template saves cannot forge, edit or delete authoritative copies.
	template, _, _ := ReplaceRoutingRulesInTemplate(syncTemplate, []map[string]any{{"type": "field", "domain": []string{"domain:forged.example"}, "outboundTag": "direct", routingSyncSourceKey: "master"}, {"type": "field", "domain": []string{"domain:new-local.example"}, "outboundTag": "direct"}})
	if err := (&XraySettingService{}).SaveXraySetting(template); err != nil {
		t.Fatal(err)
	}
	if got := savedSyncRules(t); !reflect.DeepEqual(got[1], rules[1]) || len(got) != 3 {
		t.Fatalf("managed copy overwritten: %+v", got)
	}
	state.Revision = 3
	state.Rules = []map[string]any{}
	if err := svc.Apply(state); err != nil {
		t.Fatal(err)
	}
	if got := savedSyncRules(t); len(got) != 2 || got[1][routingSyncSourceKey] != nil {
		t.Fatalf("withdrawal removed local rules: %+v", got)
	}
	var receipt routingReceipt
	if _, err := readRoutingValue(database.GetDB(), routingSyncReceivedKey, &receipt); err != nil || !receipt.Applied || receipt.Revision != 3 {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
}

func TestRoutingSyncRejectsConflictsAndMissingReferences(t *testing.T) {
	initTrafficTestDB(t)
	t.Cleanup(SetXrayProcessForTest(nil))
	if err := (&XraySettingService{}).SaveXraySetting(syncTemplate); err != nil {
		t.Fatal(err)
	}
	svc := RoutingSyncService{}
	state := routingsync.Control{Source: "master", Revision: 1, Rules: []map[string]any{syncRule("domain:shared.example")}}
	if err := svc.Apply(state); err != nil {
		t.Fatal(err)
	}
	before := savedSyncRules(t)
	cases := []struct {
		name     string
		source   string
		revision int64
		rule     map[string]any
		message  string
	}{
		{"revision content conflict", "master", 1, syncRule("domain:changed.example"), "conflicting content"},
		{"other master", "other", 2, syncRule("domain:shared.example"), "already managed"},
		{"missing outbound", "master", 2, map[string]any{"domain": []string{"domain:x.example"}, "outboundTag": "absent"}, `outbound "absent" is missing`},
		{"missing inbound", "master", 2, map[string]any{"domain": []string{"domain:x.example"}, "outboundTag": "direct", "inboundTag": []string{"absent"}}, `inbound "absent" is missing`},
		{"missing balancer", "master", 2, map[string]any{"domain": []string{"domain:x.example"}, "balancerTag": "absent"}, `balancer "absent" is missing`},
		{"control rule", "master", 2, map[string]any{"inboundTag": []string{"api"}, "outboundTag": "api"}, "control rules cannot"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.Apply(routingsync.Control{Source: tc.source, Revision: tc.revision, Rules: []map[string]any{tc.rule}})
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("expected %q, got %v", tc.message, err)
			}
			if !reflect.DeepEqual(savedSyncRules(t), before) {
				t.Fatal("invalid snapshot modified saved routing")
			}
		})
	}
	if err := svc.Apply(routingsync.Control{Source: "master", Revision: 2}); err == nil || !strings.Contains(err.Error(), "explicit rules array") {
		t.Fatalf("missing rules deleted copies: %v", err)
	}
}

func TestRoutingSyncBroadcastPartialFailureAndRecovery(t *testing.T) {
	db := initTrafficTestDB(t)
	useTestRuntimeManager(t)
	var mu sync.Mutex
	var received []routingsync.Control
	fail := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/panel/api/server/routing/sync" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("unexpected RPC: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var state routingsync.Control
		if err := json.NewDecoder(r.Body).Decode(&state); err != nil {
			t.Error(err)
		}
		mu.Lock()
		received = append(received, state)
		failing := fail
		mu.Unlock()
		if failing {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"obj":{"balancingApplied":true}}`))
	}))
	defer server.Close()
	host, portText, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	port, _ := strconv.Atoi(portText)
	node := model.Node{Name: "edge", Scheme: "http", Address: host, Port: port, BasePath: "/", ApiToken: "tok", Enable: true, Status: "offline", AllowPrivateAddress: true}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	disabled := node
	disabled.Id = 0
	disabled.Name = "disabled"
	if err := db.Create(&disabled).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&disabled).Update("enable", false).Error; err != nil {
		t.Fatal(err)
	}
	balancedTemplate := strings.Replace(syncTemplate, `"domainStrategy":`, `"balancers":[{"tag":"pool","selector":["direct"]}],"domainStrategy":`, 1)
	template, _, _ := ReplaceRoutingRulesInTemplate(balancedTemplate, []map[string]any{syncRule("domain:shared.example")})
	if err := (&XraySettingService{}).SaveXraySetting(template); err != nil {
		t.Fatal(err)
	}
	svc := RoutingSyncService{}
	if err := svc.SyncNodes(context.Background()); err == nil || !strings.Contains(err.Error(), "node edge") || !strings.Contains(err.Error(), "automatic retry") {
		t.Fatalf("partial failure hidden: %v", err)
	}
	mu.Lock()
	fail = false
	mu.Unlock()
	runtime.SetManager(runtime.NewManager(runtime.LocalDeps{}))
	if err := svc.SyncNodes(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Newly added nodes receive the existing persisted revision without a new save.
	node.Id = 0
	node.Name = "new-edge"
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.SyncNodes(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 4 {
		t.Fatalf("disabled node contacted or enabled node skipped: %d requests", len(received))
	}
	for _, state := range received {
		if !reflect.DeepEqual(state, received[0]) || state.Revision != 1 || state.Balancing == nil || len(state.Balancing.Balancers) != 1 {
			t.Fatalf("retry lost desired revision: %+v", state)
		}
	}
}

func TestRoutingSyncRejectsScriptOverride(t *testing.T) {
	for _, installed := range []bool{false, true} {
		t.Run(strconv.FormatBool(installed), func(t *testing.T) {
			initTrafficTestDB(t)
			t.Cleanup(SetXrayProcessForTest(nil))
			settings := &XraySettingService{}
			if err := settings.SaveXraySetting(syncTemplate); err != nil {
				t.Fatal(err)
			}
			state := routingsync.Control{Source: "master", Revision: 1, Rules: []map[string]any{syncRule("domain:shared.example")}}
			if installed {
				if err := (RoutingSyncService{}).Apply(state); err != nil {
					t.Fatal(err)
				}
			}
			before, err := settings.GetXrayConfigTemplate()
			if err != nil {
				t.Fatal(err)
			}
			var template map[string]any
			if err := json.Unmarshal([]byte(before), &template); err != nil {
				t.Fatal(err)
			}
			template["routing"].(map[string]any)["script"] = "router.js"
			raw, _ := json.Marshal(template)
			err = settings.SaveXraySetting(string(raw))
			if installed {
				if err == nil || !strings.Contains(err.Error(), "script would override") {
					t.Fatalf("local save bypassed managed group: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := (RoutingSyncService{}).Apply(state); err == nil || !strings.Contains(err.Error(), "script overrides") {
				t.Fatalf("ignored rules acknowledged: %v", err)
			}
		})
	}
}
