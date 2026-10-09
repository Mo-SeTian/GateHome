package router

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"sun-panel/router/openness"
	"sun-panel/router/panel"
	"sun-panel/router/system"
)

func NewRouter(web http.FileSystem) http.Handler {
	r := gin.New()
	r.Use(gin.Recovery())
	root := r.Group("/sunpanel")
	api := root.Group("/api")
	system.Init(api)
	panel.Init(api)
	openness.Init(api)
	root.Static("/uploads", "./uploads")
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

func InitRouters(addr string) error { return http.ListenAndServe(addr, NewRouter(http.Dir("./web"))) }
