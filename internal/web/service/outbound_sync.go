package service

import (
	"bytes"
	"encoding/json"
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
	"warp":        true,
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
	tag = strings.ToLower(strings.TrimSpace(tag))
	if nonProxyTags[tag] || strings.HasPrefix(tag, "warp-") || strings.HasPrefix(tag, "warp_") {
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

// ReplaceProxyOutboundsInTemplate swaps proxy outbounds and keeps system/routing outbounds.
func ReplaceProxyOutboundsInTemplate(templateJSON string, masterProxyOutbounds []map[string]any) (string, bool, error) {
	var tmpl map[string]any
	if err := json.Unmarshal([]byte(templateJSON), &tmpl); err != nil {
		return "", false, err
	}
	if tmpl == nil {
		tmpl = make(map[string]any)
	}

	var systemOutbounds []any
	var existingProxyOutbounds []map[string]any
	if rawArr, ok := tmpl["outbounds"].([]any); ok {
		for _, item := range rawArr {
			if obMap, ok := item.(map[string]any); ok {
				if IsProxyOutbound(obMap) {
					existingProxyOutbounds = append(existingProxyOutbounds, obMap)
				} else {
					systemOutbounds = append(systemOutbounds, item)
				}
			} else {
				systemOutbounds = append(systemOutbounds, item)
			}
		}
	}

	var validMaster []map[string]any
	for _, ob := range masterProxyOutbounds {
		if IsProxyOutbound(ob) {
			validMaster = append(validMaster, ob)
		}
	}

	var changed bool
	oldBytes, _ := json.Marshal(existingProxyOutbounds)
	newBytes, _ := json.Marshal(validMaster)
	changed = !bytes.Equal(oldBytes, newBytes)

	newOutbounds := make([]any, 0, len(systemOutbounds)+len(validMaster))
	newOutbounds = append(newOutbounds, systemOutbounds...)
	for _, ob := range validMaster {
		newOutbounds = append(newOutbounds, ob)
	}
	tmpl["outbounds"] = newOutbounds

	outBytes, err := json.MarshalIndent(tmpl, "", "  ")
	if err != nil {
		return "", false, err
	}
	return string(outBytes), changed, nil
}

// GetOutboundTagsFromTemplate returns outbound tags in template order, skipping
// empty tags and later duplicates. System outbounds are included.
func GetOutboundTagsFromTemplate(templateJSON string) ([]string, error) {
	var tmpl map[string]any
	if err := json.Unmarshal([]byte(templateJSON), &tmpl); err != nil {
		return nil, err
	}
	tags := make([]string, 0)
	if tmpl == nil {
		return tags, nil
	}
	rawArr, ok := tmpl["outbounds"].([]any)
	if !ok {
		return tags, nil
	}
	seen := make(map[string]bool)
	for _, item := range rawArr {
		obMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		tag, _ := obMap["tag"].(string)
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		tags = append(tags, tag)
	}
	return tags, nil
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
