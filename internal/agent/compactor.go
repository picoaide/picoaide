package agent

import (
  "context"
  "encoding/json"
  "fmt"
  "log/slog"
  "strings"
)

// ============================================================
// 上下文压缩（参考 OpenCode compaction.ts 设计）
// archive.jsonl 永久完整，live.jsonl 被压缩
// ============================================================

type CompactionConfig struct {
  Auto              bool
  ReservedTokens    int // 保留空间
  PreserveRecentMin int // 至少保留最近 N token
  PreserveRecentMax int // 最多保留
  PruneMinimum      int // 低于此值不修剪
  MaxPreserveRounds int // 至少保留最近几轮
}

func DefaultCompactionConfig() *CompactionConfig {
  return &CompactionConfig{
    Auto:              true,
    ReservedTokens:    20000,
    PreserveRecentMin: 2000,
    PreserveRecentMax: 8000,
    PruneMinimum:      20000,
    MaxPreserveRounds: 2,
  }
}

type Compactor struct {
  cfg *CompactionConfig
  llm Summarizer // 用于生成摘要的 LLM
}

type Summarizer interface {
  Summarize(ctx context.Context, prompt string) (string, error)
}

func NewCompactor(cfg *CompactionConfig) *Compactor {
  return &Compactor{cfg: cfg}
}

// SetSummarizer 设置摘要用 LLM
func (c *Compactor) SetSummarizer(llm Summarizer) {
  c.llm = llm
}

// SummarizeText 使用摘要 LLM 对任意文本生成摘要（工具结果自动压缩用）
func (c *Compactor) SummarizeText(ctx context.Context, prompt string) (string, error) {
  if c.llm == nil {
    return "", fmt.Errorf("summarizer not set")
  }
  return c.llm.Summarize(ctx, prompt)
}

// IsOverflow 判断 live 是否溢出
func (c *Compactor) IsOverflow(tokenCount int) bool {
  return c.IsOverflowWithLimit(tokenCount, 200000)
}

// IsOverflowWithLimit 使用指定上下文窗口判断是否溢出
func (c *Compactor) IsOverflowWithLimit(tokenCount int, contextWindow int) bool {
  if !c.cfg.Auto {
    return false
  }
  if contextWindow <= 0 {
    contextWindow = 200000
  }
  usable := contextWindow - c.cfg.ReservedTokens
  return tokenCount >= usable
}

// UsableTokens 返回可用的 token 上限
func (c *Compactor) UsableTokens(contextWindow int) int {
  if contextWindow <= 0 {
    contextWindow = 200000
  }
  return contextWindow - c.cfg.ReservedTokens
}

// Compact 压缩 live 消息列表，返回压缩后的消息
// 保留最近 N 轮完整，旧消息替换为摘要消息
func (c *Compactor) Compact(ctx context.Context, msgs []*Message) ([]*Message, error) {
  slog.Debug("compactor.start",
    "message_count", len(msgs),
    "max_preserve_rounds", c.cfg.MaxPreserveRounds,
  )

  if len(msgs) < c.cfg.MaxPreserveRounds*2 {
    slog.Debug("compactor.skip", "reason", "too_few_messages")
    return msgs, nil
  }

  // 找到截断点：保留最近 MaxPreserveRounds 轮
  truncateAt := findTruncatePoint(msgs, c.cfg.MaxPreserveRounds)
  if truncateAt <= 0 {
    slog.Debug("compactor.skip", "reason", "no_truncate_point")
    return msgs, nil
  }

  toSummarize := msgs[:truncateAt]
  recent := msgs[truncateAt:]

  slog.Debug("compactor.split",
    "to_summarize_count", len(toSummarize),
    "recent_count", len(recent),
    "truncate_at", truncateAt,
  )

  // 生成摘要
  summary, err := c.generateSummary(ctx, toSummarize)
  if err != nil {
    slog.Debug("compactor.summary_error", "error", err.Error())
    summary = fmt.Sprintf("历史对话摘要：共 %d 条消息", len(toSummarize))
  }

  // 构造摘要消息
  summaryMsg := &Message{
    Role:    RoleAssistant,
    Content: fmt.Sprintf("[历史摘要] %s", summary),
  }

  result := append([]*Message{summaryMsg}, recent...)
  slog.Debug("compactor.complete",
    "original_count", len(msgs),
    "compressed_count", len(result),
    "reduction", len(msgs)-len(result),
  )
  return result, nil
}

func findTruncatePoint(msgs []*Message, preserveRounds int) int {
  rounds := 0
  for i := len(msgs) - 1; i >= 0; i-- {
    if msgs[i].Role == RoleUser {
      rounds++
      if rounds > preserveRounds {
        return i + 1
      }
    }
  }
  return 0
}

func (c *Compactor) generateSummary(ctx context.Context, msgs []*Message) (string, error) {
  if c.llm == nil {
    return buildFallbackSummary(msgs), nil
  }

  var dialog strings.Builder
  for _, m := range msgs {
    prefix := ""
    switch m.Role {
    case RoleUser:
      prefix = "用户"
    case RoleAssistant:
      prefix = "助手"
    case RoleTool:
      continue
    }
    if prefix != "" {
      fmt.Fprintf(&dialog, "%s: %s\n\n", prefix, m.Content)
    }
  }

  prompt := fmt.Sprintf(`根据以上对话历史创建一个新的结构化摘要，保留关键信息和上下文。

按以下 Markdown 格式输出，保留每个章节，即使内容为空也要保留标题：

## 目标
- [一句话总结用户的核心任务]

## 约束与偏好
- [用户的限制条件、偏好设定或"(无)"]

## 进度
### 已完成
- [已完成的工作，或"(无)"]

### 进行中
- [正在进行的工作，或"(无)"]

### 阻塞
- [阻塞项，或"(无)"]

## 关键决策
- [重要决策及原因，或"(无)"]

## 下一步
- [后续行动，或"(无)"]

## 关键上下文
- [重要的技术事实、错误信息、未解决的问题，或"(无)"]

## 相关文件
- [文件路径：为什么重要，或"(无)"]

规则：
- 每个章节都必须保留，即使为空
- 使用简洁的要点，不使用段落描述
- 保留精确的文件路径、命令、错误信息和标识符
- 不要提及摘要过程或上下文已被压缩

对话历史：
%s`, dialog.String())

  summary, err := c.llm.Summarize(ctx, prompt)
  if err != nil {
    return buildFallbackSummary(msgs), nil
  }
  return summary, nil
}

func buildFallbackSummary(msgs []*Message) string {
  userCount := 0
  assistantCount := 0
  for _, m := range msgs {
    switch m.Role {
    case RoleUser:
      userCount++
    case RoleAssistant:
      assistantCount++
    }
  }
  return fmt.Sprintf("共 %d 轮对话（用户 %d 条，助手 %d 条）", userCount, userCount+assistantCount, assistantCount)
}

// ============================================================
// Engine 集成：压缩回调
// ============================================================

// CompactAndRewrite 压缩 live 并写回文件
func CompactAndRewrite(ctx context.Context, store *SessionStore, key string, compactor *Compactor) error {
  msgs, err := store.LoadLive(key)
  if err != nil || len(msgs) == 0 {
    return err
  }
  compacted, err := compactor.Compact(ctx, msgs)
  if err != nil {
    return err
  }
  if len(compacted) < len(msgs) {
    if err := store.ReplaceLive(key, compacted); err != nil {
      slog.Warn("compactor.replace_live_error", "error", err.Error())
      return err
    }
    store.SaveMeta(key, &SessionMeta{
      Summary: extractSummary(compacted),
      Count:   len(compacted),
    })
  }
  return nil
}

func extractSummary(msgs []*Message) string {
  for _, m := range msgs {
    if strings.HasPrefix(m.Content, "[历史摘要]") {
      return strings.TrimPrefix(m.Content, "[历史摘要]")
    }
  }
  return ""
}

// Summarize 实现 Summarizer 接口（通过 LLM provider）
type LLMSummarizer struct {
  provider  Provider
  model     string
  maxTokens int // 管理员配置的 max_tokens，用于 prompt 指导 AI 控制输出长度
}

func NewLLMSummarizer(provider Provider, model string, maxTokens int) *LLMSummarizer {
  return &LLMSummarizer{provider: provider, model: model, maxTokens: maxTokens}
}

func (l *LLMSummarizer) Summarize(ctx context.Context, prompt string) (string, error) {
  // API max_tokens 设为一个较大的值，避免硬截断；
  // 实际输出长度由 prompt 中的约束指导 AI 自行控制
  apiMaxTokens := 100000
  guideline := ""
  if l.maxTokens > 0 {
    guideline = fmt.Sprintf("，控制在 %d tokens 以内", l.maxTokens)
  }
  var summary string
  err := l.provider.StreamChat(ctx, &ChatRequest{
    Model:       l.model,
    System:      fmt.Sprintf("你是一个高效的摘要助手。请用中文简洁地总结%s。", guideline),
    Messages:    []LLMMessage{{Role: "user", Content: prompt}},
    MaxTokens:   apiMaxTokens,
    Temperature: 0.3,
  }, func(event StreamEvent) {
    if event.Type == "text_delta" {
      var text string
      if json.Unmarshal(event.Data, &text) == nil {
        // Use json helper
        summary += text
      }
    }
  })
  if err != nil {
    return "", err
  }
  return summary, nil
}
