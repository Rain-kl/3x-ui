package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestOutboundSync_IsProxyOutbound(t *testing.T) {
	tests := []struct {
		name string
		ob   map[string]any
		want bool
	}{
		{name: "nil outbound", ob: nil, want: false},
		{name: "empty outbound", ob: map[string]any{}, want: false},
		{name: "vmess proxy", ob: map[string]any{"protocol": "vmess"}, want: true},
		{name: "vless proxy", ob: map[string]any{"protocol": "vless"}, want: true},
		{name: "trojan proxy", ob: map[string]any{"protocol": "trojan"}, want: true},
		{name: "shadowsocks proxy", ob: map[string]any{"protocol": "shadowsocks"}, want: true},
		{name: "hysteria proxy", ob: map[string]any{"protocol": "hysteria"}, want: true},
		{name: "hysteria2 proxy", ob: map[string]any{"protocol": "hysteria2"}, want: true},
		{name: "wireguard proxy", ob: map[string]any{"protocol": "wireguard"}, want: true},
		{name: "socks proxy", ob: map[string]any{"protocol": "socks"}, want: true},
		{name: "http proxy", ob: map[string]any{"protocol": "http"}, want: true},
		{name: "case insensitive protocol", ob: map[string]any{"protocol": "VLess"}, want: true},
		{name: "freedom protocol excluded", ob: map[string]any{"protocol": "freedom"}, want: false},
		{name: "blackhole protocol excluded", ob: map[string]any{"protocol": "blackhole"}, want: false},
		{name: "dns protocol excluded", ob: map[string]any{"protocol": "dns"}, want: false},
		{name: "tag direct excluded", ob: map[string]any{"protocol": "vless", "tag": "direct"}, want: false},
		{name: "tag blocked excluded", ob: map[string]any{"protocol": "vless", "tag": "blocked"}, want: false},
		{name: "tag api excluded", ob: map[string]any{"protocol": "vmess", "tag": "api"}, want: false},
		{name: "tag metrics_out excluded", ob: map[string]any{"protocol": "trojan", "tag": "metrics_out"}, want: false},
		{name: "case insensitive tag", ob: map[string]any{"protocol": "vless", "tag": "DIRECT"}, want: false},
		{name: "arbitrary tag allowed", ob: map[string]any{"protocol": "vless", "tag": "proxy-1"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsProxyOutbound(tt.ob)
			if got != tt.want {
				t.Fatalf("IsProxyOutbound() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOutboundSync_ExtractOutboundEndpoints(t *testing.T) {
	tests := []struct {
		name string
		ob   map[string]any
		want []string
	}{
		{
			name: "vless vnext multiple servers",
			ob: map[string]any{
				"protocol": "vless",
				"settings": map[string]any{
					"vnext": []any{
						map[string]any{"address": "A.Example.com", "port": float64(443)},
						map[string]any{"address": "b.example.com", "port": float64(8443)},
					},
				},
			},
			want: []string{"a.example.com:443", "b.example.com:8443"},
		},
		{
			name: "vmess vnext single server",
			ob: map[string]any{
				"protocol": "vmess",
				"settings": map[string]any{
					"vnext": []any{
						map[string]any{"address": "vmess.example.com", "port": float64(10443)},
					},
				},
			},
			want: []string{"vmess.example.com:10443"},
		},
		{
			name: "trojan servers array",
			ob: map[string]any{
				"protocol": "trojan",
				"settings": map[string]any{
					"servers": []any{
						map[string]any{"address": "trojan.example.com", "port": float64(443)},
					},
				},
			},
			want: []string{"trojan.example.com:443"},
		},
		{
			name: "shadowsocks servers array",
			ob: map[string]any{
				"protocol": "shadowsocks",
				"settings": map[string]any{
					"servers": []any{
						map[string]any{"address": "ss.example.com", "port": float64(8388)},
					},
				},
			},
			want: []string{"ss.example.com:8388"},
		},
		{
			name: "socks servers array",
			ob: map[string]any{
				"protocol": "socks",
				"settings": map[string]any{
					"servers": []any{
						map[string]any{"address": "socks.example.com", "port": float64(1080)},
					},
				},
			},
			want: []string{"socks.example.com:1080"},
		},
		{
			name: "http servers array",
			ob: map[string]any{
				"protocol": "http",
				"settings": map[string]any{
					"servers": []any{
						map[string]any{"address": "http.example.com", "port": float64(8080)},
					},
				},
			},
			want: []string{"http.example.com:8080"},
		},
		{
			name: "hysteria flat address and port",
			ob: map[string]any{
				"protocol": "hysteria",
				"settings": map[string]any{
					"address": "hy.example.com",
					"port":    float64(443),
				},
			},
			want: []string{"hy.example.com:443"},
		},
		{
			name: "wireguard peers endpoint string",
			ob: map[string]any{
				"protocol": "wireguard",
				"settings": map[string]any{
					"peers": []any{
						map[string]any{"endpoint": "WG.Example.com:51820"},
					},
				},
			},
			want: []string{"wg.example.com:51820"},
		},
		{
			name: "wireguard peers address and port",
			ob: map[string]any{
				"protocol": "wireguard",
				"settings": map[string]any{
					"peers": []any{
						map[string]any{"address": "wg2.example.com", "port": float64(51821)},
					},
				},
			},
			want: []string{"wg2.example.com:51821"},
		},
		{
			name: "invalid vnext falls back to flat address port",
			ob: map[string]any{
				"protocol": "vless",
				"settings": map[string]any{
					"vnext":   []any{map[string]any{"address": "missing-port.com"}},
					"address": "fallback.example.com",
					"port":    float64(2053),
				},
			},
			want: []string{"fallback.example.com:2053"},
		},
		{
			name: "vless vnext ipv6",
			ob: map[string]any{
				"protocol": "vless",
				"settings": map[string]any{
					"vnext": []any{
						map[string]any{"address": "2001:db8::1", "port": float64(443)},
					},
				},
			},
			want: []string{"[2001:db8::1]:443"},
		},
		{
			name: "wireguard ipv6 endpoint",
			ob: map[string]any{
				"protocol": "wireguard",
				"settings": map[string]any{
					"peers": []any{
						map[string]any{"endpoint": "[2001:db8::2]:51820"},
					},
				},
			},
			want: []string{"[2001:db8::2]:51820"},
		},
		{
			name: "non-proxy outbound returns nil",
			ob: map[string]any{
				"protocol": "freedom",
				"settings": map[string]any{"domainStrategy": "AsIs"},
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractOutboundEndpoints(tt.ob)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ExtractOutboundEndpoints() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOutboundSync_ExtractOutboundKeys(t *testing.T) {
	tests := []struct {
		name string
		ob   map[string]any
		want []string
	}{
		{
			name: "vless key",
			ob: map[string]any{
				"protocol": "VLess",
				"settings": map[string]any{
					"address": "VLESS.EXAMPLE.COM",
					"port":    float64(443),
				},
			},
			want: []string{"vless://vless.example.com:443"},
		},
		{
			name: "wireguard key",
			ob: map[string]any{
				"protocol": "wireguard",
				"settings": map[string]any{
					"peers": []any{
						map[string]any{"endpoint": "162.159.192.1:2408"},
					},
				},
			},
			want: []string{"wireguard://162.159.192.1:2408"},
		},
		{
			name: "freedom returns nil",
			ob: map[string]any{
				"protocol": "freedom",
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractOutboundKeys(tt.ob)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ExtractOutboundKeys() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOutboundSync_MergeProxyOutboundsIntoTemplate(t *testing.T) {
	baseTemplate := `{
  "log": {"loglevel": "warning"},
  "outbounds": [
    {"protocol": "freedom", "tag": "direct"},
    {"protocol": "blackhole", "tag": "blocked"},
    {
      "protocol": "vless",
      "tag": "proxy-vless",
      "settings": {
        "vnext": [{"address": "existing.example.com", "port": 443}]
      }
    }
  ]
}`

	incoming := []map[string]any{
		// Duplicate: already exists in template
		{
			"protocol": "vless",
			"tag":      "dup-vless",
			"settings": map[string]any{
				"address": "EXISTING.EXAMPLE.COM",
				"port":    float64(443),
			},
		},
		// New outbound 1
		{
			"protocol": "trojan",
			"tag":      "new-trojan",
			"settings": map[string]any{
				"servers": []any{
					map[string]any{"address": "new-trojan.com", "port": float64(443)},
				},
			},
		},
		// Duplicate of new outbound 1 in same batch
		{
			"protocol": "trojan",
			"tag":      "dup-batch-trojan",
			"settings": map[string]any{
				"address": "new-trojan.com",
				"port":    float64(443),
			},
		},
		// Non-proxy outbound: should be ignored
		{
			"protocol": "freedom",
			"tag":      "custom-freedom",
		},
	}

	updated, count, err := MergeProxyOutboundsIntoTemplate(baseTemplate, incoming)
	if err != nil {
		t.Fatalf("MergeProxyOutboundsIntoTemplate failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("addedCount = %d, want 1", count)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(updated), &parsed); err != nil {
		t.Fatalf("updated template is not valid JSON: %v", err)
	}

	outbounds := parsed["outbounds"].([]any)
	if len(outbounds) != 4 {
		t.Fatalf("outbounds count = %d, want 4 (2 system + 1 existing proxy + 1 new proxy)", len(outbounds))
	}
	if !strings.Contains(updated, "new-trojan") {
		t.Fatalf("updated template missing new-trojan outbound")
	}
}

func TestOutboundSync_ReplaceProxyOutboundsInTemplate(t *testing.T) {
	baseTemplate := `{
  "outbounds": [
    {"protocol": "freedom", "tag": "direct"},
    {"protocol": "blackhole", "tag": "blocked"},
    {
      "protocol": "vless",
      "tag": "old-proxy",
      "settings": {"address": "old.com", "port": 443}
    }
  ]
}`

	masterOutbounds := []map[string]any{
		{
			"protocol": "trojan",
			"tag":      "master-trojan",
			"settings": map[string]any{
				"servers": []any{
					map[string]any{"address": "master.com", "port": float64(443)},
				},
			},
		},
	}

	updated, changed, err := ReplaceProxyOutboundsInTemplate(baseTemplate, masterOutbounds)
	if err != nil {
		t.Fatalf("ReplaceProxyOutboundsInTemplate failed: %v", err)
	}
	if !changed {
		t.Fatalf("changed = false, want true")
	}

	proxies, err := GetProxyOutboundsFromTemplate(updated)
	if err != nil {
		t.Fatalf("GetProxyOutboundsFromTemplate failed: %v", err)
	}
	if len(proxies) != 1 || proxies[0]["tag"] != "master-trojan" {
		t.Fatalf("proxies = %+v, want [master-trojan]", proxies)
	}

	// Idempotency: replacing with the exact same master outbounds should return changed = false
	updated2, changed2, err := ReplaceProxyOutboundsInTemplate(updated, masterOutbounds)
	if err != nil {
		t.Fatalf("second replace failed: %v", err)
	}
	if changed2 {
		t.Fatalf("second replace changed = true, want false")
	}
	if updated2 != updated {
		t.Fatalf("second replace template output mismatch")
	}
}

func TestOutboundSync_GetProxyOutboundsFromTemplate(t *testing.T) {
	template := `{
  "outbounds": [
    {"protocol": "freedom", "tag": "direct"},
    {"protocol": "blackhole", "tag": "blocked"},
    {"protocol": "vless", "tag": "proxy-1", "settings": {"address": "p1.com", "port": 443}},
    {"protocol": "trojan", "tag": "proxy-2", "settings": {"address": "p2.com", "port": 443}},
    {"protocol": "vmess", "tag": "api"}
  ]
}`

	proxies, err := GetProxyOutboundsFromTemplate(template)
	if err != nil {
		t.Fatalf("GetProxyOutboundsFromTemplate failed: %v", err)
	}
	if len(proxies) != 2 {
		t.Fatalf("proxy count = %d, want 2", len(proxies))
	}
	if proxies[0]["tag"] != "proxy-1" || proxies[1]["tag"] != "proxy-2" {
		t.Fatalf("unexpected proxies: %+v", proxies)
	}
}

func TestOutboundSync_MergeProxyOutboundsUniqueTags(t *testing.T) {
	baseTemplate := `{
  "outbounds": [
    {"protocol": "freedom", "tag": "direct"},
    {"protocol": "vless", "tag": "proxy-1", "settings": {"address": "p1.com", "port": 443}}
  ]
}`

	incoming := []map[string]any{
		// Collides with existing template tag "proxy-1"
		{
			"protocol": "trojan",
			"tag":      "proxy-1",
			"settings": map[string]any{"address": "trojan.com", "port": float64(443)},
		},
		// Empty tag: should default to protocol "vmess"
		{
			"protocol": "vmess",
			"settings": map[string]any{"address": "vmess.com", "port": float64(443)},
		},
		// "custom" tag
		{
			"protocol": "socks",
			"tag":      "custom",
			"settings": map[string]any{"address": "socks1.com", "port": float64(1080)},
		},
		// Collides with preceding incoming item in the same batch
		{
			"protocol": "socks",
			"tag":      "custom",
			"settings": map[string]any{"address": "socks2.com", "port": float64(1080)},
		},
	}

	updated, count, err := MergeProxyOutboundsIntoTemplate(baseTemplate, incoming)
	if err != nil {
		t.Fatalf("MergeProxyOutboundsIntoTemplate failed: %v", err)
	}
	if count != 4 {
		t.Fatalf("addedCount = %d, want 4", count)
	}

	proxies, err := GetProxyOutboundsFromTemplate(updated)
	if err != nil {
		t.Fatalf("GetProxyOutboundsFromTemplate failed: %v", err)
	}
	if len(proxies) != 5 {
		t.Fatalf("total proxies = %d, want 5 (1 existing + 4 merged)", len(proxies))
	}

	expectedTags := []string{"proxy-1", "proxy-1-2", "vmess", "custom", "custom-2"}
	for i, exp := range expectedTags {
		if proxies[i]["tag"] != exp {
			t.Errorf("proxy[%d] tag = %v, want %v", i, proxies[i]["tag"], exp)
		}
	}
}
