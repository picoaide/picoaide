package agent

import (
  "context"
  "encoding/json"
  "strings"
  "sync"
  "testing"
  "time"
)

// ============================================================
// mockProvider — 用于 Engine 测试
// ============================================================

type mockProvider struct {
  responseText  string
  shouldError   bool
  failAfterText []string
  toolCalls     []ToolCallData
}

func (m *mockProvider) StreamChat(_ context.Context, _ *ChatRequest, cb func(event StreamEvent)) error {
  if m.shouldError {
    return errMock
  }
  if m.responseText != "" {
    cb(TextDelta(m.responseText))
  }
  if len(m.failAfterText) > 0 {
    for _, t := range m.failAfterText {
      cb(TextDelta(t))
    }
    return errMock
  }
  for _, tc := range m.toolCalls {
    data, _ := json.Marshal(tc)
    cb(StreamEvent{Type: "tool_call_start", Data: data})
  }
  cb(FinishEvent(m.responseText, map[string]int{}))
  return nil
}

var errMock = &mockError{}

type mockError struct{}

func (e *mockError) Error() string { return "mock provider error" }

// ============================================================
// 心跳 — StartHeartbeat 应按固定间隔发送 heartbeat 事件
// ============================================================

func TestHeartbeat_EmitsAtInterval(t *testing.T) {
  ctx, cancel := context.WithCancel(context.Background())
  defer cancel()

  var mu eventsMu
  var events []StreamEvent
  cb := func(e StreamEvent) {
    mu.Lock()
    events = append(events, e)
    mu.Unlock()
  }

  StartHeartbeat(ctx, 10*time.Millisecond, cb)
  time.Sleep(35 * time.Millisecond)
  cancel()
  time.Sleep(5 * time.Millisecond)

  mu.Lock()
  count := len(events)
  mu.Unlock()

  if count < 2 {
    t.Errorf("expected at least 2 heartbeats in 35ms, got %d", count)
  }
}

func TestHeartbeat_StopsOnCancel(t *testing.T) {
  ctx, cancel := context.WithCancel(context.Background())

  var mu eventsMu
  var events []StreamEvent

  StartHeartbeat(ctx, 10*time.Millisecond, func(e StreamEvent) {
    mu.Lock()
    events = append(events, e)
    mu.Unlock()
  })

  cancel()
  time.Sleep(30 * time.Millisecond)

  mu.Lock()
  before := len(events)
  mu.Unlock()

  time.Sleep(30 * time.Millisecond)

  mu.Lock()
  after := len(events)
  mu.Unlock()

  if after != before {
    t.Errorf("heartbeat should stop after cancel: before=%d after=%d", before, after)
  }
}

// ============================================================
// 任务终止事件 — Engine.Process 完成后应发出 task_done 事件
// ============================================================

func TestEngine_EmitsTaskDoneOnSuccess(t *testing.T) {
  provider := &mockProvider{responseText: "hello"}
  tools := NewToolRegistry()
  store := NewSessionStore(t.TempDir())
  engine := NewEngine(testConfig(), provider, tools, store)

  msg := &Message{Role: RoleUser, Content: "hi"}
  var gotTaskDone bool
  err := engine.Process(context.Background(), "be helpful", nil, msg, func(e StreamEvent) {
    if e.Type == "task_done" {
      gotTaskDone = true
    }
  })

  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if !gotTaskDone {
    t.Error("expected task_done event, but none emitted")
  }
}

func TestEngine_EmitsTaskDoneOnError(t *testing.T) {
  provider := &mockProvider{}
  tools := NewToolRegistry()
  store := NewSessionStore(t.TempDir())
  engine := NewEngine(testConfig(), provider, tools, store)

  msg := &Message{Role: RoleUser, Content: "hi"}
  var gotTaskDone bool
  err := engine.Process(context.Background(), "be helpful", nil, msg, func(e StreamEvent) {
    if e.Type == "task_done" {
      gotTaskDone = true
    }
  })

  if err == nil {
    t.Fatal("expected error for empty response")
  }
  if !gotTaskDone {
    t.Error("expected task_done event even on error, but none emitted")
  }
}

// ============================================================
// 部分响应恢复 — Engine 出错前已发出的 text_delta 应为空(空响应检查在 engine 外部处理)
// ============================================================

// ============================================================
// 纯工具调用不报错 — 循环跑满 maxIter 但全是 tool_calls 时不返回错误
// ============================================================

func TestEngine_ToolOnlyLoopIsNotError(t *testing.T) {
  provider := &mockProvider{
    // 每次调用都返回 tool call，无文本
    toolCalls: []ToolCallData{
      {ID: "tc1", Name: "tool_a", Input: json.RawMessage(`{}`)},
    },
    responseText: "",
  }
  tools := NewToolRegistry()
  tools.Register(&dummyTool{name: "tool_a"})
  store := NewSessionStore(t.TempDir())
  engine := NewEngine(testConfig(), provider, tools, store)
  // 设 maxIter=2，循环两次都有 tool calls → 不 break，跑满 2 次
  engine.config.MaxIter = 2

  msg := &Message{Role: RoleUser, Content: "do tools"}
  err := engine.Process(context.Background(), "use tools only", nil, msg, func(_ StreamEvent) {})

  if err != nil {
    t.Errorf("tool-only loop should not return error, got: %v", err)
  }
}

type dummyTool struct {
  name string
}

func (t *dummyTool) Name() string { return t.name }
func (t *dummyTool) Description() string { return "dummy tool for testing" }
func (t *dummyTool) Schema() map[string]interface{} { return map[string]interface{}{"type": "object"} }
func (t *dummyTool) Execute(_ context.Context, args json.RawMessage) (*ToolResult, error) {
  return &ToolResult{Success: true, Data: "dummy result"}, nil
}

// ============================================================
// Agent Protocol — Engine 应注入企业级行为协议到 system prompt
// ============================================================

func TestEngine_InjectsAgentProtocol(t *testing.T) {
  provider := &mockSkillsProvider{}
  tools := NewToolRegistry()
  store := NewSessionStore(t.TempDir())
  engine := NewEngine(testConfig(), provider, tools, store)

  msg := &Message{Role: RoleUser, Content: "hello"}
  _ = engine.Process(context.Background(), "", nil, msg, func(_ StreamEvent) {})

  required := []string{
    "Agent Protocol",
    "绝不编造",
    "工具优先",
    "安全准则",
    "subagent",
    "企业级",
  }
  for _, s := range required {
    if !strings.Contains(provider.capturedSystem, s) {
      t.Errorf("Agent Protocol 应包含 %q，但 system prompt 中未找到\n完整 prompt:\n%s", s, provider.capturedSystem)
    }
  }
}

func TestEngine_AgentProtocolHasNoPlaceholders(t *testing.T) {
  provider := &mockSkillsProvider{}
  tools := NewToolRegistry()
  store := NewSessionStore(t.TempDir())
  engine := NewEngine(testConfig(), provider, tools, store)

  msg := &Message{Role: RoleUser, Content: "x"}
  _ = engine.Process(context.Background(), "", nil, msg, func(_ StreamEvent) {})

  placeholders := []string{"TBD", "TODO", "xxx", "your", "customize"}
  for _, p := range placeholders {
    if strings.Contains(provider.capturedSystem, p) {
      t.Errorf("Agent Protocol 包含占位符 %q，应删除或填充\n完整 prompt:\n%s", p, provider.capturedSystem)
    }
  }
}

func TestEngine_EmitsTextDeltaBeforeError(t *testing.T) {
  provider := &mockProvider{
    failAfterText: []string{"I'm about to "},
  }
  tools := NewToolRegistry()
  store := NewSessionStore(t.TempDir())
  engine := NewEngine(testConfig(), provider, tools, store)

  msg := &Message{Role: RoleUser, Content: "write something"}
  var partialText string
  err := engine.Process(context.Background(), "be helpful", nil, msg, func(e StreamEvent) {
    if e.Type == "text_delta" {
      var text string
      if json.Unmarshal(e.Data, &text) == nil {
        partialText += text
      }
    }
  })

  if err == nil {
    t.Fatal("expected error for failed provider")
  }
  expected := ""
  for i := 0; i <= maxLLMRetries; i++ {
    expected += "I'm about to "
  }
  if partialText != expected {
    t.Errorf("expected partial text %q (retried %d times), got %q", "I'm about to ", maxLLMRetries+1, partialText)
  }
}

type eventsMu struct {
  sync.Mutex
}

func testConfig() *AgentConfig {
  return &AgentConfig{
    Model: ModelConfig{
      Provider: "test",
      ModelID:  "test-model",
    },
  }
}

// ============================================================
// 空响应检测 — Engine.Process 应在 LLM 返回空内容时返回错误
// ============================================================

func TestEngine_EmptyResponseReturnsError(t *testing.T) {
  provider := &mockProvider{}
  tools := NewToolRegistry()
  store := NewSessionStore(t.TempDir())
  engine := NewEngine(testConfig(), provider, tools, store)

  msg := &Message{Role: RoleUser, Content: "hello"}
  err := engine.Process(context.Background(), "be helpful", nil, msg, func(_ StreamEvent) {})

  if err == nil {
    t.Error("expected error for empty LLM response, got nil")
  }
}
