package service

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

type AuditSitesPage struct {
	Items       []AuditAggregate `json:"items"`
	Total       int64            `json:"total" example:"1"`
	Page        int              `json:"page" example:"1"`
	PageSize    int              `json:"pageSize" example:"20"`
	Centralized bool             `json:"centralized"`
}

// auditSiteRanking is shared by the dashboard and paginated list. A site-name
// tie-breaker keeps equal-traffic groups from moving between pages.
func auditSiteRanking(base *gorm.DB) *gorm.DB {
	return base.Select("site AS name, SUM(connections) AS connections, COALESCE(SUM(uplink),0) AS uplink, COALESCE(SUM(downlink),0) AS downlink, SUM(measured) AS measured, COUNT(DISTINCT site) AS unique_sites").Group("site").Order("COALESCE(SUM(uplink),0) + COALESCE(SUM(downlink),0) DESC, site ASC")
}

func (AuditService) QuerySites(f AuditFilter, page, pageSize int) (*AuditSitesPage, error) {
	return queryAuditRanking(f, page, pageSize, "site")
}

// FORK(audit-workbench): Complete client rankings need bounded database pages, not a top-20 slice.
func (AuditService) QueryClients(f AuditFilter, page, pageSize int) (*AuditSitesPage, error) {
	return queryAuditRanking(f, page, pageSize, "client")
}

func queryAuditRanking(f AuditFilter, page, pageSize int, dimension string) (*AuditSitesPage, error) {
	if pageSize != 20 && pageSize != 50 && pageSize != 100 {
		return nil, fmt.Errorf("audit pageSize must be 20, 50 or 100")
	}
	if page < 1 || page-1 > int(^uint(0)>>1)/pageSize {
		return nil, fmt.Errorf("invalid audit page")
	}
	if len(f.Client) > 256 || len(f.Site) > 256 {
		return nil, fmt.Errorf("audit filter is too long")
	}
	if f.Day != "" {
		if _, err := time.Parse("2006-01-02", f.Day); err != nil {
			return nil, fmt.Errorf("invalid audit day %q: %w", f.Day, err)
		}
	}
	result := &AuditSitesPage{Items: []AuditAggregate{}, Page: page, PageSize: pageSize}
	err := auditReadSnapshot(func(db *gorm.DB) error {
		state, err := readAuditControl(db)
		if err != nil {
			return err
		}
		if isAuditChild(db) {
			result.Centralized = true
			return nil
		}
		policy, now := retentionFromControl(state), time.Now().UTC()
		base := func() *gorm.DB { return auditMetricsQuery(db, f, policy, now) }
		grouped := base().Select(dimension).Group(dimension)
		if err := db.Table("(?) AS audit_site_groups", grouped).Count(&result.Total).Error; err != nil {
			return fmt.Errorf("count audit %s ranking: %w", dimension, err)
		}
		// FORK(audit-workbench): Stable ordering keeps equal-count clients on deterministic pages.
		ranking := auditSiteRanking(base())
		if dimension == "client" {
			ranking = base().Select("client AS name, SUM(connections) AS connections, COALESCE(SUM(uplink),0) AS uplink, COALESCE(SUM(downlink),0) AS downlink, SUM(measured) AS measured, COUNT(DISTINCT site) AS unique_sites").Group("client").Order("SUM(connections) DESC, client ASC")
		}
		if err := ranking.Limit(pageSize).Offset((page - 1) * pageSize).Scan(&result.Items).Error; err != nil {
			return fmt.Errorf("query audit %s page: %w", dimension, err)
		}
		return nil
	})
	return result, err
}
