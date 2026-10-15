package routingsync

// Control is a complete authoritative snapshot, including an explicitly empty rule list.
type Control struct {
	Source   string           `json:"source"`
	Revision int64            `json:"revision"`
	Rules    []map[string]any `json:"rules"`
}
