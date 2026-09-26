package sub

import (
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

// TestBuildPageData_PopulatesLimitedNodes verifies that BuildPageData queries
// client_inbounds with TotalGB > 0 and computes accurate limits.
func TestBuildPageData_PopulatesLimitedNodes(t *testing.T) {
	dbDir := t.TempDir()
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	inbound := &model.Inbound{
		Remark:   "HongKong-VIP",
		Port:     38443,
		Protocol: model.VLESS,
		Tag:      "in-hk",
		Enable:   true,
	}
	database.GetDB().Create(inbound)

	client := &model.ClientRecord{
		Email: "subuser@test.com",
		SubID: "subtest123",
	}
	database.GetDB().Create(client)

	ci := &model.ClientInbound{
		ClientId:  client.Id,
		InboundId: inbound.Id,
		TotalGB:   50 * 1024 * 1024 * 1024,
		Up:        10 * 1024 * 1024 * 1024,
		Down:      15 * 1024 * 1024 * 1024,
	}
	database.GetDB().Create(ci)

	svc := NewSubService("")
	page := svc.BuildPageData("subtest123", "sub.com", xray.ClientTraffic{Total: 200 << 30}, 0, []string{"vless://..."}, []string{"subuser@test.com"}, "", "", "", "/", "Title", "")

	if len(page.LimitedNodes) != 1 {
		t.Fatalf("expected 1 limited node, got %d", len(page.LimitedNodes))
	}
	node := page.LimitedNodes[0]
	if node.Name != "HongKong-VIP" {
		t.Errorf("node.Name = %q, want HongKong-VIP", node.Name)
	}
	if node.Percent < 49.0 || node.Percent > 51.0 {
		t.Errorf("node.Percent = %f, want ~50.0", node.Percent)
	}
	if node.Depleted {
		t.Errorf("expected node not to be depleted, got true")
	}
	if node.Used == "" || node.Total == "" || node.Remained == "" {
		t.Errorf("expected formatted Used, Total, Remained strings, got Used=%q, Total=%q, Remained=%q", node.Used, node.Total, node.Remained)
	}
}

// TestBuildPageData_LimitedNodesEdgeCases verifies depleted flagging and tag fallback.
func TestBuildPageData_LimitedNodesEdgeCases(t *testing.T) {
	dbDir := t.TempDir()
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	inbound1 := &model.Inbound{
		Remark:   "",
		Tag:      "tag-depleted",
		Port:     38444,
		Protocol: model.VLESS,
		Enable:   true,
	}
	inbound2 := &model.Inbound{
		Remark:   "Unlimited-Node",
		Tag:      "tag-unlimited",
		Port:     38445,
		Protocol: model.VLESS,
		Enable:   true,
	}
	database.GetDB().Create(inbound1)
	database.GetDB().Create(inbound2)

	client := &model.ClientRecord{
		Email: "edge@test.com",
		SubID: "subedge",
	}
	database.GetDB().Create(client)

	ci1 := &model.ClientInbound{
		ClientId:  client.Id,
		InboundId: inbound1.Id,
		TotalGB:   20 * 1024 * 1024 * 1024,
		Up:        15 * 1024 * 1024 * 1024,
		Down:      10 * 1024 * 1024 * 1024,
	}
	ci2 := &model.ClientInbound{
		ClientId:  client.Id,
		InboundId: inbound2.Id,
		TotalGB:   0,
	}
	database.GetDB().Create(ci1)
	database.GetDB().Create(ci2)

	svc := NewSubService("")
	page := svc.BuildPageData("subedge", "sub.com", xray.ClientTraffic{}, 0, nil, nil, "", "", "", "/", "", "")

	if len(page.LimitedNodes) != 1 {
		t.Fatalf("expected 1 limited node, got %d", len(page.LimitedNodes))
	}
	node := page.LimitedNodes[0]
	if node.Name != "tag-depleted" {
		t.Errorf("node.Name = %q, want tag-depleted", node.Name)
	}
	if !node.Depleted {
		t.Errorf("expected node to be depleted")
	}
	if node.Percent < 100.0 {
		t.Errorf("node.Percent = %f, want >= 100.0", node.Percent)
	}
}
