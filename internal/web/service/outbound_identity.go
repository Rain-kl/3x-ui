package service

import (
	"encoding/json"
	"fmt"
)

// normalizeOutboundIdentity leaves legacy tags intact so existing routing references survive upgrades.
func normalizeOutboundIdentity(template string) (string, error) {
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal([]byte(template), &cfg); err != nil {
		return "", err
	}
	if len(cfg["outbounds"]) == 0 {
		return template, nil
	}
	var outbounds []map[string]any
	if err := json.Unmarshal(cfg["outbounds"], &outbounds); err != nil {
		return "", err
	}

	seen := map[string]bool{}
	for _, outbound := range outbounds {
		tag, _ := outbound["tag"].(string)
		if id, exists := outbound["panelId"]; exists && id != tag {
			return "", fmt.Errorf("outbound %q: fixed panelId must match tag; change remark to rename", tag)
		}
		if tag == "" {
			continue
		}
		if seen[tag] {
			return "", fmt.Errorf("duplicate outbound ID %q", tag)
		}
		seen[tag] = true

	}
	return template, nil
}

func stripOutboundPanelMetadata(raw []byte) []byte {
	var outbounds []map[string]json.RawMessage
	if json.Unmarshal(raw, &outbounds) != nil {
		return raw
	}
	changed := false
	for _, outbound := range outbounds {
		for _, key := range []string{"panelId", "remark"} {
			if _, ok := outbound[key]; ok {
				delete(outbound, key)
				changed = true
			}
		}
	}
	if !changed {
		return raw
	}
	result, err := json.Marshal(outbounds)
	if err != nil {
		return raw
	}
	return result
}
