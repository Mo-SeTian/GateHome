package router

import (
	"bytes"
	"errors"
	"github.com/gin-gonic/gin"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"sun-panel/router/openness"
	"sun-panel/router/panel"
	"sun-panel/router/system"
)

func NewRouter(web http.FileSystem) http.Handler {
	r := gin.New()
	// The bundled worker is reached through GateHome's loopback reverse proxy.
	// Forwarded addresses from any other peer are ignored.
	r.SetTrustedProxies([]string{"127.0.0.1", "::1"})
	r.Use(gin.Recovery())
	root := r.Group("/sunpanel")
	api := root.Group("/api", limitAPIRequestBody)
	system.Init(api)
	panel.Init(api)
	openness.Init(api)
	uploads := root.Group("/uploads")
	uploads.Use(uploadHeaders)
	uploads.Static("", "./uploads")
	root.Static("/custom", "./custom")
	root.GET("/healthz", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	files := http.StripPrefix("/sunpanel/", http.FileServer(web))
	r.NoRoute(func(c *gin.Context) {
		if c.Request.URL.Path == "/" {
			c.Redirect(http.StatusFound, "/sunpanel/")
			return
		}
		if c.Request.Method != "GET" && c.Request.Method != "HEAD" {
			c.Status(404)
			return
		}
		files.ServeHTTP(c.Writer, c.Request)
	})
	return r
}

// Multipart handlers apply their own limits after checking the user's session.
func limitAPIRequestBody(c *gin.Context) {
	if c.Request.Method != http.MethodPost {
		return
	}
	var limit int64 = 1 << 20
	switch c.FullPath() {
	case "/sunpanel/api/file/uploadImg", "/sunpanel/api/file/uploadFiles",
		"/sunpanel/api/panel/itemIcon/portable/inspect", "/sunpanel/api/panel/itemIcon/portable/import":
		return
	case "/sunpanel/api/login", "/sunpanel/api/notice/getListByDisplayType":
		limit = 64 << 10
	}
	reject := func() {
		c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"code": 413, "msg": "请求体超过大小限制"})
	}
	if c.Request.ContentLength > limit {
		reject()
		return
	}
	body := http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	c.Request.Body = body
	if c.Request.ContentLength >= 0 {
		return
	}
	// Bound chunked requests before handing any partial JSON to a handler.
	data, err := io.ReadAll(body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			reject()
		} else {
			c.AbortWithStatus(http.StatusBadRequest)
		}
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(data))
}

func InitRouters(addr string) error { return http.ListenAndServe(addr, NewRouter(http.Dir("./web"))) }

// Apply to old files as well as new uploads. Active content never runs in the application origin.
func uploadHeaders(c *gin.Context) {
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'")
	switch strings.ToLower(path.Ext(c.Request.URL.Path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".svg":
	default:
		c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(c.Request.URL.Path)}))
	}
}
