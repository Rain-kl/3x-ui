package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func TestClientInboundTraffic_AccountingAndDepletion(t *testing.T) {
	db := initTrafficTestDB(t)
	svc := &InboundService{}

	settingsA, _ := json.Marshal(map[string]any{
		"clients": []map[string]any{
			{"email": "user@test.com", "enable": true},
		},
	})
	settingsB, _ := json.Marshal(map[string]any{
		"clients": []map[string]any{
			{"email": "user@test.com", "enable": true},
		},
	})

	ibA := &model.Inbound{UserId: 1, Tag: "node-a", Enable: true, Port: 41001, Protocol: model.VLESS, Settings: string(settingsA)}
	ibB := &model.Inbound{UserId: 1, Tag: "node-b", Enable: true, Port: 41002, Protocol: model.VLESS, Settings: string(settingsB)}
	if err := db.Create(ibA).Error; err != nil {
		t.Fatalf("create ibA: %v", err)
	}
	if err := db.Create(ibB).Error; err != nil {
		t.Fatalf("create ibB: %v", err)
	}

	clientRecord := &model.ClientRecord{
		Email:   "user@test.com",
		TotalGB: 200 << 30,
		Enable:  true,
	}
	if err := db.Create(clientRecord).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}

	ciA := &model.ClientInbound{ClientId: clientRecord.Id, InboundId: ibA.Id, TotalGB: 0}
	ciB := &model.ClientInbound{ClientId: clientRecord.Id, InboundId: ibB.Id, TotalGB: 50 << 30}
	if err := db.Create(ciA).Error; err != nil {
		t.Fatalf("create ciA: %v", err)
	}
	if err := db.Create(ciB).Error; err != nil {
		t.Fatalf("create ciB: %v", err)
	}

	if err := svc.AddClientStat(db, ibA.Id, &model.Client{Email: "user@test.com", Enable: true, TotalGB: 200 << 30}); err != nil {
		t.Fatalf("AddClientStat: %v", err)
	}

	traffics := []*xray.ClientTraffic{
		{InboundId: ibB.Id, Email: "user@test.com", Up: 25 << 30, Down: 26 << 30},
	}
	if _, _, err := svc.AddTraffic(nil, traffics); err != nil {
		t.Fatalf("AddTraffic: %v", err)
	}

	var rowB model.ClientInbound
	if err := db.Where("client_id = ? AND inbound_id = ?", clientRecord.Id, ibB.Id).First(&rowB).Error; err != nil {
		t.Fatalf("query ciB: %v", err)
	}
	if rowB.Up+rowB.Down != 51<<30 {
		t.Errorf("ciB used = %d, want %d", rowB.Up+rowB.Down, 51<<30)
	}

	var globalRow xray.ClientTraffic
	if err := db.Where("email = ?", "user@test.com").First(&globalRow).Error; err != nil {
		t.Fatalf("query global: %v", err)
	}
	if globalRow.Up+globalRow.Down != 51<<30 {
		t.Errorf("global used = %d, want %d", globalRow.Up+globalRow.Down, 51<<30)
	}

	var cr model.ClientRecord
	if err := db.First(&cr, clientRecord.Id).Error; err != nil {
		t.Fatalf("query cr: %v", err)
	}
	if !cr.Enable {
		t.Errorf("cr.Enable = false, want true (client must remain enabled globally)")
	}

	var freshA, freshB model.Inbound
	if err := db.First(&freshA, ibA.Id).Error; err != nil {
		t.Fatalf("query ibA: %v", err)
	}
	if err := db.First(&freshB, ibB.Id).Error; err != nil {
		t.Fatalf("query ibB: %v", err)
	}
	clientsA, err := svc.GetClients(&freshA)
	if err != nil || len(clientsA) == 0 || !clientsA[0].Enable {
		t.Errorf("ibA client should remain enabled, got %+v", clientsA)
	}
	clientsB, err := svc.GetClients(&freshB)
	if err != nil || len(clientsB) == 0 || clientsB[0].Enable {
		t.Errorf("ibB client should be disabled, got %+v", clientsB)
	}
}

func TestClientInboundTraffic_ResetAndReenable(t *testing.T) {
	db := initTrafficTestDB(t)
	svc := &InboundService{}

	settingsB, _ := json.Marshal(map[string]any{
		"clients": []map[string]any{
			{"email": "depleted@test.com", "enable": false},
		},
	})
	ibB := &model.Inbound{UserId: 1, Tag: "node-b", Enable: true, Port: 42002, Protocol: model.VLESS, Settings: string(settingsB)}
	if err := db.Create(ibB).Error; err != nil {
		t.Fatalf("create ibB: %v", err)
	}

	clientRecord := &model.ClientRecord{
		Email:   "depleted@test.com",
		TotalGB: 200 << 30,
		Enable:  true,
	}
	if err := db.Create(clientRecord).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}

	ciB := &model.ClientInbound{ClientId: clientRecord.Id, InboundId: ibB.Id, TotalGB: 50 << 30, Up: 30 << 30, Down: 25 << 30}
	if err := db.Create(ciB).Error; err != nil {
		t.Fatalf("create ciB: %v", err)
	}

	if err := svc.AddClientStat(db, ibB.Id, &model.Client{Email: "depleted@test.com", Enable: true, TotalGB: 200 << 30}); err != nil {
		t.Fatalf("AddClientStat: %v", err)
	}

	if _, err := svc.ResetClientTraffic(ibB.Id, "depleted@test.com"); err != nil {
		t.Fatalf("ResetClientTraffic: %v", err)
	}

	var rowB model.ClientInbound
	if err := db.Where("client_id = ? AND inbound_id = ?", clientRecord.Id, ibB.Id).First(&rowB).Error; err != nil {
		t.Fatalf("query ciB: %v", err)
	}
	if rowB.Up != 0 || rowB.Down != 0 {
		t.Errorf("ciB counters not reset: up=%d, down=%d", rowB.Up, rowB.Down)
	}

	var reloadedIb model.Inbound
	if err := db.First(&reloadedIb, ibB.Id).Error; err != nil {
		t.Fatalf("query reloadedIb: %v", err)
	}
	clients, err := svc.GetClients(&reloadedIb)
	if err != nil || len(clients) == 0 || !clients[0].Enable {
		t.Errorf("client in ibB settings should be re-enabled, got %+v", clients)
	}

	cs := &ClientService{}
	traffics, err := cs.InboundTrafficsByClientId(clientRecord.Id)
	if err != nil {
		t.Fatalf("InboundTrafficsByClientId: %v", err)
	}
	tr, ok := traffics[ibB.Id]
	if !ok {
		t.Fatalf("missing traffic for inbound %d", ibB.Id)
	}
	if tr.Used != 0 || tr.Depleted || tr.Remained != 50<<30 {
		t.Errorf("traffic info mismatch: %+v", tr)
	}
}

func TestClientInboundTraffic_CRUD(t *testing.T) {
	db := initTrafficTestDB(t)
	inboundSvc := &InboundService{}
	clientSvc := &ClientService{}

	ib1 := &model.Inbound{UserId: 1, Tag: "ib1", Enable: true, Port: 43001, Protocol: model.VLESS, Settings: `{"clients":[]}`}
	ib2 := &model.Inbound{UserId: 1, Tag: "ib2", Enable: true, Port: 43002, Protocol: model.VLESS, Settings: `{"clients":[]}`}
	if err := db.Create(ib1).Error; err != nil {
		t.Fatalf("create ib1: %v", err)
	}
	if err := db.Create(ib2).Error; err != nil {
		t.Fatalf("create ib2: %v", err)
	}

	createPayload := &ClientCreatePayload{
		Client: model.Client{
			Email:   "crud@test.com",
			TotalGB: 100 << 30,
			Enable:  true,
			TotalGBByInbound: map[int]int64{
				ib1.Id: 30 << 30,
				ib2.Id: 40 << 30,
			},
		},
		InboundIds: []int{ib1.Id, ib2.Id},
	}
	if _, err := clientSvc.Create(inboundSvc, createPayload); err != nil {
		t.Fatalf("Create: %v", err)
	}

	c, err := clientSvc.GetClient("crud@test.com")
	if err != nil {
		t.Fatalf("GetClient: %v", err)
	}
	if c.TotalGBByInbound[ib1.Id] != 30<<30 || c.TotalGBByInbound[ib2.Id] != 40<<30 {
		t.Errorf("TotalGBByInbound mismatch: %v", c.TotalGBByInbound)
	}
	if c.InboundTraffics[ib1.Id].Total != 30<<30 || c.InboundTraffics[ib2.Id].Total != 40<<30 {
		t.Errorf("InboundTraffics totals mismatch: %+v", c.InboundTraffics)
	}

	list, err := clientSvc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].TotalGBByInbound[ib1.Id] != 30<<30 {
		t.Errorf("List TotalGBByInbound mismatch: %+v", list)
	}

	updateClient := *c
	updateClient.TotalGBByInbound = map[int]int64{
		ib1.Id: 60 << 30,
		ib2.Id: 70 << 30,
	}
	rec, err := clientSvc.GetRecordByEmail(nil, "crud@test.com")
	if err != nil {
		t.Fatalf("GetRecordByEmail: %v", err)
	}
	if _, err := clientSvc.Update(inboundSvc, rec.Id, updateClient, 0); err != nil {
		t.Fatalf("Update: %v", err)
	}

	updated, err := clientSvc.GetClient("crud@test.com")
	if err != nil {
		t.Fatalf("GetClient after update: %v", err)
	}
	if updated.TotalGBByInbound[ib1.Id] != 60<<30 || updated.TotalGBByInbound[ib2.Id] != 70<<30 {
		t.Errorf("TotalGBByInbound after update mismatch: %v", updated.TotalGBByInbound)
	}
}

func TestClientInboundTraffic_AutoRenew(t *testing.T) {
	db := initTrafficTestDB(t)
	svc := &InboundService{}

	settings, _ := json.Marshal(map[string]any{
		"clients": []map[string]any{
			{"email": "renew@test.com", "enable": false},
		},
	})
	ib := &model.Inbound{UserId: 1, Tag: "renew-node", Enable: true, Port: 44001, Protocol: model.VLESS, Settings: string(settings)}
	if err := db.Create(ib).Error; err != nil {
		t.Fatalf("create ib: %v", err)
	}

	rec := &model.ClientRecord{
		Email:   "renew@test.com",
		TotalGB: 100 << 30,
		Enable:  true,
	}
	if err := db.Create(rec).Error; err != nil {
		t.Fatalf("create rec: %v", err)
	}

	ci := &model.ClientInbound{ClientId: rec.Id, InboundId: ib.Id, TotalGB: 20 << 30, Up: 15 << 30, Down: 10 << 30}
	if err := db.Create(ci).Error; err != nil {
		t.Fatalf("create ci: %v", err)
	}

	past := time.Now().Add(-2 * time.Hour).UnixMilli()
	traffic := &xray.ClientTraffic{
		InboundId:  ib.Id,
		Email:      "renew@test.com",
		Total:      100 << 30,
		Up:         15 << 30,
		Down:       10 << 30,
		Enable:     true,
		Reset:      1,
		ExpiryTime: past,
	}
	if err := db.Create(traffic).Error; err != nil {
		t.Fatalf("create traffic: %v", err)
	}

	batch := newTrafficMutationBatch()
	if _, count, err := svc.autoRenewClients(db, batch); err != nil {
		t.Fatalf("autoRenewClients: %v", err)
	} else if count != 1 {
		t.Fatalf("expected 1 renewed client, got %d", count)
	}

	var row model.ClientInbound
	if err := db.Where("client_id = ? AND inbound_id = ?", rec.Id, ib.Id).First(&row).Error; err != nil {
		t.Fatalf("query ci: %v", err)
	}
	if row.Up != 0 || row.Down != 0 {
		t.Errorf("ci after auto-renew not reset: up=%d, down=%d", row.Up, row.Down)
	}

	var freshIb model.Inbound
	if err := db.First(&freshIb, ib.Id).Error; err != nil {
		t.Fatalf("query freshIb: %v", err)
	}
	clients, err := svc.GetClients(&freshIb)
	if err != nil || len(clients) == 0 || !clients[0].Enable {
		t.Errorf("client in settings should be re-enabled after auto-renew, got %+v", clients)
	}
}

func TestClientInboundTraffic_RemoteNodeAccounting(t *testing.T) {
	db := initTrafficTestDB(t)
	createNodeInbound(t, db, 1, "n1-in", 45001)
	svc := &InboundService{}

	var nodeIb model.Inbound
	if err := db.Where("tag = ?", "n1-in").First(&nodeIb).Error; err != nil {
		t.Fatalf("find nodeIb: %v", err)
	}

	cr := &model.ClientRecord{Email: "remotename@test.com", TotalGB: 100 << 30, Enable: true}
	if err := db.Create(cr).Error; err != nil {
		t.Fatalf("create cr: %v", err)
	}
	ci := &model.ClientInbound{ClientId: cr.Id, InboundId: nodeIb.Id, TotalGB: 30 << 30}
	if err := db.Create(ci).Error; err != nil {
		t.Fatalf("create ci: %v", err)
	}

	snap1 := &runtime.TrafficSnapshot{
		Inbounds: []*model.Inbound{{Tag: "n1-in", ClientStats: []xray.ClientTraffic{
			{Email: "remotename@test.com", Up: 500, Down: 600, Enable: true},
		}}},
	}
	if _, err := svc.setRemoteTrafficLocked(1, snap1, false, false); err != nil {
		t.Fatalf("setRemoteTrafficLocked 1: %v", err)
	}

	snap2 := &runtime.TrafficSnapshot{
		Inbounds: []*model.Inbound{{Tag: "n1-in", ClientStats: []xray.ClientTraffic{
			{Email: "remotename@test.com", Up: 700, Down: 900, Enable: true},
		}}},
	}
	if _, err := svc.setRemoteTrafficLocked(1, snap2, false, false); err != nil {
		t.Fatalf("setRemoteTrafficLocked 2: %v", err)
	}

	var row model.ClientInbound
	if err := db.Where("client_id = ? AND inbound_id = ?", cr.Id, nodeIb.Id).First(&row).Error; err != nil {
		t.Fatalf("query ci: %v", err)
	}
	if row.Up != 200 || row.Down != 300 {
		t.Errorf("ci remote accounting mismatch: up=%d (want 200), down=%d (want 300)", row.Up, row.Down)
	}
}

func TestClientInboundTraffic_ResetByEmail(t *testing.T) {
	db := initTrafficTestDB(t)
	svc := &InboundService{}

	if err := db.Create(&model.Node{Id: 1, Name: "node-1", Address: "127.0.0.1", Port: 2053, ConfigDirty: false}).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}
	createNodeInbound(t, db, 1, "n1-in", 46001)
	var nodeIb model.Inbound
	if err := db.Where("tag = ?", "n1-in").First(&nodeIb).Error; err != nil {
		t.Fatalf("find nodeIb: %v", err)
	}

	settingsA, _ := json.Marshal(map[string]any{
		"clients": []map[string]any{
			{"email": "resetall@test.com", "enable": false, "id": "uuid-a"},
		},
	})
	ibLocal := &model.Inbound{UserId: 1, Tag: "ib-local", Enable: true, Port: 46002, Protocol: model.VLESS, Settings: string(settingsA)}
	if err := db.Create(ibLocal).Error; err != nil {
		t.Fatalf("create ibLocal: %v", err)
	}

	settingsB, _ := json.Marshal(map[string]any{
		"clients": []map[string]any{
			{"email": "resetall@test.com", "enable": false, "id": "uuid-b"},
		},
	})
	if err := db.Model(&model.Inbound{}).Where("id = ?", nodeIb.Id).Update("settings", string(settingsB)).Error; err != nil {
		t.Fatalf("update nodeIb settings: %v", err)
	}

	cr := &model.ClientRecord{Email: "resetall@test.com", TotalGB: 100 << 30, Enable: false}
	if err := db.Create(cr).Error; err != nil {
		t.Fatalf("create cr: %v", err)
	}
	ci1 := &model.ClientInbound{ClientId: cr.Id, InboundId: ibLocal.Id, TotalGB: 50 << 30, Up: 30 << 30, Down: 30 << 30}
	if err := db.Create(ci1).Error; err != nil {
		t.Fatalf("create ci1: %v", err)
	}
	ci2 := &model.ClientInbound{ClientId: cr.Id, InboundId: nodeIb.Id, TotalGB: 50 << 30, Up: 40 << 30, Down: 40 << 30}
	if err := db.Create(ci2).Error; err != nil {
		t.Fatalf("create ci2: %v", err)
	}
	ct := &xray.ClientTraffic{InboundId: ibLocal.Id, Email: "resetall@test.com", Enable: false, Up: 70 << 30, Down: 70 << 30}
	if err := db.Create(ct).Error; err != nil {
		t.Fatalf("create ct: %v", err)
	}

	if err := svc.ResetClientTrafficByEmail("resetall@test.com"); err != nil {
		t.Fatalf("ResetClientTrafficByEmail: %v", err)
	}

	var refreshedCt xray.ClientTraffic
	if err := db.Where("email = ?", "resetall@test.com").First(&refreshedCt).Error; err != nil {
		t.Fatalf("query refreshedCt: %v", err)
	}
	if !refreshedCt.Enable || refreshedCt.Up != 0 || refreshedCt.Down != 0 {
		t.Errorf("ct not reset: enable=%v, up=%d, down=%d", refreshedCt.Enable, refreshedCt.Up, refreshedCt.Down)
	}

	var refreshedCr model.ClientRecord
	if err := db.First(&refreshedCr, cr.Id).Error; err != nil {
		t.Fatalf("query refreshedCr: %v", err)
	}
	if !refreshedCr.Enable {
		t.Errorf("client record enable should be true")
	}

	var cis []model.ClientInbound
	if err := db.Where("client_id = ?", cr.Id).Find(&cis).Error; err != nil {
		t.Fatalf("query cis: %v", err)
	}
	for _, ci := range cis {
		if ci.Up != 0 || ci.Down != 0 {
			t.Errorf("ci inbound %d not reset: up=%d, down=%d", ci.InboundId, ci.Up, ci.Down)
		}
	}

	var checkLocalIb model.Inbound
	if err := db.First(&checkLocalIb, ibLocal.Id).Error; err != nil {
		t.Fatalf("query checkLocalIb: %v", err)
	}
	localClients, _ := svc.GetClients(&checkLocalIb)
	if len(localClients) == 0 || !localClients[0].Enable {
		t.Errorf("local inbound client not enabled in settings: %+v", localClients)
	}

	var checkNodeIb model.Inbound
	if err := db.First(&checkNodeIb, nodeIb.Id).Error; err != nil {
		t.Fatalf("query checkNodeIb: %v", err)
	}
	nodeClients, _ := svc.GetClients(&checkNodeIb)
	if len(nodeClients) == 0 || !nodeClients[0].Enable {
		t.Errorf("node inbound client not enabled in settings: %+v", nodeClients)
	}

	var node model.Node
	if err := db.First(&node, 1).Error; err != nil {
		t.Fatalf("query node: %v", err)
	}
	if !node.ConfigDirty {
		t.Errorf("remote node was not marked dirty")
	}
}
