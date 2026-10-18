package controller

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

func registerSubscriptionAccess(g *gin.RouterGroup) {
	g.GET("/subscription-access", func(c *gin.Context) {
		page, size := notificationPaging(c)
		result, err := service.QuerySubscriptionAccess(c.Query("client"), c.Query("ip"), c.Query("day"), page, size)
		c.Header("Cache-Control", "no-store")
		jsonObj(c, result, err)
	})
	g.POST("/subscription-access/clear", func(c *gin.Context) {
		var body struct {
			Days int `json:"days"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			jsonObj(c, nil, fmt.Errorf("decode subscription log cleanup: %w", err))
			return
		}
		jsonObj(c, true, service.ClearSubscriptionAccess(body.Days))
	})
	g.POST("/subscription-access/alerts/:id/retry", func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			jsonObj(c, nil, fmt.Errorf("invalid subscription alert ID"))
			return
		}
		jsonObj(c, true, service.RetrySubscriptionAlert(id))
	})
}
