package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/robfig/cron/v3"
)

// ============================================================
// BuildSessionKey
// ============================================================

func TestBuildSessionKey(t *testing.T) {
	tests := []struct {
		name  string
		scope SessionScope
		check func(t *testing.T, key string)
	}{
		{
			name: "dimensions_match",
			scope: SessionScope{
				AgentID:    "pico",
				Account:    "fallback",
				Dimensions: []string{"user_id"},
				Values:     map[string]string{"user_id": "u123"},
			},
			check: func(t *testing.T, key string) {
				if !strings.HasPrefix(key, "sk_v1_") {
					t.Errorf("expected sk_v1_ prefix, got %q", key)
				}
				if len(key) != 70 {
					t.Errorf("expected len 70 (6+64), got %d", len(key))
				}
			},
		},
		{
			name: "account_fallback",
			scope: SessionScope{
				AgentID: "pico",
				Account: "user@example.com",
			},
			check: func(t *testing.T, key string) {
				if !strings.HasPrefix(key, "sk_v1_") {
					t.Errorf("expected sk_v1_ prefix, got %q", key)
				}
			},
		},
		{
			name: "custom_agent_id",
			scope: SessionScope{
				AgentID:    "mybot",
				Account:    "u456",
				Dimensions: []string{"user_id"},
				Values:     map[string]string{"user_id": "u456"},
			},
			check: func(t *testing.T, key string) {
				if !strings.HasPrefix(key, "sk_v1_") {
					t.Errorf("expected sk_v1_ prefix, got %q", key)
				}
			},
		},
		{
			name: "first_dimension_wins",
			scope: SessionScope{
				AgentID:    "pico",
				Dimensions: []string{"dim1", "dim2"},
				Values:     map[string]string{"dim1": "first", "dim2": "second"},
			},
			check: func(t *testing.T, key string) {
				if !strings.HasPrefix(key, "sk_v1_") {
					t.Errorf("expected sk_v1_ prefix, got %q", key)
				}
			},
		},
		{
			name: "empty_dimensions_empty_account",
			scope: SessionScope{
				AgentID: "pico",
			},
			check: func(t *testing.T, key string) {
				if !strings.HasPrefix(key, "sk_v1_") {
					t.Errorf("expected sk_v1_ prefix, got %q", key)
				}
			},
		},
	}

	seen := make(map[string]bool)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := BuildSessionKey(tt.scope)
			tt.check(t, key)
			if seen[key] {
				t.Errorf("duplicate key produced: %s", key)
			}
			seen[key] = true
		})
	}
}

func TestBuildSessionKey_differentUsersDifferentKeys(t *testing.T) {
	k1 := BuildSessionKey(SessionScope{AgentID: "pico", Account: "alice"})
	k2 := BuildSessionKey(SessionScope{AgentID: "pico", Account: "bob"})
	if k1 == k2 {
		t.Errorf("different users should get different keys")
	}
}

func TestBuildSessionKey_sameUserSameKey(t *testing.T) {
	s1 := SessionScope{AgentID: "pico", Dimensions: []string{"email"}, Values: map[string]string{"email": "a@b"}}
	s2 := SessionScope{AgentID: "pico", Dimensions: []string{"email"}, Values: map[string]string{"email": "a@b"}}
	if BuildSessionKey(s1) != BuildSessionKey(s2) {
		t.Errorf("same user should get same key")
	}
}

// ============================================================
// SanitizeKey
// ============================================================

func TestSanitizeKey(t *testing.T) {
	tests := []struct {
		in  string
		out string
	}{
		{"abc", "abc"},
		{"a:b", "a_b"},
		{"a/b", "a_b"},
		{"a\\b", "a_b"},
		{"a:b/c\\d", "a_b_c_d"},
		{"", ""},
	}
	for _, tt := range tests {
		got := SanitizeKey(tt.in)
		if got != tt.out {
			t.Errorf("SanitizeKey(%q) = %q, want %q", tt.in, got, tt.out)
		}
	}
}

// ============================================================
// Message.ToLLMMessage
// ============================================================

func TestToLLMMessage(t *testing.T) {
	msg := &Message{
		Role:       RoleUser,
		Content:    "hello",
		ToolCallID: "tc1",
		ToolCalls: []ToolCall{
			{ID: "t1", Type: "function", Function: ToolFunction{Name: "fn", Arguments: "{}"}},
		},
	}
	llm := msg.ToLLMMessage()
	if llm.Role != "user" {
		t.Errorf("Role = %q, want user", llm.Role)
	}
	if llm.Content != "hello" {
		t.Errorf("Content = %q, want hello", llm.Content)
	}
	if llm.ToolCallID != "tc1" {
		t.Errorf("ToolCallID = %q, want tc1", llm.ToolCallID)
	}
	if len(llm.ToolCalls) != 1 || llm.ToolCalls[0].ID != "t1" {
		t.Errorf("ToolCalls mismatch")
	}
}

func TestToLLMMessage_emptyFields(t *testing.T) {
	msg := &Message{Role: RoleAssistant, Content: "hi"}
	llm := msg.ToLLMMessage()
	if llm.Role != "assistant" || llm.Content != "hi" {
		t.Errorf("unexpected LLMMessage: %+v", llm)
	}
}

func TestToLLMMessage_ReasoningContent(t *testing.T) {
	msg := &Message{
		Role:             RoleAssistant,
		Content:          "visible text",
		ReasoningContent: "thinking text",
	}
	llm := msg.ToLLMMessage()
	if llm.ReasoningContent != "thinking text" {
		t.Errorf("ReasoningContent = %q, want %q", llm.ReasoningContent, "thinking text")
	}
}

// ============================================================
// ModelConfig 直接使用
// ============================================================

func TestModelConfig_DirectUse(t *testing.T) {
	cfg := &AgentConfig{
		Model: ModelConfig{
			Provider: "openai",
			ModelID:  "gpt-4",
			BaseURL:  "https://custom.api.com",
		},
	}
	if cfg.Model.Provider != "openai" {
		t.Errorf("Provider = %q, want openai", cfg.Model.Provider)
	}
	if cfg.Model.ModelID != "gpt-4" {
		t.Errorf("ModelID = %q, want gpt-4", cfg.Model.ModelID)
	}
	if cfg.Model.BaseURL != "https://custom.api.com" {
		t.Errorf("BaseURL = %q", cfg.Model.BaseURL)
	}
}

// ============================================================
// estimateTokens / estimateStringTokens
// ============================================================

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 1},
		{"a", 1},
		{"abc", 1},
		{"abcd", 2},
		{"hello world", 3},
		{"你好", 3},
		{"hello你好", 4},
		{"你好世界", 5},
		{"a你好b", 3},
	}
	for _, tt := range tests {
		got := estimateStringTokens(tt.input)
		if got != tt.want {
			t.Errorf("estimateStringTokens(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestEstimateTokensSum(t *testing.T) {
	total := estimateTokens("sys", []LLMMessage{
		{Content: "hello"},
		{Content: "world"},
	})
	sys := estimateStringTokens("sys")
	h := estimateStringTokens("hello")
	w := estimateStringTokens("world")
	want := sys + h + w
	if total != want {
		t.Errorf("estimateTokens = %d, want %d", total, want)
	}
}

func TestEstimateTokensEdgeCases(t *testing.T) {
	t.Run("empty_all", func(t *testing.T) {
		got := estimateTokens("", nil)
		if got != 1 {
			t.Errorf("empty estimateTokens = %d, want 1", got)
		}
	})
	t.Run("empty_messages", func(t *testing.T) {
		got := estimateTokens("", []LLMMessage{})
		if got != 1 {
			t.Errorf("empty estimateTokens = %d, want 1", got)
		}
	})
}

// ============================================================
// mustJSON
// ============================================================

func TestMustJSON(t *testing.T) {
	data := mustJSON(map[string]int{"a": 1})
	var m map[string]int
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m["a"] != 1 {
		t.Errorf("got %d, want 1", m["a"])
	}
}

// ============================================================
// ToolRegistry
// ============================================================

type testTool struct {
	name string
	desc string
}

func (t *testTool) Name() string                     { return t.name }
func (t *testTool) Description() string               { return t.desc }
func (t *testTool) Schema() map[string]interface{}     { return map[string]interface{}{"type": "object"} }
func (t *testTool) Execute(_ context.Context, args json.RawMessage) (*ToolResult, error) {
	return &ToolResult{Success: true, Data: "ok:" + string(args)}, nil
}

func TestToolRegistry_RegisterResolve(t *testing.T) {
	reg := NewToolRegistry()
	t1 := &testTool{name: "tool_a", desc: "Tool A"}
	t2 := &testTool{name: "tool_b", desc: "Tool B"}
	reg.Register(t1)
	reg.Register(t2)

	defs := reg.Resolve(context.Background())
	if len(defs) != 2 {
		t.Fatalf("got %d defs, want 2", len(defs))
	}
	m := map[string]string{}
	for _, d := range defs {
		m[d.Name] = d.Description
	}
	if m["tool_a"] != "Tool A" || m["tool_b"] != "Tool B" {
		t.Errorf("unexpected defs: %+v", m)
	}
}

func TestToolRegistry_Execute(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&testTool{name: "echo", desc: "Echo"})

	result, err := reg.Execute(context.Background(), "echo", json.RawMessage(`"hello"`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || result.Data != "ok:\"hello\"" {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestToolRegistry_ExecuteNotFound(t *testing.T) {
	reg := NewToolRegistry()
	_, err := reg.Execute(context.Background(), "nope", nil)
	if err == nil || !strings.Contains(err.Error(), "未找到") {
		t.Errorf("expected not-found error, got %v", err)
	}
}

// ============================================================
// SessionStore
// ============================================================

func TestSessionStore(t *testing.T) {
	workspace := t.TempDir()
	store := NewSessionStore(workspace)
	key := "test_user"

	t.Run("ArchiveSize_empty", func(t *testing.T) {
		if n := store.ArchiveSize(key); n != 0 {
			t.Errorf("expected 0, got %d", n)
		}
	})

	t.Run("Append_and_Load", func(t *testing.T) {
		msg1 := &Message{Role: RoleUser, Content: "你好"}
		msg2 := &Message{Role: RoleAssistant, Content: "你好！有什么可以帮助的？"}

		if err := store.AppendMessage(key, msg1); err != nil {
			t.Fatal(err)
		}
		if err := store.AppendMessage(key, msg2); err != nil {
			t.Fatal(err)
		}

		live, err := store.LoadLive(key)
		if err != nil {
			t.Fatal(err)
		}
		if len(live) != 2 {
			t.Fatalf("live len = %d, want 2", len(live))
		}
		if live[0].Content != "你好" || live[1].Content != "你好！有什么可以帮助的？" {
			t.Errorf("live content mismatch")
		}

		archive, err := store.LoadArchive(key)
		if err != nil {
			t.Fatal(err)
		}
		if len(archive) != 2 {
			t.Fatalf("archive len = %d, want 2", len(archive))
		}

		if n := store.ArchiveSize(key); n != 2 {
			t.Errorf("ArchiveSize = %d, want 2", n)
		}
	})

	t.Run("ReplaceLive", func(t *testing.T) {
		msgs := []*Message{
			{Role: RoleSystem, Content: "摘要消息"},
			{Role: RoleUser, Content: "新消息"},
		}
		if err := store.ReplaceLive(key, msgs); err != nil {
			t.Fatal(err)
		}
		live, err := store.LoadLive(key)
		if err != nil {
			t.Fatal(err)
		}
		if len(live) != 2 {
			t.Fatalf("after replace len = %d, want 2", len(live))
		}
		if live[0].Content != "摘要消息" {
			t.Errorf("first content = %q", live[0].Content)
		}
		if n := store.ArchiveSize(key); n != 2 {
			t.Errorf("ArchiveSize after replace = %d, want 2", n)
		}
	})

	t.Run("Meta_roundtrip", func(t *testing.T) {
		meta := &SessionMeta{Summary: "测试摘要", Count: 5, CreatedAt: "now"}
		if err := store.SaveMeta(key, meta); err != nil {
			t.Fatal(err)
		}
		loaded, err := store.LoadMeta(key)
		if err != nil {
			t.Fatal(err)
		}
		if loaded == nil {
			t.Fatal("meta is nil")
		}
		if loaded.Summary != "测试摘要" || loaded.Count != 5 || loaded.Key != key {
			t.Errorf("meta mismatch: %+v", loaded)
		}
		if loaded.UpdatedAt == "" {
			t.Errorf("UpdatedAt should be set")
		}
	})

	t.Run("LoadMeta_not_exists", func(t *testing.T) {
		meta, err := store.LoadMeta("nonexistent")
		if err != nil {
			t.Fatal(err)
		}
		if meta != nil {
			t.Errorf("expected nil, got %+v", meta)
		}
	})

	t.Run("different_key_isolation", func(t *testing.T) {
		key2 := "another_user"
		live, err := store.LoadLive(key2)
		if err != nil {
			t.Fatal(err)
		}
		if live != nil {
			t.Errorf("expected nil for unused key, got %+v", live)
		}
		if n := store.ArchiveSize(key2); n != 0 {
			t.Errorf("expected 0 archive size for unused key, got %d", n)
		}
	})
}

// ============================================================
// SessionStore non-existent file
// ============================================================

func TestSessionStore_LoadNonExistent(t *testing.T) {
	store := NewSessionStore(t.TempDir())
	live, err := store.LoadLive("nobody")
	if err != nil {
		t.Fatal(err)
	}
	if live != nil {
		t.Errorf("expected nil, got %+v", live)
	}
	archive, err := store.LoadArchive("nobody")
	if err != nil {
		t.Fatal(err)
	}
	if archive != nil {
		t.Errorf("expected nil, got %+v", archive)
	}
}

// ============================================================
// SessionStore EnsureDir
// ============================================================

func TestSessionStore_EnsureDir(t *testing.T) {
	store := NewSessionStore(t.TempDir())
	if err := store.EnsureDir("newkey"); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMessage("newkey", &Message{Role: RoleUser, Content: "test"}); err != nil {
		t.Fatal(err)
	}
}

// ============================================================
// StreamEvent helpers
// ============================================================

func TestTextDelta(t *testing.T) {
	e := TextDelta("hello")
	if e.Type != "text_delta" {
		t.Errorf("Type = %q", e.Type)
	}
	var s string
	if err := json.Unmarshal(e.Data, &s); err != nil {
		t.Fatal(err)
	}
	if s != "hello" {
		t.Errorf("data = %q", s)
	}
}

func TestFinishEvent(t *testing.T) {
	e := FinishEvent("done", map[string]int{"total": 100})
	if e.Type != "finish" {
		t.Errorf("Type = %q", e.Type)
	}
	var data struct {
		Content string         `json:"content"`
		Usage   map[string]int `json:"usage"`
	}
	if err := json.Unmarshal(e.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Content != "done" || data.Usage["total"] != 100 {
		t.Errorf("data = %+v", data)
	}
}

func TestErrorEvent(t *testing.T) {
	e := ErrorEvent("something went wrong")
	if e.Type != "error" {
		t.Errorf("Type = %q", e.Type)
	}
	var s string
	if err := json.Unmarshal(e.Data, &s); err != nil {
		t.Fatal(err)
	}
	if s != "something went wrong" {
		t.Errorf("data = %q", s)
	}
}

// ============================================================
// SessionScope validation (edge cases)
// ============================================================

func TestBuildSessionKey_dimensionValueEmpty(t *testing.T) {
	scope := SessionScope{
		AgentID:    "pico",
		Account:    "fallback",
		Dimensions: []string{"dim1", "dim2"},
		Values:     map[string]string{"dim1": "", "dim2": "real"},
	}
	key := BuildSessionKey(scope)
	if key == "" {
		t.Errorf("key should not be empty")
	}
}

// ============================================================
// NewProvider factory
// ============================================================

func TestNewProvider(t *testing.T) {
	tests := []struct {
		provider     string
		wantType     string
		wantDeepSeek bool
	}{
		{"anthropic", "*agent.AnthropicProvider", false},
		{"openai", "*agent.OpenAIProvider", false},
		{"deepseek", "*agent.OpenAIProvider", true},
		{"qwen", "*agent.OpenAIProvider", false},
		{"glm", "*agent.OpenAIProvider", false},
		{"unknown", "*agent.OpenAIProvider", false},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			p, err := NewProvider(tt.provider, "test-model", "", "sk-test")
			if err != nil {
				t.Fatal(err)
			}
			got := strings.TrimPrefix(strings.TrimPrefix(
				fmt.Sprintf("%T", p), "*agent."), "&agent.")
			if got != strings.TrimPrefix(tt.wantType, "*agent.") {
				t.Errorf("NewProvider(%q) = %T, want %s", tt.provider, p, tt.wantType)
			}
			if op, ok := p.(*OpenAIProvider); ok {
				isDeepSeek := op.providerType == "deepseek"
				if isDeepSeek != tt.wantDeepSeek {
					t.Errorf("NewProvider(%q) providerType = %q, want deepseek=%v", tt.provider, op.providerType, tt.wantDeepSeek)
				}
			}
		})
	}
}

// ============================================================
// CronParser integration (robfig/cron)
// ============================================================

func TestCronParseStandard(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		_, err := cron.ParseStandard("0 9 * * *")
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		_, err := cron.ParseStandard("bad")
		if err == nil {
			t.Errorf("expected parse error")
		}
	})
}
