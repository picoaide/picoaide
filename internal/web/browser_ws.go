package web

import "github.com/gin-gonic/gin"

// handleBrowserWS 处理 Extension WebSocket 连接（GET /api/browser/ws?token=xxx）
func (s *Server) handleBrowserWS(c *gin.Context) {
  s.handleAgentWS(c, "browser", browserSvc, &BrowserExtra{TabID: 0}, "Extension")
}
