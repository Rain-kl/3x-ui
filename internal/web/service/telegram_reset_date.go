package service

import (
	"time"
)

func nextTrafficReset(cycle string, day int, now time.Time) time.Time {
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch cycle {
	case "hourly":
		return time.Date(now.Year(), now.Month(), now.Day(), now.Hour()+1, 0, 0, 0, now.Location())
	case "daily":
		return midnight.AddDate(0, 0, 1)
	case "weekly":
		return midnight.AddDate(0, 0, 7-int(now.Weekday()))
	case "monthly":
		if day < 1 {
			day = 1
		}
		if day > 31 {
			day = 31
		}
		for m := 0; m < 2; m++ {
			month := time.Date(now.Year(), now.Month()+time.Month(m), 1, 0, 0, 0, 0, now.Location())
			last := month.AddDate(0, 1, -1).Day()
			candidate := time.Date(month.Year(), month.Month(), min(day, last), 0, 0, 0, 0, now.Location())
			if candidate.After(now) {
				return candidate
			}
		}
	}
	return time.Time{}
}

func userTrafficResetDate(clientID int, cycle string, day int) (string, error) {
	loc, err := (&SettingService{}).GetTimeLocation()
	if err != nil {
		return "", err
	}
	now := time.Now().In(loc)
	next := nextTrafficReset(cycle, day, now)
	ids, err := (&ClientService{}).GetInboundIdsForRecord(clientID)
	if err != nil {
		return "", err
	}
	for _, id := range ids {
		ib, err := (&InboundService{}).GetInbound(id)
		if err != nil {
			return "", err
		}
		candidate := nextTrafficReset(ib.TrafficReset, ib.TrafficResetDay, now)
		if !candidate.IsZero() && (next.IsZero() || candidate.Before(next)) {
			next = candidate
		}
	}
	if next.IsZero() {
		return "不自动重置", nil
	}
	return next.Format("2006-01-02 15:04 -07:00"), nil
}
