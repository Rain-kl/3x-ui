package controller

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/telegramuser"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

type telegramSendBody struct {
	TgID    int64  `json:"tgId"`
	Text    string `json:"text"`
	ReplyID int64  `json:"replyId"`
}

var telegramAttachmentSlots = make(chan struct{}, 2)

func reserveTelegramAttachment(c *gin.Context) bool {
	select {
	case telegramAttachmentSlots <- struct{}{}:
		return true
	case <-c.Request.Context().Done():
		return false
	}
}

func newTelegramUserController(g *gin.RouterGroup) {
	// FORK-BEGIN(telegram-custom-commands)
	g.GET("/commands", func(c *gin.Context) {
		result, err := telegramuser.ReadCustomCommands()
		jsonObj(c, result, err)
	})
	g.POST("/commands", func(c *gin.Context) {
		var body telegramuser.CustomCommands
		if err := c.ShouldBindJSON(&body); err != nil {
			jsonObj(c, nil, fmt.Errorf("decode Telegram custom commands: %w", err))
			return
		}
		result, err := telegramuser.SaveCustomCommands(body)
		jsonObj(c, result, err)
	})
	// FORK-END(telegram-custom-commands)

	g.GET("/settings", func(c *gin.Context) {
		result, err := telegramuser.ReadSettings()
		c.Header("Cache-Control", "no-store")
		jsonObj(c, result, err)
	})
	g.POST("/settings", func(c *gin.Context) {
		var body telegramuser.UserTelegramSettings
		if err := c.ShouldBindJSON(&body); err != nil {
			jsonObj(c, nil, fmt.Errorf("decode user Telegram settings: %w", err))
			return
		}
		if err := telegramuser.SaveSettings(body); err != nil {
			jsonObj(c, nil, err)
			return
		}
		result, err := telegramuser.ReadSettings()
		jsonObj(c, result, err)
	})
	g.GET("/status", func(c *gin.Context) { jsonObj(c, telegramuser.Default.Status(), nil) })
	g.GET("/conversations", func(c *gin.Context) {
		page, size := notificationPaging(c)
		result, err := telegramuser.Conversations(c.Query("search"), page, size)
		jsonObj(c, result, err)
	})
	g.POST("/conversations/:tgId/read", func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("tgId"), 10, 64)
		if err != nil || id <= 0 {
			jsonObj(c, nil, fmt.Errorf("invalid Telegram conversation ID"))
			return
		}
		var body struct {
			ThroughID int64 `json:"throughId"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || body.ThroughID <= 0 {
			jsonObj(c, nil, fmt.Errorf("marking a conversation read requires a valid throughId"))
			return
		}
		jsonObj(c, nil, telegramuser.MarkRead(id, body.ThroughID))
	})
	g.GET("/messages", func(c *gin.Context) {
		tgID, err := strconv.ParseInt(c.Query("tgId"), 10, 64)
		if err != nil || tgID <= 0 {
			jsonObj(c, nil, fmt.Errorf("invalid Telegram conversation ID"))
			return
		}
		page, size := notificationPaging(c)
		result, err := telegramuser.Messages(tgID, page, size)
		jsonObj(c, result, err)
	})
	g.POST("/messages/send", func(c *gin.Context) {
		var body telegramSendBody
		if err := c.ShouldBindJSON(&body); err != nil {
			jsonObj(c, nil, fmt.Errorf("decode Telegram message: %w", err))
			return
		}
		result, err := telegramuser.QueueText(body.TgID, body.Text, body.ReplyID, "web")
		jsonObj(c, result, err)
	})
	g.POST("/messages/upload", func(c *gin.Context) {
		if !reserveTelegramAttachment(c) {
			return
		}
		defer func() { <-telegramAttachmentSlots }()
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, telegramuser.MaxAttachment+(1<<20))
		file, err := c.FormFile("file")
		if err != nil || file.Size <= 0 || file.Size > telegramuser.MaxAttachment {
			jsonObj(c, nil, fmt.Errorf("Telegram attachment must be between 1 byte and 20 MiB"))
			return
		}
		tgID, err := strconv.ParseInt(c.PostForm("tgId"), 10, 64)
		if err != nil || tgID <= 0 {
			jsonObj(c, nil, fmt.Errorf("invalid attachment conversation ID"))
			return
		}
		replyID, err := strconv.ParseInt(c.DefaultPostForm("replyId", "0"), 10, 64)
		if err != nil || replyID < 0 {
			jsonObj(c, nil, fmt.Errorf("invalid attachment reply ID"))
			return
		}
		reader, err := file.Open()
		if err != nil {
			jsonObj(c, nil, fmt.Errorf("read Telegram attachment: %w", err))
			return
		}
		defer reader.Close()
		data, err := io.ReadAll(io.LimitReader(reader, telegramuser.MaxAttachment+1))
		if err != nil {
			jsonObj(c, nil, fmt.Errorf("read Telegram attachment: %w", err))
			return
		}
		if len(data) > telegramuser.MaxAttachment {
			jsonObj(c, nil, fmt.Errorf("Telegram attachment exceeds 20 MiB"))
			return
		}
		name := filepath.Base(strings.ReplaceAll(file.Filename, "\\", "/"))
		result, err := telegramuser.Default.Upload(c.Request.Context(), tgID, name, http.DetectContentType(data), c.PostForm("caption"), data, replyID)
		jsonObj(c, result, err)
	})
	g.GET("/media/:id", func(c *gin.Context) {
		if !reserveTelegramAttachment(c) {
			return
		}
		defer func() { <-telegramAttachmentSlots }()
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.Status(http.StatusBadRequest)
			return
		}
		data, _, name, err := telegramuser.Default.Download(c.Request.Context(), id)
		if err != nil {
			jsonObj(c, nil, err)
			return
		}
		contentType := http.DetectContentType(data)
		disposition := "attachment"
		if strings.HasPrefix(contentType, "image/") || strings.HasPrefix(contentType, "video/") || strings.HasPrefix(contentType, "audio/") {
			disposition = "inline"
		}
		if name == "" {
			name = "telegram-attachment"
		}
		c.Header("Cache-Control", "no-store")
		c.Header("Content-Security-Policy", "sandbox; default-src 'none'")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
		c.Data(http.StatusOK, contentType, data)
	})
	g.GET("/bindings", func(c *gin.Context) {
		page, size := notificationPaging(c)
		result, err := telegramuser.Bindings(page, size)
		jsonObj(c, result, err)
	})
	g.GET("/clients", func(c *gin.Context) {
		page, size := notificationPaging(c)
		result, err := (service.NotificationService{}).Clients(c.Query("search"), nil, page, size)
		jsonObj(c, result, err)
	})
	g.POST("/bind-code", func(c *gin.Context) {
		var body struct {
			ClientID int `json:"clientId"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || body.ClientID <= 0 {
			jsonObj(c, nil, fmt.Errorf("binding code requires a valid client ID"))
			return
		}
		c.Header("Cache-Control", "no-store")
		result, err := telegramuser.CreateCode(body.ClientID)
		jsonObj(c, result, err)
	})
	g.POST("/bindings/:tgId/unbind", func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("tgId"), 10, 64)
		if err != nil || id <= 0 {
			jsonObj(c, nil, fmt.Errorf("invalid binding Telegram account ID"))
			return
		}
		err = telegramuser.Unbind(id)
		if err == nil {
			err = telegramuser.RecordLog(id, 0, "binding.removed.admin", "binding removed in control panel")
		}
		jsonObj(c, nil, err)
	})
	g.GET("/logs", func(c *gin.Context) {
		page, size := notificationPaging(c)
		tgID := int64(0)
		if raw := c.Query("tgId"); raw != "" {
			value, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || value <= 0 {
				jsonObj(c, nil, fmt.Errorf("invalid Telegram log account filter"))
				return
			}
			tgID = value
		}
		result, err := telegramuser.Logs(c.Query("event"), tgID, page, size)
		jsonObj(c, result, err)
	})
	g.POST("/clear", func(c *gin.Context) {
		var body struct {
			Kind string `json:"kind"`
			Days int    `json:"days"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			jsonObj(c, nil, fmt.Errorf("decode Telegram cleanup request: %w", err))
			return
		}
		jsonObj(c, nil, telegramuser.Clear(body.Kind, body.Days))
	})
}
