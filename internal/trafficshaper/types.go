package trafficshaper

const (
	DefaultRootHandle    = "1:"
	DefaultRootClassID   = "1:1"
	DefaultDirectClassID = "1:9999"
	DefaultBandwidth     = "10gbit"
)

// InboundRule holds rate limit parameters and active IP state for an inbound.
type InboundRule struct {
	InboundID        int            `json:"inboundId"`
	Port             int            `json:"port"`
	InboundDownLimit int            `json:"inboundDownLimit"`
	ClientDownLimit  int            `json:"clientDownLimit"`
	ClientLimits     map[string]int `json:"clientLimits,omitempty"`
	ActiveIPs        []string       `json:"activeIps,omitempty"`
	Clients          []string       `json:"clients,omitempty"`
}

// ID returns InboundID for caller convenience.
func (r InboundRule) ID() int {
	return r.InboundID
}

// InboundLimitMbps returns InboundDownLimit in Mbps.
func (r InboundRule) InboundLimitMbps() int {
	return r.InboundDownLimit
}

// ClientLimitMbps returns ClientDownLimit in Mbps.
func (r InboundRule) ClientLimitMbps() int {
	return r.ClientDownLimit
}
