package service

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/routingsync"
)

func TestLocalPreRoutingSurvivesSharedReplacement(t *testing.T) {
	initTrafficTestDB(t)
	template := `{"outbounds":[{"tag":"direct","protocol":"freedom"},{"tag":"blocked","protocol":"blackhole"}],"routing":{"rules":[{"type":"field","network":"tcp,udp","outboundTag":"direct"},{"type":"field","sourceIP":["geoip:cn"],"localPre":true,"outboundTag":"blocked"}]}}`
	if err := (&XraySettingService{}).SaveXraySetting(template); err != nil {
		t.Fatal(err)
	}
	for rev := int64(1); rev <= 2; rev++ {
		state := routingsync.Control{Source: "master", Revision: rev, Rules: []map[string]any{{"type": "field", "domain": []any{"geosite:cn"}, "outboundTag": "direct", "sync": true}}}
		if err := (RoutingSyncService{}).Apply(state); err != nil {
			t.Fatal(err)
		}
		rules := savedSyncRules(t)
		if len(rules) != 4 || !isApiRule(rules[0]) || rules[1]["localPre"] != true || rules[2][routingSyncSourceKey] != "master" || rules[3]["outboundTag"] != "direct" {
			t.Fatalf("unexpected priority: %+v", rules)
		}
	}
}

func TestLocalPreRejectsSynchronization(t *testing.T) {
	for _, rule := range []string{`{"localPre":true,"sync":true}`, `{"localPre":true,"panelSyncSource":"master"}`, `{"localPre":"true"}`} {
		if _, err := normalizeRoutingPriority(`{"routing":{"rules":[` + rule + `]}}`); err == nil {
			t.Fatalf("accepted invalid local pre-rule %s", rule)
		}
	}
}
