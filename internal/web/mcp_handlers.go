package web

import (
  "log"
  "net/http"
  "strings"
  "time"

  "github.com/gorilla/websocket"

  "github.com/picoaide/picoaide/internal/auth"
)

var upgrader = websocket.Upgrader{
  ReadBufferSize:  4096,
  WriteBufferSize: 4096,
  CheckOrigin: func(r *http.Request) bool {
    return true
  },
}

// handleMCPToken 返回当前用户的 MCP token
func (s *Server) handleMCPToken(w http.ResponseWriter, r *http.Request) {
  username := s.requireAuth(w, r)
  if username == "" {
    return
  }
  if r.Method != "GET" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }

  token, err := auth.GetMCPToken(username)
  if err != nil {
    writeError(w, http.StatusInternalServerError, err.Error())
    return
  }
  if token == "" {
    writeError(w, http.StatusNotFound, "MCP token 不存在")
    return
  }

  writeJSON(w, http.StatusOK, struct {
    Success bool   `json:"success"`
    Token   string `json:"token"`
  }{
    Success: true,
    Token:   token,
  })
}

// handleMCPCDP 处理 WebSocket 升级，浏览器端和容器端都连这里
func (s *Server) handleMCPCDP(w http.ResponseWriter, r *http.Request) {
  kind := r.URL.Query().Get("kind")
  if kind == "" {
    kind = "mcp"
  }
  if kind != "browser" && kind != "mcp" {
    writeError(w, http.StatusBadRequest, "kind 参数必须是 browser 或 mcp")
    return
  }

  var username string

  // 统一认证：优先 token query param，其次 Authorization header，最后 session cookie
  token := r.URL.Query().Get("token")
  if token != "" {
    var ok bool
    username, ok = auth.ValidateMCPToken(token)
    if !ok {
      writeError(w, http.StatusForbidden, "无效的 MCP token")
      return
    }
  } else {
    authHeader := r.Header.Get("Authorization")
    if strings.HasPrefix(authHeader, "Bearer ") {
      var ok bool
      username, ok = auth.ValidateMCPToken(strings.TrimPrefix(authHeader, "Bearer "))
      if !ok {
        writeError(w, http.StatusForbidden, "无效的 MCP token")
        return
      }
    } else {
      username = s.getSessionUser(r)
      if username == "" {
        writeError(w, http.StatusUnauthorized, "未登录")
        return
      }
    }
  }

  // WebSocket 升级
  ws, err := upgrader.Upgrade(w, r, nil)
  if err != nil {
    log.Printf("[mcp] WebSocket 升级失败 %s %s: %v", username, kind, err)
    return
  }

  log.Printf("[mcp] %s %s 连接 (%s)", username, kind, ws.RemoteAddr())

  conn := &RelayConn{
    ws:       ws,
    kind:     kind,
    username: username,
    closeCh:  make(chan struct{}),
  }

  hub.Register(conn)

  // 防止 goroutine 泄漏：如果连接一直处于 pending 状态，设置超时
  go func() {
    time.Sleep(5 * time.Minute)
    hub.mu.RLock()
    if c, ok := hub.pending[username]; ok && c == conn {
      hub.mu.RUnlock()
      hub.mu.Lock()
      delete(hub.pending, username)
      hub.mu.Unlock()
      ws.WriteControl(websocket.CloseMessage,
        websocket.FormatCloseMessage(websocket.CloseNormalClosure, "等待超时"),
        time.Now().Add(time.Second))
      ws.Close()
      log.Printf("[mcp] %s %s 等待超时断开", username, kind)
    } else {
      hub.mu.RUnlock()
    }
  }()
}
