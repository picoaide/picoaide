package web

import (
  "embed"
  "io/fs"
  "net/http"
  "github.com/gin-gonic/gin"

  "github.com/picoaide/picoaide/internal/store"
)

//go:embed all:dist
var webUI embed.FS

func (s *Server) registerUIRoutes(r *gin.Engine) {
  uiFS, err := fs.Sub(webUI, "dist")
  if err != nil {
    panic(err)
  }

  fileServer := http.FileServer(http.FS(uiFS))
  serveFile := func(c *gin.Context) {
    fileServer.ServeHTTP(c.Writer, c.Request)
  }
  serveSPA := func(c *gin.Context) {
    data, err := fs.ReadFile(uiFS, "index.html")
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
    if store.IsSuperadmin(username) {
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
    if !store.IsSuperadmin(username) {
      c.Redirect(http.StatusFound, "/user/chat")
      return false
    }
    return true
  }

  r.GET("/", func(c *gin.Context) {
    c.Redirect(http.StatusFound, "/login")
  })
  r.GET("/login", func(c *gin.Context) {
    serveSPA(c)
  })

  // 用户页面
  userPaths := []string{
    "/user", "/user/chat", "/user/skills", "/user/files", "/user/settings",
    "/user/channels", "/user/email", "/user/teamspace", "/user/authorization",
    "/user/password", "/user/cron", "/user/wiki",
  }
  for _, path := range userPaths {
    r.GET(path, func(c *gin.Context) {
      if !requireManageUser(c) {
        return
      }
      serveSPA(c)
    })
  }

  // 旧路径兼容
  r.GET("/manage", func(c *gin.Context) {
    c.Redirect(http.StatusMovedPermanently, "/user/chat")
  })
  r.GET("/manage/*path", func(c *gin.Context) {
    c.Redirect(http.StatusMovedPermanently, "/user/chat")
  })

  // 管理页面
  adminPaths := []string{
    "/admin", "/admin/dashboard", "/admin/users", "/admin/groups",
    "/admin/skills", "/admin/superadmins", "/admin/channels",
    "/admin/models", "/admin/auth", "/admin/teamspace", "/admin/password",
    "/admin/mcp-servers", "/admin/tls", "/admin/wiki",
  }
  for _, path := range adminPaths {
    r.GET(path, func(c *gin.Context) {
      if !requireAdminUser(c) {
        return
      }
      serveSPA(c)
    })
  }

  // 静态资源
  r.GET("/assets/*filepath", func(c *gin.Context) {
    serveFile(c)
  })
  r.GET("/favicon.ico", func(c *gin.Context) {
    c.Request.URL.Path = "/favicon.svg"
    serveFile(c)
  })

  // 旧 /chat → 301 到 /user/chat
  r.GET("/chat", func(c *gin.Context) {
    c.Redirect(http.StatusMovedPermanently, "/user/chat")
  })
}
