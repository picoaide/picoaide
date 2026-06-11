# LLM Cache 优化设计

## 背景

DeepSeek API 提供自动磁盘 KV Cache，无需修改 API 调用即可享受缓存收益。
缓存命中与未命中价格差距极大（V4-Flash 50x，V4-Pro 120x）。

当前 system prompt 在会话过程中可能因 `AppendToSystemPrompt` 等原因变化，
导致 DeepSeek 缓存前缀不一致，无法充分利用缓存。

## 目标

- 最大化 DeepSeek 自动 prefix caching 命中率
- 跨用户共享 `AgentProtocol` 等公共前缀的缓存
- 同用户会话内 system prompt 冻结，所有轮次命中缓存
- Cron 等批量任务跨执行命中缓存

## DeepSeek Context Caching 机制

参考 https://api-docs.deepseek.com/guides/kv_cache

- **自动生效**：无需特殊 API 参数或 header
- **命中规则**：后续请求必须**完全匹配**一个缓存前缀单元
- **持久化时机**：
  1. 请求边界：每次请求的用户输入末尾和模型输出末尾
  2. 公共前缀检测：系统自动检测多个请求的公共前缀并缓存
  3. 固定间隔：长输入/输出按固定 token 间隔创建缓存单元
- **过期**：缓存不再使用后数小时到数天内自动清除
- **验证**：响应 `usage.prompt_cache_hit_tokens` 和 `prompt_cache_miss_tokens`
- **定价**：

| | V4-Flash | V4-Pro |
|---|---|---|
| Cache HIT / 1M tokens | $0.0028 | $0.003625 |
| Cache MISS / 1M tokens | $0.14 | $0.435 |

## 设计

### system prompt 冻结

`Engine` 新增字段：

```
frozenSystem      string        // 构建一次后冻结的 system prompt
pendingAdditions  []string      // AppendToSystemPrompt 累积的内容
```

`Engine.Process()` 流程变更：

```
Engine 创建
  └─ buildSystemPrompt(workspace内容)
       └─ frozenSystem = AgentProtocol + "\n\n" + workspace内容
                               + skills + MCP摘要

每次 AppendToSystemPrompt(text)
  └─ pendingAdditions = append(pendingAdditions, text)

每次 LLM 调用
  ├─ System: frozenSystem                    ← 永远不变 → 缓存命中
  ├─ Messages[0]: {role:"system", ...}       ← pendingAdditions（如存在）
  ├─ Messages[1]: {role:"user", ...}         ← 用户输入
  └─ Messages[2..n]: ... 对话历史
```

### buildSystemPrompt 结构优化

顺序（从静态到动态，确保最大公共前缀）：

1. **AgentProtocol** — 跨用户完全一致，DeepSeek 公共前缀检测自动缓存
2. **技能描述 + MCP 服务器摘要** — 同用户会话内冻结
3. **用户工作区文件（AGENT.md / SOUL.md / USER.md / MEMORY.md）** — 会话内稳定

### provider.go 改动

`ChatRequest` 新增 `SystemAdditions []string` 字段。
`buildOpenAIMessagesV2` 处理后追加多条 system 消息：

```go
func buildOpenAIMessagesV2(system string, additions []string, messages []LLMMessage) []openai.ChatCompletionMessageParamUnion {
  var msgs []openai.ChatCompletionMessageParamUnion
  if system != "" {
    msgs = append(msgs, openai.SystemMessage(system))
  }
  for _, a := range additions {
    msgs = append(msgs, openai.SystemMessage(a))
  }
  for _, m := range messages {
    // ... 现有消息构建逻辑
  }
  return msgs
}
```

### session.go 改动

`s.SystemAdditions` 字段持久化到会话元数据中（`live.jsonl` 第一行 session record），对话恢复时重新注入到 Engine 的 `pendingAdditions`。

### 缓存命中监控

日志记录每次 LLM 调用的缓存命中/miss token 数：

```
agent.cache_stats cache_hit_tokens=1234 cache_miss_tokens=567
```

通过在 OpenAI SDK 的 response 解析中提取 `usage.prompt_cache_hit_tokens` 实现。

## 文件变更清单

| 文件 | 改动 |
|------|------|
| `internal/agent/engine.go` | Engine 结构体新增 frozenSystem/pendingAdditions；buildSystemPrompt 只执行一次；AppendToSystemPrompt 改为累积；LLM 调用前注入 pendingAdditions 到 messages |
| `internal/agent/provider.go` | ChatRequest 新增 SystemAdditions；buildOpenAIMessagesV2 参数增加 additions；Anthropic/OpenAI 响应解析中提取 cache hit/miss token |
| `internal/agent/session.go` | 会话元数据持久化 SystemAdditions |

## 风险

- `AppendToSystemPrompt` 的调用方期望内容在 system prompt 中，改为 messages 后语义不变但位置不同。需确保模型行为一致（DeepSeek 支持多条 system 消息）
- `buildSystemPrompt` 依赖 `e.preloadedSystem`，冻结后外部修改 `preloadedSystem` 无效。需确保所有调用方改为使用 `AppendToSystemPrompt`

## 验证

1. `go test ./internal/agent/` 通过
2. 日志中出现 `prompt_cache_hit_tokens` > 0 的记录
3. 多用户场景下 AgentProtocol 命中缓存
4. Cron 任务连续执行时 system prompt 命中缓存
