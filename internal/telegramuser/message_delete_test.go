package telegramuser

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func initTelegramDeleteTestDB(t *testing.T) {
	t.Helper()
	if err := database.InitDB(filepath.Join(t.TempDir(), "tg-delete.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
}

func TestDeleteMessageCleansRelaysAndRefreshesTimestamp(t *testing.T) {
	initTelegramDeleteTestDB(t)
	db := database.GetDB()

	const tgID int64 = 265758444
	client := model.ClientRecord{Email: "alice", SubID: "sub-alice"}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TelegramBinding{ClientID: client.Id, TgID: tgID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TelegramConversation{TgID: tgID, Username: "castaway27", Name: "Alice", LastMessageAt: 2000}).Error; err != nil {
		t.Fatal(err)
	}

	m1 := model.TelegramMessage{TgID: tgID, ClientID: client.Id, Direction: "incoming", Kind: "text", Text: "older", Role: "chat", DeliveryBot: "user", Status: "received", CreatedAt: 1000}
	m2 := model.TelegramMessage{TgID: tgID, ClientID: client.Id, Direction: "incoming", Kind: "text", Text: "newer", Role: "chat", DeliveryBot: "user", Status: "received", CreatedAt: 2000}
	mPending := model.TelegramMessage{TgID: tgID, ClientID: client.Id, Direction: "outgoing", Kind: "text", Text: "queued", Role: "web", DeliveryBot: "user", Status: "pending", CreatedAt: 1500}
	mSending := model.TelegramMessage{TgID: tgID, ClientID: client.Id, Direction: "outgoing", Kind: "text", Text: "inflight", Role: "web", DeliveryBot: "user", Status: "sending", CreatedAt: 1600}
	for _, row := range []*model.TelegramMessage{&m1, &m2, &mPending, &mSending} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	adminFwd := model.TelegramMessage{TgID: tgID, Direction: "outgoing", Kind: "text", Text: "fwd", Role: "chat", DeliveryBot: "admin", Status: "sent", ReplyID: m2.Id, CreatedAt: 2001}
	if err := db.Create(&adminFwd).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TelegramAdminRelay{BotKey: "k", AdminChatID: 99, AdminMessageID: 88, TgID: tgID, UserMessageID: m2.Id}).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := DeleteMessage(mPending.Id); err == nil || !strings.Contains(err.Error(), "pending or sending") {
		t.Fatalf("expected pending deletion rejection, got %v", err)
	}
	if _, err := DeleteMessage(mSending.Id); err == nil || !strings.Contains(err.Error(), "pending or sending") {
		t.Fatalf("expected sending deletion rejection, got %v", err)
	}
	if _, err := DeleteMessage(adminFwd.Id); err == nil {
		t.Fatal("expected admin-forward copy deletion to fail")
	}
	if _, err := DeleteMessage(999999); err == nil {
		t.Fatal("expected missing message ID deletion to fail")
	}

	res, err := DeleteMessage(m2.Id)
	if err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}
	if res.DeletedID != m2.Id {
		t.Fatalf("DeletedID = %d, want %d", res.DeletedID, m2.Id)
	}

	var count int64
	if err := db.Model(&model.TelegramMessage{}).Where("id IN ?", []int64{m2.Id, adminFwd.Id}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("message and admin forward not deleted: count=%d err=%v", count, err)
	}
	if err := db.Model(&model.TelegramAdminRelay{}).Where("user_message_id = ?", m2.Id).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("admin relay not deleted: count=%d err=%v", count, err)
	}
	var conv model.TelegramConversation
	if err := db.First(&conv, "tg_id = ?", tgID).Error; err != nil {
		t.Fatalf("conversation unexpectedly removed: %v", err)
	}
	if conv.LastMessageAt != 1600 {
		t.Fatalf("LastMessageAt = %d, want 1600", conv.LastMessageAt)
	}
	if _, err := BoundClient(tgID); err != nil {
		t.Fatalf("binding must remain intact: %v", err)
	}
	var logRow model.TelegramLog
	if err := db.Where("tg_id = ? AND message_id = ? AND event = ?", tgID, m2.Id, "message.deleted.admin").First(&logRow).Error; err != nil {
		t.Fatalf("missing deletion audit log: %v", err)
	}
}

func TestDeleteConversationRemovesEmptyAndRecreatesOnBoundSend(t *testing.T) {
	initTelegramDeleteTestDB(t)
	db := database.GetDB()

	const tgID int64 = 7490278826
	client := model.ClientRecord{Email: "bob", SubID: "sub-bob"}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TelegramBinding{ClientID: client.Id, TgID: tgID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TelegramConversation{TgID: tgID, Name: "Bob", LastMessageAt: 5000}).Error; err != nil {
		t.Fatal(err)
	}
	msg := model.TelegramMessage{TgID: tgID, ClientID: client.Id, Direction: "incoming", Kind: "text", Text: "hi", Role: "chat", DeliveryBot: "user", Status: "received", CreatedAt: 5000}
	if err := db.Create(&msg).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TelegramAdminRelay{BotKey: "k", AdminChatID: 1, AdminMessageID: 2, TgID: tgID, UserMessageID: msg.Id}).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := DeleteConversationMessages(999999999); err == nil {
		t.Fatal("expected unknown conversation deletion to fail")
	}

	res, err := DeleteConversationMessages(tgID)
	if err != nil {
		t.Fatalf("DeleteConversationMessages: %v", err)
	}
	if !res.ConversationDeleted || res.DeletedMessages != 1 || res.PreservedPending != 0 || res.TgID != tgID {
		t.Fatalf("unexpected result: %+v", res)
	}

	var convCount, msgCount, relayCount int64
	db.Model(&model.TelegramConversation{}).Where("tg_id = ?", tgID).Count(&convCount)
	db.Model(&model.TelegramMessage{}).Where("tg_id = ?", tgID).Count(&msgCount)
	db.Model(&model.TelegramAdminRelay{}).Where("tg_id = ?", tgID).Count(&relayCount)
	if convCount != 0 || msgCount != 0 || relayCount != 0 {
		t.Fatalf("expected clean deletion, got conv=%d msg=%d relay=%d", convCount, msgCount, relayCount)
	}
	if _, err := BoundClient(tgID); err != nil {
		t.Fatalf("binding must survive conversation deletion: %v", err)
	}

	Default.mu.Lock()
	Default.current = &receiver{ctx: context.Background()}
	Default.mu.Unlock()
	t.Cleanup(func() {
		Default.mu.Lock()
		Default.current = nil
		Default.mu.Unlock()
	})

	queued, err := QueueText(tgID, "hello again", 0, "web")
	if err != nil {
		t.Fatalf("QueueText after conversation deletion failed for bound user: %v", err)
	}
	if queued.TgID != tgID || queued.ClientID != client.Id {
		t.Fatalf("unexpected queued message: %+v", queued)
	}
	db.Model(&model.TelegramConversation{}).Where("tg_id = ?", tgID).Count(&convCount)
	if convCount != 1 {
		t.Fatalf("expected conversation recreated on bound send, got %d", convCount)
	}
	if _, err := QueueText(111222333, "unbound", 0, "web"); err == nil {
		t.Fatal("expected unbound missing conversation send to fail")
	}
}

func TestDeleteConversationPreservesPendingMessages(t *testing.T) {
	initTelegramDeleteTestDB(t)
	db := database.GetDB()

	const tgID int64 = 8974215785
	if err := db.Create(&model.TelegramConversation{TgID: tgID, Name: "Carl", LastMessageAt: 3000}).Error; err != nil {
		t.Fatal(err)
	}
	sent := model.TelegramMessage{TgID: tgID, Direction: "outgoing", Kind: "text", Text: "sent", Role: "web", DeliveryBot: "user", Status: "sent", CreatedAt: 3000}
	pending := model.TelegramMessage{TgID: tgID, Direction: "outgoing", Kind: "text", Text: "pending", Role: "web", DeliveryBot: "user", Status: "pending", CreatedAt: 2000}
	sending := model.TelegramMessage{TgID: tgID, Direction: "outgoing", Kind: "text", Text: "sending", Role: "web", DeliveryBot: "user", Status: "sending", CreatedAt: 2500}
	for _, row := range []*model.TelegramMessage{&sent, &pending, &sending} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}

	res, err := DeleteConversationMessages(tgID)
	if err != nil {
		t.Fatalf("DeleteConversationMessages: %v", err)
	}
	if res.ConversationDeleted || res.DeletedMessages != 1 || res.PreservedPending != 2 {
		t.Fatalf("unexpected result: %+v", res)
	}

	var conv model.TelegramConversation
	if err := db.First(&conv, "tg_id = ?", tgID).Error; err != nil {
		t.Fatalf("conversation with pending messages must remain: %v", err)
	}
	if conv.LastMessageAt != 2500 {
		t.Fatalf("LastMessageAt = %d, want 2500", conv.LastMessageAt)
	}
}
