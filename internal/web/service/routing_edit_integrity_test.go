package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/routingsync"
)

func TestRoutingLocalRulesSurviveBackgroundEditsAndRejectStaleSave(t *testing.T) {
	t.Setenv("XRAY_LOCATION_ASSET", t.TempDir())
	initTrafficTestDB(t)
	t.Cleanup(SetXrayProcessForTest(nil))
	svc := &XraySettingService{}
	base := `{"outbounds":[{"tag":"direct","protocol":"freedom"},{"tag":"blocked","protocol":"blackhole"}],"routing":{"rules":[]}}`
	if err := svc.SaveXraySetting(base); err != nil {
		t.Fatal(err)
	}
	parent := routingsync.Control{Source: "master", Revision: 1, Rules: []map[string]any{syncRule("domain:shared.example")}}
	if err := (RoutingSyncService{}).Apply(parent); err != nil {
		t.Fatal(err)
	}
	stale, err := svc.GetXrayConfigTemplate()
	if err != nil {
		t.Fatal(err)
	}
	oldRules, err := GetRoutingRulesFromTemplate(stale)
	if err != nil {
		t.Fatal(err)
	}
	expectedRules, _ := json.Marshal(oldRules)
	rules := append(oldRules, map[string]any{"type": "field", "domain": []string{"domain:local.example"}, "outboundTag": "direct"}, map[string]any{"type": "field", "localPre": true, "sourceIP": []string{"geoip:cn"}, "outboundTag": "blocked"})
	fresh, _, err := ReplaceRoutingRulesInTemplate(stale, rules)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveXraySetting(fresh); err != nil {
		t.Fatal(err)
	}
	proxies := []map[string]any{{"protocol": "socks", "tag": "ob-test", "panelId": "ob-test", "remark": "first name", "settings": map[string]any{"servers": []any{map[string]any{"address": "127.0.0.1", "port": 1080}}}}}
	if changed, err := svc.ReplaceProxyOutbounds(proxies); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if len(savedSyncRules(t)) != 4 {
		t.Fatal("outbound-only update lost local rules")
	}
	before, err := svc.GetXrayConfigTemplate()
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveXraySettingIfCurrent(stale, stale); err == nil {
		t.Fatal("stale full save accepted")
	}
	if _, err := svc.ReplaceRoutingRules(oldRules, string(expectedRules)); err == nil {
		t.Fatal("stale routing save accepted")
	}
	after, _ := svc.GetXrayConfigTemplate()
	if before != after {
		t.Fatal("conflict modified persisted configuration")
	}
	parent.Revision++
	if err := (RoutingSyncService{}).Apply(parent); err != nil {
		t.Fatal(err)
	}
	if len(savedSyncRules(t)) != 4 {
		t.Fatal("parent refresh lost local rules")
	}
	current, _ := svc.GetXrayConfigTemplate()
	var pretty any
	_ = json.Unmarshal([]byte(current), &pretty)
	formatted, _ := json.MarshalIndent(pretty, "", "    ")
	if err := svc.SaveXraySettingIfCurrent(current, string(formatted)); err != nil {
		t.Fatalf("whitespace-only baseline conflict: %v", err)
	}
}

func TestRoutingEmptyOwnershipMarkersRemainLocal(t *testing.T) {
	t.Setenv("XRAY_LOCATION_ASSET", t.TempDir())
	initTrafficTestDB(t)
	t.Cleanup(SetXrayProcessForTest(nil))
	svc := &XraySettingService{}
	rules := []map[string]any{{"domain": []string{"domain:null.example"}, "outboundTag": "direct", "panelSyncSource": nil}, {"domain": []string{"domain:empty.example"}, "outboundTag": "blocked", "panelSyncSource": ""}}
	raw, _, err := ReplaceRoutingRulesInTemplate(syncTemplate, rules)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveXraySetting(raw); err != nil {
		t.Fatal(err)
	}
	state := routingsync.Control{Source: "master", Revision: 1, Rules: []map[string]any{syncRule("domain:shared.example")}}
	if err := (RoutingSyncService{}).Apply(state); err != nil {
		t.Fatal(err)
	}
	if len(savedSyncRules(t)) != 4 {
		t.Fatal("empty ownership removed local rules")
	}
	raw, _, _ = ReplaceRoutingRulesInTemplate(syncTemplate, []map[string]any{{"panelSyncSource": false, "outboundTag": "direct"}})
	if err := svc.SaveXraySetting(raw); err == nil || !strings.Contains(err.Error(), "panelSyncSource") {
		t.Fatalf("invalid ownership accepted: %v", err)
	}
}

func TestOutboundRemarkRenameKeepsRoutingIdentity(t *testing.T) {
	t.Setenv("XRAY_LOCATION_ASSET", t.TempDir())
	initTrafficTestDB(t)
	svc := &XraySettingService{}
	raw := `{"outbounds":[{"tag":"ob-fixed","panelId":"ob-fixed","remark":"old name","protocol":"freedom"},{"tag":"blocked","protocol":"blackhole"}],"routing":{"rules":[{"domain":["domain:example.com"],"outboundTag":"ob-fixed"}]}}`
	if err := svc.SaveXraySetting(raw); err != nil {
		t.Fatal(err)
	}
	next := strings.Replace(raw, "old name", "new name", 1)
	if err := svc.SaveXraySettingIfCurrent(next, raw); err == nil { // first save adds the API rule, so use persisted baseline below
		t.Fatal("baseline missing persisted API rule should conflict")
	}
	baseline, _ := svc.GetXrayConfigTemplate()
	if err := svc.SaveXraySettingIfCurrent(next, baseline); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.GetXrayConfigTemplate()
	if !strings.Contains(got, "new name") || savedSyncRules(t)[1]["outboundTag"] != "ob-fixed" {
		t.Fatal("rename modified routing ID")
	}
	bad := strings.Replace(next, `"tag":"ob-fixed"`, `"tag":"ob-changed"`, 1)
	if err := svc.SaveXraySetting(bad); err == nil {
		t.Fatal("changed immutable ID accepted")
	}
	wire := stripOutboundPanelMetadata([]byte(`[{"tag":"ob-fixed","panelId":"ob-fixed","remark":"new name","protocol":"freedom"}]`))
	if strings.Contains(string(wire), "panelId") || strings.Contains(string(wire), "remark") {
		t.Fatal("panel metadata leaked to core")
	}
}
