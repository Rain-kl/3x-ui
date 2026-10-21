package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/routingsync"
)

func balancingFromTemplate(template string) (*routingsync.Balancing, error) {
	var config struct {
		Routing struct {
			Balancers []map[string]any `json:"balancers"`
		} `json:"routing"`
		Observatory      map[string]any `json:"observatory"`
		BurstObservatory map[string]any `json:"burstObservatory"`
	}
	if err := json.Unmarshal([]byte(template), &config); err != nil {
		return nil, fmt.Errorf("read balancing configuration: %w", err)
	}
	balancers := config.Routing.Balancers
	if balancers == nil {
		balancers = []map[string]any{}
	}
	return &routingsync.Balancing{Balancers: balancers, Observatory: config.Observatory, BurstObservatory: config.BurstObservatory}, nil
}

// installBalancing owns only the balancers, observers and reserved fallback loopbacks.
func installBalancing(template string, balancing *routingsync.Balancing, source string) (string, error) {
	if balancing == nil {
		return template, nil
	}
	if len(balancing.Balancers) > 10000 {
		return "", fmt.Errorf("balancing synchronization exceeds 10000 balancers")
	}
	if balancing.Balancers == nil {
		return "", fmt.Errorf("balancing synchronization requires an explicit balancers array")
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(template), &cfg); err != nil {
		return "", err
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	routing, _ := cfg["routing"].(map[string]any)
	if routing == nil {
		routing = map[string]any{}
		cfg["routing"] = routing
	}
	if script, _ := routing["script"].(string); script != "" && len(balancing.Balancers) > 0 {
		return "", fmt.Errorf("node routing script overrides synchronized balancers; remove the script first")
	}
	tags := make(map[string]bool)
	for _, b := range balancing.Balancers {
		tag, _ := b["tag"].(string)
		if tag == "" || tags[tag] {
			return "", fmt.Errorf("synchronized balancer tag %q is empty or duplicated", tag)
		}
		tags[tag] = true
	}
	loopbacks := make([]any, 0)
	controlRules := make([]map[string]any, 0)
	seen := make(map[string]bool)
	for _, b := range balancing.Balancers {
		fallback, _ := b["fallbackTag"].(string)
		if !strings.HasPrefix(fallback, "_bl_") || seen[fallback] {
			continue
		}
		target := strings.TrimPrefix(fallback, "_bl_")
		if !tags[target] {
			return "", fmt.Errorf("synchronized balancer fallback %q references missing balancer %q", fallback, target)
		}
		seen[fallback] = true
		loopbacks = append(loopbacks, map[string]any{"tag": fallback, "protocol": "loopback", "settings": map[string]any{"inboundTag": fallback}})
		controlRules = append(controlRules, map[string]any{"type": "field", "inboundTag": []string{fallback}, "balancerTag": target, routingSyncSourceKey: source})
	}
	outbounds := make([]any, 0)
	if existing, ok := cfg["outbounds"].([]any); ok {
		for _, raw := range existing {
			ob, _ := raw.(map[string]any)
			tag, _ := ob["tag"].(string)
			if !strings.HasPrefix(tag, "_bl_") {
				outbounds = append(outbounds, raw)
			}
		}
	}
	cfg["outbounds"] = append(outbounds, loopbacks...)
	routing["balancers"] = balancing.Balancers
	for key, value := range map[string]map[string]any{"observatory": balancing.Observatory, "burstObservatory": balancing.BurstObservatory} {
		if value == nil {
			delete(cfg, key)
		} else {
			cfg[key] = value
		}
	}
	rules, err := GetRoutingRulesFromTemplate(template)
	if err != nil {
		return "", err
	}
	for _, r := range rules {
		internal := false
		for _, tag := range readInboundTags(r["inboundTag"]) {
			internal = internal || strings.HasPrefix(tag, "_bl_")
		}
		if !internal {
			controlRules = append(controlRules, r)
		}
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	updated, _, err := ReplaceRoutingRulesInTemplate(string(raw), controlRules)
	return updated, err
}

func validateBalancingTargets(balancing *routingsync.Balancing, outbounds map[string]bool) error {
	if balancing == nil {
		return nil
	}
	fallbacks := make(map[string]string)
	for _, b := range balancing.Balancers {
		tag, _ := b["tag"].(string)
		selectors := readInboundTags(b["selector"])
		matched := false
		for _, selector := range selectors {
			if selector == "" {
				return fmt.Errorf("synchronized balancer %q has an empty selector", tag)
			}
			for outbound := range outbounds {
				matched = matched || strings.HasPrefix(outbound, selector)
			}
		}
		if !matched {
			return fmt.Errorf("synchronized balancer %q: no node outbound matches selectors %v", tag, selectors)
		}
		fallback, _ := b["fallbackTag"].(string)
		if fallback != "" && !outbounds[fallback] {
			return fmt.Errorf("synchronized balancer %q: fallback outbound %q is missing", tag, fallback)
		}
		if strings.HasPrefix(fallback, "_bl_") {
			fallbacks[tag] = strings.TrimPrefix(fallback, "_bl_")
		}
	}
	for start := range fallbacks {
		visited := make(map[string]bool)
		for tag := start; tag != ""; tag = fallbacks[tag] {
			if visited[tag] {
				return fmt.Errorf("synchronized balancer %q has a fallback cycle at %q", start, tag)
			}
			visited[tag] = true
		}
	}
	return nil
}
