package xray

import (
	"encoding/json"
	"testing"
)

func TestNativeRateLimitSettings(t *testing.T) {
	const tag = "vless-in"
	input := []byte(`{"clients":[{"email":"default"},{"email":"override","downLimit":7},{"email":"unlimited","downLimit":0}],"decryption":"none"}`)
	got := NativeRateLimitSettings(tag, input, 3, 12)
	var settings map[string]any
	if err := json.Unmarshal(got, &settings); err != nil {
		t.Fatalf("unmarshal emitted settings: %v", err)
	}
	clients := settings["clients"].([]any)
	want := []struct {
		email       string
		clientLimit uint64
	}{
		{"default", 375000},
		{"override", 875000},
		{"unlimited", 375000},
	}
	for i, tc := range want {
		user := clients[i].(map[string]any)
		if user["email"] != tc.email || user["downlink_speed_limit"] != float64(tc.clientLimit) {
			t.Errorf("client %d = %v, want email %q and downlink_speed_limit %d", i, user, tc.email, tc.clientLimit)
		}
		if user["rate_limit_group"] != "inbound:"+tag || user["downlink_group_speed_limit"] != float64(1500000) {
			t.Errorf("client %q group limits = (%v, %v), want (%q, 1500000)", tc.email, user["rate_limit_group"], user["downlink_group_speed_limit"], "inbound:"+tag)
		}
	}
	if settings["decryption"] != "none" {
		t.Fatalf("unrelated settings field lost: %v", settings)
	}
}

func TestNativeRateLimitSettingsClearsStaleFields(t *testing.T) {
	got := NativeRateLimitSettings("in", []byte(`{"clients":[{"email":"e","downlink_speed_limit":99,"rate_limit_group":"old","downlink_group_speed_limit":100}]}`), 0, 0)
	var settings struct {
		Clients []map[string]any `json:"clients"`
	}
	if err := json.Unmarshal(got, &settings); err != nil {
		t.Fatalf("unmarshal emitted settings: %v", err)
	}
	if len(settings.Clients) != 1 {
		t.Fatalf("clients = %v, want one client", settings.Clients)
	}
	for _, key := range []string{"downlink_speed_limit", "rate_limit_group", "downlink_group_speed_limit"} {
		if _, exists := settings.Clients[0][key]; exists {
			t.Errorf("stale %s remains in emitted client: %v", key, settings.Clients[0])
		}
	}
}

func TestNativeRateLimitsChangedForExistingSessions(t *testing.T) {
	config := func(settings string) *Config {
		return &Config{InboundConfigs: []InboundConfig{{Tag: "in", Settings: []byte(settings)}}}
	}
	tests := []struct {
		name string
		old  string
		new  string
		want bool
	}{
		{"unchanged cap", `{"clients":[{"email":"e","downlink_speed_limit":100}]}`, `{"clients":[{"email":"e","downlink_speed_limit":100}]}`, false},
		{"existing cap changed", `{"clients":[{"email":"e","downlink_speed_limit":100}]}`, `{"clients":[{"email":"e","downlink_speed_limit":200}]}`, true},
		{"existing cap cleared", `{"clients":[{"email":"e","downlink_speed_limit":100}]}`, `{"clients":[{"email":"e"}]}`, true},
		{"new client does not affect existing sessions", `{"clients":[{"email":"e","downlink_speed_limit":100}]}`, `{"clients":[{"email":"e","downlink_speed_limit":100},{"email":"new","downlink_speed_limit":200}]}`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NativeRateLimitsChanged(config(tc.old), config(tc.new)); got != tc.want {
				t.Fatalf("NativeRateLimitsChanged() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSupportsNativeRateLimitVersion(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"v26.10.10-feat.1", false},
		{"v26.10.10-feat.2", true},
		{"v27.0.0-feat.1", true},
		{"v26.10.9-feat.99", false},
		{"v26.10.10", false},
		{"v26.10.10-audit.2", false},
	}
	for _, tc := range tests {
		t.Run(tc.version, func(t *testing.T) {
			if got := SupportsNativeRateLimitVersion(tc.version); got != tc.want {
				t.Fatalf("SupportsNativeRateLimitVersion(%q) = %v, want %v", tc.version, got, tc.want)
			}
		})
	}
}
