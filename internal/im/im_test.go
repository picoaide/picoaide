package im

import (
  "context"
  "errors"
  "testing"
)

// ============================================================
// mockProvider — 模拟 Provider 实现，不产生真实网络请求
// ============================================================

var (
  errMockStart = errors.New("mock start error")
  errMockSend  = errors.New("mock send error")
)

type mockProvider struct {
  name      string
  startErr  error
  stopErr   error
  sendErr   error

  started    bool
  stopped    bool
  sentMsg    *SendMsg
  sentUser   string
  sentUserText string
  onMessage  func(ctx context.Context, msg Message)
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) Start(_ context.Context) error {
  m.started = true
  return m.startErr
}

func (m *mockProvider) Stop(_ context.Context) error {
  m.stopped = true
  return m.stopErr
}

func (m *mockProvider) Send(_ context.Context, msg SendMsg) error {
  m.sentMsg = &msg
  return m.sendErr
}

func (m *mockProvider) SendToUser(_ context.Context, username string, text string) error {
  m.sentUser = username
  m.sentUserText = text
  return m.sendErr
}

func (m *mockProvider) SetOnMessage(handler func(ctx context.Context, msg Message)) {
  m.onMessage = handler
}

// ============================================================
// NewGateway
// ============================================================

func TestNewGateway(t *testing.T) {
  g := NewGateway()
  if g == nil {
    t.Fatal("NewGateway() returned nil")
  }
  if g.providers == nil {
    t.Fatal("providers map is nil")
  }
  if len(g.providers) != 0 {
    t.Fatalf("expected 0 providers, got %d", len(g.providers))
  }
}

// ============================================================
// Register
// ============================================================

func TestGatewayRegister_addsProviderToMap(t *testing.T) {
  g := NewGateway()
  p := &mockProvider{name: "mock"}
  g.Register(p)

  if len(g.providers) != 1 {
    t.Fatalf("expected 1 provider, got %d", len(g.providers))
  }
  got, ok := g.providers["mock"]
  if !ok {
    t.Fatal("provider not found in map by name")
  }
  if got != p {
    t.Fatal("stored provider is not the same instance")
  }
}

func TestGatewayRegister_setsOnMessageOnProvider(t *testing.T) {
  g := NewGateway()
  p := &mockProvider{name: "mock"}
  g.Register(p)

  if p.onMessage == nil {
    t.Fatal("SetOnMessage was not called; onMessage handler is nil")
  }
}

func TestGatewayRegister_onMessageChainsToGateway(t *testing.T) {
  g := NewGateway()
  p := &mockProvider{name: "mock"}

  var received []Message
  g.SetOnMessage(func(_ context.Context, msg Message) {
    received = append(received, msg)
  })
  g.Register(p)

  // Simulate an incoming message via the provider
  ctx := context.Background()
  msg := Message{Platform: "mock", UserID: "u1", Text: "hello"}
  p.onMessage(ctx, msg)

  if len(received) != 1 {
    t.Fatalf("expected 1 message, got %d", len(received))
  }
  if received[0].Platform != "mock" || received[0].UserID != "u1" || received[0].Text != "hello" {
    t.Fatalf("unexpected message: %+v", received[0])
  }
}

func TestGatewayRegister_overwritesExisting(t *testing.T) {
  g := NewGateway()
  p1 := &mockProvider{name: "mock"}
  p2 := &mockProvider{name: "mock"}
  g.Register(p1)
  g.Register(p2)

  if len(g.providers) != 1 {
    t.Fatalf("expected 1 provider after overwrite, got %d", len(g.providers))
  }
  if g.providers["mock"] != p2 {
    t.Fatal("provider was not overwritten")
  }
}

func TestGatewayRegister_onMessageChainsToGateway_onlyWhenSet(t *testing.T) {
  // onMessage should not panic when gateway-level handler is nil
  g := NewGateway()
  p := &mockProvider{name: "mock"}
  g.Register(p)

  ctx := context.Background()
  msg := Message{Platform: "mock", Text: "no panic"}
  p.onMessage(ctx, msg) // should not panic
}

// ============================================================
// SetOnMessage
// ============================================================

func TestGatewaySetOnMessage(t *testing.T) {
  g := NewGateway()
  if g.onMessage != nil {
    t.Fatal("onMessage should be nil initially")
  }

  handler := func(_ context.Context, _ Message) {}
  g.SetOnMessage(handler)
  if g.onMessage == nil {
    t.Fatal("onMessage should not be nil after SetOnMessage")
  }
}

// ============================================================
// Send
// ============================================================

func TestGatewaySend_routesToCorrectProvider(t *testing.T) {
  g := NewGateway()
  dt := &mockProvider{name: "dingtalk"}
  fs := &mockProvider{name: "feishu"}
  g.Register(dt)
  g.Register(fs)

  ctx := context.Background()
  err := g.Send(ctx, "dingtalk", "chat123", "hello")

  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if dt.sentMsg == nil {
    t.Fatal("dingtalk provider did not receive message")
  }
  if dt.sentMsg.ChatID != "chat123" || dt.sentMsg.Text != "hello" {
    t.Fatalf("dingtalk got %+v", dt.sentMsg)
  }
  if fs.sentMsg != nil {
    t.Fatal("feishu provider should not have received message")
  }
}

func TestGatewaySend_returnsErrorForUnknownPlatform(t *testing.T) {
  g := NewGateway()
  ctx := context.Background()
  err := g.Send(ctx, "unknown", "chat1", "hi")

  if err == nil {
    t.Fatal("expected error for unknown platform")
  }
}

func TestGatewaySend_passesSendMsgFieldsCorrectly(t *testing.T) {
  tests := []struct {
    name     string
    platform string
    chatID   string
    text     string
  }{
    {name: "dingtalk", platform: "dingtalk", chatID: "cid_a", text: "msg 1"},
    {name: "feishu",   platform: "feishu",   chatID: "cid_b", text: "msg 2"},
    {name: "wecom",    platform: "wecom",    chatID: "cid_c", text: "msg 3"},
  }

  for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
      g := NewGateway()
      p := &mockProvider{name: tt.platform}
      g.Register(p)

      err := g.Send(context.Background(), tt.platform, tt.chatID, tt.text)
      if err != nil {
        t.Fatalf("unexpected error: %v", err)
      }
      if p.sentMsg == nil {
        t.Fatal("provider did not receive message")
      }
      if p.sentMsg.ChatID != tt.chatID {
        t.Errorf("ChatID: got %q, want %q", p.sentMsg.ChatID, tt.chatID)
      }
      if p.sentMsg.Text != tt.text {
        t.Errorf("Text: got %q, want %q", p.sentMsg.Text, tt.text)
      }
    })
  }
}

func TestGatewaySend_propagatesProviderError(t *testing.T) {
  g := NewGateway()
  p := &mockProvider{name: "err", sendErr: errMockSend}
  g.Register(p)

  err := g.Send(context.Background(), "err", "c", "t")
  if err == nil {
    t.Fatal("expected error from provider")
  }
}

// ============================================================
// Start
// ============================================================

func TestGatewayStart_startsAllProviders(t *testing.T) {
  g := NewGateway()
  p1 := &mockProvider{name: "a"}
  p2 := &mockProvider{name: "b"}
  g.Register(p1)
  g.Register(p2)

  err := g.Start(context.Background())
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if !p1.started {
    t.Error("provider a was not started")
  }
  if !p2.started {
    t.Error("provider b was not started")
  }
}

func TestGatewayStart_returnsFirstError(t *testing.T) {
  g := NewGateway()
  p1 := &mockProvider{name: "a", startErr: errMockStart}
  p2 := &mockProvider{name: "b"}
  g.Register(p1)
  g.Register(p2)

  err := g.Start(context.Background())
  if err == nil || !errors.Is(err, errMockStart) {
    t.Fatalf("expected mock start error, got %v", err)
  }
  if p2.started {
    t.Error("provider b should NOT have been started when a previous provider failed (short-circuit)")
  }
}

// ============================================================
// Stop
// ============================================================

func TestGatewayStop_stopsAllProviders(t *testing.T) {
  g := NewGateway()
  p1 := &mockProvider{name: "a"}
  p2 := &mockProvider{name: "b"}
  g.Register(p1)
  g.Register(p2)

  g.Stop(context.Background())
  if !p1.stopped {
    t.Error("provider a was not stopped")
  }
  if !p2.stopped {
    t.Error("provider b was not stopped")
  }
}

func TestGatewayStop_emptyProviders(t *testing.T) {
  g := NewGateway()
  // Should not panic
  g.Stop(context.Background())
}

// ============================================================
// Message & SendMsg 结构
// ============================================================

func TestMessageConstruction(t *testing.T) {
  msg := Message{
    Platform: "dingtalk",
    UserID:   "user123",
    ChatID:   "chat456",
    Username: "张三",
    Text:     "你好",
    Raw: map[string]string{
      "msg_id":   "msg789",
      "msg_type": "text",
    },
  }

  if msg.Platform != "dingtalk" {
    t.Errorf("Platform: got %q", msg.Platform)
  }
  if msg.UserID != "user123" {
    t.Errorf("UserID: got %q", msg.UserID)
  }
  if msg.ChatID != "chat456" {
    t.Errorf("ChatID: got %q", msg.ChatID)
  }
  if msg.Username != "张三" {
    t.Errorf("Username: got %q", msg.Username)
  }
  if msg.Text != "你好" {
    t.Errorf("Text: got %q", msg.Text)
  }
  if msg.Raw["msg_id"] != "msg789" {
    t.Errorf("Raw[msg_id]: got %q", msg.Raw["msg_id"])
  }
  if msg.Raw["msg_type"] != "text" {
    t.Errorf("Raw[msg_type]: got %q", msg.Raw["msg_type"])
  }
}

func TestSendMsgConstruction(t *testing.T) {
  msg := SendMsg{
    ChatID: "group123",
    Text:   "通知内容",
  }
  if msg.ChatID != "group123" {
    t.Errorf("ChatID: got %q", msg.ChatID)
  }
  if msg.Text != "通知内容" {
    t.Errorf("Text: got %q", msg.Text)
  }
}

// ============================================================
// Provider 接口编译期检查
// ============================================================

func TestProviderInterface_compileCheck(t *testing.T) {
  var _ Provider = (*DingTalkProvider)(nil)
  var _ Provider = (*FeishuProvider)(nil)
  var _ Provider = (*WeComProvider)(nil)
}

// ============================================================
// 完整网关生命周期
// ============================================================

func TestGateway_lifecycle(t *testing.T) {
  g := NewGateway()
  p := &mockProvider{name: "mock"}
  g.Register(p)

  ctx := context.Background()

  if err := g.Start(ctx); err != nil {
    t.Fatalf("Start: %v", err)
  }
  if !p.started {
    t.Fatal("provider not started")
  }

  if err := g.Send(ctx, "mock", "chat", "hello"); err != nil {
    t.Fatalf("Send: %v", err)
  }
  if p.sentMsg == nil || p.sentMsg.Text != "hello" {
    t.Fatal("send not routed")
  }

  g.Stop(ctx)
  if !p.stopped {
    t.Fatal("provider not stopped")
  }
}

// ============================================================
// onMessage 回调传播 — 多个 provider 并发触发
// ============================================================

func TestOnMessage_propagatesFromMultipleProviders(t *testing.T) {
  g := NewGateway()
  p1 := &mockProvider{name: "p1"}
  p2 := &mockProvider{name: "p2"}
  g.Register(p1)
  g.Register(p2)

  var messages []Message
  g.SetOnMessage(func(_ context.Context, msg Message) {
    messages = append(messages, msg)
  })

  // Re-register to wire the new onMessage into providers
  g.Register(p1)
  g.Register(p2)

  ctx := context.Background()
  p1.onMessage(ctx, Message{Platform: "p1", Text: "a"})
  p2.onMessage(ctx, Message{Platform: "p2", Text: "b"})

  if len(messages) != 2 {
    t.Fatalf("expected 2 messages, got %d", len(messages))
  }
}

// ============================================================
// Send — 空 platform
// ============================================================

func TestGatewaySend_emptyPlatform(t *testing.T) {
  g := NewGateway()
  p := &mockProvider{name: ""}
  g.Register(p)

  err := g.Send(context.Background(), "", "c", "t")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if p.sentMsg == nil {
    t.Fatal("provider should receive message when platform is empty string")
  }
}
