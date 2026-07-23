package agent

import (
  "context"
  "encoding/json"
  "strings"
  "sync"
  "testing"
  "time"

  "google.golang.org/adk/v2/session"
)

func TestADKRun_BasicTextResponse(t *testing.T) {
  provider := &mockProvider{responseText: "你好，有什么可以帮助你？"}
  tools := NewToolRegistry()
  tools.Register(NewReadFileTool())
  tools.Register(NewWriteFileTool())
  tools.Register(NewListDirTool())

  cfg := &AgentConfig{Model: ModelConfig{Provider: "test", ModelID: "test-model"}, UserID: "test-user"}
  sessSvc := session.InMemoryService()

  msg := &Message{Role: RoleUser, Content: "你好"}

  var events []StreamEvent
  var mu sync.Mutex
  cb := func(ev StreamEvent) {
    mu.Lock()
    events = append(events, ev)
    mu.Unlock()
  }

  err := ADKRun(context.Background(), cfg, provider, tools, nil, sessSvc, "你是一个 AI 助手", msg, cb)
  if err != nil {
    t.Fatal(err)
  }

  mu.Lock()
  defer mu.Unlock()

  var fullText string
  var hasFinish bool
  for _, ev := range events {
    switch ev.Type {
    case "text_delta":
      var text string
      if json.Unmarshal(ev.Data, &text) == nil {
        fullText += text
      }
    case "finish":
      hasFinish = true
    }
  }

  if !strings.Contains(fullText, "你好") {
    t.Errorf("expected response to contain '你好', got: %s", fullText)
  }
  if !hasFinish {
    t.Error("expected finish event")
  }
}

func TestADKRun_WithToolCalls(t *testing.T) {
  origWorkspace := sandboxWorkspace
  dir := t.TempDir()
  sandboxWorkspace = dir
  defer func() { sandboxWorkspace = origWorkspace }()

  provider := &twoStageProvider{
    stage1ToolCalls: []ToolCallData{
      {
        ID:    "call_1",
        Name:  "read_file",
        Input: json.RawMessage(`{"path": "` + dir + `/test.txt"}`),
      },
    },
    stage2Text: "文件内容已读取",
  }
  tools := NewToolRegistry()
  tools.Register(NewReadFileTool())

  cfg := &AgentConfig{Model: ModelConfig{Provider: "test", ModelID: "test-model"}, UserID: "test-user"}
  sessSvc := session.InMemoryService()

  msg := &Message{Role: RoleUser, Content: "read file"}

  var events []StreamEvent
  var mu sync.Mutex
  cb := func(ev StreamEvent) {
    mu.Lock()
    events = append(events, ev)
    mu.Unlock()
  }

  err := ADKRun(context.Background(), cfg, provider, tools, nil, sessSvc, "你是一个 AI 助手", msg, cb)
  if err != nil {
    t.Fatal(err)
  }

  mu.Lock()
  defer mu.Unlock()

  var toolCalls int
  var hasFinish bool
  var hasText bool
  for _, ev := range events {
    if ev.Type == "tool_call_start" {
      toolCalls++
    }
    if ev.Type == "text_delta" {
      hasText = true
    }
    if ev.Type == "finish" {
      hasFinish = true
    }
  }

  if toolCalls != 1 {
    t.Errorf("expected 1 tool call, got %d", toolCalls)
  }
  if !hasText {
    t.Error("expected text after tool execution")
  }
  if !hasFinish {
    t.Error("expected finish event")
  }
}

// twoStageProvider: first call returns tool calls, second call returns text
type twoStageProvider struct {
  stage1ToolCalls []ToolCallData
  stage2Text      string
  callCount       int
}

func (p *twoStageProvider) StreamChat(_ context.Context, _ *ChatRequest, cb func(event StreamEvent)) error {
  p.callCount++
  if p.callCount == 1 {
    for _, tc := range p.stage1ToolCalls {
      data, _ := json.Marshal(tc)
      cb(StreamEvent{Type: "tool_call_start", Data: data})
    }
    cb(FinishEvent("", map[string]int{}))
  } else {
    if p.stage2Text != "" {
      cb(TextDelta(p.stage2Text))
    }
    cb(FinishEvent(p.stage2Text, map[string]int{}))
  }
  return nil
}

func TestADKRun_ProviderError(t *testing.T) {
  provider := &mockProvider{shouldError: true}
  tools := NewToolRegistry()

  cfg := &AgentConfig{Model: ModelConfig{Provider: "test", ModelID: "test-model"}, UserID: "test-user"}
  sessSvc := session.InMemoryService()

  msg := &Message{Role: RoleUser, Content: "hi"}

  err := ADKRun(context.Background(), cfg, provider, tools, nil, sessSvc, "你是一个 AI 助手", msg, func(ev StreamEvent) {})
  if err == nil {
    t.Error("expected error from provider, got nil")
  }
}

func TestADKRun_ContextCancellation(t *testing.T) {
  provider := &blockingProvider{}
  tools := NewToolRegistry()

  cfg := &AgentConfig{Model: ModelConfig{Provider: "test", ModelID: "test-model"}, UserID: "test-user"}
  sessSvc := session.InMemoryService()

  ctx, cancel := context.WithCancel(context.Background())
  msg := &Message{Role: RoleUser, Content: "hi"}

  done := make(chan error, 1)
  go func() {
    done <- ADKRun(ctx, cfg, provider, tools, nil, sessSvc, "test", msg, func(ev StreamEvent) {})
  }()

  // Cancel after ADKRun starts (v2 creates session lazily, does not fail eagerly)
  time.Sleep(50 * time.Millisecond)
  cancel()

  select {
  case <-done:
    // OK: ADKRun returned (may or may not be an error depending on v2 internals)
  case <-time.After(5 * time.Second):
    t.Fatal("ADKRun did not return after context cancellation")
  }
}
