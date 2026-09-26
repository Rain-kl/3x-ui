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
	"github.com/mhsanaei/3x-ui/v3/internal/web/network"
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
	SocksC2OnA = 10805

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
	fmt.Println("  [Full Real Client Workload & Xray gRPC Stats - NO FAKE MOCKS]     ")
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

	// 1. Start target HTTP server for real payload streaming
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

	// Seed ClientTraffic rows for global accounting
	for _, cr := range []*model.ClientRecord{cr1, cr2, cr3} {
		_ = svc.AddClientStat(db, ibA.Id, &model.Client{Email: cr.Email, Enable: true, TotalGB: cr.TotalGB})
	}

	// 5. Start real Xray Server Process
	serverCfgPath := filepath.Join(tmpDir, "server.json")
	createServerConfig(serverCfgPath)
	serverCmd := exec.CommandContext(context.Background(), xrayBin, "run", "-c", serverCfgPath)
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
	clientCmd := exec.CommandContext(context.Background(), xrayBin, "run", "-c", clientCfgPath)
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
	waitForTCP(fmt.Sprintf("127.0.0.1:%d", SocksC2OnA), 5*time.Second)

	fmt.Println("\n==> All server and client tunnels successfully started and ports verified.")

	// Initialize live Xray gRPC API client for traffic harvesting
	xrayAPI := &xray.XrayAPI{}
	if err := xrayAPI.Init(XrayAPIPort); err != nil {
		fatalf("xrayAPI Init: %v", err)
	}
	defer xrayAPI.Close()

	// Initial baseline poll (establishes zero mark in Xray gRPC counters)
	syncXrayTraffic(xrayAPI, svc)

	// =========================================================================
	// Scenario 1: Baseline Connectivity Check
	// =========================================================================
	fmt.Println("\n---------------------------------------------------------------------")
	fmt.Println("[Scenario 1] Verifying baseline connectivity for all client tunnels...")
	fmt.Println("---------------------------------------------------------------------")

	mustConnect(SocksC1OnA, "Client 1 on Node A (Direct)")
	mustConnect(SocksC1OnB, "Client 1 on Node B (VIP)")
	mustConnect(SocksC2OnB, "Client 2 on Node B (VIP)")
	mustConnect(SocksC3OnB, "Client 3 on Node B (VIP)")
	mustConnect(SocksC2OnA, "Client 2 on Node A (Direct)")
	fmt.Println(" PASS: Baseline connectivity 100% verified.")

	// =========================================================================
	// Scenario 2: Traffic Generation & Dual Accounting Verification
	// =========================================================================
	fmt.Println("\n---------------------------------------------------------------------")
	fmt.Println("[Scenario 2] Generating REAL traffic on Node B & verifying dual-accounting...")
	fmt.Println("---------------------------------------------------------------------")

	// Client 1 downloads 8MB via Node B through the real SOCKS proxy
	downloadSize1 := int64(8 << 20)
	fmt.Printf("==> Client 1 downloading %s payload through Node B proxy...\n", formatBytes(downloadSize1))
	mustDownload(SocksC1OnB, downloadSize1)

	// Poll real Xray gRPC stats and dispatch to 3x-ui service (NO MOCKS!)
	time.Sleep(100 * time.Millisecond)
	syncXrayTraffic(xrayAPI, svc)

	// Verify database rows reflect real transferred traffic
	var ciB1 model.ClientInbound
	db.Where("client_id = ? AND inbound_id = ?", cr1.Id, ibB.Id).First(&ciB1)
	if ciB1.Up+ciB1.Down < downloadSize1 {
		fatalf("Scenario 2: ciB1 usage too low: got %d, want >= %d", ciB1.Up+ciB1.Down, downloadSize1)
	}

	var ciA1 model.ClientInbound
	db.Where("client_id = ? AND inbound_id = ?", cr1.Id, ibA.Id).First(&ciA1)
	if ciA1.Up+ciA1.Down != 0 {
		fatalf("Scenario 2: ciA1 should be 0, got %d", ciA1.Up+ciA1.Down)
	}

	var global1 xray.ClientTraffic
	db.Where("email = ?", Email1).First(&global1)
	if global1.Up+global1.Down < downloadSize1 {
		fatalf("Scenario 2: global1 usage too low: got %d, want >= %d", global1.Up+global1.Down, downloadSize1)
	}

	cs := &service.ClientService{}
	apiTraffics2, err := cs.InboundTrafficsByClientId(cr1.Id)
	if err != nil || apiTraffics2[ibB.Id].Used < downloadSize1 {
		fatalf("Scenario 2: API InboundTrafficsByClientId mismatch: got %d, want >= %d", apiTraffics2[ibB.Id].Used, downloadSize1)
	}
	fmt.Printf("   [API Data Verified] Inbound %d: Total=%s, Used=%s, Remained=%s, Depleted=%v\n",
		ibB.Id, formatBytes(apiTraffics2[ibB.Id].Total), formatBytes(apiTraffics2[ibB.Id].Used), formatBytes(apiTraffics2[ibB.Id].Remained), apiTraffics2[ibB.Id].Depleted)
	fmt.Printf(" PASS: Dual-accounting verified: Node B = %s, Node A = %s, Global = %s, Tunnel active.\n",
		formatBytes(ciB1.Up+ciB1.Down), formatBytes(ciA1.Up+ciA1.Down), formatBytes(global1.Up+global1.Down))

	// =========================================================================
	// Scenario 3: Single-Node Depletion & Isolated Precision Cutoff
	// =========================================================================
	fmt.Println("\n---------------------------------------------------------------------")
	fmt.Println("[Scenario 3] Depleting Node B quota (20MB) with REAL traffic & verifying cutoff...")
	fmt.Println("---------------------------------------------------------------------")

	// Client 1 downloads another 15MB via Node B (total ~23MB > 20MB quota)
	downloadSize2 := int64(15 << 20)
	fmt.Printf("==> Client 1 downloading additional %s payload through Node B proxy...\n", formatBytes(downloadSize2))
	mustDownload(SocksC1OnB, downloadSize2)

	// Harvest real traffic from Xray
	time.Sleep(100 * time.Millisecond)
	syncXrayTraffic(xrayAPI, svc)

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

	// Client 1 can download real payload again on Node B
	fmt.Println("==> Client 1 downloading 2MB new payload on restored Node B...")
	mustDownload(SocksC1OnB, 2<<20)
	time.Sleep(100 * time.Millisecond)
	syncXrayTraffic(xrayAPI, svc)

	db.Where("client_id = ? AND inbound_id = ?", cr1.Id, ibB.Id).First(&ciB1)
	if ciB1.Up+ciB1.Down < 2<<20 {
		fatalf("Scenario 5: new usage on Node B should be >= 2MB, got %d", ciB1.Up+ciB1.Down)
	}
	fmt.Printf(" PASS: Auto-healing verified! Client reconnected and transferred %s on Node B.\n", formatBytes(ciB1.Up+ciB1.Down))

	// =========================================================================
	// Scenario 6: Independent Multi-Node Client Accounting
	// =========================================================================
	fmt.Println("\n---------------------------------------------------------------------")
	fmt.Println("[Scenario 6] Verifying Independent Multi-Node Real Accounting for Client 2...")
	fmt.Println("---------------------------------------------------------------------")

	var initial2A, initial2B model.ClientInbound
	db.Where("client_id = ? AND inbound_id = ?", cr2.Id, ibA.Id).First(&initial2A)
	db.Where("client_id = ? AND inbound_id = ?", cr2.Id, ibB.Id).First(&initial2B)
	base2A := initial2A.Up + initial2A.Down
	base2B := initial2B.Up + initial2B.Down

	// Client 2 downloads 5MB on Node A
	fmt.Println("==> Client 2 downloading 5MB payload on Node A...")
	mustDownload(SocksC2OnA, 5<<20)
	time.Sleep(100 * time.Millisecond)
	syncXrayTraffic(xrayAPI, svc)

	var ci2A_check model.ClientInbound
	db.Where("client_id = ? AND inbound_id = ?", cr2.Id, ibA.Id).First(&ci2A_check)
	if ci2A_check.Up+ci2A_check.Down < base2A+5<<20 {
		fatalf("Scenario 6: Client 2 Node A usage too low: got %d, want >= %d", ci2A_check.Up+ci2A_check.Down, base2A+5<<20)
	}

	var ci2B_check model.ClientInbound
	db.Where("client_id = ? AND inbound_id = ?", cr2.Id, ibB.Id).First(&ci2B_check)
	if ci2B_check.Up+ci2B_check.Down != base2B {
		fatalf("Scenario 6: Client 2 Node B should remain %d, got %d", base2B, ci2B_check.Up+ci2B_check.Down)
	}
	fmt.Printf("   [Client 2 State] Node A: %s, Node B: %s (Isolated)\n",
		formatBytes(ci2A_check.Up+ci2A_check.Down), formatBytes(ci2B_check.Up+ci2B_check.Down))

	// Client 2 downloads 6MB on Node B
	fmt.Println("==> Client 2 downloading 6MB payload on Node B...")
	mustDownload(SocksC2OnB, 6<<20)
	time.Sleep(100 * time.Millisecond)
	syncXrayTraffic(xrayAPI, svc)

	db.Where("client_id = ? AND inbound_id = ?", cr2.Id, ibB.Id).First(&ci2B_check)
	if ci2B_check.Up+ci2B_check.Down < base2B+6<<20 {
		fatalf("Scenario 6: Client 2 Node B usage too low: got %d, want >= %d", ci2B_check.Up+ci2B_check.Down, base2B+6<<20)
	}
	fmt.Printf("   [Client 2 State] Node A: %s, Node B: %s (Both active and independently tracked)\n",
		formatBytes(ci2A_check.Up+ci2A_check.Down), formatBytes(ci2B_check.Up+ci2B_check.Down))

	fmt.Println(" PASS: Independent multi-node accounting verified with real traffic!")

	// =========================================================================
	// Scenario 7: InboundTrafficsByClientId & API Serialization Contract
	// =========================================================================
	fmt.Println("\n---------------------------------------------------------------------")
	fmt.Println("[Scenario 7] Verifying InboundTrafficsByClientId & API data contract...")
	fmt.Println("---------------------------------------------------------------------")
	apiTraffics, err := cs.InboundTrafficsByClientId(cr1.Id)
	if err != nil {
		fatalf("Scenario 7: InboundTrafficsByClientId failed: %v", err)
	}
	statB, ok := apiTraffics[ibB.Id]
	if !ok {
		fatalf("Scenario 7: InboundTrafficsByClientId missing Inbound B")
	}
	if statB.Total != 20<<20 {
		fatalf("Scenario 7: statB.Total mismatch: got %d, want %d", statB.Total, 20<<20)
	}
	if statB.Used < 2<<20 {
		fatalf("Scenario 7: statB.Used should be >= 2MB, got %d", statB.Used)
	}
	fmt.Printf("   [API Data] Inbound %d: Total=%s, Used=%s, Remained=%s, Depleted=%v\n",
		ibB.Id, formatBytes(statB.Total), formatBytes(statB.Used), formatBytes(statB.Remained), statB.Depleted)
	fmt.Println(" PASS: InboundTrafficsByClientId API contract verified! Non-zero used traffic and quota properly calculated.")

	fmt.Println("\n=====================================================================")
	fmt.Println("  ALL 7 REAL SCENARIOS FULLY VERIFIED AND PASSED WITH REAL CLIENTS!")
	fmt.Println("=====================================================================")
}

// syncXrayTraffic collects actual runtime traffic from Xray-core via gRPC
// and updates the database through the 3x-ui service layer. NO MOCKS.
func syncXrayTraffic(xrayAPI *xray.XrayAPI, svc *service.InboundService) ([]*xray.Traffic, []*xray.ClientTraffic) {
	traffics, clientTraffics, err := xrayAPI.GetTraffic()
	if err != nil {
		fatalf("xrayAPI.GetTraffic failed: %v", err)
	}

	if len(traffics) > 0 || len(clientTraffics) > 0 {
		fmt.Printf("   ==> [Real Xray Stats Harvested] Inbound tags: %d, Clients: %d\n", len(traffics), len(clientTraffics))
		for _, it := range traffics {
			if it.Up > 0 || it.Down > 0 {
				fmt.Printf("       [Inbound Core Stat] Tag: %-15s Up: %-10s Down: %-10s\n", it.Tag, formatBytes(it.Up), formatBytes(it.Down))
			}
		}
		for _, ct := range clientTraffics {
			if ct.Up > 0 || ct.Down > 0 {
				fmt.Printf("       [Client Core Stat]  Email: %-18s Up: %-10s Down: %-10s\n", ct.Email, formatBytes(ct.Up), formatBytes(ct.Down))
			}
		}

		if _, _, err := svc.AddTraffic(traffics, clientTraffics); err != nil {
			fatalf("svc.AddTraffic failed: %v", err)
		}
	}
	return traffics, clientTraffics
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
	status, readBytes, err := testProxyGet(socksPort, urlStr, 30*time.Second)
	if err != nil || status != http.StatusOK {
		fatalf("mustDownload failed: status=%d, err=%v", status, err)
	}
	if readBytes != bytesCount {
		fatalf("mustDownload read bytes mismatch: got %d, want %d", readBytes, bytesCount)
	}
	fmt.Printf("   [Real Download OK] SocksPort=%d, Received=%s (%d bytes)\n", socksPort, formatBytes(readBytes), readBytes)
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
		return 0, 0, fmt.Errorf("%w (curl stderr: %s)", err, stderr.String())
	}
	out := strings.TrimSpace(stdout.String())
	parts := strings.Split(out, ":")
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("invalid curl output: %q", out)
	}
	code, _ := strconv.Atoi(parts[0])
	size, _ := strconv.ParseInt(parts[1], 10, 64)
	return code, size, nil
}

func startTargetServer(port int) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	mux.HandleFunc("/data", func(w http.ResponseWriter, r *http.Request) {
		sizeStr := r.URL.Query().Get("bytes")
		size, _ := strconv.ParseInt(sizeStr, 10, 64)
		if size <= 0 {
			size = 1024 * 1024
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		buf := make([]byte, 64*1024)
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
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		fatalf("target server listen: %v", err)
	}
	go func() {
		network.ServeHTTP(server, listener, "simtest-target")
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
			"system": map[string]any{"statsInboundUplink": true, "statsInboundDownlink": true},
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
			map[string]any{"port": SocksC2OnA, "listen": "127.0.0.1", "protocol": "socks", "settings": map[string]any{"auth": "noauth"}, "tag": "in-c2-a"},
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
			map[string]any{
				"tag":      "out-c2-a",
				"protocol": "vless",
				"settings": map[string]any{
					"vnext": []any{
						map[string]any{
							"address": "127.0.0.1",
							"port":    NodeAPort,
							"users":   []any{map[string]any{"id": UUID2, "encryption": "none"}},
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
				map[string]any{"type": "field", "inboundTag": []string{"in-c2-a"}, "outboundTag": "out-c2-a"},
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
		conn, err := (&net.Dialer{Timeout: 200 * time.Millisecond}).DialContext(context.Background(), "tcp", addr)
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
