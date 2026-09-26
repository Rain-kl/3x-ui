package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/sub"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

const (
	TargetServerPort = 18080
	XrayAPIPort      = 62789
	NodeAPort        = 41001
	NodeBPort        = 41002

	SocksC1OnA = 10801
	SocksC1OnB = 10802
	SocksC2OnB = 10803
	SocksC3OnB = 10804

	UUID1 = "11111111-1111-1111-1111-111111111111"
	UUID2 = "22222222-2222-2222-2222-222222222222"
	UUID3 = "33333333-3333-3333-3333-333333333333"

	Email1 = "client1@test.com"
	Email2 = "client2@test.com"
	Email3 = "client3@test.com"

	SubID1 = "sub-test-client1"
	SubID2 = "sub-test-client2"
	SubID3 = "sub-test-client3"
)

const targetHost = "127.0.0.1"

func main() {
	fmt.Println("=====================================================================")
	fmt.Println("  3x-ui Client Per-Node Traffic Limit Real Simulation (OrbStack VM) ")
	fmt.Println("=====================================================================")

	tmpDir, err := os.MkdirTemp("", "3x_ui_sim_*")
	if err != nil {
		fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	xrayBin := os.Getenv("XRAY_BIN")
	if xrayBin == "" {
		xrayBin = "/tmp/xray"
	}
	if _, err := os.Stat(xrayBin); err != nil {
		fatalf("xray binary not found at %s: %v", xrayBin, err)
	}

	// 1. Start target HTTP server
	startTargetServer(TargetServerPort)

	// 2. Initialize database
	dbPath := filepath.Join(tmpDir, "sim_test.db")
	if err := database.InitDB(dbPath); err != nil {
		fatalf("InitDB: %v", err)
	}
	defer func() { _ = database.CloseDB() }()

	db := database.GetDB()
	svc := &service.InboundService{}

	// 3. Create Inbound records in DB
	ibA := &model.Inbound{
		UserId:   1,
		Remark:   "Node-A-Direct",
		Port:     NodeAPort,
		Protocol: model.VLESS,
		Tag:      "node-a-direct",
		Enable:   true,
		Settings: fmt.Sprintf(`{"clients":[{"id":"%s","email":"%s","enable":true},{"id":"%s","email":"%s","enable":true},{"id":"%s","email":"%s","enable":true}],"decryption":"none"}`,
			UUID1, Email1, UUID2, Email2, UUID3, Email3),
	}
	ibB := &model.Inbound{
		UserId:   1,
		Remark:   "Node-B-VIP",
		Port:     NodeBPort,
		Protocol: model.VLESS,
		Tag:      "node-b-vip",
		Enable:   true,
		Settings: fmt.Sprintf(`{"clients":[{"id":"%s","email":"%s","enable":true},{"id":"%s","email":"%s","enable":true},{"id":"%s","email":"%s","enable":true}],"decryption":"none"}`,
			UUID1, Email1, UUID2, Email2, UUID3, Email3),
	}
	if err := db.Create(ibA).Error; err != nil {
		fatalf("create ibA: %v", err)
	}
	if err := db.Create(ibB).Error; err != nil {
		fatalf("create ibB: %v", err)
	}

	// 4. Create Clients in DB
	// Client 1: Global 100MB, Node A unlimited (0), Node B 20MB
	cr1 := &model.ClientRecord{Email: Email1, TotalGB: 100 << 20, Enable: true, SubID: SubID1}
	// Client 2: Global 100MB, Node A unlimited (0), Node B 50MB
	cr2 := &model.ClientRecord{Email: Email2, TotalGB: 100 << 20, Enable: true, SubID: SubID2}
	// Client 3: Global 100MB, Node A unlimited (0), Node B unlimited (0)
	cr3 := &model.ClientRecord{Email: Email3, TotalGB: 100 << 20, Enable: true, SubID: SubID3}

	for _, cr := range []*model.ClientRecord{cr1, cr2, cr3} {
		if err := db.Create(cr).Error; err != nil {
			fatalf("create client record: %v", err)
		}
	}

	ci1A := &model.ClientInbound{ClientId: cr1.Id, InboundId: ibA.Id, TotalGB: 0}
	ci1B := &model.ClientInbound{ClientId: cr1.Id, InboundId: ibB.Id, TotalGB: 20 << 20}
	ci2A := &model.ClientInbound{ClientId: cr2.Id, InboundId: ibA.Id, TotalGB: 0}
	ci2B := &model.ClientInbound{ClientId: cr2.Id, InboundId: ibB.Id, TotalGB: 50 << 20}
	ci3A := &model.ClientInbound{ClientId: cr3.Id, InboundId: ibA.Id, TotalGB: 0}
	ci3B := &model.ClientInbound{ClientId: cr3.Id, InboundId: ibB.Id, TotalGB: 0}

	for _, ci := range []*model.ClientInbound{ci1A, ci1B, ci2A, ci2B, ci3A, ci3B} {
		if err := db.Create(ci).Error; err != nil {
			fatalf("create client inbound: %v", err)
		}
	}

	// Also seed ClientTraffic rows for global accounting
	for _, cr := range []*model.ClientRecord{cr1, cr2, cr3} {
		_ = svc.AddClientStat(db, ibA.Id, &model.Client{Email: cr.Email, Enable: true, TotalGB: cr.TotalGB})
	}

	// 5. Start real Xray Server Process
	serverCfgPath := filepath.Join(tmpDir, "server.json")
	createServerConfig(serverCfgPath)
	serverCmd := exec.Command(xrayBin, "run", "-c", serverCfgPath)
	serverCmd.Stdout = os.Stdout
	serverCmd.Stderr = os.Stderr
	if err := serverCmd.Start(); err != nil {
		fatalf("start xray server: %v", err)
	}
	defer func() {
		_ = serverCmd.Process.Kill()
		_ = serverCmd.Wait()
	}()
	waitForTCP(fmt.Sprintf("127.0.0.1:%d", XrayAPIPort), 5*time.Second)
	waitForTCP(fmt.Sprintf("127.0.0.1:%d", NodeAPort), 5*time.Second)
	waitForTCP(fmt.Sprintf("127.0.0.1:%d", NodeBPort), 5*time.Second)

	// Bind Runtime to live Xray API
	runtimeMgr := runtime.NewManager(runtime.LocalDeps{APIPort: func() int { return XrayAPIPort }})
	runtime.SetManager(runtimeMgr)
	defer runtime.SetManager(nil)

	// 6. Start real Xray Client Proxies Process
	clientCfgPath := filepath.Join(tmpDir, "client.json")
	createClientConfig(clientCfgPath)
	clientCmd := exec.Command(xrayBin, "run", "-c", clientCfgPath)
	clientCmd.Stdout = os.Stdout
	clientCmd.Stderr = os.Stderr
	if err := clientCmd.Start(); err != nil {
		fatalf("start xray clients: %v", err)
	}
	defer func() {
		_ = clientCmd.Process.Kill()
		_ = clientCmd.Wait()
	}()
	waitForTCP(fmt.Sprintf("127.0.0.1:%d", SocksC1OnA), 5*time.Second)
	waitForTCP(fmt.Sprintf("127.0.0.1:%d", SocksC1OnB), 5*time.Second)
	waitForTCP(fmt.Sprintf("127.0.0.1:%d", SocksC2OnB), 5*time.Second)
	waitForTCP(fmt.Sprintf("127.0.0.1:%d", SocksC3OnB), 5*time.Second)

	fmt.Println("\n==> All server and client tunnels successfully started and ports verified.")

	// =========================================================================
	// Scenario 1: Baseline Connectivity Check
	// =========================================================================
	fmt.Println("\n---------------------------------------------------------------------")
	fmt.Println("[Scenario 1] Verifying baseline connectivity for all 4 client tunnels...")
	fmt.Println("---------------------------------------------------------------------")

	mustConnect(SocksC1OnA, "Client 1 on Node A (Direct)")
	mustConnect(SocksC1OnB, "Client 1 on Node B (VIP)")
	mustConnect(SocksC2OnB, "Client 2 on Node B (VIP)")
	mustConnect(SocksC3OnB, "Client 3 on Node B (VIP)")
	fmt.Println(" PASS: Baseline connectivity 100% verified.")

	// =========================================================================
	// Scenario 2: Traffic Generation & Dual Accounting Verification
	// =========================================================================
	fmt.Println("\n---------------------------------------------------------------------")
	fmt.Println("[Scenario 2] Generating traffic on Node B & verifying dual-accounting...")
	fmt.Println("---------------------------------------------------------------------")

	// Client 1 downloads 8MB via Node B
	downloadSize1 := int64(8 << 20)
	mustDownload(SocksC1OnB, downloadSize1)

	// Simulate accounting report: 8MB on Inbound B for Client 1
	reportTraffic(svc, ibB.Id, Email1, 1<<20, 7<<20)

	// Verify database rows
	var ciB1 model.ClientInbound
	db.Where("client_id = ? AND inbound_id = ?", cr1.Id, ibB.Id).First(&ciB1)
	if ciB1.Up+ciB1.Down != 8<<20 {
		fatalf("Scenario 2: ciB1 usage mismatch: got %d, want %d", ciB1.Up+ciB1.Down, 8<<20)
	}

	var ciA1 model.ClientInbound
	db.Where("client_id = ? AND inbound_id = ?", cr1.Id, ibA.Id).First(&ciA1)
	if ciA1.Up+ciA1.Down != 0 {
		fatalf("Scenario 2: ciA1 should be 0, got %d", ciA1.Up+ciA1.Down)
	}

	var global1 xray.ClientTraffic
	db.Where("email = ?", Email1).First(&global1)
	if global1.Up+global1.Down != 8<<20 {
		fatalf("Scenario 2: global1 usage mismatch: got %d, want %d", global1.Up+global1.Down, 8<<20)
	}

	// Client 1 should still be connectable on Node B (used 8MB < 20MB limit)
	mustConnect(SocksC1OnB, "Client 1 on Node B after 8MB usage")
	fmt.Printf(" PASS: Dual-accounting verified: Node B = %s, Node A = %s, Global = %s, Tunnel active.\n",
		formatBytes(ciB1.Up+ciB1.Down), formatBytes(ciA1.Up+ciA1.Down), formatBytes(global1.Up+global1.Down))

	// =========================================================================
	// Scenario 3: Single-Node Depletion & Isolated Precision Cutoff
	// =========================================================================
	fmt.Println("\n---------------------------------------------------------------------")
	fmt.Println("[Scenario 3] Depleting Node B quota (20MB) & verifying single-node cutoff...")
	fmt.Println("---------------------------------------------------------------------")

	// Client 1 downloads another 15MB via Node B (total 23MB > 20MB quota)
	downloadSize2 := int64(15 << 20)
	mustDownload(SocksC1OnB, downloadSize2)

	// Report additional 15MB traffic on Node B
	reportTraffic(svc, ibB.Id, Email1, 2<<20, 13<<20)

	// Verify database: Node B depleted, Node A enabled, Client record enabled
	db.Where("client_id = ? AND inbound_id = ?", cr1.Id, ibB.Id).First(&ciB1)
	totalUsedB := ciB1.Up + ciB1.Down
	if totalUsedB < 20<<20 {
		fatalf("Scenario 3: totalUsedB %d should exceed 20MB", totalUsedB)
	}

	var crCheck model.ClientRecord
	db.First(&crCheck, cr1.Id)
	if !crCheck.Enable {
		fatalf("Scenario 3: ClientRecord.Enable must remain TRUE, got false")
	}

	// Verify Node B settings has Client 1 disabled
	var refreshedIbB model.Inbound
	db.First(&refreshedIbB, ibB.Id)
	if clientEnabledInSettings(refreshedIbB.Settings, Email1) {
		fatalf("Scenario 3: Client 1 must be marked enable=false in Inbound B settings")
	}

	// Verify Node A settings has Client 1 still ENABLED
	var refreshedIbA model.Inbound
	db.First(&refreshedIbA, ibA.Id)
	if !clientEnabledInSettings(refreshedIbA.Settings, Email1) {
		fatalf("Scenario 3: Client 1 must remain enable=true in Inbound A settings")
	}

	time.Sleep(300 * time.Millisecond) // Allow Xray gRPC removal to settle

	// REAL NETWORK BEHAVIOR ASSERTIONS:
	// 1. Client 1 on Node B (VIP) MUST FAIL
	mustFailConnection(SocksC1OnB, "Client 1 on depleted Node B")
	// 2. Client 1 on Node A (Direct) MUST SUCCEED!
	mustConnect(SocksC1OnA, "Client 1 on Node A (unlimited node, should remain operational)")
	// 3. Client 2 on Node B (VIP) MUST SUCCEED (quota is 50MB, completely unaffected)
	mustConnect(SocksC2OnB, "Client 2 on Node B (unaffected peer)")
	// 4. Client 3 on Node B (VIP) MUST SUCCEED (quota unlimited, completely unaffected)
	mustConnect(SocksC3OnB, "Client 3 on Node B (unaffected peer)")

	fmt.Println(" PASS: Single-node depletion verified! Node B rejected Client 1, Node A operational, peers unaffected.")

	// =========================================================================
	// Scenario 4: Subscription Page Data & Progress Bar Verification
	// =========================================================================
	fmt.Println("\n---------------------------------------------------------------------")
	fmt.Println("[Scenario 4] Verifying Subscription Page Data Injection...")
	fmt.Println("---------------------------------------------------------------------")

	subSvc := sub.NewSubService("")
	subTraffic := xray.ClientTraffic{Up: global1.Up, Down: global1.Down, Total: cr1.TotalGB}
	pageData := subSvc.BuildPageData(SubID1, "sub.domain.com", subTraffic, 0, []string{"vless://..."}, []string{Email1}, "", "", "", "/", "SubTitle", "")

	if len(pageData.LimitedNodes) != 1 {
		fatalf("Scenario 4: expected 1 limited node in PageData, got %d", len(pageData.LimitedNodes))
	}
	limitedNode := pageData.LimitedNodes[0]
	if limitedNode.Name != "Node-B-VIP" {
		fatalf("Scenario 4: limitedNode.Name = %q, want 'Node-B-VIP'", limitedNode.Name)
	}
	if !limitedNode.Depleted {
		fatalf("Scenario 4: limitedNode.Depleted must be TRUE, got false")
	}
	if limitedNode.Percent < 100.0 {
		fatalf("Scenario 4: limitedNode.Percent = %f, want >= 100.0", limitedNode.Percent)
	}
	fmt.Printf(" PASS: Subscription data verified: Node %s: Total=%s, Used=%s, Remained=%s, Depleted=%v, Percent=%.1f%%\n",
		limitedNode.Name, limitedNode.Total, limitedNode.Used, limitedNode.Remained, limitedNode.Depleted, limitedNode.Percent)

	// =========================================================================
	// Scenario 5: Admin Reset & Auto-Healing (自愈重置)
	// =========================================================================
	fmt.Println("\n---------------------------------------------------------------------")
	fmt.Println("[Scenario 5] Resetting traffic & verifying auto-healing on Node B...")
	fmt.Println("---------------------------------------------------------------------")

	if _, err := svc.ResetClientTraffic(ibB.Id, Email1); err != nil {
		fatalf("Scenario 5: ResetClientTraffic failed: %v", err)
	}

	// Verify database rows reset
	db.Where("client_id = ? AND inbound_id = ?", cr1.Id, ibB.Id).First(&ciB1)
	if ciB1.Up != 0 || ciB1.Down != 0 {
		fatalf("Scenario 5: ciB1 usage should be 0, got up=%d, down=%d", ciB1.Up, ciB1.Down)
	}

	db.First(&refreshedIbB, ibB.Id)
	if !clientEnabledInSettings(refreshedIbB.Settings, Email1) {
		fatalf("Scenario 5: Client 1 must be restored to enable=true in Inbound B settings")
	}

	time.Sleep(300 * time.Millisecond) // Allow Xray gRPC add-user to settle

	// REAL NETWORK BEHAVIOR: Client 1 on Node B MUST BE RESTORED & SUCCEED!
	mustConnect(SocksC1OnB, "Client 1 on Node B after traffic reset (Auto-Healing)")
	mustConnect(SocksC1OnA, "Client 1 on Node A after traffic reset")
	fmt.Println(" PASS: Auto-healing verified! Resetting traffic immediately re-enabled Client 1 on Node B in Xray.")

	// =========================================================================
	// Scenario 6: Global Quota Depletion Contrast (全阻断对比)
	// =========================================================================
	fmt.Println("\n---------------------------------------------------------------------")
	fmt.Println("[Scenario 6] Depleting Global Quota (100MB) & verifying all-node cutoff...")
	fmt.Println("---------------------------------------------------------------------")

	// Report 105MB on Node A (exceeding global 100MB quota)
	reportTraffic(svc, ibA.Id, Email1, 5<<20, 100<<20)

	// Verify ClientRecord.Enable is now FALSE globally
	db.First(&crCheck, cr1.Id)
	if crCheck.Enable {
		fatalf("Scenario 6: ClientRecord.Enable should be FALSE after global depletion")
	}

	time.Sleep(300 * time.Millisecond)

	// Both Node A and Node B MUST FAIL for Client 1
	mustFailConnection(SocksC1OnA, "Client 1 on Node A after global depletion")
	mustFailConnection(SocksC1OnB, "Client 1 on Node B after global depletion")

	// Client 2 on Node B MUST STILL SUCCEED
	mustConnect(SocksC2OnB, "Client 2 on Node B (independent client)")

	fmt.Println(" PASS: Global depletion contrast verified! Exceeding global quota disables client on all nodes.")

	fmt.Println("\n=====================================================================")
	fmt.Println("  ALL 6 REAL SCENARIOS FULLY VERIFIED AND PASSED!")
	fmt.Println("=====================================================================")
}

func reportTraffic(svc *service.InboundService, ibID int, email string, up, down int64) {
	traffics := []*xray.ClientTraffic{
		{InboundId: ibID, Email: email, Up: up, Down: down},
	}
	if _, _, err := svc.AddTraffic(nil, traffics); err != nil {
		fatalf("reportTraffic AddTraffic: %v", err)
	}
}

func clientEnabledInSettings(settingsJSON, email string) bool {
	var s struct {
		Clients []struct {
			Email  string `json:"email"`
			Enable bool   `json:"enable"`
		} `json:"clients"`
	}
	if err := json.Unmarshal([]byte(settingsJSON), &s); err != nil {
		return false
	}
	for _, c := range s.Clients {
		if c.Email == email {
			return c.Enable
		}
	}
	return false
}

func mustConnect(socksPort int, desc string) {
	status, _, err := testProxyGet(socksPort, fmt.Sprintf("http://%s:%d/health", targetHost, TargetServerPort), 5*time.Second)
	if err != nil || status != http.StatusOK {
		fatalf("[FAIL] %s: expected 200 OK, got status=%d, err=%v", desc, status, err)
	}
	fmt.Printf("   [OK] %s: HTTP 200\n", desc)
}

func mustFailConnection(socksPort int, desc string) {
	status, _, err := testProxyGet(socksPort, fmt.Sprintf("http://%s:%d/health", targetHost, TargetServerPort), 3*time.Second)
	if err == nil && status == http.StatusOK {
		fatalf("[FAIL] %s: expected connection failure/rejection, but request unexpectedly succeeded with 200 OK!", desc)
	}
	fmt.Printf("   [BLOCKED as expected] %s (err: %v)\n", desc, err)
}

func mustDownload(socksPort int, bytesCount int64) {
	urlStr := fmt.Sprintf("http://%s:%d/data?bytes=%d", targetHost, TargetServerPort, bytesCount)
	status, readBytes, err := testProxyGet(socksPort, urlStr, 15*time.Second)
	if err != nil || status != http.StatusOK {
		fatalf("mustDownload failed: status=%d, err=%v", status, err)
	}
	if readBytes != bytesCount {
		fatalf("mustDownload read bytes mismatch: got %d, want %d", readBytes, bytesCount)
	}
}

func testProxyGet(socksPort int, targetURL string, timeout time.Duration) (int, int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "curl", "-s", "-w", "%{http_code}:%{size_download}",
		"-x", fmt.Sprintf("socks5://127.0.0.1:%d", socksPort), targetURL, "-o", "/dev/null")
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return 0, 0, fmt.Errorf("%v (curl stderr: %s)", err, stderr.String())
	}
	parts := strings.Split(strings.TrimSpace(stdout.String()), ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid curl output: %s", stdout.String())
	}
	code, _ := strconv.Atoi(parts[0])
	size, _ := strconv.ParseInt(parts[1], 10, 64)
	return code, size, nil
}

func startTargetServer(port int) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("[TargetServer] %s %s from %s\n", r.Method, r.URL.Path, r.RemoteAddr)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	mux.HandleFunc("/data", func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("[TargetServer] %s %s from %s\n", r.Method, r.URL.Path, r.RemoteAddr)
		sizeStr := r.URL.Query().Get("bytes")
		size, _ := strconv.ParseInt(sizeStr, 10, 64)
		if size <= 0 {
			size = 1024 * 1024
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		buf := make([]byte, 32*1024)
		for i := range buf {
			buf[i] = 'X'
		}
		var written int64
		for written < size {
			toWrite := int64(len(buf))
			if size-written < toWrite {
				toWrite = size - written
			}
			n, err := w.Write(buf[:toWrite])
			if err != nil {
				return
			}
			written += int64(n)
		}
	})

	server := &http.Server{
		Handler: mux,
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		fatalf("target server listen: %v", err)
	}
	go func() {
		_ = server.Serve(listener)
	}()
}

func createServerConfig(path string) {
	cfg := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"api": map[string]any{
			"services": []string{"HandlerService", "StatsService", "RoutingService"},
			"tag":      "api",
		},
		"inbounds": []any{
			map[string]any{
				"listen":   "127.0.0.1",
				"port":     XrayAPIPort,
				"protocol": "tunnel",
				"settings": map[string]any{"rewriteAddress": "127.0.0.1"},
				"tag":      "api",
			},
			map[string]any{
				"listen":   "127.0.0.1",
				"port":     NodeAPort,
				"protocol": "vless",
				"tag":      "node-a-direct",
				"settings": map[string]any{
					"clients": []any{
						map[string]any{"id": UUID1, "email": Email1},
						map[string]any{"id": UUID2, "email": Email2},
						map[string]any{"id": UUID3, "email": Email3},
					},
					"decryption": "none",
				},
			},
			map[string]any{
				"listen":   "127.0.0.1",
				"port":     NodeBPort,
				"protocol": "vless",
				"tag":      "node-b-vip",
				"settings": map[string]any{
					"clients": []any{
						map[string]any{"id": UUID1, "email": Email1},
						map[string]any{"id": UUID2, "email": Email2},
						map[string]any{"id": UUID3, "email": Email3},
					},
					"decryption": "none",
				},
			},
		},
		"outbounds": []any{
			map[string]any{
				"protocol": "freedom",
				"tag":      "direct",
				"settings": map[string]any{
					"finalRules": []any{
						map[string]any{"action": "allow"},
					},
				},
			},
		},
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules": []any{
				map[string]any{"type": "field", "inboundTag": []string{"api"}, "outboundTag": "api"},
			},
		},
		"policy": map[string]any{
			"levels": map[string]any{
				"0": map[string]any{"statsUserUplink": true, "statsUserDownlink": true},
			},
		},
		"stats": map[string]any{},
	}
	writeJSON(path, cfg)
}

func createClientConfig(path string) {
	cfg := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{
			map[string]any{"port": SocksC1OnA, "listen": "127.0.0.1", "protocol": "socks", "settings": map[string]any{"auth": "noauth"}, "tag": "in-c1-a"},
			map[string]any{"port": SocksC1OnB, "listen": "127.0.0.1", "protocol": "socks", "settings": map[string]any{"auth": "noauth"}, "tag": "in-c1-b"},
			map[string]any{"port": SocksC2OnB, "listen": "127.0.0.1", "protocol": "socks", "settings": map[string]any{"auth": "noauth"}, "tag": "in-c2-b"},
			map[string]any{"port": SocksC3OnB, "listen": "127.0.0.1", "protocol": "socks", "settings": map[string]any{"auth": "noauth"}, "tag": "in-c3-b"},
		},
		"outbounds": []any{
			map[string]any{
				"tag":      "out-c1-a",
				"protocol": "vless",
				"settings": map[string]any{
					"vnext": []any{
						map[string]any{
							"address": "127.0.0.1",
							"port":    NodeAPort,
							"users":   []any{map[string]any{"id": UUID1, "encryption": "none"}},
						},
					},
				},
			},
			map[string]any{
				"tag":      "out-c1-b",
				"protocol": "vless",
				"settings": map[string]any{
					"vnext": []any{
						map[string]any{
							"address": "127.0.0.1",
							"port":    NodeBPort,
							"users":   []any{map[string]any{"id": UUID1, "encryption": "none"}},
						},
					},
				},
			},
			map[string]any{
				"tag":      "out-c2-b",
				"protocol": "vless",
				"settings": map[string]any{
					"vnext": []any{
						map[string]any{
							"address": "127.0.0.1",
							"port":    NodeBPort,
							"users":   []any{map[string]any{"id": UUID2, "encryption": "none"}},
						},
					},
				},
			},
			map[string]any{
				"tag":      "out-c3-b",
				"protocol": "vless",
				"settings": map[string]any{
					"vnext": []any{
						map[string]any{
							"address": "127.0.0.1",
							"port":    NodeBPort,
							"users":   []any{map[string]any{"id": UUID3, "encryption": "none"}},
						},
					},
				},
			},
		},
		"routing": map[string]any{
			"rules": []any{
				map[string]any{"type": "field", "inboundTag": []string{"in-c1-a"}, "outboundTag": "out-c1-a"},
				map[string]any{"type": "field", "inboundTag": []string{"in-c1-b"}, "outboundTag": "out-c1-b"},
				map[string]any{"type": "field", "inboundTag": []string{"in-c2-b"}, "outboundTag": "out-c2-b"},
				map[string]any{"type": "field", "inboundTag": []string{"in-c3-b"}, "outboundTag": "out-c3-b"},
			},
		},
	}
	writeJSON(path, cfg)
}

func writeJSON(path string, data any) {
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		fatalf("marshal json: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		fatalf("write file %s: %v", path, err)
	}
}

func waitForTCP(addr string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	fatalf("timeout waiting for TCP %s", addr)
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func fatalf(format string, args ...any) {
	fmt.Printf("\n[FATAL ERROR] "+format+"\n", args...)
	os.Exit(1)
}
