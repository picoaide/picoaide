package web

import (
  "log"
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
    log.Printf("[browser-ws] %s WebSocket 升级失败: %v", username, err)
    return
  }

  // 默认 tabId=0，Extension 连接后会上报
  conn := browserHub.Register(username, ws, 0)
  log.Printf("[browser-ws] %s Extension WebSocket 已连接 (%s)", username, ws.RemoteAddr())

  // 等待连接断开
  select {
  case <-conn.done:
    log.Printf("[browser-ws] %s Extension WebSocket 已断开", username)
  }

  // 连接关闭后清理
  ws.WriteControl(websocket.CloseMessage,
    websocket.FormatCloseMessage(websocket.CloseNormalClosure, "连接关闭"),
    time.Now().Add(time.Second))
}
