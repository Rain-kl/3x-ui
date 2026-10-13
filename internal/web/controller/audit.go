package controller

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/audit"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

func newAuditController(g *gin.RouterGroup) {
	g.GET("/pending", func(c *gin.Context) {
		jsonObj(c, audit.Local().Snapshot(1000), nil)
	})
	g.POST("/ack", func(c *gin.Context) {
		var body struct {
			IDs []string `json:"ids"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || len(body.IDs) > 1000 {
			jsonObj(c, nil, errors.New("invalid audit acknowledgement"))
			return
		}
		jsonObj(c, true, audit.Local().Ack(body.IDs))
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
			Client: c.Query("client"), Site: c.Query("site"),
		})
		jsonObj(c, result, err)
	})
}
