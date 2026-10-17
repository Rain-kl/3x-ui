package runtime

import (
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func (l *Local) checkMixedCore(inbound *model.Inbound) error {
	if inbound.Protocol != model.Mixed {
		return nil
	}
	version := "Unknown"
	if l.deps.XrayVersion != nil {
		version = l.deps.XrayVersion()
	}
	return xray.ValidateMixedClientVersion(&xray.Config{InboundConfigs: []xray.InboundConfig{*inbound.GenXrayInboundConfig()}}, version)
}
