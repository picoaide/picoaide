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
// blockingProvider — 阻塞直到上下文取消，用于测试上下文取消场景
// ============================================================

type blockingProvider struct{}

func (p *blockingProvider) StreamChat(ctx context.Context, _ *ChatRequest, _ func(event StreamEvent)) error {
  <-ctx.Done()
  return ctx.Err()
}

// ============================================================
// Engine.Process 正常对话流
// ============================================================

func TestAppendToSystemPrompt_AddsToPendingNotPreloaded(t *testing.T) {
  e := NewEngine(testConfig(), nil, nil, nil)
  e.AppendToSystemPrompt("tool hint")
  if e.preloadedSystem != "" {
    t.Errorf("preloadedSystem should remain empty, got %q", e.preloadedSystem)
  }
  if len(e.pendingAdditions) != 1 || e.pendingAdditions[0] != "tool hint" {
    t.Errorf("pendingAdditions = %v, want [tool hint]", e.pendingAdditions)
  }
}

func TestAppendToSystemPrompt_MultipleCalls(t *testing.T) {
  e := NewEngine(testConfig(), nil, nil, nil)
  e.AppendToSystemPrompt("first")
  e.AppendToSystemPrompt("second")
  if len(e.pendingAdditions) != 2 {
    t.Errorf("pendingAdditions count = %d, want 2", len(e.pendingAdditions))
  }
}

func TestBuildSystemPrompt_ExcludesPendingAdditions(t *testing.T) {
  tools := NewToolRegistry()
  e := NewEngine(testConfig(), nil, tools, nil)
  e.preloadedServers = append(e.preloadedServers, "test") // prevent server summary
  e.AppendToSystemPrompt("should not be here")
  result := e.buildSystemPrompt("base content")
  if strings.Contains(result, "should not be here") {
    t.Errorf("system prompt should not contain pending additions: %s", result)
  }
  if !strings.Contains(result, "base content") {
    t.Errorf("system prompt should contain base content")
  }
}

func TestBuildSystemPrompt_FrozenAfterFirstBuild(t *testing.T) {
  tools := NewToolRegistry()
  e := NewEngine(testConfig(), nil, tools, nil)
  e.preloadedServers = append(e.preloadedServers, "test")
  first := e.buildSystemPrompt("base")
  e.AppendToSystemPrompt("new hint")
  second := e.buildSystemPrompt("base")
  if first != second {
    t.Errorf("system prompt should be frozen after first build: first=%q second=%q", first, second)
  }
}

func TestEngine_NormalConversationFlow(t *testing.T) {
  provider := &mockProvider{responseText: "你好，有什么可以帮助你？"}
  tools := NewToolRegistry()
  store := NewSessionStore(t.TempDir())
  engine := NewEngine(testConfig(), provider, tools, store)

  var mu sync.Mutex
  var events []StreamEvent

  msg := &Message{Role: RoleUser, Content: "你好"}
  err := engine.Process(context.Background(), "be helpful", nil, msg, func(e StreamEvent) {
    mu.Lock()
    events = append(events, e)
    mu.Unlock()
  })

  if err != nil {
    t.Fatalf("Process returned error: %v", err)
  }

  var text string
  var hasFinish, hasTaskDone, hasProgress bool

  mu.Lock()
  for _, e := range events {
    switch e.Type {
    case "text_delta":
      var part string
      if json.Unmarshal(e.Data, &part) == nil {
        text += part
      }
    case "finish":
      hasFinish = true
    case "task_done":
      hasTaskDone = true
    case "progress":
      hasProgress = true
    }
  }
  mu.Unlock()

  if text != "你好，有什么可以帮助你？" {
    t.Errorf("text_delta content = %q, want %q", text, "你好，有什么可以帮助你？")
  }
  if !hasFinish {
    t.Error("expected finish event")
  }
  if !hasTaskDone {
    t.Error("expected task_done event")
  }
  if !hasProgress {
    t.Error("expected progress event")
  }
}

// ============================================================
// Engine.Process 工具调用流
// ============================================================

func TestEngine_ToolCallFlow(t *testing.T) {
  provider := &mockProvider{
    toolCalls: []ToolCallData{
      {ID: "tc1", Name: "tool_a", Input: json.RawMessage(`{}`)},
    },
  }
  tools := NewToolRegistry()
  tools.Register(&dummyTool{name: "tool_a"})
  store := NewSessionStore(t.TempDir())
  engine := NewEngine(testConfig(), provider, tools, store)
  engine.config.MaxIter = 2

  var mu sync.Mutex
  var toolResults int
  var hasProgress bool

  msg := &Message{Role: RoleUser, Content: "执行工具"}
  err := engine.Process(context.Background(), "use tools", nil, msg, func(e StreamEvent) {
    mu.Lock()
    switch e.Type {
    case "tool_result":
      toolResults++
    case "progress":
      hasProgress = true
    }
    mu.Unlock()
  })

  if err != nil {
    t.Fatalf("Process returned error: %v", err)
  }
  if toolResults == 0 {
    t.Error("expected at least one tool_result event")
  }
  if !hasProgress {
    t.Error("expected progress event")
  }
}

// ============================================================
// Engine.Process 空响应重试
// ============================================================

func TestEngine_EmptyResponseRetry(t *testing.T) {
  provider := &mockProvider{}
  tools := NewToolRegistry()
  store := NewSessionStore(t.TempDir())
  engine := NewEngine(testConfig(), provider, tools, store)

  var hasTaskDone bool
  var hasError bool

  msg := &Message{Role: RoleUser, Content: "hello"}
  err := engine.Process(context.Background(), "be helpful", nil, msg, func(e StreamEvent) {
    switch e.Type {
    case "task_done":
      hasTaskDone = true
    case "error":
      hasError = true
    }
  })

  if err == nil {
    t.Fatal("expected error for empty LLM response")
  }
  if !hasTaskDone {
    t.Error("expected task_done event even on error")
  }
  if !hasError {
    t.Error("expected error event on empty response")
  }
}

// ============================================================
// Engine.Process 上下文取消
// ============================================================

func TestEngine_ContextCancellation(t *testing.T) {
  ctx, cancel := context.WithCancel(context.Background())
  defer cancel()

  provider := &blockingProvider{}
  tools := NewToolRegistry()
  store := NewSessionStore(t.TempDir())
  engine := NewEngine(testConfig(), provider, tools, store)

  go func() {
    time.Sleep(50 * time.Millisecond)
    cancel()
  }()

  var hasTaskDone bool
  var hasError bool

  msg := &Message{Role: RoleUser, Content: "hello"}
  err := engine.Process(ctx, "be helpful", nil, msg, func(e StreamEvent) {
    switch e.Type {
    case "task_done":
      hasTaskDone = true
    case "error":
      hasError = true
    }
  })

  if err == nil {
    t.Error("expected error from context cancellation")
  }
  if !hasTaskDone {
    t.Error("expected task_done event on cancellation")
  }
  if !hasError {
    t.Error("expected error event on cancellation")
  }
}
