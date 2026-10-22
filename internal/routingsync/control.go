package routingsync

// Control is a complete authoritative snapshot, including an explicitly empty rule list.
type Control struct {
	Source    string           `json:"source"`
	Revision  int64            `json:"revision"`
	Rules     []map[string]any `json:"rules"`
	Balancing *Balancing       `json:"balancing,omitempty"`
}

// Balancing replaces parent-owned balancers and their probe configuration as one unit.
type Balancing struct {
	Balancers        []map[string]any `json:"balancers"`
	Observatory      map[string]any   `json:"observatory,omitempty"`
	BurstObservatory map[string]any   `json:"burstObservatory,omitempty"`
}
