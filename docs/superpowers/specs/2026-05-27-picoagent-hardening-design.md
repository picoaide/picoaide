# PicoAgent 健壮性加固设计方案

## 概述

对 picoagent 核心链条进行系统性加固，覆盖 LLM API 调用、工具执行、会话持久化、并发安全和进程生命周期的薄弱环节。

## 范围

7 项改动，6 个文件，预计 ~210 行新增代码。

## 改动清单

### 1. Provider 接入网络重试 (`internal/agent/provider.go`)

**问题**：`doHTTP()` 和两边 `StreamChat()` 对网络闪断/429/5xx 无任何重试，`internal/agent/retry.go` 定义了完备的指数退避策略但未使用。

**设计**：

```go
// retryStream 包装 LLM 流式调用，自动重试可恢复的网络错误
func retryStream(ctx context.Context, name string, fn func(context.Context) error) error {
  var lastErr error
  policy := DefaultRetryPolicy()
  for attempt := 0; attempt < policy.MaxAttempts; attempt++ {
    if attempt > 0 {
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
    slog.Debug("provider.retry", "name", name, "attempt", attempt+1, "error", err.Error())
  }
  return fmt.Errorf("重试 %d 次后仍然失败: %w", policy.MaxAttempts, lastErr)
}
```

- **可重试**：连接拒绝、DNS 失败、TLS 握手失败、IO 超时、HTTP 429/5xx
- **不可重试**：HTTP 4xx(除 429)、context canceled/deadline、parse error、auth error
- `AnthropicProvider.StreamChat` 包裹 `doHTTP()` 层（连接级失败可安全再试）
- `OpenAIProvider.StreamChat` 包裹 `NewStreaming()` 层（SDK 内部无重试）

### 2. 工具错误语义化 (`internal/agent/tool_registry.go`)

**问题**：8 处工具函数在失败时返回 `Success: true` + Data 中携带错误文本，Engine 和 LLM 无法区分"工具成功执行"与"工具执行失败"。

**设计**：全量扫描工具函数，将所有真实失败场景改为 `Success: false`。

影响函数：
- `CommandTool.Execute` — 命令执行失败
- `ReadFileTool.Execute` — 文件读取失败
- `GrepTool.Execute` — grep 执行失败
- `WriteFileTool.Execute` — 目录创建/文件写入失败
- `EditFileTool.Execute` — 文件读取/写入失败
- `AppendFileTool.Execute` — 目录创建/文件打开/写入失败
- `GlobTool.Execute` — glob 搜索失败
- `DeleteFileTool.Execute` — 删除失败

```diff
- return &ToolResult{Success: true, Data: fmt.Sprintf("读取失败: %v", err)}, nil
+ return &ToolResult{Success: false, Data: fmt.Sprintf("读取失败: %v", err)}, nil
```

`Success: false` 并不影响 LLM 看到的内容（LLM 只收到 `Data` 文本），但这是语义正确性修复，为后续可能增加的结果路由提供基础。

### 3. 会话定期 fsync (`internal/agent/engine.go`)

**问题**：会话仅在 `engine.Process` 完成后保存一次。若 picoagent 在多步工具调用中途被 kill（OOM/SIGKILL），已完成的步骤和中间响应全部丢失。

**设计**：

```go
// Engine 新增
fsyncInterval int    // 默认 5 轮
iterCount      int

// 每轮工具执行完毕后检查
e.iterCount++
if e.fsyncInterval > 0 && e.iterCount%e.fsyncInterval == 0 {
  msgs := make([]*Message, len(llmMsgs))
  for i, m := range llmMsgs {
    msgs[i] = &Message{
      Role: MessageRole(m.Role),
      Content: m.Content,
      ToolCallID: m.ToolCallID,
      ToolCalls: m.ToolCalls,
    }
  }
  if err := e.store.ReplaceLive(e.sessionKey, msgs); err != nil {
    slog.Debug("agent.fsync_error", "error", err.Error())
  }
}
```

阈值 5 轮 = 在容灾粒度与写放大之间取平衡。按工具调用+响应的平均长度，5 轮约 2-10KB JSON。

### 4. 用户锁 defer 防死锁 (`internal/sandbox/manager.go`)

**问题**：`prepareSandbox()` 在 `localCleanup` 闭包定义前的早期 return 路径手动调用 `releaseUser()`，若 `os.Stat` 或 `syscall.Mount` 等操作 panic，锁永不释放。

**设计**：

```go
func (m *Manager) prepareSandbox(...) (...) {
  var released bool
  defer func() {
    if !released { m.releaseUser(username) }
  }()

  // 所有现有手动 releaseUser/releaseUser 改为 released = true
  // localCleanup 也在 cleanupOnce 中设置 released = true
  localCleanup := func() {
    cleanupOnce.Do(func() {
      // ... 现有清理逻辑 ...
      m.releaseUser(username)
      released = true
    })
  }

  // 早期 return 路径
  if _, err := os.Stat(m.rootfs); err != nil {
    released = true  // 代替 m.releaseUser(username)
    return nil, nil, nil, err
  }
}
```

### 5. MCP 重连 (`internal/agent/mcp_tool.go`)

**问题**：MCP 连接使用长 SSE 流，连接在初始化时建立后永不重试。若宿主 MCP 服务重启或 socket 瞬断，`CallTool` 永远失败。

**设计**：

```go
type MCPToolManager struct {
  // ... 现有字段 ...
  serverConfigs map[string]MCPServer // 保存 server 配置用于重连
  token         string               // 保存 token 用于重连
}

// Connect 内部重试 2 次
func (m *MCPToolManager) Connect(ctx context.Context, name string, server *MCPServer, token string) error {
  // 保存配置
  m.mu.Lock()
  m.serverConfigs[name] = *server
  m.token = token
  m.mu.Unlock()

  // 重试连接
  var lastErr error
  for attempt := 0; attempt < 2; attempt++ {
    if attempt > 0 {
      time.Sleep(time.Second)
    }
    err := m.connectOnce(ctx, name, server, token)
    if err == nil {
      return nil
    }
    lastErr = err
  }
  return lastErr
}

// CallTool 检测连接断开后自动重连
func (m *MCPToolManager) CallTool(ctx context.Context, serverName, toolName string, args map[string]interface{}) (*mcp.CallToolResult, error) {
  m.mu.Lock()
  session, ok := m.sessions[serverName]
  m.mu.Unlock()
  if !ok {
    // 尝试重连
    if err := m.tryReconnect(ctx, serverName); err != nil {
      return nil, err
    }
    m.mu.Lock()
    session = m.sessions[serverName]
    m.mu.Unlock()
  }
  return session.CallTool(ctx, &mcp.CallToolParams{Name: toolName, Arguments: args})
}
```

### 6. SIGTERM 优雅处理 (`cmd/picoagent/main.go`)

**问题**：picoagent 收到 SIGTERM/SIGINT 时被操作系统直接杀死，Engine 中的 defer（发送 task_done、保存会话）不会执行。

**设计**：

```go
// main 函数中，创建 context 后立即注册信号处理
sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
go func() {
  select {
  case <-sigCh:
    slog.Debug("picoagent.signal_received")
    cancel()        // 级联取消 engine context
    time.Sleep(2 * time.Second)  // 给 engine 收尾时间
    os.Exit(1)
  case <-ctx.Done():
  }
}()
```

关键：信号 goroutine 在 `cancel()` 被调用后才启动，避免在 context 创建前就收到信号的竞态。SIGTERM 传播到 Engine 后，`defer` 链执行 → `task_done` 发送 → 会话（部分）保存 via fsync。

### 7. Config fetch 重试 (`cmd/picoagent/main.go`)

**问题**：`fetchConfig()` 通过 Unix socket 请求宿主配置，沙箱启动时 socket 可能尚未就绪，无重试导致一次性失败。

**设计**：包裹 retry 逻辑，仅对连接类错误重试：

```go
func fetchConfigWithRetry(sock, token string) (*agent.AgentConfig, error) {
  var lastErr error
  for attempt := 0; attempt < 3; attempt++ {
    if attempt > 0 {
      time.Sleep(time.Second)
    }
    cfg, err := fetchConfig(sock, token)
    if err == nil {
      return cfg, nil
    }
    // 仅连接类错误可重试
    if !isConnError(err) {
      return nil, err
    }
    lastErr = err
    slog.Debug("picoagent.config_retry", "attempt", attempt+1, "error", err.Error())
  }
  return nil, fmt.Errorf("获取配置重试 %d 次失败: %w", 3, lastErr)
}
```

## 测试策略

### 新增单元测试

| 测试 | 文件 | 验证点 |
|------|------|--------|
| `TestRetryProvider_NetworkError` | `provider_test.go` | 网络错误时自动重试 |
| `TestRetryProvider_429` | `provider_test.go` | 429 限流时自动重试并等待 |
| `TestRetryProvider_NonRetryable` | `provider_test.go` | 4xx/parse error 不重试 |
| `TestToolSuccessSemantics` | `tool_registry_test.go` | 失败时 Success=false |
| `TestEngine_PeriodicFSync` | `agent_test.go` | N 轮后 live.jsonl 被更新 |
| `TestUserLock_PanicSafety` | `sandbox_test.go` | panic 后锁被释放 |
| `TestMCPSession_Reconnect` | `mcp_tool_test.go` | 断开后自动重连 |

### 现有测试不受影响

所有现有测试（~2,878 行）逻辑不变，新增功能通过接口注入 mock 测试。

## 不纳入范围

- 输出总量限制：32MB 单行缓冲 + 12h 超时已足够
- stdin 写入确认：pipe 对单条 JSON 足够可靠
- `RunAndWait` 重复 stderr goroutine：无害，纯代码冗余
- 架构级改动（常驻进程/中间件层）：方案 B/C 范畴
