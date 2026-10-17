package service

import "fmt"

// orderPanelRoutingRules keeps node-local access controls ahead of shared destination routing.
func orderPanelRoutingRules(rules []map[string]any) []map[string]any {
	groups := [4][]map[string]any{}
	for _, rule := range rules {
		group := 3
		switch {
		case isApiRule(rule):
			group = 0
		case rule["localPre"] == true && rule[routingSyncSourceKey] == nil:
			group = 1
		case rule[routingSyncSourceKey] != nil:
			group = 2
		}
		groups[group] = append(groups[group], rule)
	}
	result := make([]map[string]any, 0, len(rules))
	for _, group := range groups {
		result = append(result, group...)
	}
	return result
}

func normalizeRoutingPriority(template string) (string, error) {
	rules, err := GetRoutingRulesFromTemplate(template)
	if err != nil {
		return "", err
	}
	for i, rule := range rules {
		if value, exists := rule["localPre"]; exists {
			if _, ok := value.(bool); !ok {
				return "", fmt.Errorf("routing rule %d: localPre must be a boolean", i+1)
			}
			if value == true && (rule["sync"] == true || rule[routingSyncSourceKey] != nil || isApiRule(rule)) {
				return "", fmt.Errorf("routing rule %d: local pre-routing cannot be synchronized or modify control rules", i+1)
			}
		}
	}
	updated, _, err := ReplaceRoutingRulesInTemplate(template, rules)
	return updated, err
}
