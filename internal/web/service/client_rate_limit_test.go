package service

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestClientRateLimit_ComputeEffectiveLimitPriority(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	db := database.GetDB()

	// Inbound with default client down limit 10 Mbps.
	ib := mkInbound(t, 22001, model.VLESS, `{"clients":[]}`)
	ib.ClientDownLimit = 10
	if err := db.Save(ib).Error; err != nil {
		t.Fatalf("Save inbound: %v", err)
	}

	// Case 1: Inbound default only (client has 0, client_inbound has 0).
	c1 := &model.ClientRecord{Email: "case1@test.com", DownLimit: 0, Enable: true}
	if err := db.Create(c1).Error; err != nil {
		t.Fatalf("Create c1: %v", err)
	}
	ci1 := &model.ClientInbound{ClientId: c1.Id, InboundId: ib.Id, DownLimit: 0}
	if err := db.Create(ci1).Error; err != nil {
		t.Fatalf("Create ci1: %v", err)
	}

	limit1, err := svc.ComputeClientEffectiveLimit(c1.Email, ib.Id)
	if err != nil {
		t.Fatalf("ComputeClientEffectiveLimit c1: %v", err)
	}
	if limit1 != 10 {
		t.Errorf("case 1 limit = %d, want 10 (inbound default)", limit1)
	}

	// Case 2: Client down limit (20 Mbps) overrides inbound default (10 Mbps).
	c2 := &model.ClientRecord{Email: "case2@test.com", DownLimit: 20, Enable: true}
	if err := db.Create(c2).Error; err != nil {
		t.Fatalf("Create c2: %v", err)
	}
	ci2 := &model.ClientInbound{ClientId: c2.Id, InboundId: ib.Id, DownLimit: 0}
	if err := db.Create(ci2).Error; err != nil {
		t.Fatalf("Create ci2: %v", err)
	}

	limit2, err := svc.ComputeClientEffectiveLimit(c2.Email, ib.Id)
	if err != nil {
		t.Fatalf("ComputeClientEffectiveLimit c2: %v", err)
	}
	if limit2 != 20 {
		t.Errorf("case 2 limit = %d, want 20 (client limit)", limit2)
	}

	// Case 3: ClientInbound override (50 Mbps) overrides client limit (20) & inbound default (10).
	c3 := &model.ClientRecord{Email: "case3@test.com", DownLimit: 20, Enable: true}
	if err := db.Create(c3).Error; err != nil {
		t.Fatalf("Create c3: %v", err)
	}
	ci3 := &model.ClientInbound{ClientId: c3.Id, InboundId: ib.Id, DownLimit: 50}
	if err := db.Create(ci3).Error; err != nil {
		t.Fatalf("Create ci3: %v", err)
	}

	limit3, err := svc.ComputeClientEffectiveLimit(c3.Email, ib.Id)
	if err != nil {
		t.Fatalf("ComputeClientEffectiveLimit c3: %v", err)
	}
	if limit3 != 50 {
		t.Errorf("case 3 limit = %d, want 50 (client inbound override)", limit3)
	}

	// Case 4: No limits set anywhere -> 0.
	ibZero := mkInbound(t, 22002, model.VLESS, `{"clients":[]}`)
	ibZero.ClientDownLimit = 0
	if err := db.Save(ibZero).Error; err != nil {
		t.Fatalf("Save ibZero: %v", err)
	}
	c4 := &model.ClientRecord{Email: "case4@test.com", DownLimit: 0, Enable: true}
	if err := db.Create(c4).Error; err != nil {
		t.Fatalf("Create c4: %v", err)
	}
	limit4, err := svc.ComputeClientEffectiveLimit(c4.Email, ibZero.Id)
	if err != nil {
		t.Fatalf("ComputeClientEffectiveLimit c4: %v", err)
	}
	if limit4 != 0 {
		t.Errorf("case 4 limit = %d, want 0", limit4)
	}
}

func TestClientRateLimit_SaveClientDownLimitAndByInbound(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	inboundSvc := &InboundService{}
	db := database.GetDB()

	ib1 := mkInbound(t, 23001, model.VLESS, `{"clients":[]}`)
	ib1.ClientDownLimit = 10
	if err := db.Save(ib1).Error; err != nil {
		t.Fatalf("Save ib1: %v", err)
	}

	ib2 := mkInbound(t, 23002, model.VLESS, `{"clients":[]}`)
	ib2.ClientDownLimit = 15
	if err := db.Save(ib2).Error; err != nil {
		t.Fatalf("Save ib2: %v", err)
	}

	// Create client with DownLimit=100, and override for ib1=50. ib2 has no override.
	_, err := svc.Create(inboundSvc, &ClientCreatePayload{
		Client: model.Client{
			Email:              "user@test.com",
			ID:                 "11111111-2222-3333-4444-555555555555",
			SubID:              "sub-rate-limit",
			Enable:             true,
			DownLimit:          100,
			DownLimitByInbound: map[int]int{ib1.Id: 50},
		},
		InboundIds: []int{ib1.Id, ib2.Id},
	})
	if err != nil {
		t.Fatalf("Create client: %v", err)
	}

	// 1. Check clients table (ClientRecord).
	rec := lookupClientRecord(t, "user@test.com")
	if rec.DownLimit != 100 {
		t.Errorf("ClientRecord.DownLimit = %d, want 100", rec.DownLimit)
	}

	// 2. Check client_inbounds table.
	var ci1 model.ClientInbound
	if err := db.Where("client_id = ? AND inbound_id = ?", rec.Id, ib1.Id).First(&ci1).Error; err != nil {
		t.Fatalf("find ci1: %v", err)
	}
	if ci1.DownLimit != 50 {
		t.Errorf("ci1.DownLimit = %d, want 50", ci1.DownLimit)
	}

	var ci2 model.ClientInbound
	if err := db.Where("client_id = ? AND inbound_id = ?", rec.Id, ib2.Id).First(&ci2).Error; err != nil {
		t.Fatalf("find ci2: %v", err)
	}
	if ci2.DownLimit != 0 {
		t.Errorf("ci2.DownLimit = %d, want 0 (inherit)", ci2.DownLimit)
	}

	// 3. Check inbound.settings.clients.
	reloadedIb1, err := inboundSvc.GetInbound(ib1.Id)
	if err != nil {
		t.Fatalf("GetInbound ib1: %v", err)
	}
	clients1, err := inboundSvc.GetClients(reloadedIb1)
	if err != nil || len(clients1) == 0 {
		t.Fatalf("GetClients ib1: %v, len=%d", err, len(clients1))
	}
	if clients1[0].DownLimit != 50 {
		t.Errorf("ib1 embedded client DownLimit = %d, want 50 (effective)", clients1[0].DownLimit)
	}

	reloadedIb2, err := inboundSvc.GetInbound(ib2.Id)
	if err != nil {
		t.Fatalf("GetInbound ib2: %v", err)
	}
	clients2, err := inboundSvc.GetClients(reloadedIb2)
	if err != nil || len(clients2) == 0 {
		t.Fatalf("GetClients ib2: %v, len=%d", err, len(clients2))
	}
	if clients2[0].DownLimit != 100 {
		t.Errorf("ib2 embedded client DownLimit = %d, want 100 (client default)", clients2[0].DownLimit)
	}

	// 4. Check ClientLimitsByInbound.
	limits1 := svc.ClientLimitsByInbound(ib1.Id)
	if limits1["user@test.com"] != 50 {
		t.Errorf("ClientLimitsByInbound(ib1) = %v, want 50", limits1["user@test.com"])
	}
	limits2 := svc.ClientLimitsByInbound(ib2.Id)
	if limits2["user@test.com"] != 100 {
		t.Errorf("ClientLimitsByInbound(ib2) = %v, want 100", limits2["user@test.com"])
	}

	// 5. Check Update with modified limits.
	updateClient := clients1[0]
	updateClient.DownLimit = 120
	updateClient.DownLimitByInbound = map[int]int{ib1.Id: 0, ib2.Id: 80}
	if _, err := svc.Update(inboundSvc, rec.Id, updateClient, 0); err != nil {
		t.Fatalf("Update: %v", err)
	}

	recAfter := lookupClientRecord(t, "user@test.com")
	if recAfter.DownLimit != 120 {
		t.Errorf("ClientRecord.DownLimit after update = %d, want 120", recAfter.DownLimit)
	}

	if err := db.Where("client_id = ? AND inbound_id = ?", rec.Id, ib1.Id).First(&ci1).Error; err != nil {
		t.Fatalf("find ci1 after update: %v", err)
	}
	if ci1.DownLimit != 0 {
		t.Errorf("ci1.DownLimit after update = %d, want 0", ci1.DownLimit)
	}

	if err := db.Where("client_id = ? AND inbound_id = ?", rec.Id, ib2.Id).First(&ci2).Error; err != nil {
		t.Fatalf("find ci2 after update: %v", err)
	}
	if ci2.DownLimit != 80 {
		t.Errorf("ci2.DownLimit after update = %d, want 80", ci2.DownLimit)
	}

	// Verify settings.clients after update.
	ib1After, _ := inboundSvc.GetInbound(ib1.Id)
	c1After, _ := inboundSvc.GetClients(ib1After)
	if c1After[0].DownLimit != 120 {
		t.Errorf("ib1 embedded client DownLimit after update = %d, want 120", c1After[0].DownLimit)
	}
	ib2After, _ := inboundSvc.GetInbound(ib2.Id)
	c2After, _ := inboundSvc.GetClients(ib2After)
	if c2After[0].DownLimit != 80 {
		t.Errorf("ib2 embedded client DownLimit after update = %d, want 80", c2After[0].DownLimit)
	}
}

func TestClientRateLimit_SubNodeScopedUpdate(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	inboundSvc := &InboundService{}
	db := database.GetDB()

	ib1 := mkInbound(t, 24001, model.VLESS, `{"clients":[]}`)
	if err := db.Save(ib1).Error; err != nil {
		t.Fatalf("Save ib1: %v", err)
	}

	ib2 := mkInbound(t, 24002, model.VLESS, `{"clients":[]}`)
	if err := db.Save(ib2).Error; err != nil {
		t.Fatalf("Save ib2: %v", err)
	}

	_, err := svc.Create(inboundSvc, &ClientCreatePayload{
		Client: model.Client{
			Email:     "user-subnode@test.com",
			ID:        "33333333-4444-5555-6666-777777777777",
			SubID:     "sub-scoped-update",
			Enable:    true,
			DownLimit: 0,
		},
		InboundIds: []int{ib1.Id, ib2.Id},
	})
	if err != nil {
		t.Fatalf("Create client: %v", err)
	}

	rec := lookupClientRecord(t, "user-subnode@test.com")
	if rec.DownLimit != 0 {
		t.Fatalf("initial ClientRecord.DownLimit = %d, want 0", rec.DownLimit)
	}

	// Simulates master pushing UpdateUser for ib1 with effective DownLimit = 100.
	updateIb1 := model.Client{
		Email:     rec.Email,
		DownLimit: 100,
		Enable:    true,
	}
	if _, err := svc.Update(inboundSvc, rec.Id, updateIb1, 0, ib1.Id); err != nil {
		t.Fatalf("Update ib1: %v", err)
	}

	recAfter1 := lookupClientRecord(t, "user-subnode@test.com")
	if recAfter1.DownLimit != 0 {
		t.Errorf("ClientRecord.DownLimit after ib1 update = %d, want 0", recAfter1.DownLimit)
	}

	var ci1 model.ClientInbound
	if err := db.Where("client_id = ? AND inbound_id = ?", rec.Id, ib1.Id).First(&ci1).Error; err != nil {
		t.Fatalf("find ci1: %v", err)
	}
	if ci1.DownLimit != 100 {
		t.Errorf("ci1.DownLimit = %d, want 100", ci1.DownLimit)
	}

	var ci2 model.ClientInbound
	if err := db.Where("client_id = ? AND inbound_id = ?", rec.Id, ib2.Id).First(&ci2).Error; err != nil {
		t.Fatalf("find ci2: %v", err)
	}
	if ci2.DownLimit != 0 {
		t.Errorf("ci2.DownLimit = %d, want 0", ci2.DownLimit)
	}

	// Simulates master pushing UpdateUser for ib2 with effective DownLimit = 200.
	updateIb2 := model.Client{
		Email:     rec.Email,
		DownLimit: 200,
		Enable:    true,
	}
	if _, err := svc.Update(inboundSvc, rec.Id, updateIb2, 0, ib2.Id); err != nil {
		t.Fatalf("Update ib2: %v", err)
	}

	recAfter2 := lookupClientRecord(t, "user-subnode@test.com")
	if recAfter2.DownLimit != 0 {
		t.Errorf("ClientRecord.DownLimit after ib2 update = %d, want 0", recAfter2.DownLimit)
	}

	if err := db.Where("client_id = ? AND inbound_id = ?", rec.Id, ib2.Id).First(&ci2).Error; err != nil {
		t.Fatalf("find ci2 after update: %v", err)
	}
	if ci2.DownLimit != 200 {
		t.Errorf("ci2.DownLimit = %d, want 200", ci2.DownLimit)
	}

	// Verify ClientLimitsByInbound produces independent limits for ib1 and ib2.
	limits1 := svc.ClientLimitsByInbound(ib1.Id)
	if limits1[rec.Email] != 100 {
		t.Errorf("ClientLimitsByInbound(ib1) = %d, want 100", limits1[rec.Email])
	}
	limits2 := svc.ClientLimitsByInbound(ib2.Id)
	if limits2[rec.Email] != 200 {
		t.Errorf("ClientLimitsByInbound(ib2) = %d, want 200", limits2[rec.Email])
	}
}
