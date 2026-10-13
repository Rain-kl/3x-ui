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
		var policy service.AuditRetention
		if err := c.ShouldBindJSON(&policy); err != nil {
			jsonObj(c, nil, err)
			return
		}
		jsonObj(c, true, (service.AuditService{}).SetRetention(policy))
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
