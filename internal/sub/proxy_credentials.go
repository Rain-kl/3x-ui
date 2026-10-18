package sub

import (
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func (s *SubService) overlayInboundProxyCredentials(inbound *model.Inbound, clients []model.Client) ([]model.Client, error) {
	switch inbound.Protocol {
	case model.Shadowsocks, model.MTProto, model.Trojan, model.Mixed, model.TUIC, model.MASQUE:
	default:
		return clients, nil
	}
	embedded, err := s.inboundService.GetClients(inbound)
	if err != nil {
		return nil, err
	}
	if len(embedded) == 0 {
		return clients, nil
	}
	byEmail := make(map[string]model.Client, len(embedded))
	for _, client := range embedded {
		byEmail[strings.ToLower(client.Email)] = client
	}
	for i := range clients {
		peer, ok := byEmail[strings.ToLower(clients[i].Email)]
		if !ok {
			continue
		}
		if inbound.Protocol == model.MTProto {
			clients[i].Secret = peer.Secret
		} else {
			clients[i].Password = peer.Password
			if inbound.Protocol == model.Mixed {
				clients[i].Username = peer.Username
			}
		}
	}
	return clients, nil
}
