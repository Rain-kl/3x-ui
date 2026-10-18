package service

import (
	"fmt"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
)

// Security changes require acknowledged remote application, not only queued reconciliation.
func enforceSubscriptionRemoteState(clientID int) error {
	var inbounds []model.Inbound
	if err := database.GetDB().Model(&model.Inbound{}).Joins("JOIN client_inbounds ci ON ci.inbound_id = inbounds.id").Where("ci.client_id = ?", clientID).Find(&inbounds).Error; err != nil {
		return fmt.Errorf("load security update attachments: %w", err)
	}
	record, err := (&ClientService{}).GetByID(clientID)
	if err != nil {
		return err
	}
	svc := &InboundService{}
	for _, ib := range inbounds {
		if ib.NodeID == nil {
			continue
		}
		rt, push, _, err := svc.nodePushPlan(&ib)
		if err != nil {
			return fmt.Errorf("node %d security update: %w", *ib.NodeID, err)
		}
		if !push {
			return fmt.Errorf("node %d is unavailable; security update remains pending", *ib.NodeID)
		}
		clients, err := svc.GetClients(&ib)
		if err != nil {
			return fmt.Errorf("read inbound %d security state: %w", ib.Id, err)
		}
		found := false
		for _, client := range clients {
			if client.Email != record.Email {
				continue
			}
			found = true
			ctx, cancel := nodePushContext()
			err = rt.UpdateUser(runtime.WithImmediateApply(ctx), &ib, client.Email, client)
			cancel()
			if err != nil {
				return fmt.Errorf("apply security state to node %d inbound %d: %w", *ib.NodeID, ib.Id, err)
			}
			break
		}
		if !found {
			return fmt.Errorf("client %d missing from inbound %d during security update", clientID, ib.Id)
		}
	}
	return nil
}
