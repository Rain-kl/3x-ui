package xray

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

const statIdentityPrefix = "xui-stat:"

func ScopedEmail(tag, email string) string {
	if tag == "" || email == "" {
		return email
	}
	if _, _, ok := ParseScopedEmail(email); ok {
		return email
	}
	return statIdentityPrefix + base64.RawURLEncoding.EncodeToString([]byte(tag)) + ":" + base64.RawURLEncoding.EncodeToString([]byte(email))
}

func ParseScopedEmail(value string) (string, string, bool) {
	if !strings.HasPrefix(value, statIdentityPrefix) {
		return "", value, false
	}
	parts := strings.Split(strings.TrimPrefix(value, statIdentityPrefix), ":")
	if len(parts) != 2 {
		return "", value, false
	}
	tag, e1 := base64.RawURLEncoding.DecodeString(parts[0])
	email, e2 := base64.RawURLEncoding.DecodeString(parts[1])
	if e1 != nil || e2 != nil || len(tag) == 0 || len(email) == 0 {
		return "", value, false
	}
	return string(tag), string(email), true
}

func ScopeInboundSettings(tag string, settings []byte, protocol ...string) []byte {
	if len(protocol) > 0 && protocol[0] == "masque" {
		return settings
	}
	var obj map[string]any
	if json.Unmarshal(settings, &obj) != nil {
		return settings
	}
	clients, _ := obj["clients"].([]any)
	for _, item := range clients {
		if client, ok := item.(map[string]any); ok {
			if email, ok := client["email"].(string); ok {
				client["email"] = ScopedEmail(tag, email)
			}
		}
	}
	result, err := json.Marshal(obj)
	if err != nil {
		return settings
	}
	return result
}

func ScopeRoutingUsers(cfg *Config) {
	var routing map[string]any
	if json.Unmarshal(cfg.RouterConfig, &routing) != nil {
		return
	}
	rules, _ := routing["rules"].([]any)
	for _, item := range rules {
		rule, ok := item.(map[string]any)
		if !ok {
			continue
		}
		users, ok := rule["user"].([]any)
		if !ok {
			continue
		}
		expanded := append([]any{}, users...)
		for _, u := range users {
			if email, ok := u.(string); ok {
				for _, ib := range cfg.InboundConfigs {
					expanded = append(expanded, ScopedEmail(ib.Tag, email))
				}
			}
		}
		rule["user"] = expanded
	}
	if result, err := json.Marshal(routing); err == nil {
		cfg.RouterConfig = result
	}
}

func copyScopedUser(tag string, user map[string]any) map[string]any {
	result := make(map[string]any, len(user))
	for k, v := range user {
		result[k] = v
	}
	if email, ok := result["email"].(string); ok {
		result["email"] = ScopedEmail(tag, email)
	}
	return result
}

func scopeInboundConfig(raw []byte) []byte {
	var ib InboundConfig
	if json.Unmarshal(raw, &ib) != nil {
		return raw
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return raw
	}
	obj["settings"] = ScopeInboundSettings(ib.Tag, ib.Settings, ib.Protocol)
	result, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return result
}
