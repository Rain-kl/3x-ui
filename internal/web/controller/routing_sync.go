package controller

import (
	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/routingsync"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

func (a *ServerController) syncRouting(c *gin.Context) {
	var state routingsync.Control
	if err := c.ShouldBindJSON(&state); err != nil {
		jsonMsg(c, "invalid routing synchronization snapshot", err)
		return
	}
	err := (service.RoutingSyncService{}).Apply(state)
	jsonMsg(c, "apply synchronized routing rules", err)
}
