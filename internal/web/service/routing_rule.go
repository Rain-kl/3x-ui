package service

import (
	"bytes"
	"encoding/json"
)

// GetRoutingRulesFromTemplate extracts routing rules from Xray template JSON.
func GetRoutingRulesFromTemplate(templateJSON string) ([]map[string]any, error) {
	var tmpl map[string]any
	if err := json.Unmarshal([]byte(templateJSON), &tmpl); err != nil {
		return nil, err
	}
	if tmpl == nil {
		return nil, nil
	}
	routing, _ := tmpl["routing"].(map[string]any)
	if routing == nil {
		return nil, nil
	}
	rawRules, _ := routing["rules"].([]any)
	rules := make([]map[string]any, 0, len(rawRules))
	for _, r := range rawRules {
		if rMap, ok := r.(map[string]any); ok {
			rules = append(rules, rMap)
		}
	}
	return rules, nil
}

// ReplaceRoutingRulesInTemplate replaces routing rules in Xray template JSON, ensuring stats rule stays at index 0.
func ReplaceRoutingRulesInTemplate(templateJSON string, rules []map[string]any) (string, bool, error) {
	var tmpl map[string]any
	if err := json.Unmarshal([]byte(templateJSON), &tmpl); err != nil {
		return "", false, err
	}
	if tmpl == nil {
		tmpl = make(map[string]any)
	}

	routing, _ := tmpl["routing"].(map[string]any)
	if routing == nil {
		routing = make(map[string]any)
	}

	oldRules, _ := routing["rules"].([]any)
	oldBytes, _ := json.Marshal(oldRules)
	newBytes, _ := json.Marshal(rules)
	changed := !bytes.Equal(oldBytes, newBytes)

	routing["rules"] = rules
	tmpl["routing"] = routing

	outBytes, err := json.MarshalIndent(tmpl, "", "  ")
	if err != nil {
		return "", false, err
	}

	res := string(outBytes)
	if hoisted, err := EnsureStatsRouting(res); err == nil {
		res = hoisted
	}
	return res, changed, nil
}
