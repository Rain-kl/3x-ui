package telegramuser

import (
	"sync"
	"time"
)

const (
	bindAttemptLimit  = 30
	bindAttemptWindow = 10 * time.Minute
)

type bindAttemptRecord struct {
	times []time.Time
}

var (
	bindAttemptsMu sync.Mutex
	bindAttemptsBy = map[int64]*bindAttemptRecord{}
)

// allowBindAttempt records one bind attempt and reports whether it stays within
// the per-account 10-minute sliding window, together with the window count.
func allowBindAttempt(tgID int64) (bool, int) {
	now := time.Now()
	cutoff := now.Add(-bindAttemptWindow)
	bindAttemptsMu.Lock()
	defer bindAttemptsMu.Unlock()
	record := bindAttemptsBy[tgID]
	if record == nil {
		record = &bindAttemptRecord{}
		bindAttemptsBy[tgID] = record
	}
	kept := record.times[:0]
	for _, attempt := range record.times {
		if attempt.After(cutoff) {
			kept = append(kept, attempt)
		}
	}
	record.times = append(kept, now)
	return len(record.times) <= bindAttemptLimit, len(record.times)
}

// sweepBindAttempts drops accounts whose last attempt left the window.
func sweepBindAttempts() {
	cutoff := time.Now().Add(-bindAttemptWindow)
	bindAttemptsMu.Lock()
	defer bindAttemptsMu.Unlock()
	for id, record := range bindAttemptsBy {
		if len(record.times) == 0 || record.times[len(record.times)-1].Before(cutoff) {
			delete(bindAttemptsBy, id)
		}
	}
}
