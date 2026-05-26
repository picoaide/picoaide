package web

import (
  "net/http"

  "github.com/gin-gonic/gin"
)

// ============================================================
// 异步任务
// ============================================================

// handleAdminTaskStatus 返回当前任务队列状态
func (s *Server) handleAdminTaskStatus(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "GET" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }
  status := getTaskStatus()
  writeJSON(c, http.StatusOK, status)
}
