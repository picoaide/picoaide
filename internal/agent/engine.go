package agent

import (
  "context"
  "encoding/json"
  "fmt"
  "log/slog"
  "strings"
  "time"
)

// ============================================================
// Agent Protocol — 企业级行为协议（注入到所有对话的 system prompt 首部）
// ============================================================

const (
    maxOverflowRetries = 3      // 上下文溢出最大重试次数
    maxLLMRetries      = 5      // LLM 调用失败最大重试次数（超时/网络错误等）
  reservedTokens     = 20000  // 为模型回复预留的 token
)

const AgentProtocol = `# Agent Protocol

你是一个运行在企业级沙箱环境中的 AI Agent。以下协议必须严格遵守。

## 核心原则

### 1. 绝对诚实，绝不编造
- 如果你不确定答案，必须直接说"我不确定"或"我没有足够的信息"
- 所有事实性信息必须基于工具返回的结果。禁止凭训练数据编造具体数据、日期、引用、统计数据或人名
- 工具返回空结果或错误时，如实告知用户，不要自行补充缺失的内容
- 引用来源时只引用工具明确返回的内容，不添加推测

### 2. 工具优先
- 回答事实性问题前，必须优先使用相关工具获取信息
- 文件操作使用文件工具（read_file / write_file / edit_file / glob / grep）
- 网络搜索使用 web_search，获取具体页面使用 web_fetch
- shell 命令使用 command 工具
- 不要在自己的回答中猜测文件内容或命令输出，先使用工具确认

### 3. 输出规范
- 回答使用中文，结构清晰
- 适当使用 Markdown（标题、列表、代码块），代码块必须标注语言
- 步骤和操作按编号列出
- 回答长度与问题复杂度匹配：简单问题简短回答，复杂问题详细回答
- 涉及代码修改时，先展示 diff 或变更说明，再执行

### 4. 安全准则
- 禁止执行具有破坏性的命令（rm -rf /、格式化、dd 等）
- 禁止读取或泄露敏感文件（/etc/shadow、.env、密钥文件、配置文件中的密码等）
- 禁止向外部发送敏感数据

## 子代理（独立 AI）使用规范

- 使用 subagent_spawn 工具创建子代理（独立 AI 实例），使用 subagent_collect 工具收集结果
- 每个子代理拥有独立的 LLM 上下文、会话历史和全部工具调用能力（含 MCP 工具）
- 子代理与主 agent 使用相同的模型和超时配置
- 适合：批量客户查舆情、多文件并行处理、数据提取等需要 AI 判断的复杂任务
- **批量任务必须使用子代理并行处理**：先 spawn 所有子代理，再逐一 collect，实现真正的并行执行
  例如 50 个客户查舆情 → 拆 5 个子代理各查 10 家：
  1. 依次调用 subagent_spawn(name="batch1", task="查前 10 家"), subagent_spawn(name="batch2", task="查下 10 家")...
  2. 然后逐一调用 subagent_collect(name="batch1"), subagent_collect(name="batch2")... 获取结果
- 如果某个子代理失败，主 agent 重新分发该批次即可

## 技能使用规范

- 已加载的技能列表位于下方 "## 可用技能" 区段
- 执行技能相关任务时，必须遵循技能内容中的指令
- 技能指令优先于通用准则中与之冲突的部分

## 错误处理

- 工具调用失败时，先重试一次，再换用其他方式
- LLM 调用失败或超时时，保存已完成的进度，告知用户已完成的步骤
- 所有错误信息如实反馈给用户

## 对话纪律

- 禁止输出与当前任务无关的问候语（如"你好"、"有什么我可以帮你的吗"等），直接执行任务
- 禁止输出闲聊内容，不要在回复中询问用户是否需要帮助，直接给出结果
- 每轮要么调用工具，要么输出任务相关的结论信息

## 记忆管理

本工作区具备自动记忆进化能力：
- 每次对话结束后，系统会自动提取关键决策、知识点、进度状态到 MEMORY.md
- 系统会自动学习你的工作偏好并更新 USER.md
- 你也可以使用 update_memory 工具主动更新记忆
- AGENT.md 和 SOUL.md 由管理员管理，请勿手动修改
- 所有记忆修改都会备份，可追溯最近 90 天的变更历史
- 使用 update_memory 更新优于直接 write_file，可确保格式一致性和去重`

// ============================================================
// Agent 引擎 — 主循环
// ============================================================

type Engine struct {
  provider          Provider
  tools             *ToolRegistry
  store             *SessionStore
  compactor         *Compactor
  config            *AgentConfig
  skills            []*Skill
  subAgentMgr       *SubAgentManager
  sessionKey        string   // 当前会话 key，压缩后写回 store
  fsyncInterval     int      // 每隔 N 轮 fsync 一次会话，0 禁用
  iterCount         int      // 当前 Process 的迭代计数
  preloadedServers  []string // 子代理预加载的 MCP 服务器
  preloadedSystem   string   // 追加到 system prompt 的内容（如工具指引）
}

func NewEngine(cfg *AgentConfig, provider Provider, tools *ToolRegistry, store *SessionStore) *Engine {
  return &Engine{
    provider:      provider,
    tools:         tools,
    store:         store,
    compactor:     NewCompactor(DefaultCompactionConfig()),
    config:        cfg,
    fsyncInterval: 5,
  }
}

func (e *Engine) SetSessionKey(key string) {
  e.sessionKey = key
}

func (e *Engine) SetSummarizer(llm Summarizer) {
  e.compactor.SetSummarizer(llm)
}

func (e *Engine) SetSkills(skills []*Skill) {
  e.skills = skills
}

func (e *Engine) SubAgentManager() *SubAgentManager {
  return e.subAgentMgr
}

func (e *Engine) SetSubAgentManager(mgr *SubAgentManager) {
  e.subAgentMgr = mgr
}

// PreloadServer 预加载指定 MCP 服务器的工具到引擎工具列表
func (e *Engine) PreloadServer(serverName string) {
  for _, s := range e.preloadedServers {
    if s == serverName {
      return
    }
  }
  e.preloadedServers = append(e.preloadedServers, serverName)
}

// AppendToSystemPrompt 追加内容到 system prompt 末尾
func (e *Engine) AppendToSystemPrompt(text string) {
  if e.preloadedSystem != "" {
    e.preloadedSystem += "\n" + text
  } else {
    e.preloadedSystem = text
  }
}

// buildServerSummary 从工具注册表中生成 MCP 服务器摘要
func (e *Engine) buildServerSummary() string {
  servers := e.tools.ListServers()
  if len(servers) == 0 {
    return ""
  }
  var lines []string
  for _, s := range servers {
    // 尝试从 MCP 工具管理中获取摘要，如果没有则从工具列表生成
    tools := e.tools.ListByServer(s)
    summary := generateServerSummary(s, tools)
    lines = append(lines, "- "+summary)
  }
  return strings.Join(lines, "\n")
}

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
  }
}

// Process 处理一条消息，通过 cb 返回流式事件
// history 来自 store.LoadLive()，msg 是当前输入
func (e *Engine) Process(ctx context.Context, sysPrompt string, history []*Message, msg *Message, cb func(StreamEvent)) (err error) {
  // 确保任何退出路径都发出 task_done
  var taskDoneReason = "completed"
  var responseLen int
  defer func() {
    slog.Debug("agent.task_done", "reason", taskDoneReason, "response_length", responseLen)
    cb(StreamEvent{
      Type: "task_done",
      Data: mustJSON(map[string]interface{}{
        "reason":          taskDoneReason,
        "response_length": responseLen,
      }),
    })
  }()

  slog.Debug("agent.process_start",
    "history_count", len(history),
    "input_role", msg.Role,
    "input_length", len(msg.Content),
    "skills_count", len(e.skills),
  )

  var llmMsgs []LLMMessage
  for _, m := range history {
    llmMsgs = append(llmMsgs, m.ToLLMMessage())
  }
  llmMsgs = append(llmMsgs, msg.ToLLMMessage())

  // 注入 Agent Protocol 到系统提示词首部
  sysPrompt = AgentProtocol + "\n\n" + sysPrompt

  // 追加技能内容到系统提示词
  if len(e.skills) > 0 {
    skillsPrompt := BuildSkillsPrompt(e.skills)
    if skillsPrompt != "" {
      sysPrompt = sysPrompt + "\n\n" + skillsPrompt
    }
  }

  // 主 agent 模式下添加 MCP 服务器摘要
  if len(e.preloadedServers) == 0 {
    serverSummary := e.buildServerSummary()
    if serverSummary != "" {
      sysPrompt = sysPrompt + "\n\n## 可用 MCP 服务器\n" + serverSummary +
        "\n\n使用 query_server 工具快速调用某个 MCP 服务器的工具。批量任务请使用 subagent_spawn + subagent_collect。"
    }
  }

  // 追加预置内容（如子代理工具指引）
  if e.preloadedSystem != "" {
    sysPrompt = sysPrompt + "\n\n" + e.preloadedSystem
  }

  slog.Debug("agent.system_prompt_built", "sys_prompt_length", len(sysPrompt))

  var fullResponse string
  var toolCallsExecuted bool

  m := e.config.Model
  modelID := m.ModelID
  maxIter := m.MaxIter
  contextWindow := m.ContextWindow
  if contextWindow <= 0 {
    contextWindow = 200000
  }

  // 系统提示词太长时截断，保证回复空间
  sysPrompt = truncatePrompt(sysPrompt, llmMsgs, e.compactor, contextWindow)
  if maxIter <= 0 {
    maxIter = e.config.MaxIter
    if maxIter <= 0 {
      maxIter = 20
    }
  }
  // 使用管理员配置的 max_tokens。为 0 时不传参，由模型服务端自行决定输出上限
  maxTokens := m.MaxTokens
  temperature := m.Temperature
  if temperature <= 0 {
    temperature = 0.7
  }

  slog.Debug("agent.model_config",
    "model_id", modelID,
    "max_iter", maxIter,
    "context_window", contextWindow,
    "max_tokens", maxTokens,
    "temperature", temperature,
  )

  // 父 ctx（来自 main.go）用于整体取消（用户停止）
  // 但它的超时是整个 Process 累计的（request_timeout 秒）。
  // 多轮工具调用很容易超时，所以用独立 context 做单轮超时：
  // cancelCtx 由父 ctx 的 cancel 传播而来，迭代超时从 cancelCtx 派生。
  cancelCtx, cancelAll := context.WithCancel(context.Background())
  defer cancelAll()
  go func() {
    select {
    case <-ctx.Done():
      cancelAll()
    case <-cancelCtx.Done():
    }
  }()
  perIterTimeout := m.RequestTimeout
  if perIterTimeout <= 0 {
    perIterTimeout = 120
  }

  emptyRespRetries := 0
  for iter := 0; iter < maxIter; iter++ {
    // 每轮独立超时：LLM 调用 + 工具执行共享一条超时线
    iterCtx, iterCancel := context.WithTimeout(cancelCtx, time.Duration(perIterTimeout)*time.Second)

    // Token 溢出检查
    tokenCount := estimateTokens(sysPrompt, llmMsgs)
    usableTokens := contextWindow - reservedTokens
    overflow := tokenCount >= usableTokens

    slog.Debug("agent.iteration_start",
      "iter", iter,
      "token_count", tokenCount,
      "context_window", contextWindow,
      "usable_tokens", usableTokens,
      "overflow", overflow,
      "message_count", len(llmMsgs),
    )

    // 主动压缩：多轮压缩直至 token 数低于可用预算
    if e.compactor != nil && overflow {
      slog.Debug("agent.proactive_compact", "token_count", tokenCount, "budget", usableTokens)
      for pass := 0; pass < 10; pass++ {
        compacted, err := compactLLMMessages(iterCtx, e.compactor, llmMsgs, usableTokens)
        if err != nil {
          slog.Debug("agent.compact_fallback", "error", err.Error())
          llmMsgs = trimMessages(llmMsgs)
          break
        }
        llmMsgs = compacted
        tokenCount = estimateTokens(sysPrompt, llmMsgs)
        slog.Debug("agent.compact_pass", "pass", pass+1, "token_count", tokenCount)
        if tokenCount < usableTokens {
          break
        }
      }
      // 持久化压缩结果到 store，下次 Process 不再重新压缩相同历史
      if e.sessionKey != "" && len(llmMsgs) > 0 {
        persisted := make([]*Message, len(llmMsgs))
        for i, m := range llmMsgs {
          persisted[i] = &Message{
            Role:       MessageRole(m.Role),
            Content:    m.Content,
            ToolCallID: m.ToolCallID,
            ToolCalls:  m.ToolCalls,
          }
        }
        if err := e.store.ReplaceLive(e.sessionKey, persisted); err != nil {
          slog.Debug("agent.compact_persist_error", "error", err.Error())
        } else {
          slog.Debug("agent.compact_persisted", "message_count", len(persisted))
        }
      }
      slog.Debug("agent.after_proactive_compact", "token_count", tokenCount, "message_count", len(llmMsgs))
    }

    // 发送上下文进度事件
    cb(StreamEvent{
      Type: "progress",
      Data: mustJSON(map[string]int{
        "token_count":    tokenCount,
        "context_window": contextWindow,
        "max_iter":       maxIter,
        "current_iter":   iter,
      }),
    })

    // 构建工具列表：基础工具 + 预加载的 MCP 服务器工具
    toolDefs := e.tools.ListBasic()
    for _, srv := range e.preloadedServers {
      toolDefs = append(toolDefs, e.tools.ListByServer(srv)...)
    }
    var pendingTools []ToolCallData
    var currentResp string

    // LLM 调用（含上下文溢出重试）
    var llmErr error
    overflowRetry := 0
    llmRetry := 0
    for {
      slog.Debug("agent.llm_request",
        "model", modelID,
        "messages", len(llmMsgs),
        "tools", len(toolDefs),
        "max_tokens", maxTokens,
        "temperature", temperature,
      )

      llmErr = e.provider.StreamChat(iterCtx, &ChatRequest{
        Model:       modelID,
        System:      sysPrompt,
        Messages:    llmMsgs,
        Tools:       toolDefs,
        MaxTokens:   maxTokens,
        Temperature: temperature,
      }, func(event StreamEvent) {
        switch event.Type {
        case "text_delta":
          var text string
          if json.Unmarshal(event.Data, &text) == nil {
            currentResp += text
          }
          cb(event)
        case "tool_call_start":
          var tc ToolCallData
          if json.Unmarshal(event.Data, &tc) == nil {
            pendingTools = append(pendingTools, tc)
            slog.Debug("agent.tool_call_received", "tool", tc.Name, "id", tc.ID)
          }
          cb(event)
        case "finish":
          cb(event)
        case "error":
          cb(event)
        }
      })

      if llmErr == nil {
        break
      }

      // 上下文溢出 → 压缩后重试
      if isContextOverflow(llmErr) && overflowRetry < maxOverflowRetries {
        overflowRetry++
        slog.Debug("agent.llm_context_overflow", "retry", overflowRetry, "error", llmErr.Error())
        iterCancel()
        for pass := 0; pass < 10; pass++ {
          compacted, err := compactLLMMessages(context.Background(), e.compactor, llmMsgs, usableTokens)
          if err != nil {
            llmMsgs = trimMessages(llmMsgs)
            break
          }
          llmMsgs = compacted
          if estimateTokens(sysPrompt, llmMsgs) < usableTokens {
            break
          }
        }
        // 重建超时上下文
        iterCtx, iterCancel = context.WithTimeout(cancelCtx, time.Duration(perIterTimeout)*time.Second)
        pendingTools = nil
        currentResp = ""
        continue
      }

      // 其他 LLM 错误（超时/网络故障等）→ 重建上下文重试
      if llmRetry < maxLLMRetries {
        llmRetry++
        slog.Warn("agent.llm_retry", "retry", llmRetry, "max", maxLLMRetries, "error", llmErr.Error())
        iterCancel()
        iterCtx, iterCancel = context.WithTimeout(cancelCtx, time.Duration(perIterTimeout)*time.Second)
        pendingTools = nil
        currentResp = ""
        continue
      }

      break
    }

    if llmErr != nil {
      iterCancel()
      taskDoneReason = "error"
      slog.Error("agent.llm_error", "error", llmErr.Error())
      err = fmt.Errorf("LLM 调用失败: %w", llmErr)
      return
    }

    slog.Debug("agent.llm_response_complete",
      "response_length", len(currentResp),
      "tool_calls", len(pendingTools),
    )

    if len(pendingTools) == 0 {
      if len(currentResp) == 0 {
        // LLM 返回空响应（无文字、无工具调用），可能是临时问题
        // 重试最多 3 次避免死循环，超过 3 次才视为真正完成
        if emptyRespRetries >= 3 {
          fullResponse = currentResp
          slog.Debug("agent.no_tool_calls", "response_length", len(fullResponse), "empty_retries", emptyRespRetries)
          iterCancel()
          break
        }
        emptyRespRetries++
        slog.Debug("agent.empty_response_retry", "retry", emptyRespRetries)
        iterCancel()
        continue
      }
      fullResponse = currentResp
      slog.Debug("agent.no_tool_calls", "response_length", len(fullResponse))
      iterCancel()
      break
    }

    // 构建 assistant 消息（带 tool_calls）
    var toolCalls []ToolCall
    for _, tc := range pendingTools {
      toolCalls = append(toolCalls, ToolCall{
        ID:   tc.ID,
        Type: "function",
        Function: ToolFunction{
          Name:      tc.Name,
          Arguments: string(tc.Input),
        },
      })
    }
    assistantMsg := LLMMessage{
      Role:      "assistant",
      Content:   currentResp,
      ToolCalls: toolCalls,
    }
    llmMsgs = append(llmMsgs, assistantMsg)

    // 执行所有工具
    for _, tc := range pendingTools {
      inputPreview := string(tc.Input)
      if len(inputPreview) > 200 {
        inputPreview = inputPreview[:200] + "..."
      }
      slog.Debug("agent.tool_execute_start", "tool", tc.Name, "id", tc.ID, "input_preview", inputPreview)

      result, execErr := e.tools.Execute(iterCtx, tc.Name, tc.Input)
      if execErr != nil {
        result = &ToolResult{Success: false, Data: fmt.Sprintf("工具执行失败: %s", execErr)}
        slog.Debug("agent.tool_execute_error", "tool", tc.Name, "error", execErr.Error())
      } else {
        // 工具结果太大时自动压缩摘要，避免撑爆上下文
        autoCompact := len(result.Data) > 2000 && e.compactor != nil
        if autoCompact {
          compactPrompt := fmt.Sprintf("用一句话概括以下工具返回结果的核心信息（保持关键数据）：\n%s", result.Data)
          compacted, compactErr := e.compactor.SummarizeText(iterCtx, compactPrompt)
          if compactErr == nil && len(compacted) > 0 && len(compacted) < len(result.Data) {
            slog.Debug("agent.tool_result_compacted", "tool", tc.Name, "before", len(result.Data), "after", len(compacted))
            result.Data = "[自动摘要] " + compacted
          }
        }
        resultPreview := result.Data
        if len(resultPreview) > 200 {
          resultPreview = resultPreview[:200] + "..."
        }
        slog.Debug("agent.tool_execute_success", "tool", tc.Name, "result_length", len(result.Data), "result_preview", resultPreview)
      }

      llmMsgs = append(llmMsgs, LLMMessage{
        Role:       "tool",
        ToolCallID: tc.ID,
        Content:    result.Data,
      })

      cb(StreamEvent{
        Type: "tool_result",
        Data: mustJSON(map[string]interface{}{
          "id":     tc.ID,
          "name":   tc.Name,
          "result": result.Data,
        }),
      })
    }
    pendingTools = nil
    toolCallsExecuted = true
    // 回合后压缩：工具执行后 token 超 50% 则预压缩，避免下轮溢出
    if e.compactor != nil {
      postCount := estimateTokens(sysPrompt, llmMsgs)
      if postCount >= usableTokens/2 {
        slog.Debug("agent.post_turn_compact", "token_count", postCount, "threshold", usableTokens*3/4)
        for pass := 0; pass < 10; pass++ {
          compacted, err := compactLLMMessages(context.Background(), e.compactor, llmMsgs, usableTokens)
          if err != nil {
            llmMsgs = trimMessages(llmMsgs)
            break
          }
          llmMsgs = compacted
          if estimateTokens(sysPrompt, llmMsgs) < usableTokens {
            break
          }
        }
        slog.Debug("agent.post_turn_compact_done", "token_count", estimateTokens(sysPrompt, llmMsgs))
      }
    }

    // 定期 fsync 会话
    e.iterCount++
    if e.fsyncInterval > 0 && e.iterCount%e.fsyncInterval == 0 {
      e.fsyncSession(llmMsgs)
    }

    iterCancel()
  }

  responseLen = len(fullResponse)
  if fullResponse == "" {
    if !toolCallsExecuted {
      taskDoneReason = "error"
      slog.Debug("agent.empty_response")
      err = fmt.Errorf("LLM 返回空响应")
      return
    }
    // 执行了工具调用但 LLM 未生成文字回复，给用户可见的兜底文本
    fullResponse = "AI 助手已执行操作，但未生成文字回复。"
    slog.Debug("agent.no_text_response")
  }

  slog.Debug("agent.process_complete",
    "response_length", responseLen,
    "tool_calls_executed", toolCallsExecuted,
    "iterations", maxIter,
  )

  cb(FinishEvent(fullResponse, map[string]int{}))
  return nil
}

// ============================================================
// ToolCallData 流式事件中的工具调用数据
// ============================================================

type ToolCallData struct {
  ID    string          `json:"id"`
  Name  string          `json:"name"`
  Input json.RawMessage `json:"input"`
}

// ============================================================
// Token 估算（粗略：英文 4 字符/token，中文 1.5 字符/token）
// ============================================================

func estimateTokens(sysPrompt string, msgs []LLMMessage) int {
  total := estimateStringTokens(sysPrompt)
  for _, m := range msgs {
    total += estimateStringTokens(m.Content)
  }
  return total
}

func estimateStringTokens(s string) int {
  var chineseCount int
  for _, r := range s {
    if r > '\u007f' {
      chineseCount++
    }
  }
  asciiCount := len(s) - chineseCount
  return asciiCount/4 + chineseCount*2/3 + 1
}

// compactLLMMessages 按 token 预算压缩消息列表：
// - 从尾部向前扫描，保留消息直到填满 budget 的 80%
// - 超出部分用 LLM 摘要替代
// - 若 LLM 摘要失败，直接截断超出部分
func compactLLMMessages(ctx context.Context, c *Compactor, msgs []LLMMessage, budget int) ([]LLMMessage, error) {
  if len(msgs) <= 2 || budget <= 0 {
    return msgs, nil
  }

  // 从后向前累加 token，确定保留多少条消息
  tailBudget := budget * 4 / 5 // 预留 80% 给 tail
  tailStart := len(msgs)
  var tailTokens int
  for i := len(msgs) - 1; i >= 0; i-- {
    t := estimateStringTokens(msgs[i].Content)
    if tailTokens+t > tailBudget && tailTokens > 0 {
      break
    }
    tailTokens += t
    tailStart = i
  }

  // 头部不需要压缩
  if tailStart == 0 {
    return msgs, nil
  }

  head := msgs[:tailStart]
  tail := msgs[tailStart:]

  // 用 LLM 摘要头部消息
  history := make([]*Message, len(head))
  for i := range head {
    history[i] = &Message{
      Role:       MessageRole(head[i].Role),
      Content:    head[i].Content,
      ToolCallID: head[i].ToolCallID,
      ToolCalls:  head[i].ToolCalls,
    }
  }

  summary, err := c.generateSummary(ctx, history)
  if err != nil {
    return nil, err
  }

  result := make([]LLMMessage, 0, len(tail)+1)
  result = append(result, LLMMessage{
    Role:    "assistant",
    Content: fmt.Sprintf("[历史摘要] %s", summary),
  })
  result = append(result, tail...)
  return result, nil
}

// trimMessages 作为压缩失败兜底：保留最近 10 条消息
func trimMessages(msgs []LLMMessage) []LLMMessage {
  if len(msgs) <= 10 {
    return msgs
  }
  return msgs[len(msgs)-10:]
}

func mustJSON(v interface{}) json.RawMessage {
  data, _ := json.Marshal(v)
  return data
}

// truncatePrompt 系统提示词超出上限时截断末尾（优先级最低的部分：MEMORY.md 等）
func truncatePrompt(prompt string, msgs []LLMMessage, compactor *Compactor, contextWindow int) string {
  if compactor == nil {
    return prompt
  }
  total := estimateTokens(prompt, msgs)
  if !compactor.IsOverflowWithLimit(total, contextWindow) {
    return prompt
  }
  limit := compactor.UsableTokens(contextWindow)
  keepRatio := float64(limit) / float64(total)
  keepChars := int(float64(len(prompt)) * keepRatio)
  if keepChars < 200 {
    keepChars = 200
  }
  if keepChars > len(prompt) {
    keepChars = len(prompt)
  }
  return prompt[:keepChars] + "\n\n[提示词过长已截断]"
}

func isContextOverflow(err error) bool {
  if err == nil {
    return false
  }
  msg := strings.ToLower(err.Error())
  return strings.Contains(msg, "context_length_exceeded") ||
    strings.Contains(msg, "context length") ||
    strings.Contains(msg, "maximum context") ||
    strings.Contains(msg, "token limit") ||
    strings.Contains(msg, "too many tokens")
}
