package web

import (
  "log"
  "sync"
  "time"

  "github.com/gorilla/websocket"
)

// RelayConn 包装一个 WebSocket 连接
type RelayConn struct {
  ws       *websocket.Conn
  kind     string // "browser" 或 "mcp"
  username string
  closeCh  chan struct{}
}

// RelayPair 管理一对已匹配的连接
type RelayPair struct {
  browser *RelayConn
  mcp     *RelayConn
  done    chan struct{}
}

// RelayHub 管理所有活跃的 MCP 中继连接
type RelayHub struct {
  mu      sync.RWMutex
  pending map[string]*RelayConn // 等待匹配的连接
  active  map[string]*RelayPair // 已匹配的活跃对
}

var hub = &RelayHub{
  pending: make(map[string]*RelayConn),
  active:  make(map[string]*RelayPair),
}

// Register 注册连接，尝试匹配对端
func (h *RelayHub) Register(conn *RelayConn) {
  h.mu.Lock()
  defer h.mu.Unlock()

  username := conn.username

  // 检查是否有活跃对（同 kind 踢掉旧的）
  if pair, ok := h.active[username]; ok {
    existing := pair.browser
    if conn.kind == "mcp" {
      existing = pair.mcp
    }
    if existing != nil && existing.ws != nil {
      existing.ws.WriteControl(websocket.CloseMessage,
        websocket.FormatCloseMessage(websocket.CloseNormalClosure, "被新连接替换"),
        time.Now().Add(time.Second))
      existing.ws.Close()
    }
    // 替换并继续 relay
    if conn.kind == "browser" {
      pair.browser = conn
    } else {
      pair.mcp = conn
    }
    go h.relay(pair)
    log.Printf("[relay] %s %s 替换已匹配连接，继续中继", username, conn.kind)
    return
  }

  // 检查是否有等待中的对端
  if pending, ok := h.pending[username]; ok && pending.kind != conn.kind {
    // 匹配成功
    delete(h.pending, username)
    pair := &RelayPair{
      browser: pending,
      mcp:     conn,
      done:    make(chan struct{}),
    }
    if conn.kind == "browser" {
      pair.browser = conn
      pair.mcp = pending
    }
    h.active[username] = pair
    go h.relay(pair)
    log.Printf("[relay] %s 浏览器和 MCP 匹配成功，开始中继", username)
    return
  }

  // 没有对端等待，踢掉同 kind 的 pending
  if old, ok := h.pending[username]; ok && old.kind == conn.kind {
    old.ws.WriteControl(websocket.CloseMessage,
      websocket.FormatCloseMessage(websocket.CloseNormalClosure, "被新连接替换"),
      time.Now().Add(time.Second))
    old.ws.Close()
  }

  h.pending[username] = conn
  log.Printf("[relay] %s %s 等待匹配", username, conn.kind)
}

// Unregister 清理连接
func (h *RelayHub) Unregister(username string) {
  h.mu.Lock()
  defer h.mu.Unlock()

  delete(h.pending, username)
  if pair, ok := h.active[username]; ok {
    close(pair.done)
    delete(h.active, username)
  }
}

// relay 双向透传消息
func (h *RelayHub) relay(pair *RelayPair) {
  done := pair.done
  errCh := make(chan error, 2)

  // browser → mcp
  go func() {
    errCh <- h.copy(pair.mcp.ws, pair.browser.ws, done)
  }()

  // mcp → browser
  go func() {
    errCh <- h.copy(pair.browser.ws, pair.mcp.ws, done)
  }()

  err := <-errCh

  h.mu.Lock()
  delete(h.active, pair.browser.username)
  h.mu.Unlock()

  close(done)

  // 关闭双方连接
  pair.browser.ws.WriteControl(websocket.CloseMessage,
    websocket.FormatCloseMessage(websocket.CloseNormalClosure, "对端断开"),
    time.Now().Add(time.Second))
  pair.mcp.ws.WriteControl(websocket.CloseMessage,
    websocket.FormatCloseMessage(websocket.CloseNormalClosure, "对端断开"),
    time.Now().Add(time.Second))
  pair.browser.ws.Close()
  pair.mcp.ws.Close()

  if err != nil {
    log.Printf("[relay] %s 中继结束: %v", pair.browser.username, err)
  } else {
    log.Printf("[relay] %s 中继结束", pair.browser.username)
  }
}

// copy 从 src 读取消息写入 dst
func (h *RelayHub) copy(dst, src *websocket.Conn, done chan struct{}) error {
  for {
    select {
    case <-done:
      return nil
    default:
    }

    src.SetReadDeadline(time.Now().Add(300 * time.Second))
    mt, data, err := src.ReadMessage()
    if err != nil {
      return err
    }

    dst.SetWriteDeadline(time.Now().Add(10 * time.Second))
    if err := dst.WriteMessage(mt, data); err != nil {
      return err
    }
  }
}

// ActiveCount 返回活跃连接数
func (h *RelayHub) ActiveCount() int {
  h.mu.RLock()
  defer h.mu.RUnlock()
  return len(h.active)
}

// PendingCount 返回等待连接数
func (h *RelayHub) PendingCount() int {
  h.mu.RLock()
  defer h.mu.RUnlock()
  return len(h.pending)
}
