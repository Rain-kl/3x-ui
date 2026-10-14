package controller

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/audit"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

func newAuditController(g *gin.RouterGroup) {
	// FORK(audit-retention): Administrator-only configuration and cleanup routes.
	g.GET("/settings", func(c *gin.Context) { policy, err := (service.AuditService{}).Retention(); jsonObj(c, policy, err) })
	g.POST("/settings", func(c *gin.Context) {
		// FORK(audit-workbench): Legacy requests omitting rate thresholds preserve the current values.
		policy, err := (service.AuditService{}).Retention()
		if err != nil {
			jsonObj(c, nil, err)
			return
		}
		if err := c.ShouldBindJSON(&policy); err != nil {
			jsonObj(c, nil, err)
			return
		}
		jsonObj(c, true, (service.AuditService{}).SetRetention(policy))
	})
	// FORK(audit-workbench): Recent rates and minute history use fixed windows, independent of historical totals.
	g.GET("/rates", func(c *gin.Context) {
		nodeID, _ := strconv.Atoi(c.Query("nodeId"))
		result, err := (service.AuditService{}).QueryRates(service.AuditFilter{NodeID: nodeID, Client: c.Query("client"), Site: c.Query("site")})
		jsonObj(c, result, err)
	})
	g.GET("/rates/history", func(c *gin.Context) {
		nodeID, _ := strconv.Atoi(c.Query("nodeId"))
		result, err := (service.AuditService{}).QueryRateHistory(service.AuditFilter{NodeID: nodeID, Client: c.Query("client"), Site: c.Query("site")})
		jsonObj(c, result, err)
	})
	g.POST("/clear", func(c *gin.Context) {
		var body struct {
			OlderThanDays *int `json:"olderThanDays"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || body.OlderThanDays == nil {
			jsonObj(c, nil, errors.New("olderThanDays is required; zero explicitly clears all logs"))
			return
		}
		jsonObj(c, true, (service.AuditService{}).Clear(*body.OlderThanDays))
	})

	// FORK(audit-retention): Parent RPC propagates policy and cleanup without allowing child UI overrides.
	g.POST("/sync", func(c *gin.Context) {
		if _, ok := c.Get("api_token_scope"); !ok {
			c.AbortWithStatusJSON(403, gin.H{"success": false, "msg": "audit policy sync requires an authenticated parent node channel"})
			return
		}
		var state audit.Control
		if err := c.ShouldBindJSON(&state); err != nil {
			jsonObj(c, nil, err)
			return
		}
		jsonObj(c, true, (service.AuditService{}).ApplyControl(audit.Local(), state))
	})

	g.GET("/pending", func(c *gin.Context) {
		// FORK(audit-node-consistency): Export also establishes the persistent parent relationship.
		events, err := (service.AuditService{}).FetchPending(audit.Local(), c.Query("summaries") == "1")
		jsonObj(c, events, err)
	})
	g.POST("/ack", func(c *gin.Context) {
		var body struct {
			IDs []string `json:"ids"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || len(body.IDs) > 1000 {
			jsonObj(c, nil, errors.New("invalid audit acknowledgement"))
			return
		}
		jsonObj(c, true, (service.AuditService{}).AckPending(audit.Local(), body.IDs))
	})
	// FORK(audit-workbench): Fetch all ranked sites in bounded pages using the dashboard's filters.
	g.GET("/sites", func(c *gin.Context) {
		page, pageSize := 1, 20
		for _, param := range []struct {
			name  string
			value *int
		}{{"page", &page}, {"pageSize", &pageSize}} {
			if raw := c.Query(param.name); raw != "" {
				parsed, err := strconv.Atoi(raw)
				if err != nil {
					jsonObj(c, nil, errors.New("invalid audit "+param.name))
					return
				}
				*param.value = parsed
			}
		}
		hours, _ := strconv.Atoi(c.Query("hours"))
		nodeID, _ := strconv.Atoi(c.Query("nodeId"))
		result, err := (service.AuditService{}).QuerySites(service.AuditFilter{Hours: hours, NodeID: nodeID, Client: c.Query("client"), Site: c.Query("site"), Day: c.Query("day")}, page, pageSize)
		jsonObj(c, result, err)
	})
	g.GET("/summary", func(c *gin.Context) {
		hours, _ := strconv.Atoi(c.Query("hours"))
		nodeID, _ := strconv.Atoi(c.Query("nodeId"))
		offset, _ := strconv.Atoi(c.Query("offset"))
		if len(c.Query("client")) > 256 || len(c.Query("site")) > 256 {
			jsonObj(c, nil, errors.New("audit filter is too long"))
			return
		}
		result, err := (service.AuditService{}).Query(service.AuditFilter{
			Hours: hours, NodeID: nodeID, Offset: offset,
			Client: c.Query("client"), Site: c.Query("site"), Day: c.Query("day"),
		})
		jsonObj(c, result, err)
	})
}
