package agent

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/adk/v2/session"
)

// white-box tests — need access to sandboxWorkspace and other unexported symbols.
// External/black-box tests using testutil suite are in adk_run_ext_test.go.

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
	cb := collectEvents(&events)

	err := ADKRun(context.Background(), cfg, provider, tools, nil, sessSvc, "你是一个 AI 助手", msg, cb)
	if err != nil {
		t.Fatal(err)
	}

	if !containsText(events, "你好") {
		t.Errorf("expected response to contain '你好'")
	}
	if !hasEvent(events, "finish") {
		t.Error("expected finish event")
	}
	if hasEvent(events, "error") {
		t.Error("unexpected error event")
	}
}

// twoStageProvider: first call returns tool calls, second returns text.
// Required by ADK's llmagent loop: tool call → execute → call LLM again.
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
	cb := collectEvents(&events)

	err := ADKRun(context.Background(), cfg, provider, tools, nil, sessSvc, "你是一个 AI 助手", msg, cb)
	if err != nil {
		t.Fatal(err)
	}

	if toolCallCount(events) != 1 {
		t.Errorf("expected 1 tool call, got %d", toolCallCount(events))
	}
	if !containsText(events, "文件内容已读取") {
		t.Error("expected text after tool execution")
	}
	if !hasEvent(events, "finish") {
		t.Error("expected finish event")
	}
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

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ADKRun did not return after context cancellation")
	}
}

func TestADKRun_MultiTurn(t *testing.T) {
	origWorkspace := sandboxWorkspace
	dir := t.TempDir()
	sandboxWorkspace = dir
	defer func() { sandboxWorkspace = origWorkspace }()

	cfg := &AgentConfig{Model: ModelConfig{Provider: "test", ModelID: "test-model"}, UserID: "test-user"}
	sessSvc := session.InMemoryService()
	tools := NewToolRegistry()
	tools.Register(NewWriteFileTool())
	tools.Register(NewReadFileTool())

	// Turn 1: write a file
	t1Provider := &twoStageProvider{
		stage1ToolCalls: []ToolCallData{
			{
				ID:    "call_t1",
				Name:  "write_file",
				Input: json.RawMessage(`{"path": "` + dir + `/hello.txt", "content": "world"}`),
			},
		},
		stage2Text: "文件已创建",
	}
	msg1 := &Message{Role: RoleUser, Content: "create hello.txt"}
	var events1 []StreamEvent
	cb1 := collectEvents(&events1)

	err := ADKRun(context.Background(), cfg, t1Provider, tools, nil, sessSvc, "你是一个 AI 助手", msg1, cb1)
	if err != nil {
		t.Fatal(err)
	}
	if toolCallCount(events1) != 1 {
		t.Errorf("turn 1: expected 1 tool call, got %d", toolCallCount(events1))
	}
	if !hasEvent(events1, "finish") {
		t.Error("turn 1: expected finish event")
	}
	if !containsText(events1, "文件已创建") {
		t.Error("turn 1: expected text")
	}

	// Append turn 1 to session store so turn 2 has context
	sessionKey := BuildSessionKey(SessionScope{
		Version: 1, AgentID: "pico", Account: "test-user",
		Dimensions: []string{"user"}, Values: map[string]string{"user": "test-user"},
	})
	store := NewSessionStore(dir)
	store.AppendMessage(sessionKey, msg1)

	var assistantContent string
	json.Unmarshal(events1[len(events1)-1].Data, &assistantContent)
	store.AppendMessage(sessionKey, &Message{Role: RoleAssistant, Content: assistantContent})

	// Turn 2: verify the file
	t2Provider := &twoStageProvider{
		stage1ToolCalls: []ToolCallData{
			{
				ID:    "call_t2",
				Name:  "read_file",
				Input: json.RawMessage(`{"path": "` + dir + `/hello.txt"}`),
			},
		},
		stage2Text: "文件内容是 world",
	}
	msg2 := &Message{Role: RoleUser, Content: "check hello.txt"}
	var events2 []StreamEvent
	cb2 := collectEvents(&events2)

	err = ADKRun(context.Background(), cfg, t2Provider, tools, nil, sessSvc, "你是一个 AI 助手", msg2, cb2)
	if err != nil {
		t.Fatal(err)
	}
	if toolCallCount(events2) != 1 {
		t.Errorf("turn 2: expected 1 tool call, got %d", toolCallCount(events2))
	}
	if !hasEvent(events2, "finish") {
		t.Error("turn 2: expected finish event")
	}
	if !containsText(events2, "world") {
		t.Error("turn 2: expected text containing 'world'")
	}
}

func TestADKRun_SandboxWorkspaceIsolation(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	origWorkspace := sandboxWorkspace
	defer func() { sandboxWorkspace = origWorkspace }()

	sandboxWorkspace = dir1
	provider := &twoStageProvider{
		stage1ToolCalls: []ToolCallData{
			{
				ID:    "call_1",
				Name:  "write_file",
				Input: json.RawMessage(`{"path": "` + dir1 + `/data.txt", "content": "from sandbox1"}`),
			},
		},
		stage2Text: "done",
	}
	tools := NewToolRegistry()
	tools.Register(NewWriteFileTool())
	cfg := &AgentConfig{Model: ModelConfig{Provider: "test", ModelID: "test-model"}, UserID: "test-user"}
	sessSvc := session.InMemoryService()

	msg := &Message{Role: RoleUser, Content: "write file"}
	var events []StreamEvent
	cb := collectEvents(&events)
	err := ADKRun(context.Background(), cfg, provider, tools, nil, sessSvc, "test", msg, cb)
	if err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir2)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > 0 {
		t.Errorf("dir2 should be empty (workspace isolation), got: %v", entryNames(entries))
	}
}

// helpers

func collectEvents(events *[]StreamEvent) func(StreamEvent) {
	var mu sync.Mutex
	return func(ev StreamEvent) {
		mu.Lock()
		*events = append(*events, ev)
		mu.Unlock()
	}
}

func toolCallCount(events []StreamEvent) int {
	var n int
	for _, ev := range events {
		if ev.Type == "tool_call_start" {
			n++
		}
	}
	return n
}

func hasEvent(events []StreamEvent, typ string) bool {
	for _, ev := range events {
		if ev.Type == typ {
			return true
		}
	}
	return false
}

func containsText(events []StreamEvent, substr string) bool {
	var full string
	for _, ev := range events {
		if ev.Type == "text_delta" {
			var text string
			if json.Unmarshal(ev.Data, &text) == nil {
				full += text
			}
		}
	}
	return strings.Contains(full, substr)
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names
}
