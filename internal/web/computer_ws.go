package web

import (
  "log"
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
    log.Printf("[computer-ws] %s WebSocket 升级失败: %v", username, err)
    return
  }

  conn := computerSvc.Register(username, ws, nil)
  log.Printf("[computer-ws] %s 桌面代理已连接 (%s)", username, ws.RemoteAddr())

  select {
  case <-conn.done:
    log.Printf("[computer-ws] %s 桌面代理已断开", username)
  }

  ws.WriteControl(websocket.CloseMessage,
    websocket.FormatCloseMessage(websocket.CloseNormalClosure, "连接关闭"),
    time.Now().Add(time.Second))
}
