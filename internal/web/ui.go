package web

import (
  "embed"
  "io/fs"
  "net/http"
  "strings"

  "github.com/gin-gonic/gin"

  "github.com/picoaide/picoaide/internal/auth"
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
  requireUIUser := func(c *gin.Context) (string, bool) {
    username := s.getSessionUser(c)
    if username == "" {
      c.Redirect(http.StatusFound, "/login")
      return "", false
    }
    return username, true
  }
  requireManageUser := func(c *gin.Context) bool {
    username, ok := requireUIUser(c)
    if !ok {
      return false
    }
    if auth.IsSuperadmin(username) {
      c.Redirect(http.StatusFound, "/admin/dashboard")
      return false
    }
    return true
  }
  requireAdminUser := func(c *gin.Context) bool {
    username, ok := requireUIUser(c)
    if !ok {
      return false
    }
    if !auth.IsSuperadmin(username) {
      c.Redirect(http.StatusFound, "/manage")
      return false
    }
    return true
  }

  r.GET("/", func(c *gin.Context) {
    c.Redirect(http.StatusFound, "/login")
  })
  r.GET("/login", func(c *gin.Context) {
    serveHTML(c, "login.html")
  })
  r.GET("/initializing", func(c *gin.Context) {
    if !requireManageUser(c) {
      return
    }
    serveHTML(c, "initializing.html")
  })
  r.GET("/manage", func(c *gin.Context) {
    c.Redirect(http.StatusMovedPermanently, "/manage/channels")
  })
  manageSections := []string{"skills", "channels", "files", "teamspace", "password"}
  for _, section := range manageSections {
    sectionPath := "/manage/" + section
    r.GET(sectionPath, func(c *gin.Context) {
      if !requireManageUser(c) {
        return
      }
      username := s.getSessionUser(c)
      if username != "" && auth.IsExternalUser(username) && !s.userEnvironmentReady(username) {
        c.Redirect(http.StatusFound, "/initializing")
        return
      }
      serveHTML(c, "manage/index.html")
    })
  }
  r.GET("/admin", func(c *gin.Context) {
    c.Redirect(http.StatusMovedPermanently, "/admin/dashboard")
  })
  r.GET("/admin/", func(c *gin.Context) {
    c.Redirect(http.StatusMovedPermanently, "/admin/dashboard")
  })
  adminSections := []string{"dashboard", "superadmins", "users", "groups", "images", "picoclaw", "models", "skills", "auth", "teamspace", "tls", "settings"}
  for _, section := range adminSections {
    sectionPath := "/admin/" + section
    r.GET(sectionPath, func(c *gin.Context) {
      if !requireAdminUser(c) {
        return
      }
      serveHTML(c, "admin/index.html")
    })
  }

  staticPrefixes := []string{"/css/", "/js/", "/images/", "/admin/modules/", "/admin/templates/", "/manage/modules/", "/manage/templates/"}
  for _, prefix := range staticPrefixes {
    r.GET(prefix+"*filepath", func(c *gin.Context) {
      cleanPath := strings.TrimPrefix(c.Request.URL.Path, "/")
      c.Request.URL.Path = "/" + cleanPath
      serveFile(c)
    })
  }
  r.GET("/manage/manage.js", serveFile)
  r.GET("/manage.js", serveFile)
  r.GET("/login.js", serveFile)
  r.GET("/initializing.js", serveFile)
  r.GET("/admin/admin.js", serveFile)
}
