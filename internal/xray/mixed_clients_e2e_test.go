package xray

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/proxy"
)

func mixedProbe(port int, protocol, name, pass, target string) error {
	tr := &http.Transport{DisableKeepAlives: true, TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	defer tr.CloseIdleConnections()
	endpoint := fmt.Sprintf("127.0.0.1:%d", port)
	if protocol == "http" {
		u := &url.URL{Scheme: "http", Host: endpoint}
		if name != "" {
			u.User = url.UserPassword(name, pass)
		}
		tr.Proxy = http.ProxyURL(u)
	} else {
		var auth *proxy.Auth
		if name != "" {
			auth = &proxy.Auth{User: name, Password: pass}
		}
		dialer, err := proxy.SOCKS5("tcp", endpoint, auth, &net.Dialer{Timeout: time.Second})
		if err != nil {
			return err
		}
		tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialer.(proxy.ContextDialer).DialContext(ctx, network, address)
		}
	}
	client := &http.Client{Transport: tr, Timeout: 3 * time.Second}
	resp, err := client.Get(target)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK || string(body) != "mixed-ok" {
		return fmt.Errorf("proxy response %d: %q", resp.StatusCode, body)
	}
	return nil
}

func TestMixedClientsE2E(t *testing.T) {
	port := freePort(t)
	c := startE2ECore(t, []any{map[string]any{"tag": "mixed-e2e", "listen": "127.0.0.1", "port": port, "protocol": "mixed", "settings": map[string]any{"auth": "password", "clients": []any{}}}})
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "mixed-ok") }))
	defer target.Close()
	tlsTarget := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "mixed-ok") }))
	defer tlsTarget.Close()
	probe := func(protocol, name, pass string, allowed bool) {
		t.Helper()
		for _, address := range []string{target.URL, tlsTarget.URL} {
			err := mixedProbe(port, protocol, name, pass, address)
			if (err == nil) != allowed {
				t.Fatalf("%s allowed=%t name=%q target=%s: %v", protocol, allowed, name, address, err)
			}
		}
	}
	for _, proto := range []string{"http", "socks5"} {
		probe(proto, "", "", false)
	}
	if _, _, err := c.api.GetTraffic(); err != nil {
		t.Fatal(err)
	}
	user := panelUser("client@example.test", map[string]any{"username": "login", "password": "pass"})
	if err := c.api.AddUser("mixed", "mixed-e2e", user); err != nil {
		t.Fatal(err)
	}
	for _, proto := range []string{"http", "socks5"} {
		probe(proto, "login", "pass", true)
		probe(proto, "login", "wrong", false)
		probe(proto, "", "", false)
	}
	_, stats, err := c.api.GetTraffic()
	if err != nil {
		t.Fatal(err)
	}
	var metered bool
	for _, st := range stats {
		if st.Email == "client@example.test" && st.InboundTag == "mixed-e2e" && st.Down > 0 {
			metered = true
		}
	}
	if !metered {
		t.Fatalf("missing client billing: %+v", stats)
	}
	if err := c.api.RemoveUser("mixed-e2e", "client@example.test"); err != nil {
		t.Fatal(err)
	}
	for _, proto := range []string{"http", "socks5"} {
		probe(proto, "login", "pass", false)
		probe(proto, "", "", false)
	}
	user["password"] = "new-pass"
	if err := c.api.AddUser("mixed", "mixed-e2e", user); err != nil {
		t.Fatal(err)
	}
	for _, proto := range []string{"http", "socks5"} {
		probe(proto, "login", "pass", false)
		probe(proto, "login", "new-pass", true)
	}
	rules := []byte(`{"domainStrategy":"AsIs","rules":[{"type":"field","inboundTag":["api"],"outboundTag":"api"},{"type":"field","inboundTag":["mixed-e2e"],"sourceIP":["127.0.0.0/8"],"outboundTag":"blocked"},{"type":"field","network":"tcp,udp","outboundTag":"direct"}]}`)
	if err := c.api.ApplyRoutingConfig(rules); err != nil {
		t.Fatal(err)
	}
	for _, proto := range []string{"http", "socks5"} {
		probe(proto, "login", "new-pass", false)
	}
	if !c.alive() {
		t.Fatal("core stopped")
	}
}

func TestMixedHotDiffPreservesListener(t *testing.T) {
	cfg := func(password string) *Config {
		return &Config{InboundConfigs: []InboundConfig{{Tag: "mixed", Protocol: "mixed", Port: 12345, Settings: []byte(fmt.Sprintf(`{"auth":"password","clients":[{"user":"login","pass":%q,"email":"user"}]}`, password))}}}
	}
	diff, ok := ComputeHotDiff(cfg("old"), cfg("new"))
	if !ok || len(diff.RemovedInboundTags) > 0 || len(diff.RemovedUsers) != 1 || len(diff.AddedUsers) != 1 {
		t.Fatalf("mixed account edit did not hot-diff: %#v, %t", diff, ok)
	}
	account, err := buildUserAccount("mixed", diff.AddedUsers[0].User)
	if err != nil || account == nil {
		t.Fatalf("mixed wire account: %v", err)
	}
	if strings.Contains(string(account.Value), "old") {
		t.Fatal("old password survived edit")
	}
}

func TestManagedMixedRejectsOldRuntime(t *testing.T) {
	cfg := &Config{InboundConfigs: []InboundConfig{{Protocol: "mixed", Settings: []byte(`{"auth":"password","clients":[]}`)}}}
	for _, version := range []string{"Unknown", "26.10.10", "26.10.10-feat.3"} {
		if err := ValidateMixedClientVersion(cfg, version); err == nil || !strings.Contains(err.Error(), MixedClientsMinimumVersion) {
			t.Fatalf("old runtime %q accepted: %v", version, err)
		}
	}
	if err := ValidateMixedClientVersion(cfg, "26.10.10-feat.4"); err != nil {
		t.Fatal(err)
	}
}
