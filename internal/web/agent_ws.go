package web

import (
  "log/slog"
  "time"

  "github.com/gin-gonic/gin"
  "github.com/gorilla/websocket"

  "github.com/picoaide/picoaide/internal/logger"
)

// computerSvc 桌面控制服务的连接管理器
var computerSvc = NewServiceHub("computer")

// handleAgentWS 处理代理 WebSocket 连接
func (s *Server) handleAgentWS(c *gin.Context, svcName string, hub *ServiceHub, extra interface{}, displayName string) {
  username := validateBearerOrQueryToken(c)
  if username == "" {
    return
  }
  logger.DebugRecv("GET", "/api/"+svcName+"/ws", "username", username, "remote", c.ClientIP())

  ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
  if err != nil {
    slog.Error(displayName+" WebSocket 升级失败", "username", username, "error", err)
    return
  }

  conn := hub.Register(username, ws, extra)
  slog.Info(displayName+" WebSocket 已连接", "username", username, "remote", ws.RemoteAddr())
  logger.DebugProcess("ws_connected", "service", svcName, "username", username, "remote", ws.RemoteAddr())

  <-conn.done
  slog.Info(displayName+" WebSocket 已断开", "username", username)
  logger.DebugProcess("ws_disconnected", "service", svcName, "username", username)

  ws.WriteControl(websocket.CloseMessage,
    websocket.FormatCloseMessage(websocket.CloseNormalClosure, "连接关闭"),
    time.Now().Add(time.Second))
}
