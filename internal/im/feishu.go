package im

import (
  "context"
  "encoding/json"
  "fmt"
  "log/slog"
  "strings"
  "sync"

  lark "github.com/larksuite/oapi-sdk-go/v3"
  larkdispatcher "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
  larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
  larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

// ============================================================
// 飞书实现（WebSocket 模式）— 支持每用户独立连接
// ============================================================

type feishuUserConn struct {
  username        string
  appID           string
  appSecret       string
  client          *lark.Client
  wsClient        *larkws.Client
  cancel          context.CancelFunc
  defaultChat     string
  sessionWebhooks sync.Map // chatID -> marker
}

type FeishuProvider struct {
  mu         sync.Mutex
  conns      map[string]*feishuUserConn // username -> connection
  rootCtx    context.Context
  rootCancel context.CancelFunc
  onMessage  func(ctx context.Context, msg Message)
}

func NewFeishuProvider() *FeishuProvider {
  return &FeishuProvider{
    conns: make(map[string]*feishuUserConn),
  }
}

func (f *FeishuProvider) Name() string { return "feishu" }

func (f *FeishuProvider) SetOnMessage(handler func(ctx context.Context, msg Message)) {
  f.onMessage = handler
}

func (f *FeishuProvider) Start(ctx context.Context) error {
  f.rootCtx, f.rootCancel = context.WithCancel(ctx)

  f.mu.Lock()
  for _, uc := range f.conns {
    f.startConn(f.rootCtx, uc)
  }
  f.mu.Unlock()
  return nil
}

func (f *FeishuProvider) Stop(ctx context.Context) error {
  f.mu.Lock()
  for _, uc := range f.conns {
    f.stopConn(uc)
  }
  f.mu.Unlock()

  if f.rootCancel != nil {
    f.rootCancel()
  }
  return nil
}

// AddUser 为指定用户添加飞书连接。如果用户已有连接则先关闭旧连接。
func (f *FeishuProvider) AddUser(username, appID, appSecret string, defaultChat string) {
  slog.Info("飞书渠道添加用户连接", "username", username, "app_id", appID)

  f.mu.Lock()
  defer f.mu.Unlock()

  // 关闭旧连接
  if old, ok := f.conns[username]; ok {
    f.stopConn(old)
  }

  uc := &feishuUserConn{
    username:    username,
    appID:       appID,
    appSecret:   appSecret,
    defaultChat: defaultChat,
  }
  f.conns[username] = uc

  if f.rootCtx != nil {
    f.startConn(f.rootCtx, uc)
  }
}

// RemoveUser 移除用户的飞书连接
func (f *FeishuProvider) RemoveUser(username string) {
  slog.Info("飞书渠道移除用户连接", "username", username)

  f.mu.Lock()
  defer f.mu.Unlock()

  if uc, ok := f.conns[username]; ok {
    f.stopConn(uc)
    delete(f.conns, username)
  }
}

func (f *FeishuProvider) startConn(ctx context.Context, uc *feishuUserConn) {
  connCtx, cancel := context.WithCancel(ctx)
  uc.cancel = cancel

  uc.client = lark.NewClient(uc.appID, uc.appSecret)

  // 捕获 username 闭包，消息回调中携带正确的用户名
  dispatcher := larkdispatcher.NewEventDispatcher("", "").
    OnP2MessageReceiveV1(func(callbackCtx context.Context, event *larkim.P2MessageReceiveV1) error {
      return f.onUserMessage(uc, callbackCtx, event)
    })

  uc.wsClient = larkws.NewClient(
    uc.appID,
    uc.appSecret,
    larkws.WithEventHandler(dispatcher),
  )

  go func() {
    if err := uc.wsClient.Start(connCtx); err != nil {
      slog.Warn("飞书用户连接启动失败",
        "username", uc.username,
        "app_id", uc.appID,
        "error", err,
      )
    }
  }()
}

func (f *FeishuProvider) stopConn(uc *feishuUserConn) {
  if uc.cancel != nil {
    uc.cancel()
  }
}

func (f *FeishuProvider) onUserMessage(uc *feishuUserConn, ctx context.Context, event *larkim.P2MessageReceiveV1) error {
  if event == nil || event.Event == nil || event.Event.Message == nil {
    return nil
  }

  message := event.Event.Message

  chatID := stringValue(message.ChatId)
  if chatID == "" {
    return nil
  }

  // 保存会话信息到用户自己的连接中，Send 时用来查找对应的 client
  uc.sessionWebhooks.Store(chatID, "1")
  uc.defaultChat = chatID

  messageType := stringValue(message.MessageType)
  rawContent := stringValue(message.Content)

  content := extractFeishuContent(messageType, rawContent)
  if content == "" {
    if messageType == larkim.MsgTypeText {
      return nil
    }
    content = "[empty]"
  }

  chatType := stringValue(message.ChatType)
  inboundChatType := "direct"
  isMentioned := false
  if chatType != "p2p" {
    inboundChatType = "group"
    if len(message.Mentions) > 0 {
      isMentioned = true
      content = stripFeishuMentions(content, message.Mentions)
    }
  }

  if f.onMessage != nil {
    f.onMessage(ctx, Message{
      Platform: "feishu",
      UserID:   uc.username, // 用 PicoAide 用户名，而非飞书 ID
      ChatID:   chatID,
      Text:     content,
      Raw: map[string]string{
        "message_id":   stringValue(message.MessageId),
        "message_type": messageType,
        "chat_type":    inboundChatType,
        "mentioned":    fmt.Sprintf("%v", isMentioned),
        "username":     uc.username,
      },
    })
  }

  return nil
}

func (f *FeishuProvider) Send(ctx context.Context, msg SendMsg) error {
  if msg.ChatID == "" {
    return fmt.Errorf("飞书 chat_id 为空")
  }

  // 在所有用户的连接中查找匹配 chatID 的会话
  f.mu.Lock()
  for _, uc := range f.conns {
    if _, ok := uc.sessionWebhooks.Load(msg.ChatID); ok {
      f.mu.Unlock()

      content, _ := json.Marshal(map[string]string{"text": msg.Text})
      req := larkim.NewCreateMessageReqBuilder().
        ReceiveIdType(larkim.ReceiveIdTypeChatId).
        Body(larkim.NewCreateMessageReqBodyBuilder().
          ReceiveId(msg.ChatID).
          MsgType(larkim.MsgTypeText).
          Content(string(content)).
          Build()).
        Build()

      resp, err := uc.client.Im.V1.Message.Create(ctx, req)
      if err != nil {
        return fmt.Errorf("飞书发送失败: %w", err)
      }
      if !resp.Success() {
        return fmt.Errorf("飞书 API 错误 (code=%d msg=%s)", resp.Code, resp.Msg)
      }
      return nil
    }
  }
  f.mu.Unlock()

  return fmt.Errorf("未找到会话客户端，无法回复")
}

func (f *FeishuProvider) SendToUser(ctx context.Context, username string, text string) error {
  f.mu.Lock()
  uc, ok := f.conns[username]
  f.mu.Unlock()
  if !ok {
    return fmt.Errorf("用户 %s 未连接飞书", username)
  }

  chatID := uc.defaultChat
  if chatID == "" {
    uc.sessionWebhooks.Range(func(key, value interface{}) bool {
      chatID, _ = key.(string)
      return false
    })
  }
  if chatID == "" {
    return fmt.Errorf("用户 %s 没有可用的飞书会话", username)
  }

  content, _ := json.Marshal(map[string]string{"text": text})
  req := larkim.NewCreateMessageReqBuilder().
    ReceiveIdType(larkim.ReceiveIdTypeChatId).
    Body(larkim.NewCreateMessageReqBodyBuilder().
      ReceiveId(chatID).
      MsgType(larkim.MsgTypeText).
      Content(string(content)).
      Build()).
    Build()

  resp, err := uc.client.Im.V1.Message.Create(ctx, req)
  if err != nil {
    return fmt.Errorf("飞书发送失败: %w", err)
  }
  if !resp.Success() {
    return fmt.Errorf("飞书 API 错误 (code=%d msg=%s)", resp.Code, resp.Msg)
  }
  return nil
}

func extractFeishuSenderID(sender *larkim.EventSender) string {
  if sender == nil || sender.SenderId == nil {
    return ""
  }
  if sender.SenderId.UserId != nil && *sender.SenderId.UserId != "" {
    return *sender.SenderId.UserId
  }
  if sender.SenderId.OpenId != nil && *sender.SenderId.OpenId != "" {
    return *sender.SenderId.OpenId
  }
  if sender.SenderId.UnionId != nil && *sender.SenderId.UnionId != "" {
    return *sender.SenderId.UnionId
  }
  return ""
}

func extractFeishuContent(messageType, rawContent string) string {
  if rawContent == "" {
    return ""
  }
  if messageType == larkim.MsgTypeText {
    var textPayload struct {
      Text string `json:"text"`
    }
    if err := json.Unmarshal([]byte(rawContent), &textPayload); err == nil {
      return textPayload.Text
    }
    return rawContent
  }
  return rawContent
}

func stripFeishuMentions(content string, mentions []*larkim.MentionEvent) string {
  for _, m := range mentions {
    if m.Key != nil && *m.Key != "" {
      content = strings.ReplaceAll(content, *m.Key, "")
    }
  }
  return strings.TrimSpace(content)
}

func stringValue(s *string) string {
  if s == nil {
    return ""
  }
  return *s
}
