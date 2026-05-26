package im

import (
  "context"
  "crypto/rand"
  "encoding/json"
  "fmt"
  "log/slog"
  "math/big"
  "sync"
  "time"

  "github.com/gorilla/websocket"
)

// ============================================================
// 企业微信实现（WebSocket 模式）— 支持每用户独立连接
// ============================================================

const (
  wecomConnectTimeout    = 15 * time.Second
  wecomCommandTimeout    = 10 * time.Second
  wecomHeartbeatInterval = 30 * time.Second
  wecomDefaultWSUrl      = "wss://wss.weixin.qq.com/"
)

type wecomUserConn struct {
  username  string
  botID     string
  secret    string
  wsURL     string
  conn      *websocket.Conn
  connMu    sync.Mutex
  ctx       context.Context
  cancel    context.CancelFunc
  defaultChat string
  pendingMu sync.Mutex
  pending   map[string]chan wecomEnvelope
}

type WeComProvider struct {
  mu           sync.Mutex
  conns        map[string]*wecomUserConn // username -> 连接
  rootCtx      context.Context
  rootCancel   context.CancelFunc
  onMessage    func(ctx context.Context, msg Message)
  chatIDToUser sync.Map // chatID -> username，用于 Send 查找连接
}

type wecomEnvelope struct {
  Cmd     string          `json:"cmd"`
  Headers wecomHeaders    `json:"headers"`
  Body    json.RawMessage `json:"body,omitempty"`
  ErrCode int             `json:"errcode,omitempty"`
  ErrMsg  string          `json:"errmsg,omitempty"`
}

type wecomHeaders struct {
  ReqID string `json:"req_id"`
}

type wecomCommand struct {
  Cmd     string      `json:"cmd"`
  Headers wecomHeaders `json:"headers"`
  Body    interface{}  `json:"body,omitempty"`
}

type wecomIncomingMessage struct {
  Sender struct {
    UserID string `json:"user_id"`
  } `json:"sender"`
  MsgID    string `json:"msg_id"`
  MsgType  string `json:"msg_type"`
  ChatType string `json:"chat_type"`
  Content  struct {
    Text string `json:"text"`
  } `json:"content"`
  Text struct {
    Content string `json:"content"`
  } `json:"text"`
  AIBotID string `json:"ai_bot_id"`
}

func NewWeComProvider() *WeComProvider {
  return &WeComProvider{
    conns: make(map[string]*wecomUserConn),
  }
}

func (w *WeComProvider) Name() string { return "wecom" }

func (w *WeComProvider) SetOnMessage(handler func(ctx context.Context, msg Message)) {
  w.onMessage = handler
}

func (w *WeComProvider) Start(ctx context.Context) error {
  w.rootCtx, w.rootCancel = context.WithCancel(ctx)

  w.mu.Lock()
  for _, uc := range w.conns {
    w.startConn(w.rootCtx, uc)
  }
  w.mu.Unlock()
  return nil
}

func (w *WeComProvider) Stop(ctx context.Context) error {
  w.mu.Lock()
  for _, uc := range w.conns {
    w.stopConn(uc)
  }
  w.mu.Unlock()

  if w.rootCancel != nil {
    w.rootCancel()
  }
  return nil
}

// AddUser 为指定用户添加企微连接。如果用户已有连接则先关闭旧连接。
func (w *WeComProvider) AddUser(username, botID, secret string, defaultChat string) {
  slog.Info("企微渠道添加用户连接", "username", username, "bot_id", botID)

  w.mu.Lock()
  defer w.mu.Unlock()

  // 关闭旧连接
  if old, ok := w.conns[username]; ok {
    w.stopConn(old)
  }

  uc := &wecomUserConn{
    username:    username,
    botID:       botID,
    secret:      secret,
    wsURL:       wecomDefaultWSUrl,
    defaultChat: defaultChat,
    pending:     make(map[string]chan wecomEnvelope),
  }
  w.conns[username] = uc

  if w.rootCtx != nil {
    w.startConn(w.rootCtx, uc)
  }
}

// RemoveUser 移除用户的企微连接
func (w *WeComProvider) RemoveUser(username string) {
  slog.Info("企微渠道移除用户连接", "username", username)

  w.mu.Lock()
  defer w.mu.Unlock()

  if uc, ok := w.conns[username]; ok {
    w.stopConn(uc)
    delete(w.conns, username)
  }
}

func (w *WeComProvider) startConn(ctx context.Context, uc *wecomUserConn) {
  connCtx, cancel := context.WithCancel(ctx)
  uc.ctx = connCtx
  uc.cancel = cancel

  go w.connectLoop(uc)
}

func (w *WeComProvider) stopConn(uc *wecomUserConn) {
  if uc.cancel != nil {
    uc.cancel()
  }
  uc.connMu.Lock()
  if uc.conn != nil {
    uc.conn.Close()
    uc.conn = nil
  }
  uc.connMu.Unlock()
}

func (w *WeComProvider) connectLoop(uc *wecomUserConn) {
  backoff := time.Second
  for {
    select {
    case <-uc.ctx.Done():
      return
    default:
    }

    if err := w.runConnection(uc); err != nil {
      slog.Warn("企微 WebSocket 连接断开",
        "username", uc.username,
        "error", err,
        "backoff", backoff,
      )
      select {
      case <-time.After(backoff):
      case <-uc.ctx.Done():
        return
      }
      if backoff < time.Minute {
        backoff *= 2
        if backoff > time.Minute {
          backoff = time.Minute
        }
      }
      continue
    }
    return
  }
}

func (w *WeComProvider) runConnection(uc *wecomUserConn) error {
  dialCtx, cancel := context.WithTimeout(uc.ctx, wecomConnectTimeout)
  defer cancel()

  conn, _, err := websocket.DefaultDialer.DialContext(dialCtx, uc.wsURL, nil)
  if err != nil {
    return fmt.Errorf("企微 WebSocket 连接失败: %w", err)
  }

  uc.connMu.Lock()
  uc.conn = conn
  uc.connMu.Unlock()
  defer func() {
    uc.connMu.Lock()
    if uc.conn == conn {
      uc.conn = nil
    }
    uc.connMu.Unlock()
    conn.Close()
  }()

  // 订阅消息
  if err := w.writeAndWait(uc, conn, wecomCommand{
    Cmd:     "subscribe",
    Headers: wecomHeaders{ReqID: randomID(10)},
    Body: map[string]string{
      "bot_id": uc.botID,
      "secret": uc.secret,
    },
  }, wecomCommandTimeout); err != nil {
    return err
  }

  slog.Info("企微 WebSocket 已连接并订阅", "username", uc.username)

  // 心跳
  heartbeatDone := make(chan struct{})
  go func() {
    defer close(heartbeatDone)
    ticker := time.NewTicker(wecomHeartbeatInterval)
    defer ticker.Stop()
    for {
      select {
      case <-ticker.C:
        if err := w.writeAndWait(uc, conn, wecomCommand{
          Cmd:     "ping",
          Headers: wecomHeaders{ReqID: randomID(10)},
        }, wecomCommandTimeout); err != nil {
          return
        }
      case <-uc.ctx.Done():
        return
      }
    }
  }()

  // 读循环
  readErr := w.readLoop(uc, conn)
  conn.Close()
  <-heartbeatDone
  return readErr
}

func (w *WeComProvider) readLoop(uc *wecomUserConn, conn *websocket.Conn) error {
  for {
    _, raw, err := conn.ReadMessage()
    if err != nil {
      select {
      case <-uc.ctx.Done():
        return nil
      default:
        return fmt.Errorf("企微读取错误: %w", err)
      }
    }

    var env wecomEnvelope
    if err := json.Unmarshal(raw, &env); err != nil {
      continue
    }

    // ACK 消息
    if env.Cmd == "" && env.Headers.ReqID != "" {
      uc.pendingMu.Lock()
      ch, ok := uc.pending[env.Headers.ReqID]
      if ok {
        delete(uc.pending, env.Headers.ReqID)
      }
      uc.pendingMu.Unlock()
      if ok {
        ch <- env
      }
      continue
    }

    // 消息回调
    if env.Cmd == "message_callback" {
      w.handleIncoming(uc, env)
    }
  }
}

func (w *WeComProvider) handleIncoming(uc *wecomUserConn, env wecomEnvelope) {
  var msg wecomIncomingMessage
  if err := json.Unmarshal(env.Body, &msg); err != nil {
    return
  }

  senderID := msg.Sender.UserID
  if senderID == "" {
    senderID = "unknown"
  }
  chatID := msg.MsgID
  if chatID == "" {
    chatID = senderID
  }

  content := msg.Text.Content
  if content == "" {
    content = msg.Content.Text
  }
  if content == "" {
    content = "[empty]"
  }

  // 记录 chatID -> username 映射，用于 Send 查找连接
  w.chatIDToUser.Store(chatID, uc.username)
  uc.defaultChat = chatID

  if w.onMessage != nil {
    w.onMessage(uc.ctx, Message{
      Platform: "wecom",
      UserID:   uc.username, // 用 PicoAide 用户名，而非企微 ID
      ChatID:   chatID,
      Text:     content,
      Raw: map[string]string{
        "msg_id":    msg.MsgID,
        "msg_type":  msg.MsgType,
        "chat_type": msg.ChatType,
      },
    })
  }
}

func (w *WeComProvider) Send(ctx context.Context, msg SendMsg) error {
  // 查找 chatID 对应的用户名
  userRaw, ok := w.chatIDToUser.Load(msg.ChatID)
  if !ok {
    return fmt.Errorf("企微发送失败: 未找到 chatID 对应的用户连接")
  }
  username, _ := userRaw.(string)

  w.mu.Lock()
  uc, ok := w.conns[username]
  w.mu.Unlock()
  if !ok {
    return fmt.Errorf("企微发送失败: 未找到用户连接 %s", username)
  }

  return w.sendCommand(uc, wecomCommand{
    Cmd:     "send_msg",
    Headers: wecomHeaders{ReqID: randomID(10)},
    Body: map[string]interface{}{
      "chat_id":  msg.ChatID,
      "msg_type": "markdown",
      "markdown": map[string]string{
        "content": msg.Text,
      },
    },
  })
}

func (w *WeComProvider) SendToUser(ctx context.Context, username string, text string) error {
  w.mu.Lock()
  uc, ok := w.conns[username]
  w.mu.Unlock()
  if !ok {
    return fmt.Errorf("企微发送失败: 未找到用户连接 %s", username)
  }
  if uc.defaultChat == "" {
    return fmt.Errorf("用户 %s 没有可用的企微会话", username)
  }

  return w.sendCommand(uc, wecomCommand{
    Cmd:     "send_msg",
    Headers: wecomHeaders{ReqID: randomID(10)},
    Body: map[string]interface{}{
      "chat_id":  uc.defaultChat,
      "msg_type": "markdown",
      "markdown": map[string]string{
        "content": text,
      },
    },
  })
}

func (w *WeComProvider) sendCommand(uc *wecomUserConn, cmd wecomCommand) error {
  uc.connMu.Lock()
  conn := uc.conn
  uc.connMu.Unlock()
  if conn == nil {
    return fmt.Errorf("企微未连接")
  }
  return w.writeAndWait(uc, conn, cmd, wecomCommandTimeout)
}

func (w *WeComProvider) writeAndWait(uc *wecomUserConn, conn *websocket.Conn, cmd wecomCommand, timeout time.Duration) error {
  if cmd.Headers.ReqID == "" {
    cmd.Headers.ReqID = randomID(10)
  }
  waitCh := make(chan wecomEnvelope, 1)
  uc.pendingMu.Lock()
  uc.pending[cmd.Headers.ReqID] = waitCh
  uc.pendingMu.Unlock()
  defer func() {
    uc.pendingMu.Lock()
    delete(uc.pending, cmd.Headers.ReqID)
    uc.pendingMu.Unlock()
  }()

  data, err := json.Marshal(cmd)
  if err != nil {
    return fmt.Errorf("企微命令序列化失败: %w", err)
  }
  uc.connMu.Lock()
  err = conn.WriteMessage(websocket.TextMessage, data)
  uc.connMu.Unlock()
  if err != nil {
    return fmt.Errorf("企微写入失败: %w", err)
  }

  timer := time.NewTimer(timeout)
  defer timer.Stop()
  select {
  case <-waitCh:
    return nil
  case <-timer.C:
    return fmt.Errorf("企微命令超时")
  case <-uc.ctx.Done():
    return uc.ctx.Err()
  }
}

func randomID(n int) string {
  const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
  if n <= 0 {
    n = 10
  }
  buf := make([]byte, n)
  for i := range buf {
    v, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
    buf[i] = alphabet[v.Int64()]
  }
  return string(buf)
}
