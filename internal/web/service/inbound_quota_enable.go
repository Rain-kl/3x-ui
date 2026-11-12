package service

import (
	"encoding/json"
	"fmt"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// Quota plans need the exact changed memberships while sharing upstream enable mutations.
func (s *InboundService) setQuotaClientsEnabledInSettings(tx *gorm.DB, inboundID int, emails map[string]struct{}, enabled bool) (*model.Inbound, *model.Inbound, []string, error) {
	var old, next *model.Inbound
	if !enabled {
		var err error
		old, next, err = s.markClientsDisabledInSettings(tx, inboundID, emails)
		if err != nil {
			return nil, nil, nil, err
		}
	} else {
		var snapshot model.Inbound
		if err := tx.First(&snapshot, inboundID).Error; err != nil {
			return nil, nil, nil, err
		}
		old = &snapshot
		for email := range emails {
			if err := s.markClientEnabledInSettings(tx, inboundID, email); err != nil {
				return nil, nil, nil, fmt.Errorf("enable quota client %q on inbound %d: %w", email, inboundID, err)
			}
		}
		var current model.Inbound
		if err := tx.First(&current, inboundID).Error; err != nil {
			return nil, nil, nil, err
		}
		next = &current
	}
	var before, after struct {
		Clients []model.Client `json:"clients"`
	}
	if err := json.Unmarshal([]byte(old.Settings), &before); err != nil {
		return old, next, nil, nil
	}
	if err := json.Unmarshal([]byte(next.Settings), &after); err != nil {
		return nil, nil, nil, fmt.Errorf("read changed quota memberships on inbound %d: %w", inboundID, err)
	}
	states := make(map[string]bool, len(before.Clients))
	for _, client := range before.Clients {
		states[client.Email] = client.Enable
	}
	var changed []string
	for _, client := range after.Clients {
		if _, selected := emails[client.Email]; selected && states[client.Email] != client.Enable {
			changed = append(changed, client.Email)
		}
	}
	return old, next, changed, nil
}
