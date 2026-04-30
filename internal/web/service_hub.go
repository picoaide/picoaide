package web

import (
  "context"
  "encoding/json"
  "fmt"
  "log"
  "sync"
  "time"

  "github.com/gorilla/websocket"
)

const commandTimeout = 30 * time.Second

// PendingCall 跟踪一个等待代理响应的工具调用
type PendingCall struct {
  resultCh chan<- json.RawMessage
  timer    *time.Timer
}

// ServiceHub 管理某一类 MCP 服务的后端 WebSocket 连接
type ServiceHub struct {
  name  string
  mu    sync.RWMutex
  conns map[string]*AgentConn
}

// AgentConn 包装一个用户的代理 WebSocket 连接
type AgentConn struct {
  serviceName string
  username    string
  ws          *websocket.Conn
  mu          sync.Mutex
  pending     sync.Map // map[int]*PendingCall
  nextID      int
  done        chan struct{}
  Extra       interface{} // 服务特定数据
}

// NewServiceHub 创建一个服务连接管理器
func NewServiceHub(name string) *ServiceHub {
  return &ServiceHub{
    name:  name,
    conns: make(map[string]*AgentConn),
  }
}

// Register 注册代理连接，踢掉旧连接
func (h *ServiceHub) Register(username string, ws *websocket.Conn, extra interface{}) *AgentConn {
  h.mu.Lock()
  defer h.mu.Unlock()

  // 踢掉旧连接
  if old, ok := h.conns[username]; ok {
    old.Close()
  }

  conn := &AgentConn{
    serviceName: h.name,
    username:    username,
    ws:          ws,
    done:        make(chan struct{}),
    Extra:       extra,
  }
  h.conns[username] = conn

  go conn.readPump(h)
  go conn.keepAlive()

  log.Printf("[%s-hub] %s 代理注册", h.name, username)
  return conn
}

// Unregister 移除代理连接
func (h *ServiceHub) Unregister(username string) {
  h.mu.Lock()
  defer h.mu.Unlock()
  delete(h.conns, username)
  log.Printf("[%s-hub] %s 代理注销", h.name, username)
}

// GetConnection 获取用户的代理连接
func (h *ServiceHub) GetConnection(username string) (*AgentConn, bool) {
  h.mu.RLock()
  defer h.mu.RUnlock()
  conn, ok := h.conns[username]
  return conn, ok
}

// Close 关闭 AgentConn
func (c *AgentConn) Close() {
  select {
  case <-c.done:
    return
  default:
  }

  c.ws.WriteControl(websocket.CloseMessage,
    websocket.FormatCloseMessage(websocket.CloseNormalClosure, "被新连接替换"),
    time.Now().Add(time.Second))
  c.ws.Close()

  // 取消所有等待中的调用
  c.pending.Range(func(key, value interface{}) bool {
    call := value.(*PendingCall)
    call.timer.Stop()
    c.pending.Delete(key)
    return true
  })
}

// SendCommand 向代理发送工具命令并等待响应
func (c *AgentConn) SendCommand(ctx context.Context, tool string, params map[string]interface{}) (json.RawMessage, error) {
  c.mu.Lock()
  c.nextID++
  id := c.nextID
  c.mu.Unlock()

  cmd := map[string]interface{}{
    "id":     id,
    "tool":   tool,
    "params": params,
  }
  data, err := json.Marshal(cmd)
  if err != nil {
    return nil, fmt.Errorf("序列化命令失败: %w", err)
  }

  resultCh := make(chan json.RawMessage, 1)
  call := &PendingCall{resultCh: resultCh}

  call.timer = time.AfterFunc(commandTimeout, func() {
    c.pending.Delete(id)
    close(resultCh) // 超时关闭 channel
  })

  c.pending.Store(id, call)
  defer c.pending.Delete(id)

  c.mu.Lock()
  err = c.ws.WriteMessage(websocket.TextMessage, data)
  c.mu.Unlock()
  if err != nil {
    call.timer.Stop()
    return nil, fmt.Errorf("发送命令失败: %w", err)
  }

  select {
  case result, ok := <-resultCh:
    call.timer.Stop()
    if !ok {
      return nil, fmt.Errorf("工具调用超时 (%v)", commandTimeout)
    }
    return result, nil
  case <-ctx.Done():
    call.timer.Stop()
    return nil, ctx.Err()
  case <-c.done:
    call.timer.Stop()
    return nil, fmt.Errorf("代理连接已断开")
  }
}

// readPump 从代理 WebSocket 读取消息并分发到等待中的调用
func (c *AgentConn) readPump(hub *ServiceHub) {
  defer func() {
    close(c.done)
    hub.Unregister(c.username)
    c.ws.Close()
  }()

  for {
    _, data, err := c.ws.ReadMessage()
    if err != nil {
      if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
        log.Printf("[%s-hub] %s 读取错误: %v", hub.name, c.username, err)
      }
      return
    }

    var msg struct {
      ID     int             `json:"id"`
      Result json.RawMessage `json:"result"`
      Error  interface{}     `json:"error"`
    }
    if err := json.Unmarshal(data, &msg); err != nil || msg.ID == 0 {
      continue
    }

    if call, ok := c.pending.Load(msg.ID); ok {
      c.pending.Delete(msg.ID)
      call.(*PendingCall).timer.Stop()

      if msg.Error != nil {
        errData, _ := json.Marshal(map[string]interface{}{
          "id":     msg.ID,
          "result": nil,
          "error":  msg.Error,
        })
        call.(*PendingCall).resultCh <- errData
      } else {
        call.(*PendingCall).resultCh <- data
      }
    }
  }
}

// keepAlive 每 30 秒发送一次 ping
func (c *AgentConn) keepAlive() {
  ticker := time.NewTicker(30 * time.Second)
  defer ticker.Stop()
  for {
    select {
    case <-ticker.C:
      c.mu.Lock()
      err := c.ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
      c.mu.Unlock()
      if err != nil {
        return
      }
    case <-c.done:
      return
    }
  }
}
