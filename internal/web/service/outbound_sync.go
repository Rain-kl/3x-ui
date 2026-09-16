package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
)

var proxyProtocols = map[string]bool{
	"vmess":       true,
	"vless":       true,
	"trojan":      true,
	"shadowsocks": true,
	"hysteria":    true,
	"hysteria2":   true,
	"wireguard":   true,
	"socks":       true,
	"http":        true,
}

var nonProxyTags = map[string]bool{
	"api":         true,
	"metrics_out": true,
	"blocked":     true,
	"direct":      true,
}

var nonProxyProtocols = map[string]bool{
	"freedom":   true,
	"blackhole": true,
	"dns":       true,
}

// IsProxyOutbound determines if an outbound configuration represents a proxy outbound
// rather than a system or local routing outbound.
func IsProxyOutbound(ob map[string]any) bool {
	if ob == nil {
		return false
	}
	tag, _ := ob["tag"].(string)
	if nonProxyTags[strings.ToLower(strings.TrimSpace(tag))] {
		return false
	}
	protocol, _ := ob["protocol"].(string)
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if nonProxyProtocols[protocol] {
		return false
	}
	return proxyProtocols[protocol]
}

// ExtractOutboundEndpoints extracts host:port endpoints from an outbound config map.
func ExtractOutboundEndpoints(ob map[string]any) []string {
	if !IsProxyOutbound(ob) {
		return nil
	}
	settings, _ := ob["settings"].(map[string]any)
	if settings == nil {
		return nil
	}

	var out []string
	seen := make(map[string]bool)

	addEndpoint := func(ep string) {
		ep = strings.ToLower(strings.TrimSpace(ep))
		if ep != "" && !seen[ep] {
			seen[ep] = true
			out = append(out, ep)
		}
	}

	addServer := func(addr any, port any) {
		host, _ := addr.(string)
		host = strings.ToLower(strings.TrimSpace(host))
		p := numAsInt(port)
		if p == 0 && host != "" {
			if h, ps, err := net.SplitHostPort(host); err == nil {
				host = strings.ToLower(strings.TrimSpace(h))
				p = numAsInt(ps)
			}
		}
		if host != "" && p > 0 {
			addEndpoint(net.JoinHostPort(host, strconv.Itoa(p)))
		}
	}

	if vnext, ok := settings["vnext"].([]any); ok {
		for _, v := range vnext {
			if vm, ok := v.(map[string]any); ok {
				addServer(vm["address"], vm["port"])
			}
		}
	}
	if servers, ok := settings["servers"].([]any); ok {
		for _, s := range servers {
			if sm, ok := s.(map[string]any); ok {
				addServer(sm["address"], sm["port"])
			}
		}
	}
	if peers, ok := settings["peers"].([]any); ok {
		for _, p := range peers {
			if pm, ok := p.(map[string]any); ok {
				if ep, _ := pm["endpoint"].(string); ep != "" {
					ep = strings.ToLower(strings.TrimSpace(ep))
					if h, ps, err := net.SplitHostPort(ep); err == nil {
						ep = net.JoinHostPort(strings.ToLower(strings.TrimSpace(h)), ps)
					} else if port := numAsInt(pm["port"]); port > 0 {
						ep = net.JoinHostPort(ep, strconv.Itoa(port))
					}
					addEndpoint(ep)
				} else {
					addServer(pm["address"], pm["port"])
				}
			}
		}
	}
	if len(out) == 0 {
		addServer(settings["address"], settings["port"])
	}

	return out
}

// ExtractOutboundKeys returns normalized protocol://host:port keys for deduplication.
func ExtractOutboundKeys(ob map[string]any) []string {
	if !IsProxyOutbound(ob) {
		return nil
	}
	protocol, _ := ob["protocol"].(string)
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	endpoints := ExtractOutboundEndpoints(ob)
	if len(endpoints) == 0 {
		return nil
	}

	keys := make([]string, 0, len(endpoints))
	for _, ep := range endpoints {
		keys = append(keys, fmt.Sprintf("%s://%s", protocol, ep))
	}
	return keys
}

// MergeProxyOutboundsIntoTemplate adds incoming proxy outbounds without duplicating endpoints.
func MergeProxyOutboundsIntoTemplate(templateJSON string, incomingProxyOutbounds []map[string]any) (string, int, error) {
	var tmpl map[string]any
	if err := json.Unmarshal([]byte(templateJSON), &tmpl); err != nil {
		return "", 0, err
	}
	if tmpl == nil {
		tmpl = make(map[string]any)
	}

	var rawOutbounds []any
	if arr, ok := tmpl["outbounds"].([]any); ok {
		rawOutbounds = arr
	}

	existingKeys := make(map[string]bool)
	existingTags := make(map[string]bool)
	for _, item := range rawOutbounds {
		if obMap, ok := item.(map[string]any); ok {
			for _, k := range ExtractOutboundKeys(obMap) {
				existingKeys[k] = true
			}
			if tag, ok := obMap["tag"].(string); ok && tag != "" {
				existingTags[tag] = true
			}
		}
	}

	addedCount := 0
	for _, incoming := range incomingProxyOutbounds {
		if !IsProxyOutbound(incoming) {
			continue
		}
		keys := ExtractOutboundKeys(incoming)
		if len(keys) == 0 {
			continue
		}
		hasDuplicate := false
		for _, k := range keys {
			if existingKeys[k] {
				hasDuplicate = true
				break
			}
		}
		if hasDuplicate {
			continue
		}
		for _, k := range keys {
			existingKeys[k] = true
		}

		tag, _ := incoming["tag"].(string)
		tag = strings.TrimSpace(tag)
		if tag == "" {
			if proto, _ := incoming["protocol"].(string); proto != "" {
				tag = strings.TrimSpace(proto)
			} else {
				tag = "proxy"
			}
		}
		candidateTag := tag
		for i := 2; existingTags[candidateTag]; i++ {
			candidateTag = fmt.Sprintf("%s-%d", tag, i)
		}
		existingTags[candidateTag] = true

		cloned := make(map[string]any, len(incoming))
		for k, v := range incoming {
			cloned[k] = v
		}
		cloned["tag"] = candidateTag
		rawOutbounds = append(rawOutbounds, cloned)
		addedCount++
	}

	tmpl["outbounds"] = rawOutbounds
	outBytes, err := json.MarshalIndent(tmpl, "", "  ")
	if err != nil {
		return "", 0, err
	}
	return string(outBytes), addedCount, nil
}

// ReplaceProxyOutboundsInTemplate replaces proxy outbounds while preserving system outbounds.
func ReplaceProxyOutboundsInTemplate(templateJSON string, masterProxyOutbounds []map[string]any) (string, bool, error) {
	var tmpl map[string]any
	if err := json.Unmarshal([]byte(templateJSON), &tmpl); err != nil {
		return "", false, err
	}
	if tmpl == nil {
		tmpl = make(map[string]any)
	}

	var systemOutbounds []any
	var oldProxyOutbounds []map[string]any
	if rawArr, ok := tmpl["outbounds"].([]any); ok {
		for _, item := range rawArr {
			if obMap, ok := item.(map[string]any); ok {
				if IsProxyOutbound(obMap) {
					oldProxyOutbounds = append(oldProxyOutbounds, obMap)
				} else {
					systemOutbounds = append(systemOutbounds, item)
				}
			} else {
				systemOutbounds = append(systemOutbounds, item)
			}
		}
	}

	var filteredMaster []map[string]any
	for _, ob := range masterProxyOutbounds {
		if IsProxyOutbound(ob) {
			filteredMaster = append(filteredMaster, ob)
		}
	}

	var changed bool
	if len(oldProxyOutbounds) == 0 && len(filteredMaster) == 0 {
		changed = false
	} else {
		oldBytes, _ := json.Marshal(oldProxyOutbounds)
		newBytes, _ := json.Marshal(filteredMaster)
		changed = !bytes.Equal(oldBytes, newBytes)
	}

	newOutbounds := make([]any, 0, len(systemOutbounds)+len(filteredMaster))
	newOutbounds = append(newOutbounds, systemOutbounds...)
	for _, ob := range filteredMaster {
		newOutbounds = append(newOutbounds, ob)
	}
	tmpl["outbounds"] = newOutbounds

	outBytes, err := json.MarshalIndent(tmpl, "", "  ")
	if err != nil {
		return "", false, err
	}
	return string(outBytes), changed, nil
}

// GetProxyOutboundsFromTemplate extracts only proxy outbounds from the template JSON.
func GetProxyOutboundsFromTemplate(templateJSON string) ([]map[string]any, error) {
	var tmpl map[string]any
	if err := json.Unmarshal([]byte(templateJSON), &tmpl); err != nil {
		return nil, err
	}
	result := make([]map[string]any, 0)
	if tmpl == nil {
		return result, nil
	}
	rawArr, ok := tmpl["outbounds"].([]any)
	if !ok {
		return result, nil
	}
	for _, item := range rawArr {
		if obMap, ok := item.(map[string]any); ok {
			if IsProxyOutbound(obMap) {
				result = append(result, obMap)
			}
		}
	}
	return result, nil
}

func numAsInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case string:
		if i, err := strconv.Atoi(n); err == nil {
			return i
		}
	}
	return 0
}
