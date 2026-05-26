package im

import (
  "context"
  "fmt"
  "log/slog"
  "strings"
  "sync"

  dingo "github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"
  "github.com/open-dingtalk/dingtalk-stream-sdk-go/client"
)

// ============================================================
// DingTalk 实现（Stream Mode WebSocket）— 支持每用户独立连接
// ============================================================

type userConn struct {
  username        string
  clientID        string
  clientSecret    string
  streamClient    *client.StreamClient
  cancel          context.CancelFunc
  defaultChat     string
  sessionWebhooks sync.Map // chatID -> sessionWebhook
}

type DingTalkProvider struct {
  mu        sync.Mutex
  conns     map[string]*userConn // username -> connection
  rootCtx   context.Context
  rootCancel context.CancelFunc
  onMessage func(ctx context.Context, msg Message)
}

func NewDingTalkProvider() *DingTalkProvider {
  return &DingTalkProvider{
    conns: make(map[string]*userConn),
  }
}

func (d *DingTalkProvider) Name() string { return "dingtalk" }

func (d *DingTalkProvider) SetOnMessage(handler func(ctx context.Context, msg Message)) {
  d.onMessage = handler
}

func (d *DingTalkProvider) Start(ctx context.Context) error {
  d.rootCtx, d.rootCancel = context.WithCancel(ctx)

  d.mu.Lock()
  for _, uc := range d.conns {
    d.startConn(d.rootCtx, uc)
  }
  d.mu.Unlock()
  return nil
}

func (d *DingTalkProvider) Stop(ctx context.Context) error {
  d.mu.Lock()
  for _, uc := range d.conns {
    d.stopConn(uc)
  }
  d.mu.Unlock()

  if d.rootCancel != nil {
    d.rootCancel()
  }
  return nil
}

// AddUser 为指定用户添加钉钉连接。如果用户已有连接则先关闭旧连接。
func (d *DingTalkProvider) AddUser(username, clientID, clientSecret string, defaultChat string) {
  slog.Info("钉钉渠道添加用户连接", "username", username, "client_id", clientID)

  d.mu.Lock()
  defer d.mu.Unlock()

  // 关闭旧连接
  if old, ok := d.conns[username]; ok {
    d.stopConn(old)
  }

  uc := &userConn{
    username:     username,
    clientID:     clientID,
    clientSecret: clientSecret,
    defaultChat:  defaultChat,
  }
  d.conns[username] = uc

  if d.rootCtx != nil {
    d.startConn(d.rootCtx, uc)
  }
}

// RemoveUser 移除用户的钉钉连接
func (d *DingTalkProvider) RemoveUser(username string) {
  slog.Info("钉钉渠道移除用户连接", "username", username)

  d.mu.Lock()
  defer d.mu.Unlock()

  if uc, ok := d.conns[username]; ok {
    d.stopConn(uc)
    delete(d.conns, username)
  }
}

func (d *DingTalkProvider) startConn(ctx context.Context, uc *userConn) {
  connCtx, cancel := context.WithCancel(ctx)
  uc.cancel = cancel

  cred := client.NewAppCredentialConfig(uc.clientID, uc.clientSecret)
  uc.streamClient = client.NewStreamClient(
    client.WithAppCredential(cred),
    client.WithAutoReconnect(true),
  )

  // 捕获 username 闭包，消息回调中携带正确的用户名
  uc.streamClient.RegisterChatBotCallbackRouter(
    func(callbackCtx context.Context, data *dingo.BotCallbackDataModel) ([]byte, error) {
      return d.onUserMessage(uc, callbackCtx, data)
    },
  )

  go func() {
    if err := uc.streamClient.Start(connCtx); err != nil {
      slog.Warn("钉钉用户连接启动失败",
        "username", uc.username,
        "client_id", uc.clientID,
        "error", err,
      )
    }
  }()
}

func (d *DingTalkProvider) stopConn(uc *userConn) {
  if uc.cancel != nil {
    uc.cancel()
  }
  if uc.streamClient != nil {
    uc.streamClient.Close()
  }
}

func (d *DingTalkProvider) onUserMessage(uc *userConn, ctx context.Context, data *dingo.BotCallbackDataModel) ([]byte, error) {
  if data == nil {
    return nil, nil
  }

  content := strings.TrimSpace(data.Text.Content)
  if content == "" {
    if contentMap, ok := data.Content.(map[string]any); ok {
      if textContent, ok := contentMap["content"].(string); ok {
        content = strings.TrimSpace(textContent)
      }
    }
  }
  if content == "" {
    return nil, nil
  }

  senderID := strings.TrimSpace(data.SenderStaffId)
  if senderID == "" {
    senderID = strings.TrimSpace(data.SenderId)
  }
  senderNick := strings.TrimSpace(data.SenderNick)

  chatID := strings.TrimSpace(data.ConversationId)
  if chatID == "" && data.ConversationType == "1" {
    chatID = senderID
  }
  if chatID == "" {
    return nil, nil
  }

  // 保存 session webhook 到用户自己的连接中
  uc.sessionWebhooks.Store(chatID, data.SessionWebhook)
  uc.defaultChat = chatID

  // 群聊中去除 @提及
  if data.ConversationType != "1" {
    content = stripLeadingAtMentions(content)
  }

  if d.onMessage != nil {
    d.onMessage(ctx, Message{
      Platform: "dingtalk",
      UserID:   uc.username, // 用 PicoAide 用户名，而非钉钉 ID
      ChatID:   chatID,
      Username: senderNick,
      Text:     content,
      Raw: map[string]string{
        "sender_name":       senderNick,
        "conversation_id":   data.ConversationId,
        "conversation_type": data.ConversationType,
        "username":          uc.username,
      },
    })
  }

  return nil, nil
}

func (d *DingTalkProvider) Send(ctx context.Context, msg SendMsg) error {
  // 在所有用户的连接中查找 session webhook
  d.mu.Lock()
  for _, uc := range d.conns {
    if webhookRaw, ok := uc.sessionWebhooks.Load(msg.ChatID); ok {
      d.mu.Unlock()
      webhook, _ := webhookRaw.(string)
      if webhook == "" {
        break
      }
      replier := dingo.NewChatbotReplier()
      err := replier.SimpleReplyMarkdown(
        ctx,
        webhook,
        []byte("PicoAgent"),
        []byte(msg.Text),
      )
      if err != nil {
        return fmt.Errorf("钉钉发送失败: %w", err)
      }
      return nil
    }
  }
  d.mu.Unlock()

  return fmt.Errorf("未找到会话 webhook，无法回复")
}

func (d *DingTalkProvider) SendToUser(ctx context.Context, username string, text string) error {
  d.mu.Lock()
  uc, ok := d.conns[username]
  d.mu.Unlock()
  if !ok {
    return fmt.Errorf("用户 %s 未连接钉钉", username)
  }

  // 优先使用最近会话，否则尝试任意可用会话
  chatID := uc.defaultChat
  if chatID == "" {
    uc.sessionWebhooks.Range(func(key, value interface{}) bool {
      chatID, _ = key.(string)
      return false
    })
  }
  if chatID == "" {
    return fmt.Errorf("用户 %s 没有可用的钉钉会话", username)
  }

  webhookRaw, ok := uc.sessionWebhooks.Load(chatID)
  if !ok {
    return fmt.Errorf("用户 %s 的钉钉会话已失效", username)
  }
  webhook, _ := webhookRaw.(string)
  if webhook == "" {
    return fmt.Errorf("用户 %s 的钉钉 webhook 为空", username)
  }
  replier := dingo.NewChatbotReplier()
  return replier.SimpleReplyMarkdown(ctx, webhook, []byte("PicoAgent"), []byte(text))
}

func stripLeadingAtMentions(content string) string {
  fields := strings.Fields(content)
  if len(fields) == 0 {
    return ""
  }
  i := 0
  for i < len(fields) && strings.HasPrefix(fields[i], "@") {
    i++
  }
  if i == 0 {
    return strings.TrimSpace(content)
  }
  return strings.Join(fields[i:], " ")
}
