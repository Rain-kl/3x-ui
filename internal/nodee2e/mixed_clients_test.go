package nodee2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/net/proxy"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func prepareMixedCore(t *testing.T, p *panel, core string) {
	t.Helper()
	if err := os.Symlink(core, filepath.Join(p.dir, "bin", xray.GetBinaryName())); err != nil {
		t.Fatal(err)
	}
	sharedDBMu.Lock()
	defer sharedDBMu.Unlock()
	if err := database.InitDB(filepath.Join(p.dir, "db", "x-ui.db")); err != nil {
		t.Fatal(err)
	}
	defer database.CloseDB()
	template, err := (&service.SettingService{}).GetXrayConfigTemplate()
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(template), &cfg); err != nil {
		t.Fatal(err)
	}
	cfg["inbounds"].([]any)[0].(map[string]any)["port"] = freePort(t)
	delete(cfg, "metrics")
	cfg["outbounds"] = []any{map[string]any{"tag": "direct", "protocol": "freedom", "settings": map[string]any{}}, map[string]any{"tag": "blocked", "protocol": "blackhole", "settings": map[string]any{}}}
	cfg["routing"] = map[string]any{"domainStrategy": "AsIs", "rules": []any{map[string]any{"type": "field", "inboundTag": []string{"api"}, "outboundTag": "api"}}}
	raw, _ := json.Marshal(cfg)
	if err := database.GetDB().Where("key = ?", "xrayTemplateConfig").Delete(&model.Setting{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Create(&model.Setting{Key: "xrayTemplateConfig", Value: string(raw)}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if p.cmd != nil && p.cmd.Process != nil {
			_ = p.cmd.Process.Signal(os.Interrupt)
			done := make(chan struct{})
			go func() { _, _ = p.cmd.Process.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				_ = p.cmd.Process.Kill()
				<-done
			}
			p.cmd = nil
			if t.Failed() {
				if logs, err := os.ReadFile(filepath.Join(p.dir, "stdout.log")); err == nil {
					t.Logf("%s logs:\n%s", p.name, logs)
				}
			}
		}
	})
}

func probeMixedNode(port int, protocol, password, target string) error {
	endpoint := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	tr := &http.Transport{DisableKeepAlives: true}
	defer tr.CloseIdleConnections()
	if protocol == "http" {
		tr.Proxy = http.ProxyURL(&url.URL{Scheme: "http", Host: endpoint, User: url.UserPassword("mixed-login", password)})
	} else {
		dial, err := proxy.SOCKS5("tcp", endpoint, &proxy.Auth{User: "mixed-login", Password: password}, &net.Dialer{Timeout: time.Second})
		if err != nil {
			return err
		}
		tr.DialContext = func(ctx context.Context, n, a string) (net.Conn, error) {
			return dial.(proxy.ContextDialer).DialContext(ctx, n, a)
		}
	}
	resp, err := (&http.Client{Transport: tr, Timeout: 2 * time.Second}).Get(target)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if string(body) != "node-mixed-ok" {
		return fmt.Errorf("response %d: %q", resp.StatusCode, body)
	}
	return nil
}

func TestMixedMasterChildLive(t *testing.T) {
	bin := panelBinary(t)
	core := os.Getenv("XRAY_E2E_BINARY")
	if core == "" {
		t.Skip("XRAY_E2E_BINARY required")
	}
	master := newPanel(t, bin, "mixed-master")
	node := newPanel(t, bin, "mixed-node")
	prepareMixedCore(t, master, core)
	prepareMixedCore(t, node, core)
	linkToken := node.mintToken("master-link", "node-sync")
	master.start()
	node.start()
	var n struct {
		Id int `json:"id"`
	}
	raw := master.call(http.MethodPost, "/panel/api/nodes/add", map[string]any{"name": "mixed-node", "scheme": "http", "address": "127.0.0.1", "port": node.port, "basePath": "/", "apiToken": linkToken, "enable": true, "allowPrivateAddress": true})
	if err := json.Unmarshal(raw, &n); err != nil || n.Id == 0 {
		t.Fatalf("node: %s %v", raw, err)
	}
	eventually(t, settleTimeout, "node reports mixed-capable core", func() (bool, string) {
		var nodes []struct {
			Id          int    `json:"id"`
			XrayVersion string `json:"xrayVersion"`
		}
		raw := master.call(http.MethodGet, "/panel/api/nodes/list", nil)
		if err := json.Unmarshal(raw, &nodes); err != nil {
			return false, err.Error()
		}
		for _, candidate := range nodes {
			if candidate.Id == n.Id && xray.SupportsMixedClientVersion(candidate.XrayVersion) {
				return true, ""
			}
		}
		return false, string(raw)
	})
	port := freePort(t)
	var ib inboundView
	raw = master.call(http.MethodPost, "/panel/api/inbounds/add", map[string]any{"remark": "managed-mixed", "enable": true, "listen": "127.0.0.1", "port": port, "protocol": "mixed", "nodeId": n.Id, "settings": `{"auth":"password","clients":[{"email":"mixed-client","username":"mixed-login","password":"initial","enable":true}]}`, "streamSettings": `{"network":"tcp","security":"none"}`, "sniffing": `{"enabled":false}`})
	if err := json.Unmarshal(raw, &ib); err != nil || ib.Id == 0 {
		t.Fatalf("mixed add: %s %v", raw, err)
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "node-mixed-ok") }))
	defer target.Close()
	for _, protocol := range []string{"http", "socks5"} {
		eventually(t, settleTimeout, "master-deployed "+protocol, func() (bool, string) {
			err := probeMixedNode(port, protocol, "initial", target.URL)
			return err == nil, fmt.Sprint(err)
		})
	}
	mib, _ := (&pair{master: master, nodeID: n.Id}).masterInbound(port)
	client := mib.client("mixed-client")
	if client == nil {
		t.Fatal("master lost mixed client")
	}
	client["password"] = "changed"
	master.call(http.MethodPost, "/panel/api/clients/update/mixed-client", client)
	for _, protocol := range []string{"http", "socks5"} {
		eventually(t, settleTimeout, "changed credentials "+protocol, func() (bool, string) {
			err := probeMixedNode(port, protocol, "changed", target.URL)
			child, found := node.inboundOnPort(port)
			return err == nil, fmt.Sprintf("%v childFound=%t settings=%s", err, found, child.Settings)
		})
		if err := probeMixedNode(port, protocol, "initial", target.URL); err == nil {
			t.Fatalf("old password accepted by %s", protocol)
		}
	}
	client["enable"] = false
	master.call(http.MethodPost, "/panel/api/clients/update/mixed-client", client)
	for _, protocol := range []string{"http", "socks5"} {
		eventually(t, settleTimeout, "disable "+protocol, func() (bool, string) {
			err := probeMixedNode(port, protocol, "changed", target.URL)
			return err != nil, "still accepting disabled client"
		})
	}
	client["enable"] = true
	master.call(http.MethodPost, "/panel/api/clients/update/mixed-client", client)
	for _, protocol := range []string{"http", "socks5"} {
		eventually(t, settleTimeout, "re-enable "+protocol, func() (bool, string) {
			err := probeMixedNode(port, protocol, "changed", target.URL)
			return err == nil, fmt.Sprint(err)
		})
	}
	client["expiryTime"] = time.Now().Add(-time.Minute).UnixMilli()
	master.call(http.MethodPost, "/panel/api/clients/update/mixed-client", client)
	for _, protocol := range []string{"http", "socks5"} {
		eventually(t, settleTimeout, "expiry "+protocol, func() (bool, string) {
			err := probeMixedNode(port, protocol, "changed", target.URL)
			return err != nil, "still accepting expired client"
		})
	}
	client["expiryTime"] = 0
	client["enable"] = true
	master.call(http.MethodPost, "/panel/api/clients/update/mixed-client", client)
	for _, protocol := range []string{"http", "socks5"} {
		eventually(t, settleTimeout, "renew "+protocol, func() (bool, string) {
			err := probeMixedNode(port, protocol, "changed", target.URL)
			return err == nil, fmt.Sprint(err)
		})
	}
	master.call(http.MethodPost, "/panel/api/clients/mixed-client/detach", map[string]any{"inboundIds": []int{ib.Id}})
	for _, protocol := range []string{"http", "socks5"} {
		eventually(t, settleTimeout, "detach "+protocol, func() (bool, string) {
			err := probeMixedNode(port, protocol, "changed", target.URL)
			return err != nil, "still accepting detached client"
		})
	}
}
