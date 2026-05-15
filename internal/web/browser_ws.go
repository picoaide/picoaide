package web

import (
  "log/slog"
  "time"

  "github.com/gin-gonic/gin"
  "github.com/gorilla/websocket"
)

// handleBrowserWS 处理 Extension WebSocket 连接（GET /api/browser/ws?token=xxx）
func (s *Server) handleBrowserWS(c *gin.Context) {
  username := validateBearerOrQueryToken(c)
  if username == "" {
    return
  }

  ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
  if err != nil {
    slog.Error("Extension WebSocket 升级失败", "username", username, "error", err)
    return
  }

  conn := browserSvc.Register(username, ws, &BrowserExtra{TabID: 0})
  slog.Info("Extension WebSocket 已连接", "username", username, "remote", ws.RemoteAddr())

  <-conn.done
  slog.Info("Extension WebSocket 已断开", "username", username)

  ws.WriteControl(websocket.CloseMessage,
    websocket.FormatCloseMessage(websocket.CloseNormalClosure, "连接关闭"),
    time.Now().Add(time.Second))
}
