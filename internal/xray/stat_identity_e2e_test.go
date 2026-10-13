package xray

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestScopedTraffic_E2E(t *testing.T) {
	bin := os.Getenv("XRAY_E2E_BINARY")
	if bin == "" {
		t.Skip("set XRAY_E2E_BINARY to run real traffic accounting")
	}
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = echo.Close() })
	go func() {
		for {
			conn, err := echo.Accept()
			if err != nil {
				return
			}
			go func() { defer conn.Close(); _, _ = io.Copy(conn, conn) }()
		}
	}()
	apiPort := freePort(t)
	cfg := map[string]any{
		"log":       map[string]any{"loglevel": "warning"},
		"api":       map[string]any{"tag": "api", "services": []string{"HandlerService", "StatsService"}},
		"stats":     map[string]any{},
		"policy":    map[string]any{"system": map[string]any{"statsInboundUplink": true, "statsInboundDownlink": true}, "levels": map[string]any{"0": map[string]any{"statsUserUplink": true, "statsUserDownlink": true}}},
		"inbounds":  []any{map[string]any{"tag": "api", "listen": "127.0.0.1", "port": apiPort, "protocol": "tunnel", "settings": map[string]any{"rewriteAddress": "127.0.0.1"}}},
		"outbounds": []any{map[string]any{"tag": "blocked", "protocol": "blackhole"}, map[string]any{"tag": "direct", "protocol": "freedom", "settings": map[string]any{"finalRules": []any{map[string]any{"action": "allow", "ip": []string{"127.0.0.1"}}}}}},
		"routing":   map[string]any{"rules": []any{map[string]any{"type": "field", "inboundTag": []string{"api"}, "outboundTag": "api"}, map[string]any{"type": "field", "user": []string{"shared"}, "outboundTag": "direct"}}},
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var routingConfig Config
	if err := json.Unmarshal(raw, &routingConfig); err != nil {
		t.Fatal(err)
	}
	routingConfig.InboundConfigs = append(routingConfig.InboundConfigs, InboundConfig{Tag: "meter-0"}, InboundConfig{Tag: "meter-1"})
	ScopeRoutingUsers(&routingConfig)
	cfg["routing"] = json.RawMessage(routingConfig.RouterConfig)
	raw, err = json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "run", "-c", path)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	ready := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", apiPort), 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			ready = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		t.Fatal("core did not start")
	}
	api := &XrayAPI{}
	if err := api.Init(apiPort); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { api.Close() })
	ports := []int{freePort(t), freePort(t)}
	for i, port := range ports {
		ib := []byte(fmt.Sprintf(`{"tag":"meter-%d","listen":"127.0.0.1","port":%d,"protocol":"vless","settings":{"decryption":"none","clients":[{"id":"11111111-1111-1111-1111-111111111111","email":"shared"}]}}`, i, port))
		if err := api.AddInbound(ib); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := api.GetTraffic(); err != nil {
		t.Fatal(err)
	}
	destPort := echo.Addr().(*net.TCPAddr).Port
	for i, port := range ports {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
		header := append([]byte{0}, bytes.Repeat([]byte{0x11}, 16)...)
		header = append(header, 0, 1, byte(destPort>>8), byte(destPort), 1, 127, 0, 0, 1)
		payload := bytes.Repeat([]byte{byte(i + 1)}, (i+1)*1024)
		if _, err := conn.Write(append(header, payload...)); err != nil {
			t.Fatal(err)
		}
		response := make([]byte, len(payload)+2)
		if _, err := io.ReadFull(conn, response); err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
		if !bytes.Equal(response[2:], payload) {
			t.Fatal("echo payload mismatch")
		}
	}
	_, rows, err := api.GetTraffic()
	if err != nil {
		t.Fatal(err)
	}
	totals := map[string][2]int64{}
	for _, row := range rows {
		if row.Email == "shared" {
			totals[row.InboundTag] = [2]int64{row.Up, row.Down}
		}
	}
	for i, size := range []int64{1024, 2048} {
		if got := totals[fmt.Sprintf("meter-%d", i)]; got != [2]int64{size, size} {
			t.Fatalf("inbound %d=%v, want %d/%d", i, got, size, size)
		}
	}
}
