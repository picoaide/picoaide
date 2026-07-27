package im

import (
  "context"
  "crypto/rand"
  "encoding/json"
  "fmt"
  "log/slog"
  "strings"
  "sync"
  "time"

  "github.com/gorilla/websocket"
)

// ============================================================
// 企业微信智能机器人（长连接 API 模式，aibot 协议）
// ============================================================

const (
  wecomConnectTimeout    = 15 * time.Second
  wecomCommandTimeout    = 10 * time.Second
  wecomHeartbeatInterval = 30 * time.Second
  wecomReadTimeout       = 60 * time.Second
  wecomWSUrl             = "wss://openws.work.weixin.qq.com"
)

type wecomConn struct {
  username      string
  botID         string
  secret        string
  conn          *websocket.Conn
  connMu        sync.Mutex
  ctx           context.Context
  cancel        context.CancelFunc
  defaultChat   string
  defaultChatMu sync.Mutex
  pendingMu     sync.Mutex
  pending       map[string]chan wecomEnvelope
}

type WeComProvider struct {
  mu          sync.Mutex
  conns       map[string]*wecomConn
  chatIDToUser sync.Map // chatID -> username, O(1) Send lookup
  rootCtx     context.Context
  rootCancel  context.CancelFunc
  onMessage   func(ctx context.Context, msg Message)
}

type wecomEnvelope struct {
  Cmd     string          `json:"cmd"`
  Headers wecomHeaders    `json:"headers"`
  Body    json.RawMessage `json:"body,omitempty"`
  ErrCode int             `json:"errcode,omitempty"`
  ErrMsg  string          `json:"errmsg,omitempty"`
}

type wecomHeaders struct {
  ReqID string `json:"req_id,omitempty"`
}

type wecomCommand struct {
  Cmd     string      `json:"cmd"`
  Headers wecomHeaders `json:"headers"`
  Body    interface{}  `json:"body,omitempty"`
}

type wecomMsgCallback struct {
  MsgID   string       `json:"msgid"`
  AIBotID string       `json:"aibotid"`
  ChatID  string       `json:"chatid,omitempty"`
  ChatType string      `json:"chattype"`
  From    wecomFrom    `json:"from"`
  MsgType string       `json:"msgtype"`
  Text    *wecomText   `json:"text,omitempty"`
  Mixed   *wecomMixed  `json:"mixed,omitempty"`
  Image   *wecomMedia  `json:"image,omitempty"`
  File    *wecomMedia  `json:"file,omitempty"`
  Voice   *wecomMedia  `json:"voice,omitempty"`
  Video   *wecomMedia  `json:"video,omitempty"`
}

type wecomFrom struct {
  UserID string `json:"userid"`
}

type wecomText struct {
  Content string `json:"content"`
}

type wecomMixed struct {
  Content string `json:"content"`
}

type wecomMedia struct {
  URL    string `json:"url,omitempty"`
  AESKey string `json:"aeskey,omitempty"`
}

type wecomEventCallback struct {
  MsgID      string     `json:"msgid"`
  CreateTime int64      `json:"create_time"`
  AIBotID    string     `json:"aibotid"`
  ChatID     string     `json:"chatid,omitempty"`
  ChatType   string     `json:"chattype,omitempty"`
  From       *wecomFrom  `json:"from,omitempty"`
  MsgType    string     `json:"msgtype"`
  Event      wecomEvent `json:"event"`
}

type wecomEvent struct {
  EventType string `json:"eventtype"`
}

func NewWeComProvider() *WeComProvider {
  return &WeComProvider{
    conns: make(map[string]*wecomConn),
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

func (w *WeComProvider) AddUser(username, botID, secret string, defaultChat string) {
  slog.Info("企微智能机器人添加用户连接", "username", username, "bot_id", botID)

  w.mu.Lock()
  defer w.mu.Unlock()

  if old, ok := w.conns[username]; ok {
    w.stopConn(old)
  }

  uc := &wecomConn{
    username:    username,
    botID:       botID,
    secret:      secret,
    defaultChat: defaultChat,
    pending:     make(map[string]chan wecomEnvelope),
  }
  w.conns[username] = uc

  if w.rootCtx != nil {
    w.startConn(w.rootCtx, uc)
  }
}

func (w *WeComProvider) RemoveUser(username string) {
  slog.Info("企微智能机器人移除用户连接", "username", username)

  w.mu.Lock()
  defer w.mu.Unlock()

  if uc, ok := w.conns[username]; ok {
    w.stopConn(uc)
    delete(w.conns, username)
  }
}

func (w *WeComProvider) startConn(ctx context.Context, uc *wecomConn) {
  connCtx, cancel := context.WithCancel(ctx)
  uc.ctx = connCtx
  uc.cancel = cancel

  go w.connectLoop(uc)
}

func (w *WeComProvider) stopConn(uc *wecomConn) {
  if uc.cancel != nil {
    uc.cancel()
  }
  w.closeConn(uc)
}

func (w *WeComProvider) connectLoop(uc *wecomConn) {
  backoff := time.Second
  for {
    select {
    case <-uc.ctx.Done():
      return
    default:
    }

    if err := w.runConnection(uc); err != nil {
      slog.Warn("企微智能机器人连接断开", "username", uc.username, "error", err, "backoff", backoff)
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

func (w *WeComProvider) runConnection(uc *wecomConn) error {
  dialCtx, cancel := context.WithTimeout(uc.ctx, wecomConnectTimeout)
  defer cancel()

  conn, _, err := websocket.DefaultDialer.DialContext(dialCtx, wecomWSUrl, nil)
  if err != nil {
    return fmt.Errorf("企微 WebSocket 连接失败: %w", err)
  }

  uc.connMu.Lock()
  uc.conn = conn
  uc.connMu.Unlock()
  defer func() {
    w.closeConn(uc)
    conn.Close()
  }()

  readDone := make(chan error, 1)
  go func() {
    readDone <- w.readLoop(uc, conn)
  }()

  if err := w.writeAndWait(uc, conn, wecomCommand{
    Cmd:     "aibot_subscribe",
    Headers: wecomHeaders{ReqID: newReqID()},
    Body: map[string]string{
      "bot_id": uc.botID,
      "secret": uc.secret,
    },
  }, wecomCommandTimeout); err != nil {
    return err
  }

  slog.Info("企微智能机器人已连接并订阅", "username", uc.username)

  heartbeatDone := make(chan struct{})
  go func() {
    defer close(heartbeatDone)
    ticker := time.NewTicker(wecomHeartbeatInterval)
    defer ticker.Stop()
    for {
      select {
      case <-uc.ctx.Done():
        return
      case <-ticker.C:
        if err := w.writeAndWait(uc, conn, wecomCommand{
          Cmd:     "ping",
          Headers: wecomHeaders{ReqID: newReqID()},
        }, wecomCommandTimeout); err != nil {
          return
        }
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

func (w *WeComProvider) readLoop(uc *wecomConn, conn *websocket.Conn) error {
  for {
    select {
    case <-uc.ctx.Done():
      return uc.ctx.Err()
    default:
    }

    if err := conn.SetReadDeadline(time.Now().Add(wecomReadTimeout)); err != nil {
      return fmt.Errorf("设置读取截止时间失败: %w", err)
    }

    var env wecomEnvelope
    if err := conn.ReadJSON(&env); err != nil {
      // 区分超时和真正的错误
      select {
      case <-uc.ctx.Done():
        return uc.ctx.Err()
      default:
        if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
          continue
        }
        return fmt.Errorf("微信读取错误: %w", err)
      }
    }

    if reqID := env.Headers.ReqID; reqID != "" {
      uc.pendingMu.Lock()
      if ch, ok := uc.pending[reqID]; ok {
        ch <- env
        uc.pendingMu.Unlock()
        continue
      }
      uc.pendingMu.Unlock()
    }

    switch env.Cmd {
    case "aibot_msg_callback":
      w.handleMsgCallback(uc, env)
    case "aibot_event_callback":
      w.handleEventCallback(uc, env)
    }
  }
}

func (w *WeComProvider) handleMsgCallback(uc *wecomConn, env wecomEnvelope) {
  var cb wecomMsgCallback
  if err := json.Unmarshal(env.Body, &cb); err != nil {
    return
  }

  content := ""
  if cb.Text != nil {
    content = strings.TrimSpace(cb.Text.Content)
  }
  if content == "" && cb.Mixed != nil {
    content = strings.TrimSpace(cb.Mixed.Content)
  }
  if content == "" {
    return
  }

  chatID := cb.From.UserID
  if cb.ChatID != "" {
    chatID = cb.ChatID
  }

  uc.defaultChatMu.Lock()
  uc.defaultChat = cb.From.UserID
  uc.defaultChatMu.Unlock()

  // 注册 chatID → username 映射，供 Send O(1) 查找
  w.chatIDToUser.Store(cb.From.UserID, uc.username)
  if cb.ChatID != "" {
    w.chatIDToUser.Store(cb.ChatID, uc.username)
  }

  if w.onMessage != nil {
    w.onMessage(context.Background(), Message{
      Platform: "wecom",
      UserID:   uc.username,
      ChatID:   chatID,
      Text:     content,
      Raw: map[string]string{
        "aibotid":  cb.AIBotID,
        "chattype": cb.ChatType,
        "msgid":    cb.MsgID,
        "req_id":   env.Headers.ReqID,
      },
    })
  }
}

func (w *WeComProvider) handleEventCallback(uc *wecomConn, env wecomEnvelope) {
  var cb wecomEventCallback
  if err := json.Unmarshal(env.Body, &cb); err != nil {
    return
  }

  switch cb.Event.EventType {
  case "enter_chat":
    if cb.From != nil {
      uc.defaultChatMu.Lock()
      uc.defaultChat = cb.From.UserID
      uc.defaultChatMu.Unlock()
      w.chatIDToUser.Store(cb.From.UserID, uc.username)
      if cb.ChatID != "" {
        w.chatIDToUser.Store(cb.ChatID, uc.username)
      }
      slog.Info("企微用户进入会话", "username", uc.username, "userid", cb.From.UserID)
    }
  case "disconnected_event":
    slog.Warn("企微连接被踢出，准备重连", "username", uc.username)
    w.closeConn(uc)
  }
}

func (w *WeComProvider) Send(ctx context.Context, msg SendMsg) error {
  userRaw, ok := w.chatIDToUser.Load(msg.ChatID)
  if !ok {
    return fmt.Errorf("未找到企微会话 %s", msg.ChatID)
  }
  username, _ := userRaw.(string)

  w.mu.Lock()
  uc, ok := w.conns[username]
  w.mu.Unlock()
  if !ok {
    return fmt.Errorf("未找到用户连接 %s", username)
  }

  reqID := msg.ReqID
  if reqID == "" {
    reqID = newReqID()
  }

  return w.sendCommand(uc, wecomCommand{
    Cmd:     "aibot_respond_msg",
    Headers: wecomHeaders{ReqID: reqID},
    Body: map[string]interface{}{
      "msgtype": "markdown",
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
    return fmt.Errorf("用户 %s 未连接企微智能机器人", username)
  }

  uc.defaultChatMu.Lock()
  dc := uc.defaultChat
  uc.defaultChatMu.Unlock()

  if dc == "" {
    return fmt.Errorf("用户 %s 没有可用的企微会话", username)
  }

  return w.sendCommand(uc, wecomCommand{
    Cmd:     "aibot_send_msg",
    Headers: wecomHeaders{ReqID: newReqID()},
    Body: map[string]interface{}{
      "chatid":    dc,
      "chat_type": 1,
      "msgtype":   "markdown",
      "markdown": map[string]string{
        "content": text,
      },
    },
  })
}

func (w *WeComProvider) sendCommand(uc *wecomConn, cmd wecomCommand) error {
  uc.connMu.Lock()
  conn := uc.conn
  uc.connMu.Unlock()

  if conn == nil {
    return fmt.Errorf("企微连接未就绪")
  }

  return w.writeAndWait(uc, conn, cmd, wecomCommandTimeout)
}

func (w *WeComProvider) writeAndWait(uc *wecomConn, conn *websocket.Conn, cmd wecomCommand, timeout time.Duration) error {
  waitCh := make(chan wecomEnvelope, 1)
  reqID := cmd.Headers.ReqID

  uc.pendingMu.Lock()
  uc.pending[reqID] = waitCh
  uc.pendingMu.Unlock()

  defer func() {
    uc.pendingMu.Lock()
    delete(uc.pending, reqID)
    uc.pendingMu.Unlock()
  }()

  uc.connMu.Lock()
  err := conn.WriteJSON(cmd)
  uc.connMu.Unlock()
  if err != nil {
    return fmt.Errorf("企微发送命令失败: %w", err)
  }

  select {
  case res := <-waitCh:
    if res.ErrCode != 0 {
      return fmt.Errorf("企微命令错误: errcode=%d errmsg=%s", res.ErrCode, res.ErrMsg)
    }
    return nil
  case <-time.After(timeout):
    return fmt.Errorf("企微命令超时")
  case <-uc.ctx.Done():
    return uc.ctx.Err()
  }
}

func (w *WeComProvider) closeConn(uc *wecomConn) {
  uc.connMu.Lock()
  defer uc.connMu.Unlock()
  if uc.conn != nil {
    uc.conn.Close()
    uc.conn = nil
  }
}

func newReqID() string {
  b := make([]byte, 16)
  rand.Read(b)
  return fmt.Sprintf("%x", b)
}
