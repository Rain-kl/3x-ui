package sub

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

func subscriptionAccessIP(c *gin.Context) string {
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		host = c.Request.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return "unknown"
	}
	peer = peer.Unmap()
	cidrs, err := (&service.SettingService{}).GetTrustedProxyCIDRs()
	if err != nil || strings.TrimSpace(cidrs) == "" || !remoteAddrInCIDRs(peer.String(), cidrs) {
		return peer.String()
	}
	forwarded := c.GetHeader("X-Forwarded-For")
	if len(forwarded) > 4096 {
		return peer.String()
	}
	if forwarded == "" {
		forwarded = c.GetHeader("X-Real-IP")
	}
	chain := strings.Split(forwarded, ",")
	current := peer
	for i := len(chain) - 1; i >= 0; i-- {
		if !remoteAddrInCIDRs(current.String(), cidrs) {
			return current.String()
		}
		addr, err := netip.ParseAddr(strings.TrimSpace(chain[i]))
		if err != nil {
			return peer.String()
		}
		current = addr.Unmap()
	}
	return current.String()
}

func (a *SUBController) subscriptionAccess(c *gin.Context) {
	format := "raw/html"
	if strings.HasPrefix(c.FullPath(), a.subJsonPath) && a.jsonEnabled {
		format = "json"
	}
	if a.clashEnabled && (strings.HasPrefix(c.FullPath(), a.subClashPath) || strings.HasPrefix(c.FullPath(), subMihomoPath) || strings.HasPrefix(c.FullPath(), subClashLegacyPath)) {
		format = "clash"
	}
	request, blocked, err := service.CaptureSubscriptionAccess(c.Param("subid"), subscriptionAccessIP(c), c.Request.Method, format)
	if err != nil {
		logger.Warningf("subscription access protection failed: %v", err)
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	if blocked {
		if strings.HasSuffix(c.FullPath(), "/hwid-status") {
			c.AbortWithStatus(http.StatusNotFound)
		} else {
			c.AbortWithStatus(http.StatusForbidden)
		}
	} else {
		c.Next()
	}
	if err := service.CompleteSubscriptionAccess(request, c.Writer.Status()); err != nil {
		logger.Warningf("complete subscription access audit: %v", err)
	}
}
