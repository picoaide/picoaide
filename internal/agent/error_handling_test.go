package agent

import (
	"context"
	"encoding/json"
)

// ============================================================
// mockProvider — 用于测试
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
// blockingProvider — 阻塞直到上下文取消，用于测试上下文取消场景
// ============================================================

type blockingProvider struct{}

func (p *blockingProvider) StreamChat(ctx context.Context, _ *ChatRequest, _ func(event StreamEvent)) error {
	<-ctx.Done()
	return ctx.Err()
}

// ============================================================
// testConfig — 通用测试配置
// ============================================================

func testConfig() *AgentConfig {
	return &AgentConfig{
		Model: ModelConfig{
			Provider: "test",
			ModelID:  "test-model",
		},
	}
}
