package controller

import (
	"bytes"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/panelai"
	"github.com/mhsanaei/3x-ui/v3/internal/panelai/officialweb"
	"github.com/mhsanaei/3x-ui/v3/internal/web/session"
)

var openCodeThemeScript = regexp.MustCompile(`(?s)<script id="oc-theme-preload-script">.*?</script>`)

func newPanelAIWebController(g *gin.RouterGroup) {
	g.GET("/panel/ai/opencode", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if !session.IsLogin(c) {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Redirect(http.StatusTemporaryRedirect, c.Request.URL.Path+"/")
	})
	g.Any("/panel/ai/opencode/*resource", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if !session.IsLogin(c) {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		if origin := c.GetHeader("Origin"); origin != "" {
			parsed, err := url.Parse(origin)
			if err != nil || parsed.Host != c.Request.Host || (parsed.Scheme != "https" && parsed.Scheme != "http") {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
		}
		if c.GetHeader("Sec-Fetch-Site") == "cross-site" {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		resource := c.Param("resource")
		base := strings.TrimSuffix(c.Request.URL.Path, resource)
		if resource == "/csrf" && c.Request.Method == http.MethodGet {
			token, err := session.EnsureCSRFToken(c)
			if err != nil {
				c.AbortWithStatus(http.StatusInternalServerError)
				return
			}
			c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(token))
			return
		}
		upgrade := strings.EqualFold(c.GetHeader("Upgrade"), "websocket")
		if upgrade && c.GetHeader("Origin") == "" {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead:
		default:
			if !session.ValidateCSRFToken(c) {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
		}
		clean := path.Clean(resource)
		if clean != resource && resource != "/" {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		if strings.HasPrefix(clean, "/api/") {
			if clean == "/api/pair" || strings.HasPrefix(clean, "/api/pair/") {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			proxyOpenCode(c, resource)
			return
		}
		if strings.HasPrefix(clean, "/auth/") || upgrade || (c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		files, err := officialweb.Files()
		if err != nil {
			c.String(http.StatusInternalServerError, "OpenCode UI assets: %s", err)
			return
		}
		name := strings.TrimPrefix(clean, "/")
		if clean == "/" {
			name = "index.html"
		}
		data, err := fs.ReadFile(files, name)
		if err != nil {
			if strings.HasPrefix(name, "_assets/") || path.Ext(name) != "" {
				c.AbortWithStatus(http.StatusNotFound)
				return
			}
			name = "index.html"
			data, err = fs.ReadFile(files, name)
		}
		if err != nil {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.Header("X-Frame-Options", "SAMEORIGIN")
		c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https:; font-src 'self' data:; connect-src 'self'; worker-src 'self' blob:; frame-ancestors 'self'; base-uri 'self'; object-src 'none'; form-action 'self'")
		if name == "index.html" {
			data = openCodeThemeScript.ReplaceAll(data, nil)
			data = bytes.Replace(data, []byte("<head>"), []byte(`<head><base href="`+html.EscapeString(base)+`/"><meta name="panel-opencode-base" content="`+html.EscapeString(base)+`">`), 1)
			data = bytes.ReplaceAll(data, []byte(`href="/`), []byte(`href="`+html.EscapeString(base)+`/`))
			data = bytes.ReplaceAll(data, []byte(`src="/`), []byte(`src="`+html.EscapeString(base)+`/`))
		}
		http.ServeContent(c.Writer, c.Request, name, time.Time{}, bytes.NewReader(data))
	})
}

func proxyOpenCode(c *gin.Context, resource string) {
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Time{})
	target, password, err := panelai.Default.WebEndpoint(c.Request.Context())
	if err != nil {
		c.String(http.StatusBadGateway, "OpenCode: %s", err)
		return
	}
	proxy := &httputil.ReverseProxy{
		FlushInterval: -1,
		Rewrite: func(p *httputil.ProxyRequest) {
			p.SetURL(target)
			p.Out.URL.Path = resource
			p.Out.URL.RawPath = ""
			panelai.StripWebCredentials(p.Out.Header)
			p.Out.Host = target.Host
			p.Out.SetBasicAuth("opencode", password)
			p.Out.Header.Del("Origin")
		},
		ModifyResponse: func(response *http.Response) error {
			response.Header.Del("Set-Cookie")
			response.Header.Del("Access-Control-Allow-Origin")
			response.Header.Del("WWW-Authenticate")
			response.Header.Set("Cache-Control", "no-store")
			response.Header.Set("X-Accel-Buffering", "no")
			if location := response.Header.Get("Location"); location != "" {
				return fmt.Errorf("OpenCode upstream redirects are not exposed")
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, fmt.Sprintf("OpenCode upstream connection: %v", err), http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(c.Writer, c.Request)
}
