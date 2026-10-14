package audit

import "time"

// Control is the master's persisted audit policy. ClearBefore is an absolute,
// monotonic watermark, so retrying cleanup never deletes newer connections.
type Control struct {
	DetailHours int       `json:"detailHours"`
	SummaryDays int       `json:"summaryDays"`
	Revision    int64     `json:"revision"`
	ClearBefore time.Time `json:"clearBefore"`
}
