package controller

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

func downloadAuditExport(c *gin.Context) {
	if err := http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(6 * time.Minute)); err != nil {
		jsonObj(c, nil, fmt.Errorf("extend audit download deadline: %w", err))
		return
	}
	file, err := service.BuildAuditExport(c.Request.Context(), c.Query("kind"), c.Query("client"))
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	defer func() { _ = file.Close(); _ = os.Remove(file.Name()) }()
	info, err := file.Stat()
	if err != nil {
		jsonObj(c, nil, fmt.Errorf("stat audit archive: %w", err))
		return
	}
	if err := http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(30 * time.Minute)); err != nil {
		jsonObj(c, nil, fmt.Errorf("extend archive transfer deadline: %w", err))
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Disposition", `attachment; filename="audit-export.zip"`)
	c.DataFromReader(http.StatusOK, info.Size(), "application/zip", file, nil)
}
