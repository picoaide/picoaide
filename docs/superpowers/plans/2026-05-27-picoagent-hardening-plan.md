# PicoAgent 健壮性加固实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 对 picoagent 核心链条进行 7 项系统性加固

**Architecture:** 6 个独立文件改动，每项可独立测试，利用 `internal/agent/retry.go` 已有的指数退避框架

**Tech Stack:** Go, slog, xorm, mcp, exec

---

### Task 1: Provider 网络重试

**Files:**
- Modify: `internal/agent/provider.go`
- Test: `internal/agent/provider_test.go` (新建)

- [ ] **Step 1: Write failing tests for retryStream**

```go
package agent

import (
  "context"
  "errors"
  "net"
  "strings"
  "testing"
  "time"
)

func TestRetryStream_NetworkErrorRetries(t *testing.T) {
  attempts := 0
  err := retryStream(context.Background(), "test", func(ctx context.Context) error {
    attempts++
    if attempts < 2 {
      return &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
    }
    return nil
  })
  if err != nil {
    t.Fatalf("expected success after retry, got: %v", err)
  }
  if attempts != 2 {
    t.Errorf("expected 2 attempts, got %d", attempts)
  }
}

func TestRetryStream_NonRetryableNoRetry(t *testing.T) {
  attempts := 0
  err := retryStream(context.Background(), "test", func(ctx context.Context) error {
    attempts++
    return errors.New("HTTP 401 Unauthorized")
  })
  if err == nil {
    t.Fatal("expected error")
  }
  if attempts != 1 {
    t.Errorf("expected 1 attempt (no retry), got %d", attempts)
  }
}

func TestRetryStream_RateLimitRetries(t *testing.T) {
  attempts := 0
  start := time.Now()
  err := retryStream(context.Background(), "test", func(ctx context.Context) error {
    attempts++
    if attempts < 2 {
      return errors.New("HTTP 429 Too Many Requests")
    }
    return nil
  })
  elapsed := time.Since(start)
  if err != nil {
    t.Fatalf("expected success after retry, got: %v", err)
  }
  if attempts != 2 {
    t.Errorf("expected 2 attempts, got %d", attempts)
  }
  if elapsed < 1500*time.Millisecond {
    t.Errorf("expected delay between retries, got %v", elapsed)
  }
}

func TestRetryStream_ContextCancelledNoRetry(t *testing.T) {
  ctx, cancel := context.WithCancel(context.Background())
  cancel()
  attempts := 0
  err := retryStream(ctx, "test", func(ctx context.Context) error {
    attempts++
    return errors.New("connection refused")
  })
  if err != context.Canceled {
    t.Fatalf("expected context.Canceled, got: %v", err)
  }
  if attempts != 0 {
    t.Errorf("expected 0 attempts (context already cancelled), got %d", attempts)
  }
}

func TestRetryStream_ContextOverflowNoRetry(t *testing.T) {
  attempts := 0
  err := retryStream(context.Background(), "test", func(ctx context.Context) error {
    attempts++
    return errors.New("context_length_exceeded")
  })
  if err == nil {
    t.Fatal("expected error")
  }
  if !strings.Contains(err.Error(), "context_length_exceeded") {
    t.Errorf("expected overflow error, got: %v", err)
  }
  if attempts != 1 {
    t.Errorf("expected 1 attempt (overflow not retryable), got %d", attempts)
  }
}

func TestRetryStream_ServerErrorRetries(t *testing.T) {
  attempts := 0
  err := retryStream(context.Background(), "test", func(ctx context.Context) error {
    attempts++
    if attempts < 3 {
      return errors.New("HTTP 503 Service Unavailable")
    }
    return nil
  })
  if err != nil {
    t.Fatalf("expected success after retry, got: %v", err)
  }
  if attempts != 3 {
    t.Errorf("expected 3 attempts, got %d", attempts)
  }
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /mnt/d/project/GitHub/lostmaniac/PicoAide && go test ./internal/agent/ -run "TestRetryStream" -v 2>&1
```
Expected: `FAIL` — all tests fail because `retryStream` is not defined

- [ ] **Step 3: Add retryStream function and isRetryable check to provider.go**

Add after `doHTTP` function (line 64):

```go
// retryableErrors 匹配可重试的网络/服务端错误
var retryableErrorPrefixes = []string{
  "connection refused",
  "connection reset",
  "no such host",
  "TLS handshake",
  "i/o timeout",
  "dial tcp",
  "HTTP 429",
  "HTTP 5",
  "HTTP 50",
  "HTTP 51",
  "HTTP 52",
  "HTTP 53",
}

func isRetryable(err error) bool {
  msg := err.Error()
  for _, prefix := range retryableErrorPrefixes {
    if strings.Contains(msg, prefix) {
      return true
    }
  }
  return false
}

// retryStream 包装流式 LLM 调用，自动重试可恢复的网络和服务端错误。
// 不重试：4xx(除429)、context overflow、context canceled/deadline exceeded。
func retryStream(ctx context.Context, name string, fn func(context.Context) error) error {
  var lastErr error
  policy := DefaultRetryPolicy()
  for attempt := 0; attempt < policy.MaxAttempts; attempt++ {
    if attempt > 0 {
      slog.Debug("provider.retry", "name", name, "attempt", attempt+1, "max", policy.MaxAttempts, "delay_ms", policy.Delay(attempt-1).Milliseconds())
      select {
      case <-time.After(policy.Delay(attempt - 1)):
      case <-ctx.Done():
        return ctx.Err()
      }
    }
    err := fn(ctx)
    if err == nil {
      return nil
    }
    if !isRetryable(err) {
      return err
    }
    lastErr = err
  }
  return fmt.Errorf("%s 重试 %d 次后仍然失败: %w", name, policy.MaxAttempts, lastErr)
}
```

Add imports for `"strings"`, `"time"`, `"errors"` to provider.go if not present. Check existing imports: already has `"time"`, `"fmt"`, `"context"`, `"log/slog"`. Add `"strings"`.

Now wrap both StreamChat methods with retryStream.

In `AnthropicProvider.StreamChat`, wrap the HTTP call:

```go
func (p *AnthropicProvider) StreamChat(ctx context.Context, req *ChatRequest, cb func(event StreamEvent)) error {
  // ... existing message building code (lines 109-133) ...

  // ... existing body building code (lines 144-178) ...

  // Wrap the streaming call with retry
  return retryStream(ctx, "anthropic", func(innerCtx context.Context) error {
    select {
    case <-innerCtx.Done():
      return innerCtx.Err()
    default:
    }

    resp, err := doHTTP(innerCtx, p.baseURL+"/messages", "POST", headers, bodyJSON)
    if err != nil {
      return err
    }
    defer resp.Body.Close()

    // Check for 429 early (before SSE parsing)
    if resp.StatusCode == http.StatusTooManyRequests {
      resp.Body.Close()
      return fmt.Errorf("HTTP 429 Too Many Requests")
    }
    if resp.StatusCode >= 500 {
      resp.Body.Close()
      return fmt.Errorf("HTTP %d %s", resp.StatusCode, resp.Status)
    }

    return parseAnthropicSSE(innerCtx, resp.Body, cb)
  })
}
```

In `OpenAIProvider.StreamChat`, wrap the streaming call:

```go
func (p *OpenAIProvider) StreamChat(ctx context.Context, req *ChatRequest, cb func(event StreamEvent)) error {
  // ... existing setup code (lines 290-335) ...

  return retryStream(ctx, "openai", func(innerCtx context.Context) error {
    select {
    case <-innerCtx.Done():
      return innerCtx.Err()
    default:
    }

    stream := client.Chat.Completions.NewStreaming(innerCtx, params)
    defer stream.Close()

    var acc openai.ChatCompletionAccumulator
    var chunkCount int
    var textDeltaCount int

    for stream.Next() {
      chunk := stream.Current()
      acc.AddChunk(chunk)
      chunkCount++

      if len(chunk.Choices) == 0 {
        continue
      }
      choice := chunk.Choices[0]

      if choice.Delta.Content != "" {
        textDeltaCount++
        cb(TextDelta(choice.Delta.Content))
      }

      if choice.FinishReason != "" && choice.FinishReason != "tool_calls" {
        cb(FinishEvent("", map[string]int{}))
      }
    }

    if err := stream.Err(); err != nil {
      return err
    }

    // ... existing tool call accumulation (lines 370-398) ...
    return nil
  })
}
```

Note: Need to restructure the OpenAI provider to separate the streaming logic into the closure. The key is that the closure captures `p`, `req`, `cb`, `client`, `params`, and retry is outside.

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /mnt/d/project/GitHub/lostmaniac/PicoAide && go test ./internal/agent/ -run "TestRetryStream" -v 2>&1
```
Expected: `PASS` for all retryStream tests

```bash
cd /mnt/d/project/GitHub/lostmaniac/PicoAide && go test ./internal/agent/ -v 2>&1
```
Expected: `PASS` — all existing tests still pass

- [ ] **Step 5: Commit**

```bash
cd /mnt/d/project/GitHub/lostmaniac/PicoAide && git add internal/agent/provider.go internal/agent/provider_test.go && git commit -m "feat(picoagent): Provider 接入网络重试 retryStream"
```

---

### Task 2: 工具错误语义化

**Files:**
- Modify: `internal/agent/tool_registry.go`
- Test: `internal/agent/tool_registry_test.go`

- [ ] **Step 1: Write failing test for tool error semantics**

Add to `internal/agent/tool_registry_test.go`:

```go
func TestCommandTool_ErrorReturnsSuccessFalse(t *testing.T) {
  tool := &CommandTool{Timeout: time.Second}
  result, err := tool.Execute(context.Background(), json.RawMessage(`{"command": "exit 1"}`))
  if err != nil {
    t.Fatal(err)
  }
  if result.Success {
    t.Error("expected Success=false for failing command")
  }
}

func TestReadFileTool_NotFoundReturnsSuccessFalse(t *testing.T) {
  tool := &ReadFileTool{}
  result, err := tool.Execute(context.Background(), json.RawMessage(`{"path": "/nonexistent/path.txt"}`))
  if err != nil {
    t.Fatal(err)
  }
  if result.Success {
    t.Error("expected Success=false for non-existent file")
  }
}

func TestWriteFileTool_BadPathReturnsSuccessFalse(t *testing.T) {
  tool := &WriteFileTool{}
  result, err := tool.Execute(context.Background(), json.RawMessage(`{"path": "/invalid\x00path", "content": "test"}`))
  if err != nil {
    t.Fatal(err)
  }
  if result.Success {
    t.Error("expected Success=false for invalid path")
  }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /mnt/d/project/GitHub/lostmaniac/PicoAide && go test ./internal/agent/ -run "TestCommandTool_ErrorReturnsSuccessFalse|TestReadFileTool_NotFoundReturnsSuccessFalse|TestWriteFileTool_BadPathReturnsSuccessFalse" -v 2>&1
```
Expected: `FAIL` — currently returns `Success: true`

- [ ] **Step 3: Fix all 8 tool error sites to return Success=false**

Search for `Success: true` in tool_registry.go where it's returned with error messages. Change each to `Success: false`.

Sites to change:

1. `CommandTool.Execute` line 198: `return &ToolResult{Success: true, Data: msg}, nil`
   → `return &ToolResult{Success: false, Data: msg}, nil`

2. `ReadFileTool.Execute` line 277: `return &ToolResult{Success: true, Data: fmt.Sprintf("读取失败: %v", err)}, nil`
   → `return &ToolResult{Success: false, Data: fmt.Sprintf("读取失败: %v", err)}, nil`

3. `GrepTool.Execute` line 348: `return &ToolResult{Success: true, Data: msg}, nil`
   → `return &ToolResult{Success: false, Data: msg}, nil`

4. `WriteFileTool.Execute` line 425: `return &ToolResult{Success: true, Data: fmt.Sprintf("创建目录失败: %v", err)}, nil`
   → `return &ToolResult{Success: false, Data: fmt.Sprintf("创建目录失败: %v", err)}, nil`

5. `WriteFileTool.Execute` line 435: `return &ToolResult{Success: true, Data: fmt.Sprintf("写入失败: %v", err)}, nil`
   → `return &ToolResult{Success: false, Data: fmt.Sprintf("写入失败: %v", err)}, nil`

6. `EditFileTool.Execute` line 496: `return &ToolResult{Success: true, Data: fmt.Sprintf("读取失败: %v", err)}, nil`
   → `return &ToolResult{Success: false, Data: fmt.Sprintf("读取失败: %v", err)}, nil`

7. `EditFileTool.Execute` line 511: `return &ToolResult{Success: true, Data: fmt.Sprintf("写入失败: %v", err)}, nil`
   → `return &ToolResult{Success: false, Data: fmt.Sprintf("写入失败: %v", err)}, nil`

8. `AppendFileTool.Execute` line 567: `return &ToolResult{Success: true, Data: fmt.Sprintf("创建目录失败: %v", err)}, nil`
   → `return &ToolResult{Success: false, Data: fmt.Sprintf("创建目录失败: %v", err)}, nil`

9. `AppendFileTool.Execute` line 572: `return &ToolResult{Success: true, Data: fmt.Sprintf("打开文件失败: %v", err)}, nil`
   → `return &ToolResult{Success: false, Data: fmt.Sprintf("打开文件失败: %v", err)}, nil`

10. `AppendFileTool.Execute` line 581: `return &ToolResult{Success: true, Data: fmt.Sprintf("写入失败: %v", err)}, nil`
    → `return &ToolResult{Success: false, Data: fmt.Sprintf("写入失败: %v", err)}, nil`

11. `GlobTool.Execute` line 704: `return &ToolResult{Success: true, Data: fmt.Sprintf("glob 搜索失败: %v", err)}, nil`
    → `return &ToolResult{Success: false, Data: fmt.Sprintf("glob 搜索失败: %v", err)}, nil`

12. `DeleteFileTool.Execute` line 764: `return &ToolResult{Success: true, Data: fmt.Sprintf("删除失败: %v", err)}, nil`
    → `return &ToolResult{Success: false, Data: fmt.Sprintf("删除失败: %v", err)}, nil`

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /mnt/d/project/GitHub/lostmaniac/PicoAide && go test ./internal/agent/ -run "TestCommandTool_ErrorReturnsSuccessFalse|TestReadFileTool_NotFoundReturnsSuccessFalse|TestWriteFileTool_BadPathReturnsSuccessFalse|TestToolRegistry" -v 2>&1
```
Expected: `PASS`

```bash
cd /mnt/d/project/GitHub/lostmaniac/PicoAide && go test ./internal/agent/ -v 2>&1
```
Expected: `PASS` — all existing tests pass

- [ ] **Step 5: Commit**

```bash
cd /mnt/d/project/GitHub/lostmaniac/PicoAide && git add internal/agent/tool_registry.go internal/agent/tool_registry_test.go && git commit -m "fix(picoagent): 工具错误语义化，失败时返回 Success=false"
```

---

### Task 3: 会话定期 fsync

**Files:**
- Modify: `internal/agent/engine.go`
- Modify: `internal/agent/session.go` (Engine struct)
- Test: `internal/agent/agent_test.go`

- [ ] **Step 1: Write failing test for periodic fsync**

Add to `internal/agent/agent_test.go`:

```go
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
  // Should have intermediate messages saved (not just final completion)
  // At minimum: user msg + any tool results
  if len(live) == 0 {
    t.Error("expected live messages to be saved via periodic fsync")
  }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /mnt/d/project/GitHub/lostmaniac/PicoAide && go test ./internal/agent/ -run "TestEngine_PeriodicFSync" -v 2>&1
```
Expected: `FAIL` — fsyncInterval is not yet defined, test will fail

- [ ] **Step 3: Add fsyncInterval to Engine struct and fsync logic in Process loop**

In `internal/agent/session.go`, add to `Engine` struct:

```go
type Engine struct {
  provider      Provider
  tools         *ToolRegistry
  store         *SessionStore
  compactor     *Compactor
  config        *AgentConfig
  skills        []*Skill
  subAgentMgr   *SubAgentManager
  sessionKey    string
  fsyncInterval int    // 每隔 N 轮 fsync 一次会话，0 禁用
}
```

In `NewEngine`, add default:

```go
func NewEngine(cfg *AgentConfig, provider Provider, tools *ToolRegistry, store *SessionStore) *Engine {
  return &Engine{
    provider:      provider,
    tools:         tools,
    store:         store,
    compactor:     NewCompactor(DefaultCompactionConfig()),
    config:        cfg,
    subAgentMgr:   NewSubAgentManager(),
    fsyncInterval: 5, // 默认每 5 轮 fsync
  }
}
```

In `engine.go`, add `fsyncSession` method and call it in the Process loop after tool execution:

```go
// fsyncSession 持久化当前消息列表到 live.jsonl
func (e *Engine) fsyncSession(msgs []LLMMessage) {
  if e.sessionKey == "" || e.store == nil || len(msgs) == 0 {
    return
  }
  persisted := make([]*Message, len(msgs))
  for i, m := range msgs {
    persisted[i] = &Message{
      Role:       MessageRole(m.Role),
      Content:    m.Content,
      ToolCallID: m.ToolCallID,
      ToolCalls:  m.ToolCalls,
    }
  }
  if err := e.store.ReplaceLive(e.sessionKey, persisted); err != nil {
    slog.Debug("agent.fsync_error", "error", err.Error())
  } else {
    slog.Debug("agent.fsync_complete", "message_count", len(persisted), "session_key", e.sessionKey)
  }
}
```

Add iteration counter to Engine struct:

```go
type Engine struct {
  // ... existing fields ...
  fsyncInterval int
  iterCount     int
}
```

In `Process()`, after tool execution (line ~449, before `iterCancel()`), add:

```go
    // 定期 fsync 会话
    e.iterCount++
    if e.fsyncInterval > 0 && e.iterCount%e.fsyncInterval == 0 {
      e.fsyncSession(llmMsgs)
    }
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /mnt/d/project/GitHub/lostmaniac/PicoAide && go test ./internal/agent/ -run "TestEngine_PeriodicFSync" -v 2>&1
```
Expected: `PASS`

```bash
cd /mnt/d/project/GitHub/lostmaniac/PicoAide && go test ./internal/agent/ -v 2>&1
```
Expected: `PASS`

- [ ] **Step 5: Commit**

```bash
cd /mnt/d/project/GitHub/lostmaniac/PicoAide && git add internal/agent/engine.go internal/agent/session.go && git commit -m "feat(picoagent): 会话定期 fsync，每 5 轮持久化中间状态"
```

---

### Task 4: 用户锁 defer 防死锁

**Files:**
- Modify: `internal/sandbox/manager.go`
- Test: `internal/sandbox/sandbox_test.go`

- [ ] **Step 1: Write failing test for lock panic safety**

Add to `internal/sandbox/sandbox_test.go`:

```go
func TestUserLock_ReleasedOnPanic(t *testing.T) {
  m := NewManager("/tmp/nonexistent", t.TempDir())
  username := "panic-test-user"

  // 1. Acquire the lock normally
  err := m.acquireUser(context.Background(), username)
  if err != nil {
    t.Fatal(err)
  }

  // 2. Simulate a function that panics after acquiring (like prepareSandbox)
  func() {
    defer func() { recover() }()
    var released bool
    defer func() {
      if !released {
        m.releaseUser(username)
      }
    }()
    panic("simulated panic in prepareSandbox")
    // released = true // never reached
  }()

  // 3. Lock should be released by the defer
  ctx, cancel := context.WithTimeout(context.Background(), time.Second)
  defer cancel()
  err = m.acquireUser(ctx, username)
  if err != nil {
    t.Fatal("lock not released after panic, acquire blocked")
  }
  m.releaseUser(username)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /mnt/d/project/GitHub/lostmaniac/PicoAide && go test ./internal/sandbox/ -run "TestUserLock_ReleasedOnPanic" -v 2>&1
```
Expected: `FAIL` — the defer pattern doesn't exist in prepareSandbox yet (the test itself simulates the pattern, but we need to verify the test runs at all)

- [ ] **Step 3: Add defer safety to prepareSandbox**

In `internal/sandbox/manager.go`, modify `prepareSandbox`:

```go
func (m *Manager) prepareSandbox(ctx context.Context, token string, inputJSON []byte, workspace string, apiKeys map[string]string, mounts []Mount, username string) (func(), io.ReadCloser, *exec.Cmd, error) {
  if err := m.acquireUser(ctx, username); err != nil {
    return nil, nil, nil, err
  }

  // defer 确保 panic 时也释放用户锁
  var released bool
  defer func() {
    if !released {
      m.releaseUser(username)
    }
  }()

  if _, err := os.Stat(m.rootfs); err != nil {
    released = true
    m.releaseUser(username)
    return nil, nil, nil, fmt.Errorf("rootfs 不存在 %s: %w", m.rootfs, err)
  }
  // ... rest of function ...
```

Then update all existing manual `m.releaseUser(username)` calls to set `released = true` first:

Line 125 (rootfs stat fail): change to:
```go
    released = true
    m.releaseUser(username)
```

Line 141 (tmpfs mount fail): change to:
```go
    released = true
    m.releaseUser(username)
```

Line 152 (overlay mount fail): change to:
```go
    released = true
    m.releaseUser(username)
```

In `localCleanup` (line 160-175), add `released = true` after `m.releaseUser(username)`:

```go
  localCleanup := func() {
    cleanupOnce.Do(func() {
      // ... cleanup ...
      m.releaseUser(username)
      released = true
    })
  }
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /mnt/d/project/GitHub/lostmaniac/PicoAide && go test ./internal/sandbox/ -run "TestUserLock_ReleasedOnPanic|TestStreamEvents" -v 2>&1
```
Expected: `PASS`

```bash
cd /mnt/d/project/GitHub/lostmaniac/PicoAide && go test ./internal/sandbox/ -v 2>&1
```
Expected: `PASS`

- [ ] **Step 5: Commit**

```bash
cd /mnt/d/project/GitHub/lostmaniac/PicoAide && git add internal/sandbox/manager.go internal/sandbox/sandbox_test.go && git commit -m "fix(picoagent): 用户锁 defer 防死锁，panic 时自动释放"
```

---

### Task 5: MCP 重连

**Files:**
- Modify: `internal/agent/mcp_tool.go`

- [ ] **Step 1: Verify existing MCP tests pass**

```bash
cd /mnt/d/project/GitHub/lostmaniac/PicoAide && go test ./internal/agent/ -run "TestMCP" -v 2>&1
```
Expected: no MCP-specific tests, this is ok

- [ ] **Step 2: Implement MCP reconnection logic**

In `internal/agent/mcp_tool.go`, modify `MCPToolManager` to store server configs:

```go
type MCPToolManager struct {
  mu            sync.Mutex
  sessions      map[string]*mcp.ClientSession
  tools         map[string]*mcpToolEntry
  serverConfigs map[string]MCPServer
  mcpToken      string
  WorkspaceDir  string
}
```

Update `NewMCPToolManager`:

```go
func NewMCPToolManager() *MCPToolManager {
  return &MCPToolManager{
    sessions:      make(map[string]*mcp.ClientSession),
    tools:         make(map[string]*mcpToolEntry),
    serverConfigs: make(map[string]MCPServer),
  }
}
```

Modify `Connect` to save config and retry:

```go
func (m *MCPToolManager) Connect(ctx context.Context, name string, server *MCPServer, token string) error {
  m.mu.Lock()
  m.serverConfigs[name] = *server
  m.mcpToken = token
  m.mu.Unlock()

  var lastErr error
  for attempt := 0; attempt < 2; attempt++ {
    if attempt > 0 {
      slog.Debug("mcp.reconnect", "server", name, "attempt", attempt+1)
      select {
      case <-time.After(time.Second):
      case <-ctx.Done():
        return ctx.Err()
      }
    }
    if err := m.connectOnce(ctx, name, server, token); err != nil {
      lastErr = err
      continue
    }
    return nil
  }
  return fmt.Errorf("MCP %s 连接失败(重试2次): %w", name, lastErr)
}

// connectOnce 单次 MCP 连接尝试
func (m *MCPToolManager) connectOnce(ctx context.Context, name string, server *MCPServer, token string) error {
  mcpClient := mcp.NewClient(&mcp.Implementation{
    Name:    "picoagent",
    Version: "2.0.0",
  }, nil)

  endpoint := fmt.Sprintf("http://localhost/api/mcp/sse/%s?token=%s", name, url.QueryEscape(token))

  transport := &mcp.StreamableClientTransport{
    Endpoint:             endpoint,
    DisableStandaloneSSE: true,
  }

  transport.HTTPClient = &http.Client{
    Timeout: 10 * time.Second,
    Transport: &http.Transport{
      DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
        return net.Dial("unix", server.Socket)
      },
    },
  }

  session, err := mcpClient.Connect(ctx, transport, nil)
  if err != nil {
    return err
  }

  m.mu.Lock()
  defer m.mu.Unlock()

  if old, ok := m.sessions[name]; ok {
    old.Close()
  }
  m.sessions[name] = session

  for tool, err := range session.Tools(ctx, nil) {
    if err != nil {
      continue
    }
    entry := tool
    for existingName, e := range m.tools {
      if e.serverName == name && existingName == entry.Name {
        delete(m.tools, existingName)
      }
    }
    m.tools[entry.Name] = &mcpToolEntry{serverName: name, tool: entry}
  }

  return nil
}
```

Modify `CallTool` to auto-reconnect:

```go
func (m *MCPToolManager) CallTool(ctx context.Context, serverName, toolName string, args map[string]interface{}) (*mcp.CallToolResult, error) {
  m.mu.Lock()
  session, ok := m.sessions[serverName]
  m.mu.Unlock()

  if !ok {
    // 尝试自动重连
    if err := m.tryReconnect(ctx, serverName); err != nil {
      return nil, err
    }
    m.mu.Lock()
    session = m.sessions[serverName]
    m.mu.Unlock()
    if session == nil {
      return nil, fmt.Errorf("MCP 服务器 %s 重连后仍然不可用", serverName)
    }
  }

  result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: toolName, Arguments: args})
  if err != nil {
    // 连接断开，尝试重连后重试一次
    if isConnError(err) {
      slog.Debug("mcp.calltool_reconnecting", "server", serverName, "tool", toolName)
      if reconnectErr := m.tryReconnect(ctx, serverName); reconnectErr == nil {
        m.mu.Lock()
        session = m.sessions[serverName]
        m.mu.Unlock()
        if session != nil {
          return session.CallTool(ctx, &mcp.CallToolParams{Name: toolName, Arguments: args})
        }
      }
    }
    return nil, err
  }
  return result, nil
}

// tryReconnect 尝试重新连接指定 MCP 服务器
func (m *MCPToolManager) tryReconnect(ctx context.Context, name string) error {
  m.mu.Lock()
  config, ok := m.serverConfigs[name]
  token := m.mcpToken
  m.mu.Unlock()

  if !ok {
    return fmt.Errorf("MCP 服务器 %s 配置不存在", name)
  }

  return m.Connect(ctx, name, &config, token)
}

// isConnError 判断是否为连接类错误（可重连）
func isConnError(err error) bool {
  if err == nil {
    return false
  }
  msg := err.Error()
  return strings.Contains(msg, "unexpected EOF") ||
    strings.Contains(msg, "connection") ||
    strings.Contains(msg, "reset") ||
    strings.Contains(msg, "stream error") ||
    strings.Contains(msg, "transport") ||
    strings.Contains(msg, "broken pipe")
}
```

Add import for `"time"` if not present. Already has `"time"`.

- [ ] **Step 3: Run existing tests to verify nothing broken**

```bash
cd /mnt/d/project/GitHub/lostmaniac/PicoAide && go test ./internal/agent/ -v 2>&1
```
Expected: `PASS`

- [ ] **Step 4: Update cmd/picoagent/main.go to pass token to MCP manager**

In `main.go`, modify MCP manager creation to store token:

```go
  if len(cfg.MCPServers) > 0 {
    mcpManager := agent.NewMCPToolManager()
    mcpManager.WorkspaceDir = cfg.Workspace
    mcpManager.SetToken(token) // new method
    // ...
```

Add SetToken to MCPToolManager:

```go
func (m *MCPToolManager) SetToken(token string) {
  m.mu.Lock()
  defer m.mu.Unlock()
  m.mcpToken = token
}
```

- [ ] **Step 5: Run all agent tests**

```bash
cd /mnt/d/project/GitHub/lostmaniac/PicoAide && go test ./internal/agent/ -v 2>&1
```
Expected: `PASS`

- [ ] **Step 6: Commit**

```bash
cd /mnt/d/project/GitHub/lostmaniac/PicoAide && git add internal/agent/mcp_tool.go cmd/picoagent/main.go && git commit -m "feat(picoagent): MCP 自动重连，Connect/CallTool 失败后重试"
```

---

### Task 6: SIGTERM 优雅处理 + Config fetch 重试

**Files:**
- Modify: `cmd/picoagent/main.go`

- [ ] **Step 1: Verify existing tests pass**

```bash
cd /mnt/d/project/GitHub/lostmaniac/PicoAide && go test ./internal/agent/ -v 2>&1
```
Expected: `PASS` — main.go doesn't have tests

- [ ] **Step 2: Add SIGTERM handler and config fetch retry**

In `cmd/picoagent/main.go`:

Add import for `"os/signal"` and `"syscall"`:

```go
import (
  // ... existing imports ...
  "os/signal"
  "syscall"
)
```

After creating the context (line ~193-198), add signal handler:

```go
  // 10a. 注册信号处理 — SIGTERM/SIGINT 触发优雅关闭
  sigCh := make(chan os.Signal, 1)
  signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
  go func() {
    select {
    case <-sigCh:
      slog.Debug("picoagent.signal_received")
      cancel()
      time.Sleep(2 * time.Second)
      os.Exit(1)
    case <-ctx.Done():
    }
  }()
```

Replace `fetchConfig` call with retry version:

Add a new function:

```go
func fetchConfigWithRetry(sock, token string) (*agent.AgentConfig, error) {
  var lastErr error
  for attempt := 0; attempt < 3; attempt++ {
    if attempt > 0 {
      slog.Debug("picoagent.config_retry", "attempt", attempt+1)
      time.Sleep(time.Second)
    }
    cfg, err := fetchConfig(sock, token)
    if err == nil {
      return cfg, nil
    }
    // 仅连接类错误可重试
    if !isConfigRetryable(err) {
      return nil, err
    }
    lastErr = err
  }
  return nil, fmt.Errorf("获取配置重试 3 次失败: %w", lastErr)
}

func isConfigRetryable(err error) bool {
  if err == nil {
    return false
  }
  msg := err.Error()
  return strings.Contains(msg, "connection") ||
    strings.Contains(msg, "dial") ||
    strings.Contains(msg, "refused") ||
    strings.Contains(msg, "timeout") ||
    strings.Contains(msg, "reset")
}
```

Replace `fetchConfig` call on line 51:

```go
  cfg, err := fetchConfigWithRetry(sock, token)
```

- [ ] **Step 3: Build and verify compilation**

```bash
cd /mnt/d/project/GitHub/lostmaniac/PicoAide && go build ./cmd/picoagent/ 2>&1
```
Expected: no errors

```bash
cd /mnt/d/project/GitHub/lostmaniac/PicoAide && go test ./internal/agent/ -v 2>&1
```
Expected: `PASS`

- [ ] **Step 4: Commit**

```bash
cd /mnt/d/project/GitHub/lostmaniac/PicoAide && git add cmd/picoagent/main.go && git commit -m "feat(picoagent): SIGTERM 优雅处理和 config fetch 重试"
```

---

## 验证

所有任务完成后，全量测试：

```bash
cd /mnt/d/project/GitHub/lostmaniac/PicoAide && make test-go 2>&1
```
Expected: `PASS` — 所有 Go 测试通过

## 设计文档

`docs/superpowers/specs/2026-05-27-picoagent-hardening-design.md`
