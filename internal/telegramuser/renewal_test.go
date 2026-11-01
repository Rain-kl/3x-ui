package telegramuser

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestRenewalRemindersDeduplicateAndInvalidate(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "renewals.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	db := database.GetDB()
	now := time.Now().Truncate(time.Second)
	c := model.ClientRecord{Email: "renewal@test", Enable: true, ExpiryTime: now.Add(7 * 24 * time.Hour).UnixMilli()}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TelegramBinding{ClientID: c.Id, TgID: 12345}).Error; err != nil {
		t.Fatal(err)
	}
	for _, days := range []int{0, 0, 4, 4, 6, 6} {
		if err := queueRenewalReminders(now.Add(time.Duration(days) * 24 * time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	var rows []model.TelegramMessage
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d reminders, want 7/3/1 exactly once", len(rows))
	}
	for _, row := range rows {
		if row.TgID != 12345 || row.ClientID != c.Id {
			t.Fatal("wrong audience")
		}
	}
	if err := db.Model(&c).Update("expiry_time", now.Add(30*24*time.Hour).UnixMilli()).Error; err != nil {
		t.Fatal(err)
	}
	if valid, err := validRenewal(&rows[0]); err != nil || valid {
		t.Fatalf("renewed reminder valid=%v err=%v", valid, err)
	}
	if err := queueRenewalReminders(now.Add(23 * 24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&model.TelegramMessage{}).Count(&count)
	if count != 4 {
		t.Fatalf("new expiry reminders=%d", count)
	}
}
