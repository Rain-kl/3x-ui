package service

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func TestTrafficRatio_ClientTrafficMultiplier(t *testing.T) {
	db := initTrafficTestDB(t)
	svc := &InboundService{}

	// Inbound with TrafficRatio = 2.0
	ib := &model.Inbound{
		UserId:       1,
		Tag:          "in-ratio-2",
		Enable:       true,
		Port:         45001,
		Protocol:     model.VLESS,
		TrafficRatio: 2.0,
		Settings:     `{"clients":[]}`,
	}
	if err := db.Create(ib).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}

	// Register client stat for user@ratio.com attached to this inbound
	if err := svc.AddClientStat(db, ib.Id, &model.Client{
		Email:  "user@ratio.com",
		Enable: true,
	}); err != nil {
		t.Fatalf("AddClientStat: %v", err)
	}

	// Local traffic tick: Up 1000, Down 2000
	clientTraffics := []*xray.ClientTraffic{
		{
			InboundId: ib.Id,
			Email:     "user@ratio.com",
			Up:        1000,
			Down:      2000,
		},
	}
	if _, _, err := svc.AddTraffic(nil, clientTraffics); err != nil {
		t.Fatalf("AddTraffic: %v", err)
	}

	var row xray.ClientTraffic
	if err := db.Where("email = ?", "user@ratio.com").First(&row).Error; err != nil {
		t.Fatalf("find client traffic: %v", err)
	}
	// With 2.0 ratio: 1000 * 2.0 = 2000, 2000 * 2.0 = 4000
	if row.Up != 2000 {
		t.Errorf("row.Up = %d, want 2000 (1000 * 2.0)", row.Up)
	}
	if row.Down != 4000 {
		t.Errorf("row.Down = %d, want 4000 (2000 * 2.0)", row.Down)
	}

	// Second tick with another 500 up / 500 down -> +1000 up, +1000 down
	clientTraffics2 := []*xray.ClientTraffic{
		{
			InboundId: ib.Id,
			Email:     "user@ratio.com",
			Up:        500,
			Down:      500,
		},
	}
	if _, _, err := svc.AddTraffic(nil, clientTraffics2); err != nil {
		t.Fatalf("AddTraffic 2: %v", err)
	}
	if err := db.Where("email = ?", "user@ratio.com").First(&row).Error; err != nil {
		t.Fatalf("find client traffic: %v", err)
	}
	if row.Up != 3000 {
		t.Errorf("row.Up after second tick = %d, want 3000", row.Up)
	}
	if row.Down != 5000 {
		t.Errorf("row.Down after second tick = %d, want 5000", row.Down)
	}
}

func TestTrafficRatio_RemoteTrafficMultiplier(t *testing.T) {
	db := initTrafficTestDB(t)
	svc := &InboundService{}

	// Setup node and central inbound with TrafficRatio = 1.5
	nodeID := 1
	node := &model.Node{Id: nodeID, Name: "node1"}
	if err := db.Create(node).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}
	ib := &model.Inbound{
		UserId:       1,
		NodeID:       &nodeID,
		Tag:          "node-ib-1",
		Enable:       true,
		Port:         46001,
		Protocol:     model.VLESS,
		TrafficRatio: 1.5,
		Settings:     `{"clients":[]}`,
	}
	if err := db.Create(ib).Error; err != nil {
		t.Fatalf("create central inbound: %v", err)
	}

	// Initial client stat at 0
	if err := svc.AddClientStat(db, ib.Id, &model.Client{
		Email:  "remote@ratio.com",
		Enable: true,
	}); err != nil {
		t.Fatalf("AddClientStat: %v", err)
	}

	// First snapshot establishes baseline (0 up, 0 down)
	snap1 := &runtime.TrafficSnapshot{
		Inbounds: []*model.Inbound{
			{
				Tag: "node-ib-1",
				ClientStats: []xray.ClientTraffic{
					{Email: "remote@ratio.com", Up: 0, Down: 0},
				},
			},
		},
	}
	if _, err := svc.setRemoteTrafficLocked(nodeID, snap1, false, false); err != nil {
		t.Fatalf("setRemoteTrafficLocked baseline: %v", err)
	}

	// Second snapshot reports delta (Up: 1000, Down: 2000)
	snap2 := &runtime.TrafficSnapshot{
		Inbounds: []*model.Inbound{
			{
				Tag: "node-ib-1",
				ClientStats: []xray.ClientTraffic{
					{Email: "remote@ratio.com", Up: 1000, Down: 2000},
				},
			},
		},
	}
	if _, err := svc.setRemoteTrafficLocked(nodeID, snap2, false, false); err != nil {
		t.Fatalf("setRemoteTrafficLocked delta: %v", err)
	}

	var row xray.ClientTraffic
	if err := db.Where("email = ?", "remote@ratio.com").First(&row).Error; err != nil {
		t.Fatalf("find client traffic: %v", err)
	}
	// With 1.5 ratio: 1000 * 1.5 = 1500, 2000 * 1.5 = 3000
	if row.Up != 1500 {
		t.Errorf("row.Up = %d, want 1500 (1000 * 1.5)", row.Up)
	}
	if row.Down != 3000 {
		t.Errorf("row.Down = %d, want 3000 (2000 * 1.5)", row.Down)
	}
}
