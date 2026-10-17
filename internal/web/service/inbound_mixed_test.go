package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestMixedLegacySaveCreatesNormalClients(t *testing.T) {
	initTrafficTestDB(t)
	svc := &InboundService{}
	inbound := &model.Inbound{Enable: true, Tag: "mixed-legacy", Port: 35432, Protocol: model.Mixed, Settings: `{"auth":"password","accounts":[{"user":"old-user","pass":"old-pass"}]}`, StreamSettings: `{"network":"tcp","security":"none"}`}
	added, _, err := svc.AddInbound(inbound)
	if err != nil {
		t.Fatal(err)
	}
	var original []model.Client
	for range 2 {
		clients, err := svc.ListClientsForInbound(added.Id)
		if err != nil || len(clients) != 1 {
			t.Fatalf("managed clients: %v %+v", err, clients)
		}
		if clients[0].Username != "old-user" || clients[0].Password != "old-pass" {
			t.Fatalf("credentials changed: %q", clients[0].Email)
		}
		if original == nil {
			original = clients
		} else if original[0].Email != clients[0].Email {
			t.Fatal("migration changed client identity")
		}
		var settings map[string]json.RawMessage
		if err := json.Unmarshal([]byte(added.Settings), &settings); err != nil {
			t.Fatal(err)
		}
		if settings["accounts"] != nil || settings["clients"] == nil {
			t.Fatalf("two user sources: %s", added.Settings)
		}
		updated, _, err := svc.UpdateInbound(added)
		if err != nil {
			t.Fatal(err)
		}
		added = updated
	}
	var count int64
	database.GetDB().Model(&model.ClientRecord{}).Count(&count)
	if count != 1 {
		t.Fatalf("duplicate migrated clients: %d", count)
	}
}

func TestMixedClientAttachmentAndDuplicateLogin(t *testing.T) {
	initTrafficTestDB(t)
	svc := &InboundService{}
	inbound, _, err := svc.AddInbound(&model.Inbound{Enable: true, Tag: "mixed-test", Port: 35433, Protocol: model.Mixed, Settings: `{"auth":"password","clients":[]}`, StreamSettings: `{"network":"tcp","security":"none"}`})
	if err != nil {
		t.Fatal(err)
	}
	clients := &ClientService{}
	for i, email := range []string{"first@example.test", "second@example.test"} {
		_, err := clients.Create(svc, &ClientCreatePayload{Client: model.Client{Email: email, Username: "same-login", Password: "secret", Enable: true}, InboundIds: []int{inbound.Id}})
		if i == 0 && err != nil {
			t.Fatal(err)
		}
		if i == 1 && (err == nil || !strings.Contains(err.Error(), "already used")) {
			t.Fatalf("duplicate login: %v", err)
		}
	}
	stored, err := svc.ListClientsForInbound(inbound.Id)
	if err != nil || len(stored) != 1 || stored[0].Username != "same-login" {
		t.Fatalf("attachment: %+v %v", stored, err)
	}
}

func TestCopiedMixedClientGetsIndependentLogin(t *testing.T) {
	svc := &InboundService{}
	source := model.Client{Email: "original", Username: "original-login", Password: "old", Enable: true}
	target, err := svc.buildTargetClientFromSource(source, &model.Inbound{Protocol: model.Mixed}, "copied-client", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Username != "copied-client" || target.Password == "old" || target.Password == "" {
		t.Fatal("mixed copy reused the original identity or credentials")
	}
}
