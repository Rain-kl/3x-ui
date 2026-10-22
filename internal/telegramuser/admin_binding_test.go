package telegramuser

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestAdministratorBindingUsesSharedLifecycle(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "bindings.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	db := database.GetDB()
	client := model.ClientRecord{Email: "manual-client", SubID: "manual-sub", Group: "paid"}
	other := model.ClientRecord{Email: "other-client", SubID: "other-sub"}
	for _, c := range []*model.ClientRecord{&client, &other} {
		if err := db.Create(c).Error; err != nil {
			t.Fatal(err)
		}
	}
	code, err := CreateCode(client.Id)
	if err != nil {
		t.Fatal(err)
	}
	notices := []model.SubscriptionNotification{
		{Title: "matching", Body: "hello", TargetType: "both", ClientIDs: []int{client.Id}, Groups: []string{"paid"}, Published: true},
		{Title: "other", Body: "hidden", TargetType: "clients", ClientIDs: []int{other.Id}, Published: true},
	}
	if err := db.Create(&notices).Error; err != nil {
		t.Fatal(err)
	}
	first, err := AdminBind(AdminBindInput{ClientID: client.Id, TgID: 12345})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		repeat, err := AdminBind(AdminBindInput{ClientID: client.Id, TgID: 12345})
		if err != nil || repeat.Id != first.Id {
			t.Fatalf("idempotent binding=%+v err=%v", repeat, err)
		}
	}
	bound, err := BoundClient(12345)
	if err != nil || bound.Id != client.Id {
		t.Fatalf("user commands cannot resolve manual binding: %+v %v", bound, err)
	}
	conversations, err := Conversations("", 1, 20)
	if err != nil || len(conversations.Items) != 1 || conversations.Items[0].ClientID != client.Id {
		t.Fatalf("missing conversation: %+v %v", conversations, err)
	}
	var messages []model.TelegramMessage
	if err := db.Find(&messages).Error; err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].NoticeID != notices[0].Id || messages[0].TgID != 12345 {
		t.Fatalf("historical notice delivery duplicated or misdirected: %+v", messages)
	}
	var invitation model.TelegramBindCode
	if err := db.Where("hash = ?", codeHash(code.Code)).First(&invitation).Error; err != nil || invitation.UsedAt == 0 {
		t.Fatalf("invitation not consumed: %+v %v", invitation, err)
	}
	for _, input := range []AdminBindInput{{ClientID: other.Id, TgID: 12345}, {ClientID: client.Id, TgID: 54321}} {
		if _, err := AdminBind(input); err == nil || !strings.Contains(err.Error(), "already bound") {
			t.Fatalf("binding ownership overwritten: %v", err)
		}
	}
	if _, err := Bind(54321, client.SubID); err == nil || !strings.Contains(err.Error(), "already bound") {
		t.Fatalf("user binding overwrote admin binding: %v", err)
	}
	if err := Unbind(12345); err != nil {
		t.Fatal(err)
	}
	if _, err := Bind(54321, client.SubID); err != nil {
		t.Fatal(err)
	}
	if _, err := AdminBind(AdminBindInput{ClientID: other.Id, TgID: 54321}); err == nil || !strings.Contains(err.Error(), "already bound") {
		t.Fatalf("admin overwrote user binding: %v", err)
	}
}

func TestAdministratorBindingFailureRollsBackAllEffects(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "rollback.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	db := database.GetDB()
	client := model.ClientRecord{Email: "rollback-client", SubID: "rollback-sub"}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	code, err := CreateCode(client.Id)
	if err != nil {
		t.Fatal(err)
	}
	notice := model.SubscriptionNotification{Body: "notice", TargetType: "clients", ClientIDs: []int{client.Id}, Published: true}
	if err := db.Create(&notice).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropTable(&model.TelegramConversation{}); err != nil {
		t.Fatal(err)
	}
	_, err = AdminBind(AdminBindInput{ClientID: client.Id, TgID: 12345})
	if err == nil || !strings.Contains(err.Error(), "create manually bound Telegram conversation") {
		t.Fatalf("missing transactional failure: %v", err)
	}
	var invitation model.TelegramBindCode
	if err := db.Where("hash = ?", codeHash(code.Code)).First(&invitation).Error; err != nil || invitation.UsedAt != 0 {
		t.Fatalf("failed binding consumed invitation: %+v %v", invitation, err)
	}
	for _, table := range []any{&model.TelegramBinding{}, &model.TelegramMessage{}, &model.TelegramLog{}} {
		var count int64
		if err := db.Model(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("partial binding side effects in %T: %d %v", table, count, err)
		}
	}
	for _, input := range []AdminBindInput{{ClientID: client.Id, TgID: 0}, {ClientID: client.Id, TgID: -1}, {ClientID: client.Id, TgID: 9007199254740992}, {ClientID: 0, TgID: 12345}} {
		if _, err := AdminBind(input); err == nil || !strings.Contains(err.Error(), "valid numeric") {
			t.Fatalf("invalid ID accepted: %+v %v", input, err)
		}
	}
	if _, err := AdminBind(AdminBindInput{ClientID: client.Id + 100, TgID: 12345}); err == nil || !strings.Contains(err.Error(), "load binding client") {
		t.Fatalf("missing client accepted: %v", err)
	}
}
