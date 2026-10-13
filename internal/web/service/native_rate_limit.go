package service

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func validateNativeRateLimits(ib *model.Inbound, clients ...model.Client) error {
	if ib.InboundDownLimit < 0 || ib.ClientDownLimit < 0 {
		return fmt.Errorf("bandwidth limits cannot be negative")
	}
	if uint64(ib.InboundDownLimit) > math.MaxUint64/125000 || uint64(ib.ClientDownLimit) > math.MaxUint64/125000 {
		return fmt.Errorf("bandwidth limit is too large")
	}
	limited := ib.InboundDownLimit > 0 || ib.ClientDownLimit > 0
	if len(clients) == 0 {
		var settings struct {
			Clients []model.Client `json:"clients"`
		}
		_ = json.Unmarshal([]byte(ib.Settings), &settings)
		clients = settings.Clients
	}
	for _, client := range clients {
		if uint64(max(client.DownLimit, 0)) > math.MaxUint64/125000 {
			return fmt.Errorf("client bandwidth limit is too large")
		}
		if client.DownLimit < 0 {
			return fmt.Errorf("client bandwidth limit cannot be negative")
		}
		if client.Enable && client.DownLimit > 0 {
			limited = true
		}
	}
	if !ib.Enable {
		return nil
	}
	if limited && !xray.NativeRateLimitSupported(string(ib.Protocol)) {
		return fmt.Errorf("native bandwidth limiting is not supported for %s; use a supported Xray user protocol", ib.Protocol)
	}
	if limited && ib.NodeID == nil {
		process := currentXrayProcess()
		if process != nil && process.IsRunning() && !xray.SupportsNativeRateLimitVersion(process.GetXrayVersion()) {
			return fmt.Errorf("bandwidth limits require Rain-kl/Xray-core v26.10.10-feat.2 or newer")
		}
	}
	return nil
}

func hasLocalNativeRateLimits() bool {
	svc := &InboundService{}
	inbounds, err := svc.GetAllInbounds()
	if err != nil {
		return true
	}
	for _, ib := range inbounds {
		if ib.Enable && ib.NodeID == nil && (ib.InboundDownLimit > 0 || ib.ClientDownLimit > 0 || len((&ClientService{}).ClientLimitsByInbound(ib.Id)) > 0) {
			return true
		}
	}
	return false
}
