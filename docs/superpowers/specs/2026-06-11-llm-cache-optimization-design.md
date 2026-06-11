# LLM Cache 优化设计

## 背景

DeepSeek API 提供自动磁盘 KV Cache，缓存命中与未命中价格差距极大：
（V4-Flash 50x，V4-Pro 120x）。当前 system prompt 因 `AppendToSystemPrompt`
等动态追加而频繁变化，导致缓存前缀不一致。

## 目标

- 最大化 DeepSeek 自动 prefix caching 命中率
- system prompt 冻结，同用户会话内所有轮次命中缓存
- Cron 等批量任务跨执行命中缓存
- 仅对 DeepSeek provider 生效，不改变其他 provider 行为

## DeepSeek 官方 API 合规分析

对照 https://api-docs.deepseek.com/zh-cn/guides/ 检查当前实现。

### 思考模式

| 要求 | 当前状态 | 需改动 |
|------|---------|--------|
| 默认 thinking mode 为 enabled | ✓ 不传参即为默认 | 无 |
| 复杂 Agent 请求默认 effort=max | ✓ 服务端自动处理 | 无 |
| temperature 在思考模式下不生效 | ✓ 传参不会报错 | 无 |
| 有工具调用的轮次，reasoning_content 必须回传 | ✗ LLMMessage 无 ReasoningContent 字段 | 新增字段，DeepSeek 构建消息时包含 |
| 无工具调用的轮次，reasoning_content 可忽略 | ✓ 不传即忽略 | 无 |

### 多轮对话

| 要求 | 当前状态 | 需改动 |
|------|---------|--------|
| 无状态 API，每次请求需拼接完整历史 | ✓ 已是此模式 | 无 |
| assistant response 需 append 到 messages | ✓ 已正确拼接 | 无 |

### 上下文硬盘缓存

| 要求 | 当前状态 | 需改动 |
|------|---------|--------|
| 默认开启，无需修改代码 | ✓ | 无 |
| system prompt 作为前缀被自动缓存 | ✗ 因动态追加导致变化 | system prompt 冻结 |

### 限速与 user_id

| 要求 | 当前状态 | 需改动 |
|------|---------|--------|
| 支持 user_id 参数做 KVCache 隔离 | ✗ 未传递 | ChatRequest 新增 UserID，DeepSeek 通过 extra_body 传递 |
| 多用户场景下各用户 cache 隔离 | ✗ 未传递 | 同上一并使用 |
| user_id 格式 `[a-zA-Z0-9\-_]+` | ✓ 用户名符合此格式 | 无 |

## 设计

### 原则：仅 DeepSeek 生效

所有 DeepSeek 专项配置通过 `OpenAIProvider.providerType` 判断，
仅在 `providerType == "deepseek"` 时生效。其他 OpenAI 兼容供应商
（openai/openrouter/qwen/glm）行为不变。

### 1. system prompt 冻结（所有 provider 通用）

`Engine` 新增字段：

```
frozenSystem      string        // 构建一次后冻结的 system prompt
pendingAdditions  []string      // AppendToSystemPrompt 累积的内容
```

流程：

```
Engine 创建
  └─ buildSystemPrompt(workspace内容) → frozenSystem（冻结，不再变化）

每次 AppendToSystemPrompt(text)
  └─ pendingAdditions = append(pendingAdditions, text)

每次 LLM 调用
  ├─ System: frozenSystem                    ← 永远不变 → 缓存命中
  ├─ Messages[0]: {role:"system", ...}       ← pendingAdditions 合并
  ├─ Messages[1]: {role:"user", ...}         ← 用户输入
  └─ Messages[2..n]: ... 对话历史
```

### 2. reasoning_content 持久化（仅 DeepSeek）

`LLMMessage` 新增 `ReasoningContent string` 字段。

`Message` 结构体（用于 `live.jsonl`）同步增加 `ReasoningContent` 字段。

`buildOpenAIMessagesV2` 增加 `providerType string` 参数：
- 当 `providerType == "deepseek"` 且 assistant 消息有 ReasoningContent 时：
  构建消息时包含 `reasoning_content` 字段
- 其他 provider：忽略 ReasoningContent

### 3. user_id 传递（仅 DeepSeek）

`ChatRequest` 新增 `UserID string` 字段。

`OpenAIProvider` 新增 `providerType string` 字段，在工厂方法中设置：
- "deepseek" → providerType = "deepseek"
- 其他 → providerType = ""

`StreamChat` 中，当 `providerType == "deepseek"` 且 `req.UserID != ""` 时：
通过 `extra_body` 参数传递 `{"user_id": "xxx"}`。

Anthropic 端通过 `metadata.user_id` 传递（DeepSeek 也支持 Anthropic 格式）。

### 4. 缓存命中监控（仅 DeepSeek）

DeepSeek API 响应中提取 `usage.prompt_cache_hit_tokens` 和
`prompt_cache_miss_tokens`。通过 OpenAI SDK v3 的
`RawJSON()` 方法解析响应中的 `usage` 字段。

日志格式：
```
agent.cache_stats provider=deepseek cache_hit_tokens=1234 cache_miss_tokens=567
```

### 5. 压缩与缓存的关系

上下文的自动压缩（`internal/agent/compactor.go`）会替换旧消息为摘要：

```
压缩前: [frozenSystem, msg1, msg2, ..., msg48, msg49, msg50]
压缩后: [frozenSystem, [历史摘要], recent_round_1, recent_round_2]
```

压缩后前缀变化导致短暂 miss，但：
1. frozenSystem 始终是前缀起点，DeepSeek 公共前缀检测会独立缓存它
2. 压缩摘要统一以 `[历史摘要]` 开头，公共前缀 `frozenSystem + "[历史摘要]"` 也会被缓存
3. compactor 不需要改动

## 数据结构变更

### LLMMessage
```go
type LLMMessage struct {
  Role             string     `json:"role"`
  Content          string     `json:"content,omitempty"`
  ReasoningContent string     `json:"reasoning_content,omitempty"` // 新增，仅 DeepSeek
  ToolCallID       string     `json:"tool_call_id,omitempty"`
  ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
}
```

### ChatRequest
```go
type ChatRequest struct {
  Model       string
  System      string
  Messages    []LLMMessage
  Tools       []ToolDef
  MaxTokens   int
  Temperature float64
  UserID      string     // 新增，仅 DeepSeek 使用
}
```

### OpenAIProvider
```go
type OpenAIProvider struct {
  apiKey       string
  baseURL      string
  model        string
  providerType string     // 新增: "deepseek" 或 ""
}
```

## 文件变更清单

| 文件 | 改动 |
|------|------|
| `internal/agent/engine.go` | frozenSystem/pendingAdditions；buildSystemPrompt 只执行一次；AppendToSystemPrompt 改为累积 |
| `internal/agent/provider.go` | OpenAIProvider 新增 providerType；ChatRequest 新增 UserID；StreamChat DeepSeek 分支处理 thinking/user_id/缓存监控 |
| `internal/agent/session.go` | LLMMessage 新增 ReasoningContent；序列化/反序列化同步更新 |
| `internal/agent/provider.go` | buildOpenAIMessagesV2 参数增加 providerType，DeepSeek 时处理 reasoning_content |

## 风险

- reasoning_content 持久化增加 live.jsonl 体积，但对含工具调用的轮次是 DeepSeek API 强制要求
- user_id 传递后每个用户 cache 隔离，损失跨用户共享缓存收益，但符合隐私管理要求
- 所有 DeepSeek 专项代码通过 providerType 隔离，不影响其他供应商

## 验证

1. `go test ./internal/agent/` 全部通过
2. 日志中出现 `prompt_cache_hit_tokens` > 0 的记录
3. 非 DeepSeek provider 行为无变化
4. DeepSeek 含工具调用的多轮对话正常（reasoning_content 正确回传）
