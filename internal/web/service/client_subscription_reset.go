package service

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	wgutil "github.com/mhsanaei/3x-ui/v3/internal/util/wireguard"
)

var subscriptionMutationMu sync.Mutex

// ResetSubscriptionCredentials revokes proxy identities as well as the subscription bearer.
func (s *ClientService) ResetSubscriptionCredentials(clientID int) (result *model.ClientRecord, resetErr error) {
	subscriptionMutationMu.Lock()
	defer subscriptionMutationMu.Unlock()
	record, err := s.GetByID(clientID)
	if err != nil {
		return nil, err
	}
	var inbounds []model.Inbound
	if err := database.GetDB().Model(&model.Inbound{}).Joins("JOIN client_inbounds ci ON ci.inbound_id = inbounds.id").Where("ci.client_id = ?", clientID).Order("inbounds.id").Find(&inbounds).Error; err != nil {
		return nil, fmt.Errorf("load reset client inbounds: %w", err)
	}
	for _, ib := range inbounds {
		switch ib.Protocol {
		case model.VLESS, model.VMESS, model.Trojan, model.Mixed, model.Shadowsocks, model.Hysteria, model.MTProto, model.TUIC, model.MASQUE, model.WireGuard, model.AmneziaWG:
		default:
			return nil, fmt.Errorf("inbound %d protocol %s does not support individual credential revocation", ib.Id, ib.Protocol)
		}
	}
	inboundSvc := &InboundService{}
	disabled, restart, err := s.BulkSetEnable(inboundSvc, []string{record.Email}, false)
	if restart {
		(&XrayService{}).SetToNeedRestart()
	}
	if err != nil || len(disabled.Skipped) > 0 {
		return nil, fmt.Errorf("disable old credentials before reset: %w", errors.Join(err, fmt.Errorf("skipped clients: %v", disabled.Skipped)))
	}
	defer func() {
		if resetErr == nil {
			return
		}
		failed, reload, disableErr := s.BulkSetEnable(inboundSvc, []string{record.Email}, false)
		if reload {
			(&XrayService{}).SetToNeedRestart()
			disableErr = errors.Join(disableErr, (&XrayService{}).RestartXray(false))
		}
		if len(failed.Skipped) > 0 {
			disableErr = errors.Join(disableErr, fmt.Errorf("fail-closed client disable skipped: %v", failed.Skipped))
		}
		resetErr = errors.Join(resetErr, disableErr, enforceSubscriptionRemoteState(clientID))
	}()
	updated := record.ToClient()
	updated.Enable = false
	updated.SubID = uuid.NewString()
	updated.ID = uuid.NewString()
	updated.Password = strings.ReplaceAll(uuid.NewString(), "-", "") + strings.ReplaceAll(uuid.NewString(), "-", "")
	// Use one shared password where ciphers permit it; incompatible Shadowsocks ciphers retain inbound-specific keys.
	for _, ib := range inbounds {
		if ib.Protocol == model.Shadowsocks {
			updated.Password = randomShadowsocksClientKey(shadowsocksMethodFromSettings(ib.Settings))
			break
		}
	}
	updated.Auth = strings.ReplaceAll(uuid.NewString(), "-", "")
	updated.Secret = model.GenerateFakeTLSSecret(defaultMtprotoDomain)
	var mappings []model.ClientInbound
	if err := database.GetDB().Where("client_id = ?", clientID).Find(&mappings).Error; err != nil {
		return nil, fmt.Errorf("load reset client limits: %w", err)
	}
	updated.DownLimitByInbound = map[int]int{}
	updated.TotalGBByInbound = map[int]int64{}
	for _, mapping := range mappings {
		updated.DownLimitByInbound[mapping.InboundId] = mapping.DownLimit
		updated.TotalGBByInbound[mapping.InboundId] = mapping.TotalGB
	}
	apply := func(client model.Client, ids ...int) error {
		needsRestart, err := s.Update(inboundSvc, clientID, client, record.LimitHwid, ids...)
		if needsRestart {
			restart = true
			(&XrayService{}).SetToNeedRestart()
		}
		if err != nil {
			return fmt.Errorf("rotate client credentials: %w", err)
		}
		return nil
	}
	if len(inbounds) == 0 {
		if err := apply(*updated); err != nil {
			return nil, err
		}
	}
	for _, ib := range inbounds {
		client := *updated
		switch ib.Protocol {
		case model.Shadowsocks:
			if !validShadowsocksClientKey(shadowsocksMethodFromSettings(ib.Settings), client.Password) {
				client.Password = randomShadowsocksClientKey(shadowsocksMethodFromSettings(ib.Settings))
			}
		case model.MTProto:
			client.Secret = model.GenerateFakeTLSSecret(mtprotoDomainFromSettings(ib.Settings))
		case model.WireGuard, model.AmneziaWG:
			peers, err := inboundSvc.GetClients(&ib)
			if err != nil {
				return nil, fmt.Errorf("load tunnel %d peer: %w", ib.Id, err)
			}
			found := false
			for _, peer := range peers {
				if peer.Email == record.Email {
					found = true
					client.AllowedIPs = peer.AllowedIPs
					client.ForwardedPorts = peer.ForwardedPorts
					client.PreSharedKey = peer.PreSharedKey
					client.KeepAlive = peer.KeepAlive
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("tunnel %d client peer not found; reset remains disabled", ib.Id)
			}
			client.PrivateKey, client.PublicKey, err = wgutil.GenerateWireguardKeypair()
			if err != nil {
				return nil, fmt.Errorf("rotate tunnel %d keys: %w", ib.Id, err)
			}
			client.AllowedIPsByInbound = map[int][]string{ib.Id: client.AllowedIPs}
			if client.PreSharedKey != "" {
				private, _, err := wgutil.GenerateWireguardKeypair()
				if err != nil {
					return nil, err
				}
				client.PreSharedKey = private
			}
		}
		if err := apply(client, ib.Id); err != nil {
			return nil, fmt.Errorf("inbound %d reset remains disabled: %w", ib.Id, err)
		}
	}

	if restart {
		if err := (&XrayService{}).RestartXray(false); err != nil {
			return nil, fmt.Errorf("reload credentials while client remains disabled: %w", err)
		}
	}
	if err := enforceSubscriptionRemoteState(clientID); err != nil {
		return nil, fmt.Errorf("confirm revoked credentials on child nodes: %w", err)
	}
	if record.Enable {
		enabled, needsRestart, err := s.BulkSetEnable(inboundSvc, []string{record.Email}, true)
		if needsRestart {
			(&XrayService{}).SetToNeedRestart()
		}
		if err != nil || len(enabled.Skipped) > 0 {
			return nil, fmt.Errorf("restore client after rotation: %w", errors.Join(err, fmt.Errorf("skipped clients: %v", enabled.Skipped)))
		}
		if needsRestart {
			if err := (&XrayService{}).RestartXray(false); err != nil {
				return nil, fmt.Errorf("apply restored credential state: %w", err)
			}
		}
	}
	if err := enforceSubscriptionRemoteState(clientID); err != nil {
		return nil, fmt.Errorf("confirm rotated credentials on child nodes: %w", err)
	}
	// Keep the canonical identity used by mixed/Trojan/TUIC while per-inbound ciphers keep their own keys.
	if err := database.GetDB().Model(&model.ClientRecord{}).Where("id = ?", clientID).Updates(map[string]any{"password": updated.Password, "secret": updated.Secret}).Error; err != nil {
		return nil, fmt.Errorf("save canonical rotated credentials: %w", err)
	}
	return s.GetByID(clientID)
}
