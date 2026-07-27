package agent_test

import (
	"context"
	"testing"

	"google.golang.org/adk/v2/session"

	"github.com/picoaide/picoaide/internal/agent"
	"github.com/picoaide/picoaide/internal/agent/testutil"
)

// External (black-box) tests — use only exported API + testutil mocks.
// These tests verify the public contract of ADKRun without accessing
// unexported symbols like sandboxWorkspace.

func TestADKRunExternal_Basic(t *testing.T) {
	provider := &testutil.MockProvider{ResponseText: "hello from mock"}
	tools := agent.NewToolRegistry()
	sessSvc := session.InMemoryService()
	cfg := &agent.AgentConfig{
		Model:  agent.ModelConfig{Provider: "test", ModelID: "test-model"},
		UserID: "ext-test-user",
	}
	collector := testutil.NewEventCollector()

	err := agent.ADKRun(
		context.Background(), cfg, provider, tools, nil, sessSvc,
		"you are a test agent",
		&agent.Message{Role: agent.RoleUser, Content: "hi"},
		collector.Callback(),
	)
	if err != nil {
		t.Fatal(err)
	}

	events := collector.Get()
	testutil.AssertTextContains(t, events, "hello")
	testutil.AssertFinish(t, events)
	testutil.AssertNoError(t, events)
	testutil.AssertToolCallCount(t, events, 0)
}

func TestADKRunExternal_ProviderError(t *testing.T) {
	provider := &testutil.MockProvider{ShouldError: true}
	cfg := &agent.AgentConfig{
		Model:  agent.ModelConfig{Provider: "test", ModelID: "test-model"},
		UserID: "ext-test-user",
	}
	sessSvc := session.InMemoryService()

	err := agent.ADKRun(
		context.Background(), cfg, provider, agent.NewToolRegistry(), nil, sessSvc,
		"test",
		&agent.Message{Role: agent.RoleUser, Content: "hi"},
		func(ev agent.StreamEvent) {},
	)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestADKRunExternal_ContextCancel(t *testing.T) {
	provider := &testutil.BlockingProvider{}
	cfg := &agent.AgentConfig{
		Model:  agent.ModelConfig{Provider: "test", ModelID: "test-model"},
		UserID: "ext-test-user",
	}
	sessSvc := session.InMemoryService()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- agent.ADKRun(
			ctx, cfg, provider, agent.NewToolRegistry(), nil, sessSvc,
			"test",
			&agent.Message{Role: agent.RoleUser, Content: "hi"},
			func(ev agent.StreamEvent) {},
		)
	}()

	cancel()
	<-done // ADKRun returned after cancellation
}

func TestADKRunExternal_MockCallCount(t *testing.T) {
	provider := &testutil.MockProvider{ResponseText: "ok"}
	cfg := &agent.AgentConfig{
		Model:  agent.ModelConfig{Provider: "test", ModelID: "test-model"},
		UserID: "ext-test-user",
	}
	sessSvc := session.InMemoryService()

	for i := 0; i < 3; i++ {
		collector := testutil.NewEventCollector()
		err := agent.ADKRun(
			context.Background(), cfg, provider, agent.NewToolRegistry(), nil, sessSvc,
			"test",
			&agent.Message{Role: agent.RoleUser, Content: "msg"},
			collector.Callback(),
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	if provider.CallCount != 3 {
		t.Errorf("expected 3 calls to provider, got %d", provider.CallCount)
	}
	if len(provider.Requests) != 3 {
		t.Errorf("expected 3 recorded requests, got %d", len(provider.Requests))
	}
}
