package web

import (
  "log/slog"
  "net/http"
  "time"

  "github.com/gorilla/websocket"
)

// handleBrowserWS 处理 Extension WebSocket 连接（GET /api/browser/ws?token=xxx）
func (s *Server) handleBrowserWS(w http.ResponseWriter, r *http.Request) {
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
    slog.Error("Extension WebSocket 升级失败", "username", username, "error", err)
    return
  }

  conn := browserSvc.Register(username, ws, &BrowserExtra{TabID: 0})
  slog.Info("Extension WebSocket 已连接", "username", username, "remote", ws.RemoteAddr())

  select {
  case <-conn.done:
    slog.Info("Extension WebSocket 已断开", "username", username)
  }

  ws.WriteControl(websocket.CloseMessage,
    websocket.FormatCloseMessage(websocket.CloseNormalClosure, "连接关闭"),
    time.Now().Add(time.Second))
}
