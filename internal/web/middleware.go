package web

import (
  "net/http"

  "github.com/gin-gonic/gin"
)

// requireLocalMode 仅允许本地模式访问（创建用户、修改密码）
func requireLocalMode(s *Server) gin.HandlerFunc {
  return func(c *gin.Context) {
    if s.cfg.UnifiedAuthEnabled() {
      writeError(c, http.StatusForbidden, "统一认证模式下不允许此操作，用户由外部认证系统管理")
      c.Abort()
      return
    }
    c.Next()
  }
}

// requireUnifiedMode 仅允许统一认证模式访问（白名单管理）
func requireUnifiedMode(s *Server) gin.HandlerFunc {
  return func(c *gin.Context) {
    if !s.cfg.UnifiedAuthEnabled() {
      writeError(c, http.StatusForbidden, "本地模式下无需白名单管理")
      c.Abort()
      return
    }
    c.Next()
  }
}
