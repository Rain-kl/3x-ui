package service

import (
	"testing"
	"time"
)

func TestNextTrafficResetCalendar(t *testing.T) {
	loc := time.FixedZone("SGT", 8*3600)
	now := time.Date(2026, 2, 28, 14, 20, 0, 0, loc)
	for _, tt := range []struct {
		cycle string
		day   int
		want  string
	}{{"monthly", 31, "2026-03-31 00:00"}, {"daily", 0, "2026-03-01 00:00"}, {"weekly", 0, "2026-03-01 00:00"}, {"hourly", 0, "2026-02-28 15:00"}} {
		if got := nextTrafficReset(tt.cycle, tt.day, now).Format("2006-01-02 15:04"); got != tt.want {
			t.Fatalf("%s got %s want %s", tt.cycle, got, tt.want)
		}
	}
	if !nextTrafficReset("never", 0, now).IsZero() {
		t.Fatal("never must not schedule")
	}
}
