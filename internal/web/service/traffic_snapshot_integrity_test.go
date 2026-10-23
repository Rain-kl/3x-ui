package service

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func TestTrafficSnapshotReadFailureNeverBillsSharedTotals(t *testing.T) {
	for _, ratio := range []float64{1, 2} {
		t.Run(fmt.Sprintf("ratio-%g", ratio), func(t *testing.T) {
			db := initTrafficTestDB(t)
			svc := &InboundService{}
			nodeID := 1
			if err := db.Create(&model.Node{Id: nodeID, Name: "snapshot-node"}).Error; err != nil {
				t.Fatal(err)
			}
			client := model.ClientRecord{Email: "snapshot@test", Enable: true}
			if err := db.Create(&client).Error; err != nil {
				t.Fatal(err)
			}
			inbounds := []model.Inbound{{UserId: 1, NodeID: &nodeID, Tag: "snapshot-a", Port: 51990, Protocol: model.VLESS, Enable: true, TrafficRatio: ratio}, {UserId: 1, NodeID: &nodeID, Tag: "snapshot-b", Port: 51991, Protocol: model.VLESS, Enable: true, TrafficRatio: 1}}
			for i := range inbounds {
				if err := db.Create(&inbounds[i]).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Create(&model.ClientInbound{ClientId: client.Id, InboundId: inbounds[i].Id, Up: []int64{1000, 3000}[i]}).Error; err != nil {
					t.Fatal(err)
				}
			}
			initial := int64(1000*ratio) + 3000
			if err := db.Create(&xray.ClientTraffic{Email: client.Email, InboundId: inbounds[0].Id, Up: initial, Enable: true}).Error; err != nil {
				t.Fatal(err)
			}
			merge := func(rows []*model.Inbound) {
				t.Helper()
				if _, err := svc.setRemoteTrafficLocked(nodeID, &runtime.TrafficSnapshot{Inbounds: rows}, false, false); err != nil {
					t.Fatal(err)
				}
			}
			rows, err := svc.GetInbounds(1)
			if err != nil {
				t.Fatal(err)
			}
			merge(rows)
			var before []model.NodeClientTraffic
			if err := db.Find(&before).Error; err != nil {
				t.Fatal(err)
			}
			fault := errors.New("injected raw-counter read failure")
			const callback = "snapshot_integrity_fault"
			if err := db.Callback().Row().Before("gorm:row").Register(callback, func(tx *gorm.DB) {
				if strings.Contains(strings.Join(tx.Statement.Selects, ","), "LOWER(clients.email)") {
					_ = tx.AddError(fault)
				}
			}); err != nil {
				t.Fatal(err)
			}
			for name, get := range map[string]func() error{
				"list":   func() error { _, err := svc.GetInbounds(1); return err },
				"all":    func() error { _, err := svc.GetAllInbounds(); return err },
				"detail": func() error { _, err := svc.GetInboundDetail(inbounds[0].Id); return err },
			} {
				if err := get(); !errors.Is(err, fault) || !strings.Contains(err.Error(), "read raw per-inbound client traffic") {
					t.Fatalf("%s returned %v", name, err)
				}
			}
			if err := db.Callback().Row().Remove(callback); err != nil {
				t.Fatal(err)
			}
			var after []model.NodeClientTraffic
			if err := db.Find(&after).Error; err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) || readTraffic(t, db, client.Email).Up != initial {
				t.Fatal("failed reads changed checkpoints or billed traffic")
			}
			recovered, err := svc.GetInbounds(1)
			if err != nil {
				t.Fatal(err)
			}
			merge(recovered)
			if got := readTraffic(t, db, client.Email).Up; got != initial {
				t.Fatalf("recovery rebilled history: got %d want %d", got, initial)
			}
			if err := db.Model(&model.ClientInbound{}).Where("client_id = ? AND inbound_id = ?", client.Id, inbounds[0].Id).Update("up", 1100).Error; err != nil {
				t.Fatal(err)
			}
			moved, err := svc.GetInbounds(1)
			if err != nil {
				t.Fatal(err)
			}
			merge(moved)
			merge(moved)
			if got, want := readTraffic(t, db, client.Email).Up, initial+int64(100*ratio); got != want {
				t.Fatalf("new traffic billed %d want %d", got, want)
			}
		})
	}
}

func TestTrafficSnapshotMembershipAndBackfillErrorsPropagate(t *testing.T) {
	db := initTrafficTestDB(t)
	svc := &InboundService{}
	ib := model.Inbound{UserId: 1, Tag: "snapshot-errors", Port: 51992, Protocol: model.VLESS, Enable: true}
	if err := db.Create(&ib).Error; err != nil {
		t.Fatal(err)
	}
	client := model.ClientRecord{Email: "snapshot-errors@test", Enable: true}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientInbound{ClientId: client.Id, InboundId: ib.Id}).Error; err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"clients", "client_traffics"} {
		t.Run(table, func(t *testing.T) {
			fault := fmt.Errorf("injected %s read failure", table)
			const callback = "snapshot_membership_fault"
			if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table == table {
					_ = tx.AddError(fault)
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer db.Callback().Query().Remove(callback)
			if _, err := svc.backfillClientStats(db, []*model.Inbound{{Id: ib.Id}}); !errors.Is(err, fault) {
				t.Fatalf("backfill error=%v", err)
			}
			if table == "clients" {
				if _, err := svc.GetInboundsSlim(1); !errors.Is(err, fault) {
					t.Fatalf("slim error=%v", err)
				}
			}
		})
	}
}

func TestTrafficSnapshotRejectsMissingRawAndDropsDetachedStats(t *testing.T) {
	db := initTrafficTestDB(t)
	svc := &InboundService{}
	ib := model.Inbound{UserId: 1, Tag: "snapshot-missing", Port: 51993, Protocol: model.VLESS, Enable: true}
	if err := db.Create(&ib).Error; err != nil {
		t.Fatal(err)
	}
	client := model.ClientRecord{Email: "snapshot-missing@test", Enable: true}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	link := model.ClientInbound{ClientId: client.Id, InboundId: ib.Id, Up: 1000}
	if err := db.Create(&link).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&xray.ClientTraffic{Email: client.Email, InboundId: ib.Id, Up: 9000, Enable: true}).Error; err != nil {
		t.Fatal(err)
	}
	const callback = "snapshot_missing_raw"
	if err := db.Callback().Row().Before("gorm:row").Register(callback, func(tx *gorm.DB) {
		if strings.Contains(strings.Join(tx.Statement.Selects, ","), "LOWER(clients.email)") {
			tx.Where("1 = 0")
		}
	}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.GetInbounds(1)
	if err == nil || !strings.Contains(err.Error(), "raw client traffic missing") {
		t.Fatalf("missing raw counters error=%v", err)
	}
	if err := db.Callback().Row().Remove(callback); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("client_id = ? AND inbound_id = ?", client.Id, ib.Id).Delete(&model.ClientInbound{}).Error; err != nil {
		t.Fatal(err)
	}
	rows, err := svc.GetInbounds(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || len(rows[0].ClientStats) != 0 {
		t.Fatalf("detached shared stats escaped into snapshot: %+v", rows)
	}
}
