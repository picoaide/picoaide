package web

import (
  "log/slog"
  "net/http"
  "time"

  "github.com/gorilla/websocket"
)

// computerSvc 桌面控制服务的连接管理器
var computerSvc = NewServiceHub("computer")

// handleComputerWS 处理桌面代理 WebSocket 连接（GET /api/computer/ws?token=xxx）
func (s *Server) handleComputerWS(w http.ResponseWriter, r *http.Request) {
  if r.Method != "GET" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }

  username := validateBearerOrQueryToken(w, r)
  if username == "" {
    return
  }

  ws, err := upgrader.Upgrade(w, r, nil)
  if err != nil {
    slog.Error("桌面代理 WebSocket 升级失败", "username", username, "error", err)
    return
  }

  conn := computerSvc.Register(username, ws, nil)
  slog.Info("桌面代理已连接", "username", username, "remote", ws.RemoteAddr())

  select {
  case <-conn.done:
    slog.Info("桌面代理已断开", "username", username)
  }

  ws.WriteControl(websocket.CloseMessage,
    websocket.FormatCloseMessage(websocket.CloseNormalClosure, "连接关闭"),
    time.Now().Add(time.Second))
}
