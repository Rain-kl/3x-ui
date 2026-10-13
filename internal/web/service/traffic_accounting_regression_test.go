package service

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func TestRemoteTrafficResetAndRatioChange(t *testing.T) {
	for _, scenario := range []string{"counter-reset", "ratio-change", "failed-write"} {
		t.Run(scenario, func(t *testing.T) {
			db := initTrafficTestDB(t)
			svc := &InboundService{}
			const email = "accounting@test"
			createNodeInboundWithClient(t, db, 1, "accounting-in", 45678, email)
			var ib model.Inbound
			if err := db.Where("tag = ?", "accounting-in").First(&ib).Error; err != nil {
				t.Fatal(err)
			}
			cr := model.ClientRecord{Email: email, Enable: true}
			if err := db.Create(&cr).Error; err != nil {
				t.Fatal(err)
			}
			ci := model.ClientInbound{ClientId: cr.Id, InboundId: ib.Id}
			if err := db.Create(&ci).Error; err != nil {
				t.Fatal(err)
			}
			if err := svc.AddClientStat(db, ib.Id, &model.Client{Email: email, Enable: true}); err != nil {
				t.Fatal(err)
			}
			push := func(up int64) {
				t.Helper()
				snap := &runtime.TrafficSnapshot{Inbounds: []*model.Inbound{{Id: 101, Tag: ib.Tag, Enable: true, Settings: ib.Settings, ClientStats: []xray.ClientTraffic{{InboundId: 101, Email: email, Up: up, Enable: true}}}}}
				if _, err := svc.setRemoteTrafficLocked(1, snap, false, false); err != nil {
					t.Fatal(err)
				}
			}
			push(0)
			push(1000)
			wantBill, wantDetail := int64(1200), int64(1200)
			switch scenario {
			case "counter-reset":
				push(200)
			case "ratio-change":
				if err := db.Model(&ib).Update("traffic_ratio", 2.0).Error; err != nil {
					t.Fatal(err)
				}
				push(1000)
				if got := readTraffic(t, db, email).Up; got != 1000 {
					t.Fatalf("ratio change rebilled history: got %d, want 1000", got)
				}
				push(1100)
				wantDetail = 1100
			case "failed-write":
				if err := db.Exec(`CREATE TRIGGER reject_usage BEFORE UPDATE ON client_inbounds BEGIN SELECT RAISE(ABORT, 'accounting write rejected'); END`).Error; err != nil {
					t.Fatal(err)
				}
				snap := &runtime.TrafficSnapshot{Inbounds: []*model.Inbound{{Id: 101, Tag: ib.Tag, Enable: true, Settings: ib.Settings, ClientStats: []xray.ClientTraffic{{InboundId: 101, Email: email, Up: 1100, Enable: true}}}}}
				if _, err := svc.setRemoteTrafficLocked(1, snap, false, false); err == nil || err.Error() != "accounting write rejected" {
					t.Fatalf("rejected write error=%v", err)
				}
				if got := readTraffic(t, db, email).Up; got != 1000 {
					t.Fatalf("failed transaction committed bill=%d", got)
				}
				if err := db.Exec(`DROP TRIGGER reject_usage`).Error; err != nil {
					t.Fatal(err)
				}
				push(1100)
				wantBill, wantDetail = 1100, 1100
			}
			push(map[string]int64{"counter-reset": 200, "ratio-change": 1100, "failed-write": 1100}[scenario])
			if err := db.Where("client_id = ? AND inbound_id = ?", cr.Id, ib.Id).First(&ci).Error; err != nil {
				t.Fatal(err)
			}
			if bill := readTraffic(t, db, email).Up; bill != wantBill || ci.Up != wantDetail {
				t.Fatalf("bill/detail=%d/%d, want %d/%d", bill, ci.Up, wantBill, wantDetail)
			}
		})
	}
}

func TestScopedLocalTrafficAcrossConcurrentInbounds(t *testing.T) {
	db := initTrafficTestDB(t)
	svc := &InboundService{}
	cr := model.ClientRecord{Email: "parallel@test", Enable: true}
	if err := db.Create(&cr).Error; err != nil {
		t.Fatal(err)
	}
	inbounds := []model.Inbound{{UserId: 1, Tag: "parallel-a", Port: 45679, Protocol: model.VLESS, Enable: true, TrafficRatio: 2}, {UserId: 1, Tag: "parallel-b", Port: 45680, Protocol: model.VLESS, Enable: true, TrafficRatio: 1}}
	for i := range inbounds {
		if err := db.Create(&inbounds[i]).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.ClientInbound{ClientId: cr.Id, InboundId: inbounds[i].Id}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.AddClientStat(db, inbounds[0].Id, &model.Client{Email: cr.Email, Enable: true}); err != nil {
		t.Fatal(err)
	}
	samples := []*xray.ClientTraffic{{Email: cr.Email, InboundTag: inbounds[0].Tag, Up: 100}, {Email: cr.Email, InboundTag: inbounds[1].Tag, Up: 300}}
	if _, _, err := svc.AddTraffic(nil, samples); err != nil {
		t.Fatal(err)
	}
	if got := readTraffic(t, db, cr.Email).Up; got != 500 {
		t.Fatalf("billed=%d, want 500", got)
	}
	for i, want := range []int64{100, 300} {
		var ci model.ClientInbound
		if err := db.Where("client_id = ? AND inbound_id = ?", cr.Id, inbounds[i].Id).First(&ci).Error; err != nil {
			t.Fatal(err)
		}
		if ci.Up != want {
			t.Fatalf("inbound %s=%d, want %d", inbounds[i].Tag, ci.Up, want)
		}
		if samples[i].Up != want {
			t.Fatalf("sample mutated: %d, want %d", samples[i].Up, want)
		}
	}
}
