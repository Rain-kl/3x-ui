package xray

import (
	"encoding/json"
	"strconv"
	"strings"
)

func NativeRateLimitSupported(protocol string) bool {
	switch protocol {
	// FORK(mixed-clients): Managed mixed carries the same authenticated limits as other user protocols.
	case "mixed", "vless", "vmess", "trojan", "shadowsocks", "hysteria", "masque":
		return true
	}
	return false
}

func SupportsNativeRateLimitVersion(version string) bool {
	base, suffix, ok := strings.Cut(strings.TrimPrefix(version, "v"), "-feat.")
	if !ok {
		return false
	}
	n, err := strconv.Atoi(suffix)
	if err != nil || n < 1 {
		return false
	}
	parts := strings.Split(base, ".")
	if len(parts) != 3 {
		return false
	}

	values := make([]int, 3)
	for i, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return false
		}
		values[i] = value
	}
	minimum := []int{26, 10, 10}
	for i, value := range values {
		if value > minimum[i] {
			return true
		}
		if value < minimum[i] {
			return false
		}
	}

	return n >= 2
}

func SetUserRateLimits(user map[string]any, group string, clientMbps, groupMbps int) {
	delete(user, "downlink_speed_limit")
	delete(user, "rate_limit_group")
	delete(user, "downlink_group_speed_limit")
	if clientMbps > 0 {
		user["downlink_speed_limit"] = uint64(clientMbps) * 125000
	}
	if clientMbps > 0 || groupMbps > 0 {
		user["rate_limit_group"] = "inbound:" + group
	}
	if groupMbps > 0 {
		user["downlink_group_speed_limit"] = uint64(groupMbps) * 125000
	}
}

func NativeRateLimitSettings(tag string, raw []byte, clientDefault, inboundLimit int) []byte {
	var settings map[string]any
	if json.Unmarshal(raw, &settings) != nil {
		return raw
	}
	clients, _ := settings["clients"].([]any)
	for _, item := range clients {
		client, ok := item.(map[string]any)
		if !ok {
			continue
		}
		limit := clientDefault
		if explicit, ok := client["downLimit"].(float64); ok && explicit > 0 {
			limit = int(explicit)
		}
		SetUserRateLimits(client, tag, limit, inboundLimit)
	}
	result, err := json.Marshal(settings)
	if err != nil {
		return raw
	}
	return result
}

func rateLimitedUsers(cfg *Config) map[string]string {
	users := make(map[string]string)
	if cfg == nil {
		return users
	}
	for _, ib := range cfg.InboundConfigs {
		var settings struct {
			Clients []map[string]any `json:"clients"`
		}
		if json.Unmarshal(ib.Settings, &settings) != nil {
			continue
		}
		for _, user := range settings.Clients {
			email, _ := user["email"].(string)
			data, _ := json.Marshal([]any{user["uplink_speed_limit"], user["downlink_speed_limit"], user["rate_limit_group"], user["downlink_group_speed_limit"]})
			users[ib.Tag+"\x00"+email] = string(data)
		}
	}
	return users
}

func NativeRateLimitsChanged(oldCfg, newCfg *Config) bool {
	oldUsers := rateLimitedUsers(oldCfg)
	for key, value := range rateLimitedUsers(newCfg) {
		if oldValue, exists := oldUsers[key]; exists && oldValue != value {
			return true
		}
	}
	return false
}

func HasNativeRateLimits(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	for _, ib := range cfg.InboundConfigs {
		var settings struct {
			Clients []map[string]any `json:"clients"`
		}
		if json.Unmarshal(ib.Settings, &settings) != nil {
			continue
		}
		for _, user := range settings.Clients {
			for _, key := range []string{"uplink_speed_limit", "downlink_speed_limit", "downlink_group_speed_limit"} {
				if value, ok := user[key].(float64); ok && value > 0 {
					return true
				}
			}
		}
	}
	return false
}
