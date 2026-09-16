package service

import (
	"encoding/json"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestRoutingRuleHelpers(t *testing.T) {
	tmpl := `{
  "routing": {
    "rules": [
      {
        "type": "field",
        "inboundTag": ["api"],
        "outboundTag": "api"
      },
      {
        "type": "field",
        "domain": ["geosite:cn"],
        "outboundTag": "direct"
      }
    ]
  }
}`

	rules, err := GetRoutingRulesFromTemplate(tmpl)
	if err != nil {
		t.Fatalf("GetRoutingRulesFromTemplate failed: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}

	newRules := []map[string]any{
		{
			"type":        "field",
			"domain":      []string{"google.com"},
			"outboundTag": "proxy",
		},
	}

	updated, changed, err := ReplaceRoutingRulesInTemplate(tmpl, newRules)
	if err != nil {
		t.Fatalf("ReplaceRoutingRulesInTemplate failed: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed to be true")
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(updated), &parsed); err != nil {
		t.Fatalf("unmarshal updated failed: %v", err)
	}
	rRouting, _ := parsed["routing"].(map[string]any)
	rRules, _ := rRouting["rules"].([]any)

	// EnsureStatsRouting should have inserted or preserved api rule at index 0
	if len(rRules) != 2 {
		t.Fatalf("expected 2 rules after EnsureStatsRouting, got %d", len(rRules))
	}
	rule0, _ := rRules[0].(map[string]any)
	if rule0["outboundTag"] != "api" {
		t.Fatalf("expected rule 0 outboundTag to be 'api', got %v", rule0["outboundTag"])
	}
}

func TestGetInboundTagsByNode(t *testing.T) {
	setupConflictDB(t)
	db := database.GetDB()

	nodeID := 1
	inboundMaster := &model.Inbound{
		UserId: 1, NodeID: nil, Tag: "master-inbound", Enable: true, Port: 10001, Protocol: model.VLESS, Settings: `{"clients":[]}`,
	}
	inboundWorker := &model.Inbound{
		UserId: 1, NodeID: &nodeID, Tag: "worker-inbound", Enable: true, Port: 10002, Protocol: model.VLESS, Settings: `{"clients":[]}`,
	}
	if err := db.Create(inboundMaster).Error; err != nil {
		t.Fatalf("create master inbound: %v", err)
	}
	if err := db.Create(inboundWorker).Error; err != nil {
		t.Fatalf("create worker inbound: %v", err)
	}

	svc := &InboundService{}

	// Master tags
	masterTagsJSON, err := svc.GetInboundTagsByNode(nil)
	if err != nil {
		t.Fatalf("GetInboundTagsByNode(nil) failed: %v", err)
	}
	var masterTags []string
	if err := json.Unmarshal([]byte(masterTagsJSON), &masterTags); err != nil {
		t.Fatalf("unmarshal master tags: %v", err)
	}
	if len(masterTags) != 1 || masterTags[0] != "master-inbound" {
		t.Fatalf("unexpected master tags: %v", masterTags)
	}

	// Worker tags
	workerTagsJSON, err := svc.GetInboundTagsByNode(&nodeID)
	if err != nil {
		t.Fatalf("GetInboundTagsByNode(&nodeID) failed: %v", err)
	}
	var workerTags []string
	if err := json.Unmarshal([]byte(workerTagsJSON), &workerTags); err != nil {
		t.Fatalf("unmarshal worker tags: %v", err)
	}
	if len(workerTags) != 1 || workerTags[0] != "worker-inbound" {
		t.Fatalf("unexpected worker tags: %v", workerTags)
	}
}

