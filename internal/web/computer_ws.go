package web

import "github.com/gin-gonic/gin"

// handleComputerWS 处理桌面代理 WebSocket 连接（GET /api/computer/ws?token=xxx）
func (s *Server) handleComputerWS(c *gin.Context) {
  s.handleAgentWS(c, "computer", computerSvc, nil, "桌面代理")
}
