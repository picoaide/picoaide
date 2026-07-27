package testutil

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/picoaide/picoaide/internal/agent"
)

// MockProvider implements agent.Provider with deterministic responses.
type MockProvider struct {
	mu           sync.Mutex
	ResponseText string
	ShouldError  bool
	ToolCalls    []agent.ToolCallData
	// callbacks for inspecting what was sent to the provider
	CallCount int
	Requests  []*agent.ChatRequest
}

func (m *MockProvider) StreamChat(_ context.Context, req *agent.ChatRequest, cb func(agent.StreamEvent)) error {
	m.mu.Lock()
	m.CallCount++
	m.Requests = append(m.Requests, req)
	m.mu.Unlock()

	if m.ShouldError {
		return errMock
	}
	if m.ResponseText != "" {
		cb(agent.TextDelta(m.ResponseText))
	}
	for _, tc := range m.ToolCalls {
		data, _ := json.Marshal(tc)
		cb(agent.StreamEvent{Type: "tool_call_start", Data: data})
	}
	cb(agent.FinishEvent(m.ResponseText, map[string]int{}))
	return nil
}

var errMock = &mockError{}

type mockError struct{}

func (e *mockError) Error() string { return "mock provider error" }

// BlockingProvider blocks until ctx is cancelled, for timeout/cancel tests.
type BlockingProvider struct{}

func (*BlockingProvider) StreamChat(ctx context.Context, _ *agent.ChatRequest, _ func(agent.StreamEvent)) error {
	<-ctx.Done()
	return ctx.Err()
}

// CollectEvents collects stream events into a slice with mutex safety.
type EventCollector struct {
	mu     sync.Mutex
	Events []agent.StreamEvent
}

func NewEventCollector() *EventCollector {
	return &EventCollector{}
}

func (c *EventCollector) Callback() func(agent.StreamEvent) {
	return func(ev agent.StreamEvent) {
		c.mu.Lock()
		c.Events = append(c.Events, ev)
		c.mu.Unlock()
	}
}

func (c *EventCollector) Get() []agent.StreamEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]agent.StreamEvent, len(c.Events))
	copy(result, c.Events)
	return result
}

// AssertFinish checks that the event stream contains a finish event.
func AssertFinish(t *testing.T, events []agent.StreamEvent) bool {
	t.Helper()
	for _, ev := range events {
		if ev.Type == "finish" {
			return true
		}
	}
	t.Error("expected finish event, got none")
	return false
}

// AssertToolCall checks that the event stream contains exactly n tool_call_start events.
func AssertToolCallCount(t *testing.T, events []agent.StreamEvent, n int) bool {
	t.Helper()
	var count int
	for _, ev := range events {
		if ev.Type == "tool_call_start" {
			count++
		}
	}
	if count != n {
		t.Errorf("expected %d tool call(s), got %d", n, count)
		return false
	}
	return true
}

// AssertTextContains checks that the combined text_delta contains substr.
func AssertTextContains(t *testing.T, events []agent.StreamEvent, substr string) bool {
	t.Helper()
	var full string
	for _, ev := range events {
		if ev.Type == "text_delta" {
			var text string
			if json.Unmarshal(ev.Data, &text) == nil {
				full += text
			}
		}
	}
	if !contains(full, substr) {
		t.Errorf("expected text containing %q, got %q", substr, full)
		return false
	}
	return true
}

// AssertNoError checks that no error events are in the stream.
func AssertNoError(t *testing.T, events []agent.StreamEvent) bool {
	t.Helper()
	for _, ev := range events {
		if ev.Type == "error" {
			t.Errorf("unexpected error event: %s", string(ev.Data))
			return false
		}
	}
	return true
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
