package service

import (
	"bytes"
	"encoding/json"
	"fmt"
)

func sameTemplateJSON(a, b string) bool {
	var first, second any
	if json.Unmarshal([]byte(a), &first) != nil || json.Unmarshal([]byte(b), &second) != nil {
		return false
	}
	x, _ := json.Marshal(first)
	y, _ := json.Marshal(second)
	return bytes.Equal(x, y)
}

// SaveXraySettingIfCurrent rejects edits based on a stale browser snapshot.
func (s *XraySettingService) SaveXraySettingIfCurrent(template, expected string) error {
	xrayTemplateMu.Lock()
	defer xrayTemplateMu.Unlock()
	current, err := s.GetXrayConfigTemplate()
	if err != nil {
		return err
	}
	if expected != "" && !sameTemplateJSON(UnwrapXrayTemplateConfig(expected), UnwrapXrayTemplateConfig(current)) {
		return fmt.Errorf("Xray configuration changed since it was loaded; reload before saving (local routing was preserved)")
	}
	return s.saveXraySettingLocked(template)
}

func (s *XraySettingService) editTemplate(edit func(string) (string, bool, error)) (bool, error) {
	xrayTemplateMu.Lock()
	defer xrayTemplateMu.Unlock()
	current, err := s.GetXrayConfigTemplate()
	if err != nil {
		return false, err
	}
	updated, changed, err := edit(current)
	if err != nil || !changed {
		return false, err
	}
	if err := s.saveXraySettingLocked(updated); err != nil {
		return false, err
	}
	return true, nil
}

func (s *XraySettingService) ReplaceProxyOutbounds(outbounds []map[string]any) (bool, error) {
	return s.editTemplate(func(current string) (string, bool, error) { return ReplaceProxyOutboundsInTemplate(current, outbounds) })
}

func (s *XraySettingService) ReplaceRoutingRules(rules []map[string]any, expected string) (bool, error) {
	if rules == nil {
		return false, fmt.Errorf("routing save requires an explicit rules array")
	}
	return s.editTemplate(func(current string) (string, bool, error) {
		if expected != "" {
			before, err := GetRoutingRulesFromTemplate(current)
			if err != nil {
				return "", false, err
			}
			raw, err := json.Marshal(before)
			if err != nil {
				return "", false, err
			}
			if !sameTemplateJSON(expected, string(raw)) {
				return "", false, fmt.Errorf("node routing changed since it was loaded; reload before saving (local routing was preserved)")
			}
		}
		return ReplaceRoutingRulesInTemplate(current, rules)
	})
}

func normalizeRoutingOwnership(template string) (string, error) {
	rules, err := GetRoutingRulesFromTemplate(template)
	if err != nil {
		return "", err
	}
	changed := false
	for i, rule := range rules {
		value, exists := rule[routingSyncSourceKey]
		if !exists {
			continue
		}
		if value == nil || value == "" {
			delete(rule, routingSyncSourceKey)
			changed = true
			continue
		}
		if _, ok := value.(string); !ok {
			return "", fmt.Errorf("routing rule %d: panelSyncSource must be a string or null", i+1)
		}
	}
	if !changed {
		return template, nil
	}
	updated, _, err := ReplaceRoutingRulesInTemplate(template, rules)
	return updated, err
}
