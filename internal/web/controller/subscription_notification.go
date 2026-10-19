package controller

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

func newNotificationController(g *gin.RouterGroup) {
	svc := service.NotificationService{}
	g.GET("", func(c *gin.Context) {
		page, size := notificationPaging(c)
		result, err := svc.List(page, size)
		jsonObj(c, result, err)
	})
	g.GET("/clients", func(c *gin.Context) {
		page, size := notificationPaging(c)
		var ids []int
		if raw := c.Query("ids"); raw != "" {
			parts := strings.Split(raw, ",")
			if len(parts) > 2000 {
				jsonObj(c, nil, fmt.Errorf("notification client lookup exceeds 2000 IDs"))
				return
			}
			for _, part := range parts {
				id, err := strconv.Atoi(part)
				if err != nil || id <= 0 {
					jsonObj(c, nil, fmt.Errorf("invalid notification client ID %q", part))
					return
				}
				ids = append(ids, id)
			}
		}
		result, err := svc.Clients(c.Query("search"), ids, page, size)
		jsonObj(c, result, err)
	})
	g.POST("/save", func(c *gin.Context) {
		var body service.NotificationSaveInput
		if err := c.ShouldBindJSON(&body); err != nil {
			jsonObj(c, nil, fmt.Errorf("decode subscription notification: %w", err))
			return
		}
		result, err := svc.Save(body)
		jsonObj(c, result, err)
	})
	g.POST("/:id/delete", func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil || id <= 0 {
			jsonObj(c, nil, fmt.Errorf("invalid notification ID %q", c.Param("id")))
			return
		}
		jsonObj(c, nil, svc.Delete(id))
	})
}

func notificationPaging(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	return min(max(page, 1), 1000000), min(max(size, 1), 100)
}
