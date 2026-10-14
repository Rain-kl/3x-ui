package service

import (
	"gorm.io/gorm"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// recordAuditReceipts uses bounded SQL batches. RETURNING identifies only newly
// accepted IDs, preserving idempotency without two SQL round trips per visit.
func recordAuditReceipts(tx *gorm.DB, rows []model.AuditEvent) (map[string]bool, error) {
	accepted := make(map[string]bool, len(rows))
	for start := 0; start < len(rows); start += 200 {
		batch := rows[start:min(start+200, len(rows))]
		ids := make([]string, len(batch))
		for i, row := range batch {
			ids[i] = row.ID
		}
		var existing []string
		if err := tx.Model(&model.AuditEvent{}).Where("id IN ?", ids).Pluck("id", &existing).Error; err != nil {
			return nil, err
		}
		rawIDs := make(map[string]bool, len(existing))
		for _, id := range existing {
			rawIDs[id] = true
		}
		values := make([]string, 0, len(batch))
		args := make([]any, 0, 3*len(batch))
		for _, row := range batch {
			if rawIDs[row.ID] {
				continue
			}
			values = append(values, "(?,?,?)")
			args = append(args, row.ID, row.NodeID, row.Time)
		}
		if len(values) == 0 {
			continue
		}
		var inserted []model.AuditReceipt
		query := "INSERT INTO audit_receipts (id,node_id,time) VALUES " + strings.Join(values, ",") + " ON CONFLICT (id) DO NOTHING RETURNING id"
		if err := tx.Raw(query, args...).Scan(&inserted).Error; err != nil {
			return nil, err
		}
		for _, row := range inserted {
			accepted[row.ID] = true
		}
	}
	return accepted, nil
}
