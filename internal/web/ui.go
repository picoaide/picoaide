package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed ui/*
var webUI embed.FS

func (s *Server) registerUIRoutes(r *gin.Engine) {
	uiFS, err := fs.Sub(webUI, "ui")
	if err != nil {
		panic(err)
	}

	fileServer := http.FileServer(http.FS(uiFS))
	serveFile := func(c *gin.Context) {
		fileServer.ServeHTTP(c.Writer, c.Request)
	}
	serveHTML := func(c *gin.Context, name string) {
		data, err := fs.ReadFile(uiFS, name)
		if err != nil {
			c.String(http.StatusNotFound, "404 page not found")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", data)
	}

	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/login")
	})
	r.GET("/login", func(c *gin.Context) {
		serveHTML(c, "login.html")
	})
	r.GET("/manage", func(c *gin.Context) {
		serveHTML(c, "manage.html")
	})
	r.GET("/manage/", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/manage")
	})
	r.GET("/admin", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/admin/dashboard")
	})
	r.GET("/admin/", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/admin/dashboard")
	})
	adminSections := []string{"dashboard", "superadmins", "users", "groups", "images", "picoclaw", "models", "skills", "auth", "settings"}
	for _, section := range adminSections {
		sectionPath := "/admin/" + section
		r.GET(sectionPath, func(c *gin.Context) {
			serveHTML(c, "admin/index.html")
		})
	}

	staticPrefixes := []string{"/css/", "/js/", "/admin/modules/", "/admin/templates/"}
	for _, prefix := range staticPrefixes {
		r.GET(prefix+"*filepath", func(c *gin.Context) {
			cleanPath := strings.TrimPrefix(c.Request.URL.Path, "/")
			c.Request.URL.Path = "/" + cleanPath
			serveFile(c)
		})
	}
	r.GET("/manage.js", serveFile)
	r.GET("/login.js", serveFile)
	r.GET("/admin/admin.js", serveFile)
}
