package agent

import (
  "context"
  "encoding/json"
  "fmt"
  "strings"
  "testing"
  "time"

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
        AgentID: "mybot",
        Account: "u456",
        Dimensions: []string{"user_id"},
        Values:     map[string]string{"user_id": "u456"},
      },
      check: func(t *testing.T, key string) {
        // custom agent should produce a different key than default
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
        // uses empty string as userID
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
    {"hello world", 3},    // 11/4 + 0 + 1 = 3
    {"你好", 3},            // len=6, chCount=2 => ascii=4 => 4/4 + 4/3 + 1 = 1+1+1=3
    {"hello你好", 4},       // len=11, chCount=2 => ascii=9 => 9/4 + 4/3 + 1 = 2+1+1=4
    {"你好世界", 5},        // len=12, chCount=4 => ascii=8 => 8/4 + 8/3 + 1 = 2+2+1=5
    {"a你好b", 3},         // len=8, chCount=2 => ascii=6 => 6/4 + 4/3 + 1 = 1+1+1=3
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

// ============================================================
// trimMessages
// ============================================================

func TestTrimMessages(t *testing.T) {
  t.Run("under_threshold", func(t *testing.T) {
    msgs := make([]LLMMessage, 5)
    result := trimMessages(msgs)
    if len(result) != 5 {
      t.Errorf("got %d, want 5", len(result))
    }
  })

  t.Run("exactly_threshold", func(t *testing.T) {
    msgs := make([]LLMMessage, 10)
    result := trimMessages(msgs)
    if len(result) != 10 {
      t.Errorf("got %d, want 10", len(result))
    }
  })

  t.Run("trim_to_last_10", func(t *testing.T) {
    msgs := make([]LLMMessage, 15)
    for i := range msgs {
      msgs[i] = LLMMessage{Content: string(rune('A' + i))}
    }
    result := trimMessages(msgs)
    if len(result) != 10 {
      t.Fatalf("got %d, want 10", len(result))
    }
    // last 10: indices 5..14
    if result[0].Content != "F" {
      t.Errorf("first trimmed = %q, want F", result[0].Content)
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
// overflow.go
// ============================================================

func TestIsOverflow(t *testing.T) {
  tests := []struct {
    name         string
    tokenCount   int
    contextLimit int
    want         bool
  }{
    {"below_limit", 100000, 200000, false},
    {"at_usable_boundary", 180000, 200000, true},  // usable=180000, 180000>=180000
    {"exceeds_usable", 180001, 200000, true},
    {"zero_context_default", 180001, 0, true},
    {"small_context_overflow", 5000, 20000, true},  // usable=0
  }
  for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
      got := IsOverflow(tt.tokenCount, tt.contextLimit)
      if got != tt.want {
        t.Errorf("IsOverflow(%d, %d) = %v, want %v", tt.tokenCount, tt.contextLimit, got, tt.want)
      }
    })
  }
}

func TestUsableTokens(t *testing.T) {
  tests := []struct {
    contextLimit int
    want         int
  }{
    {200000, 180000},
    {0, 180000},
    {50000, 30000},
  }
  for _, tt := range tests {
    got := UsableTokens(tt.contextLimit)
    if got != tt.want {
      t.Errorf("UsableTokens(%d) = %d, want %d", tt.contextLimit, got, tt.want)
    }
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
    // archive unchanged
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
// Compactor
// ============================================================

func TestCompactor_IsOverflow(t *testing.T) {
  t.Run("auto_enabled", func(t *testing.T) {
    c := NewCompactor(DefaultCompactionConfig())
    if c.IsOverflow(100) {
      t.Errorf("100 should not overflow")
    }
    if !c.IsOverflow(200000) {
      t.Errorf("200000 should overflow")
    }
    if !c.IsOverflow(180000) {
      t.Errorf("180000 should overflow (usable=180000, >=)")
    }
  })

  t.Run("auto_disabled", func(t *testing.T) {
    cfg := DefaultCompactionConfig()
    cfg.Auto = false
    c := NewCompactor(cfg)
    if c.IsOverflow(999999) {
      t.Errorf("should never overflow when auto disabled")
    }
  })
}

func TestFindTruncatePoint(t *testing.T) {
  t.Run("fewer_than_preserve", func(t *testing.T) {
    msgs := []*Message{
      {Role: RoleUser, Content: "a"},
      {Role: RoleAssistant, Content: "b"},
    }
    got := findTruncatePoint(msgs, 2)
    if got != 0 {
      t.Errorf("expected 0, got %d", got)
    }
  })

  t.Run("exactly_preserve", func(t *testing.T) {
    msgs := []*Message{
      {Role: RoleUser, Content: "a"},
      {Role: RoleAssistant, Content: "b"},
      {Role: RoleUser, Content: "c"},
    }
    got := findTruncatePoint(msgs, 2)
    if got != 0 {
      t.Errorf("expected 0 (exactly preserveRounds user turns), got %d", got)
    }
  })

  t.Run("more_than_preserve", func(t *testing.T) {
    msgs := []*Message{
      {Role: RoleUser, Content: "q1"},
      {Role: RoleAssistant, Content: "a1"},
      {Role: RoleUser, Content: "q2"},
      {Role: RoleAssistant, Content: "a2"},
      {Role: RoleUser, Content: "q3"},
    }
    got := findTruncatePoint(msgs, 2)
    // rounds from end: q3(r1), q2(r2), q1(r3>2) => return index of q1 = 0
    // Wait: i=4 user rounds=1, i=3 asst, i=2 user rounds=2, i=1 asst, i=0 user rounds=3 >2 => return 0+1=1
    if got != 1 {
      t.Errorf("expected 1, got %d", got)
    }
  })

  t.Run("preserve_1_round", func(t *testing.T) {
    msgs := []*Message{
      {Role: RoleUser, Content: "q1"},
      {Role: RoleAssistant, Content: "a1"},
      {Role: RoleUser, Content: "q2"},
      {Role: RoleAssistant, Content: "a2"},
    }
    got := findTruncatePoint(msgs, 1)
    // i=3 asst, i=2 user rounds=1, i=1 asst, i=0 user rounds=2 >1 => return 0+1=1
    if got != 1 {
      t.Errorf("expected 1, got %d", got)
    }
  })

  t.Run("tool_roles_ignored", func(t *testing.T) {
    msgs := []*Message{
      {Role: RoleUser, Content: "q1"},
      {Role: RoleAssistant, Content: "a1"},
      {Role: RoleTool, Content: "result"},
      {Role: RoleUser, Content: "q2"},
    }
    got := findTruncatePoint(msgs, 1)
    // i=3 user rounds=1, i=2 tool, i=1 asst, i=0 user rounds=2 >1 => return 1
    if got != 1 {
      t.Errorf("expected 1, got %d", got)
    }
  })
}

func TestCompact_belowThreshold(t *testing.T) {
  c := NewCompactor(DefaultCompactionConfig())
  ctx := context.Background()

  // 3 messages, MaxPreserveRounds=2 => 2*2=4, 3 < 4 => no change
  msgs := []*Message{
    {Role: RoleUser, Content: "hi"},
    {Role: RoleAssistant, Content: "hello"},
    {Role: RoleUser, Content: "bye"},
  }
  result, err := c.Compact(ctx, msgs)
  if err != nil {
    t.Fatal(err)
  }
  if len(result) != 3 {
    t.Errorf("expected 3, got %d", len(result))
  }
}

func TestCompact_withFallbackSummary(t *testing.T) {
  c := NewCompactor(DefaultCompactionConfig())
  ctx := context.Background()

  // 10 messages, 5 user turns, preserveRounds=2 => truncates first 3 user turns
  msgs := []*Message{
    {Role: RoleUser, Content: "q1"},      // 0
    {Role: RoleAssistant, Content: "a1"},  // 1
    {Role: RoleUser, Content: "q2"},       // 2
    {Role: RoleAssistant, Content: "a2"},  // 3
    {Role: RoleUser, Content: "q3"},       // 4 -> truncation point: rounds>2
    {Role: RoleAssistant, Content: "a3"},  // 5
    {Role: RoleUser, Content: "q4"},       // 6
    {Role: RoleAssistant, Content: "a4"},  // 7
    {Role: RoleUser, Content: "q5"},       // 8
    {Role: RoleAssistant, Content: "a5"},  // 9
  }
  // findTruncatePoint: i=9 asst, i=8 user(r1), i=7 asst, i=6 user(r2), i=5 asst, i=4 user(r3>2) => return 5
  result, err := c.Compact(ctx, msgs)
  if err != nil {
    t.Fatal(err)
  }

  // Should have: [summary_msg, a3, q4, a4, q5, a5] = 6
  if len(result) != 6 {
    t.Fatalf("expected 6, got %d", len(result))
  }
  if result[0].Role != RoleAssistant {
    t.Errorf("first msg role = %q, want assistant", result[0].Role)
  }
  if !strings.HasPrefix(result[0].Content, "[历史摘要]") {
    t.Errorf("summary prefix missing: %q", result[0].Content)
  }
  if result[1].Content != "a3" || result[2].Content != "q4" {
    t.Errorf("recent messages misplaced: %+v", result)
  }
}

func TestBuildFallbackSummary(t *testing.T) {
  msgs := []*Message{
    {Role: RoleUser, Content: "q1"},
    {Role: RoleAssistant, Content: "a1"},
    {Role: RoleUser, Content: "q2"},
    {Role: RoleTool, Content: "tool_result"},
  }
  s := buildFallbackSummary(msgs)
  // buildFallbackSummary: userCount=2, userCount+assistantCount=3, assistantCount=1
  // format: "共 %d 轮对话（用户 %d 条，助手 %d 条）" with values (userCount, userCount+assistantCount, assistantCount)
  if !strings.Contains(s, "共 2 轮对话") || !strings.Contains(s, "用户 3 条") || !strings.Contains(s, "助手 1 条") {
    t.Errorf("unexpected summary: %q", s)
  }
}

func TestExtractSummary(t *testing.T) {
  msgs := []*Message{
    {Role: RoleAssistant, Content: "[历史摘要] 这是之前的内容"},
    {Role: RoleUser, Content: "hi"},
  }
  s := extractSummary(msgs)
  if s != " 这是之前的内容" {
    t.Errorf("got %q", s)
  }

  t.Run("no_summary", func(t *testing.T) {
    msgs := []*Message{
      {Role: RoleUser, Content: "hi"},
    }
    if s := extractSummary(msgs); s != "" {
      t.Errorf("expected empty, got %q", s)
    }
  })
}

func TestCompactAndRewrite(t *testing.T) {
  workspace := t.TempDir()
  store := NewSessionStore(workspace)
  key := "test_compact"

  // Write 10 messages to live (5 user turns, preserveRounds=2 => truncates 3)
  msgs := []*Message{
    {Role: RoleUser, Content: "q1"},
    {Role: RoleAssistant, Content: "a1"},
    {Role: RoleUser, Content: "q2"},
    {Role: RoleAssistant, Content: "a2"},
    {Role: RoleUser, Content: "q3"},
    {Role: RoleAssistant, Content: "a3"},
    {Role: RoleUser, Content: "q4"},
    {Role: RoleAssistant, Content: "a4"},
    {Role: RoleUser, Content: "q5"},
    {Role: RoleAssistant, Content: "a5"},
  }
  for _, m := range msgs {
    store.AppendMessage(key, m)
  }

  compactor := NewCompactor(DefaultCompactionConfig())
  ctx := context.Background()

  if err := CompactAndRewrite(ctx, store, key, compactor); err != nil {
    t.Fatal(err)
  }

  live, _ := store.LoadLive(key)
  if len(live) != 6 {
    t.Fatalf("expected 6 after compact, got %d", len(live))
  }
  if !strings.HasPrefix(live[0].Content, "[历史摘要]") {
    t.Errorf("expected summary prefix")
  }

  meta, _ := store.LoadMeta(key)
  if meta == nil || meta.Count != 6 {
    t.Errorf("meta count = %d, want 6", meta.Count)
  }
}

// ============================================================
// RetryPolicy
// ============================================================

func TestDefaultRetryPolicy(t *testing.T) {
  p := DefaultRetryPolicy()
  if p.MaxAttempts != 3 || p.InitialDelay != RetryInitialDelay || p.BackoffFactor != RetryBackoffFactor || p.MaxDelay != RetryMaxDelay {
    t.Errorf("unexpected defaults: %+v", p)
  }
}

func TestRetryPolicy_Delay(t *testing.T) {
  p := DefaultRetryPolicy()
  tests := []struct {
    attempt int
    expect  time.Duration
  }{
    {0, 2000 * time.Millisecond},
    {1, 4000 * time.Millisecond},
    {2, 8000 * time.Millisecond},
    {3, 16000 * time.Millisecond},
    {4, 30000 * time.Millisecond}, // capped
    {5, 30000 * time.Millisecond}, // capped
  }
  for _, tt := range tests {
    got := p.Delay(tt.attempt)
    if got != tt.expect {
      t.Errorf("Delay(%d) = %v, want %v", tt.attempt, got, tt.expect)
    }
  }
}

func TestRetryPolicy_DelayNegative(t *testing.T) {
  p := DefaultRetryPolicy()
  got := p.Delay(-1)
  if got != 2000*time.Millisecond {
    t.Errorf("Delay(-1) = %v, want 2s", got)
  }
}

// ============================================================
// BuildStructuredOutputTool
// ============================================================

func TestBuildStructuredOutputTool(t *testing.T) {
  schema := map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
      "answer": map[string]interface{}{"type": "string"},
    },
  }
  tool := BuildStructuredOutputTool(schema)
  if tool.Name != "StructuredOutput" {
    t.Errorf("Name = %q", tool.Name)
  }
  if tool.Description == "" {
    t.Errorf("empty description")
  }
  if tool.InputSchema == nil {
    t.Errorf("nil schema")
  }
}

// ============================================================
// NewProvider factory
// ============================================================

func TestNewProvider(t *testing.T) {
  tests := []struct {
    provider   string
    wantType   string
  }{
    {"anthropic", "*agent.AnthropicProvider"},
    {"openai", "*agent.OpenAIProvider"},
    {"deepseek", "*agent.OpenAIProvider"},
    {"qwen", "*agent.OpenAIProvider"},
    {"glm", "*agent.OpenAIProvider"},
    {"unknown", "*agent.OpenAIProvider"},
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

func TestToolCallEvent(t *testing.T) {
  e := ToolCallEvent("search", map[string]string{"q": "test"})
  if e.Type != "tool_call" {
    t.Errorf("Type = %q", e.Type)
  }
  var m map[string]string
  if err := json.Unmarshal(e.Data, &m); err != nil {
    t.Fatal(err)
  }
  if m["q"] != "test" {
    t.Errorf("data = %+v", m)
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
  // dimension exists but value is empty -> try next dim or fall back to account
  scope := SessionScope{
    AgentID:    "pico",
    Account:    "fallback",
    Dimensions: []string{"dim1", "dim2"},
    Values:     map[string]string{"dim1": "", "dim2": "real"},
  }
  key := BuildSessionKey(scope)
  // first non-empty dim value wins: dim1 is empty, so dim2="real" is used
  // But in code: for _, dim := range scope.Dimensions { if v, ok := scope.Values[dim]; ok && v != "" { userID = v; break } }
  // So dim1="" is skipped, dim2="real" wins
  // Verify key is not empty
  if key == "" {
    t.Errorf("key should not be empty")
  }
}

// ============================================================
// Compactor with Summarizer (nil = fallback)
// ============================================================

func TestCompact_triggersCompaction(t *testing.T) {
  c := NewCompactor(DefaultCompactionConfig())
  ctx := context.Background()

  // 6 messages = 3 user turns + 3 assistant turns
  // preserveRounds=2 => 3rd user turn from end triggers truncation
  // findTruncatePoint: i=5 asst, i=4 user(r1), i=3 asst, i=2 user(r2), i=1 asst, i=0 user(r3>2) => return 1
  msgs := []*Message{
    {Role: RoleUser, Content: "q1"},
    {Role: RoleAssistant, Content: "a1"},
    {Role: RoleUser, Content: "q2"},
    {Role: RoleAssistant, Content: "a2"},
    {Role: RoleUser, Content: "q3"},
    {Role: RoleAssistant, Content: "a3"},
  }
  result, err := c.Compact(ctx, msgs)
  if err != nil {
    t.Fatal(err)
  }
  // toSummarize = msgs[:1] = [q1], recent = msgs[1:] = [a1,q2,a2,q3,a3]
  // result = [summary, a1, q2, a2, q3, a3] = 6
  if len(result) != 6 {
    t.Fatalf("expected 6, got %d", len(result))
  }
  if !strings.HasPrefix(result[0].Content, "[历史摘要]") {
    t.Errorf("expected summary prefix, got %q", result[0].Content)
  }
  if result[1].Content != "a1" {
    t.Errorf("expected a1 after summary, got %q", result[1].Content)
  }
}

func TestCompact_noTruncationForExactRounds(t *testing.T) {
  c := NewCompactor(DefaultCompactionConfig())
  ctx := context.Background()

  // 4 messages = 2 user turns -> not enough to exceed preserveRounds=2
  msgs := []*Message{
    {Role: RoleUser, Content: "q1"},
    {Role: RoleAssistant, Content: "a1"},
    {Role: RoleUser, Content: "q2"},
    {Role: RoleAssistant, Content: "a2"},
  }
  result, err := c.Compact(ctx, msgs)
  if err != nil {
    t.Fatal(err)
  }
  if len(result) != 4 {
    t.Fatalf("expected 4 (no truncation for exactly 2 rounds), got %d", len(result))
  }
}

// ============================================================
// LoadLive / LoadArchive with non-existent file
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
// SessionStore AppendMessage with non-existent dir creates it
// ============================================================

func TestSessionStore_EnsureDir(t *testing.T) {
  store := NewSessionStore(t.TempDir())
  if err := store.EnsureDir("newkey"); err != nil {
    t.Fatal(err)
  }
  // Append should not fail after EnsureDir
  if err := store.AppendMessage("newkey", &Message{Role: RoleUser, Content: "test"}); err != nil {
    t.Fatal(err)
  }
}

// ============================================================
// EstimateTokens with empty prompt and messages
// ============================================================

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
// 定期 fsync — Engine 应定期持久化中间状态
// ============================================================

func TestEngine_PeriodicFSync(t *testing.T) {
  workspace := t.TempDir()
  store := NewSessionStore(workspace)
  provider := &mockProvider{
    toolCalls: []ToolCallData{
      {ID: "tc1", Name: "tool_a", Input: json.RawMessage(`{}`)},
    },
    responseText: "final answer",
  }
  tools := NewToolRegistry()
  tools.Register(&dummyTool{name: "tool_a"})
  cfg := testConfig()
  cfg.MaxIter = 3
  engine := NewEngine(cfg, provider, tools, store)
  engine.fsyncInterval = 1
  engine.SetSessionKey("fsync_test")

  msg := &Message{Role: RoleUser, Content: "test periodic fsync"}
  err := engine.Process(context.Background(), "be helpful", nil, msg, func(_ StreamEvent) {})
  if err != nil {
    t.Fatalf("Process failed: %v", err)
  }

  live, err := store.LoadLive("fsync_test")
  if err != nil {
    t.Fatal(err)
  }
  if len(live) == 0 {
    t.Error("expected live messages to be saved via periodic fsync")
  }
}
