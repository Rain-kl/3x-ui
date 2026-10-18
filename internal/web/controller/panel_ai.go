package controller

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/panelai"
)

func aiPage(c *gin.Context) (int, error) {
	raw := c.DefaultQuery("page", "1")
	page, err := strconv.Atoi(raw)
	if err != nil || page < 1 || page > 10000 {
		return 0, fmt.Errorf("AI page must be 1..10000")
	}
	return page, nil
}

func newPanelAIController(g *gin.RouterGroup) {
	g.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256<<10)
		c.Next()
	})
	g.GET("/settings", func(c *gin.Context) { value, err := panelai.ReadSettings(); jsonObj(c, value, err) })
	g.POST("/settings", func(c *gin.Context) {
		var value panelai.AISettings
		if err := c.ShouldBindJSON(&value); err != nil {
			jsonObj(c, nil, fmt.Errorf("decode AI settings: %w", err))
			return
		}
		// FORK(panel-ai): Never echo credentials from the settings request.
		if err := panelai.SaveSettings(value); err != nil {
			jsonObj(c, nil, err)
			return
		}
		result, err := panelai.ReadSettings()
		jsonObj(c, result, err)
	})
	g.GET("/status", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
		defer cancel()
		jsonObj(c, panelai.Default.ConnectionStatus(ctx), nil)
	})
	g.POST("/reconnect", func(c *gin.Context) {
		panelai.Default.ResetConnection()
		ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
		defer cancel()
		jsonObj(c, panelai.Default.ConnectionStatus(ctx), nil)
	})
	g.GET("/models", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
		defer cancel()
		result, err := panelai.Default.Models(ctx)
		jsonObj(c, result, err)
	})
	g.GET("/sessions", func(c *gin.Context) {
		page, err := aiPage(c)
		if err != nil {
			jsonObj(c, nil, err)
			return
		}
		value, err := panelai.Conversations(page)
		jsonObj(c, value, err)
	})
	g.POST("/sessions", func(c *gin.Context) {
		var body struct {
			Title string `json:"title"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			jsonObj(c, nil, err)
			return
		}
		value, err := panelai.NewConversation(body.Title)
		jsonObj(c, value, err)
	})
	g.GET("/sessions/:id", func(c *gin.Context) {
		page, err := aiPage(c)
		if err != nil {
			jsonObj(c, nil, err)
			return
		}
		value, err := panelai.Conversation(c.Param("id"), page)
		jsonObj(c, value, err)
	})
	g.POST("/sessions/:id/delete", func(c *gin.Context) { jsonObj(c, true, panelai.Default.DeleteConversation(c.Param("id"))) })
	g.POST("/sessions/:id/messages", func(c *gin.Context) {
		var body struct {
			Prompt string `json:"prompt"`
			Model  string `json:"model"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			jsonObj(c, nil, err)
			return
		}
		value, err := panelai.Default.Begin(c.Param("id"), "", body.Prompt, body.Model, false)
		jsonObj(c, value, err)
	})
	g.GET("/runs", func(c *gin.Context) {
		page, err := aiPage(c)
		if err != nil {
			jsonObj(c, nil, err)
			return
		}
		value, err := panelai.Runs(c.Query("taskId"), "", page)
		jsonObj(c, value, err)
	})
	g.GET("/runs/:id", func(c *gin.Context) { value, err := panelai.Run(c.Param("id")); jsonObj(c, value, err) })
	g.POST("/runs/:id/cancel", func(c *gin.Context) { jsonObj(c, true, panelai.Default.Cancel(c.Param("id"))) })
	g.GET("/tasks", func(c *gin.Context) {
		page, err := aiPage(c)
		if err != nil {
			jsonObj(c, nil, err)
			return
		}
		value, err := panelai.Tasks(page)
		jsonObj(c, value, err)
	})
	g.POST("/tasks", func(c *gin.Context) {
		var value panelai.AITaskInput
		if err := c.ShouldBindJSON(&value); err != nil {
			jsonObj(c, nil, fmt.Errorf("decode AI task: %w", err))
			return
		}
		result, err := panelai.SaveTask(value)
		jsonObj(c, result, err)
	})
	g.POST("/tasks/:id/delete", func(c *gin.Context) { jsonObj(c, true, panelai.Default.DeleteTask(c.Param("id"))) })
	g.POST("/tasks/:id/run", func(c *gin.Context) {
		value, err := panelai.Default.Begin("", c.Param("id"), "", "", false)
		jsonObj(c, value, err)
	})
}
