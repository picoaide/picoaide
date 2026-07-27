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
// 微信实现（WebSocket 模式）— 支持每用户独立连接
// ============================================================

const (
  wechatConnectTimeout    = 15 * time.Second
  wechatCommandTimeout    = 10 * time.Second
  wechatHeartbeatInterval = 30 * time.Second
  wechatDefaultWSUrl      = "wss://wss.weixin.qq.com/"
)

type wechatUserConn struct {
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
  pending   map[string]chan wechatEnvelope
}

type WeChatProvider struct {
  mu           sync.Mutex
  conns        map[string]*wechatUserConn // username -> 连接
  rootCtx      context.Context
  rootCancel   context.CancelFunc
  onMessage    func(ctx context.Context, msg Message)
  chatIDToUser sync.Map // chatID -> username，用于 Send 查找连接
}

type wechatEnvelope struct {
  Cmd     string          `json:"cmd"`
  Headers wechatHeaders    `json:"headers"`
  Body    json.RawMessage `json:"body,omitempty"`
  ErrCode int             `json:"errcode,omitempty"`
  ErrMsg  string          `json:"errmsg,omitempty"`
}

type wechatHeaders struct {
  ReqID string `json:"req_id"`
}

type wechatCommand struct {
  Cmd     string      `json:"cmd"`
  Headers wechatHeaders `json:"headers"`
  Body    interface{}  `json:"body,omitempty"`
}

type wechatIncomingMessage struct {
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

func NewWeChatProvider() *WeChatProvider {
  return &WeChatProvider{
    conns: make(map[string]*wechatUserConn),
  }
}

func (w *WeChatProvider) Name() string { return "wechat" }

func (w *WeChatProvider) SetOnMessage(handler func(ctx context.Context, msg Message)) {
  w.onMessage = handler
}

func (w *WeChatProvider) Start(ctx context.Context) error {
  w.rootCtx, w.rootCancel = context.WithCancel(ctx)

  w.mu.Lock()
  for _, uc := range w.conns {
    w.startConn(w.rootCtx, uc)
  }
  w.mu.Unlock()
  return nil
}

func (w *WeChatProvider) Stop(ctx context.Context) error {
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

// AddUser 为指定用户添加微信连接。如果用户已有连接则先关闭旧连接。
func (w *WeChatProvider) AddUser(username, botID, secret string, defaultChat string) {
  slog.Info("微信渠道添加用户连接", "username", username, "bot_id", botID)

  w.mu.Lock()
  defer w.mu.Unlock()

  // 关闭旧连接
  if old, ok := w.conns[username]; ok {
    w.stopConn(old)
  }

  uc := &wechatUserConn{
    username:    username,
    botID:       botID,
    secret:      secret,
    wsURL:       wechatDefaultWSUrl,
    defaultChat: defaultChat,
    pending:     make(map[string]chan wechatEnvelope),
  }
  w.conns[username] = uc

  if w.rootCtx != nil {
    w.startConn(w.rootCtx, uc)
  }
}

// RemoveUser 移除用户的微信连接
func (w *WeChatProvider) RemoveUser(username string) {
  slog.Info("微信渠道移除用户连接", "username", username)

  w.mu.Lock()
  defer w.mu.Unlock()

  if uc, ok := w.conns[username]; ok {
    w.stopConn(uc)
    delete(w.conns, username)
  }
}

func (w *WeChatProvider) startConn(ctx context.Context, uc *wechatUserConn) {
  connCtx, cancel := context.WithCancel(ctx)
  uc.ctx = connCtx
  uc.cancel = cancel

  go w.connectLoop(uc)
}

func (w *WeChatProvider) stopConn(uc *wechatUserConn) {
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

func (w *WeChatProvider) connectLoop(uc *wechatUserConn) {
  backoff := time.Second
  for {
    select {
    case <-uc.ctx.Done():
      return
    default:
    }

    if err := w.runConnection(uc); err != nil {
      slog.Warn("微信 WebSocket 连接断开",
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

func (w *WeChatProvider) runConnection(uc *wechatUserConn) error {
  dialCtx, cancel := context.WithTimeout(uc.ctx, wechatConnectTimeout)
  defer cancel()

  conn, _, err := websocket.DefaultDialer.DialContext(dialCtx, uc.wsURL, nil)
  if err != nil {
    return fmt.Errorf("微信 WebSocket 连接失败: %w", err)
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

  readDone := make(chan error, 1)
  go func() {
    readDone <- w.readLoop(uc, conn)
  }()

  // 先启动读循环，再订阅（订阅响应会被 readLoop 读取并投递）
  if err := w.writeAndWait(uc, conn, wechatCommand{
    Cmd:     "subscribe",
    Headers: wechatHeaders{ReqID: randomID(10)},
    Body: map[string]string{
      "bot_id": uc.botID,
      "secret": uc.secret,
    },
  }, wechatCommandTimeout); err != nil {
    return err
  }

  slog.Info("微信 WebSocket 已连接并订阅", "username", uc.username)

  // 心跳
  heartbeatDone := make(chan struct{})
  go func() {
    defer close(heartbeatDone)
    ticker := time.NewTicker(wechatHeartbeatInterval)
    defer ticker.Stop()
    for {
      select {
      case <-ticker.C:
        if err := w.writeAndWait(uc, conn, wechatCommand{
          Cmd:     "ping",
          Headers: wechatHeaders{ReqID: randomID(10)},
        }, wechatCommandTimeout); err != nil {
          return
        }
      case <-uc.ctx.Done():
        return
      }
    }
  }()

  select {
  case err := <-readDone:
    return err
  case <-uc.ctx.Done():
    return uc.ctx.Err()
  case <-heartbeatDone:
    return fmt.Errorf("心跳异常")
  }
}

func (w *WeChatProvider) readLoop(uc *wechatUserConn, conn *websocket.Conn) error {
  for {
    _, raw, err := conn.ReadMessage()
    if err != nil {
      select {
      case <-uc.ctx.Done():
        return nil
      default:
        return fmt.Errorf("微信读取错误: %w", err)
      }
    }

    var env wechatEnvelope
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

func (w *WeChatProvider) handleIncoming(uc *wechatUserConn, env wechatEnvelope) {
  var msg wechatIncomingMessage
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
      Platform: "wechat",
      UserID:   uc.username, // 用 PicoAide 用户名，而非微信 ID
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

func (w *WeChatProvider) Send(ctx context.Context, msg SendMsg) error {
  // 查找 chatID 对应的用户名
  userRaw, ok := w.chatIDToUser.Load(msg.ChatID)
  if !ok {
    return fmt.Errorf("微信发送失败: 未找到 chatID 对应的用户连接")
  }
  username, _ := userRaw.(string)

  w.mu.Lock()
  uc, ok := w.conns[username]
  w.mu.Unlock()
  if !ok {
    return fmt.Errorf("微信发送失败: 未找到用户连接 %s", username)
  }

  return w.sendCommand(uc, wechatCommand{
    Cmd:     "send_msg",
    Headers: wechatHeaders{ReqID: randomID(10)},
    Body: map[string]interface{}{
      "chat_id":  msg.ChatID,
      "msg_type": "markdown",
      "markdown": map[string]string{
        "content": msg.Text,
      },
    },
  })
}

func (w *WeChatProvider) SendToUser(ctx context.Context, username string, text string) error {
  w.mu.Lock()
  uc, ok := w.conns[username]
  w.mu.Unlock()
  if !ok {
    return fmt.Errorf("微信发送失败: 未找到用户连接 %s", username)
  }
  if uc.defaultChat == "" {
    return fmt.Errorf("用户 %s 没有可用的微信会话", username)
  }

  return w.sendCommand(uc, wechatCommand{
    Cmd:     "send_msg",
    Headers: wechatHeaders{ReqID: randomID(10)},
    Body: map[string]interface{}{
      "chat_id":  uc.defaultChat,
      "msg_type": "markdown",
      "markdown": map[string]string{
        "content": text,
      },
    },
  })
}

func (w *WeChatProvider) sendCommand(uc *wechatUserConn, cmd wechatCommand) error {
  uc.connMu.Lock()
  conn := uc.conn
  uc.connMu.Unlock()
  if conn == nil {
    return fmt.Errorf("微信未连接")
  }
  return w.writeAndWait(uc, conn, cmd, wechatCommandTimeout)
}

func (w *WeChatProvider) writeAndWait(uc *wechatUserConn, conn *websocket.Conn, cmd wechatCommand, timeout time.Duration) error {
  if cmd.Headers.ReqID == "" {
    cmd.Headers.ReqID = randomID(10)
  }
  waitCh := make(chan wechatEnvelope, 1)
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
    return fmt.Errorf("微信命令序列化失败: %w", err)
  }
  uc.connMu.Lock()
  err = conn.WriteMessage(websocket.TextMessage, data)
  uc.connMu.Unlock()
  if err != nil {
    return fmt.Errorf("微信写入失败: %w", err)
  }

  timer := time.NewTimer(timeout)
  defer timer.Stop()
  select {
  case <-waitCh:
    return nil
  case <-timer.C:
    return fmt.Errorf("微信命令超时")
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
