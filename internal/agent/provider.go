package agent

import (
  "bytes"
  "context"
  "encoding/json"
  "fmt"
  "io"
  "log/slog"
  "net/http"
  "strings"
  "time"

  "github.com/openai/openai-go/v3"
  "github.com/openai/openai-go/v3/option"
  "github.com/openai/openai-go/v3/shared"
)

// reasoningEvent 发出推理/思考内容增量事件
func reasoningEvent(text string) StreamEvent {
  return StreamEvent{
    Type: "reasoning",
    Data: mustJSON(text),
  }
}

// ============================================================
// LLM Provider 接口
// ============================================================

type Provider interface {
  StreamChat(ctx context.Context, req *ChatRequest, cb func(event StreamEvent)) error
}

type ChatRequest struct {
  Model       string
  System      string
  Messages    []LLMMessage
  Tools       []ToolDef
  MaxTokens   int
  Temperature float64
  UserID      string
}

// ============================================================
// Provider 工厂
// ============================================================

func NewProvider(provider, modelID, baseURL, apiKey string) (Provider, error) {
  switch provider {
  case "anthropic":
    return NewAnthropicProvider(apiKey, baseURL, modelID), nil
  case "deepseek":
    return NewDeepSeekProvider(apiKey, baseURL, modelID), nil
  case "openai", "openrouter", "qwen", "glm":
    return NewOpenAIProvider(apiKey, baseURL, modelID), nil
  default:
    return NewOpenAIProvider(apiKey, baseURL, modelID), nil
  }
}

// ============================================================
// HTTP 请求辅助（避免 curl 命令行参数泄露）
// ============================================================

func doHTTP(ctx context.Context, url, method string, headers map[string]string, body []byte) (*http.Response, error) {
  req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
  if err != nil {
    return nil, err
  }
  for k, v := range headers {
    req.Header.Set(k, v)
  }
  client := &http.Client{Timeout: 120 * time.Second}
  return client.Do(req)
}

// retryableErrorPrefixes 可重试的网络/服务端错误前缀匹配
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
    if ctx.Err() != nil {
      return ctx.Err()
    }
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

// ============================================================
// Anthropic 实现（直接 HTTP，无 curl）
// ============================================================

type AnthropicProvider struct {
  apiKey  string
  baseURL string
  model   string
}

func NewAnthropicProvider(apiKey, baseURL, model string) *AnthropicProvider {
  if baseURL == "" {
    baseURL = "https://api.anthropic.com/v1"
  }
  return &AnthropicProvider{
    apiKey:  apiKey,
    baseURL: baseURL,
    model:   model,
  }
}

type anthropicReq struct {
  Model       string          `json:"model"`
  System      string          `json:"system,omitempty"`
  MaxTokens   int             `json:"max_tokens"`
  Temperature float64         `json:"temperature,omitempty"`
  Messages    []anthropicMsg  `json:"messages"`
  Tools       []anthropicTool `json:"tools,omitempty"`
  Stream      bool            `json:"stream"`
}

type anthropicMsg struct {
  Role    string          `json:"role"`
  Content json.RawMessage `json:"content"`
}

type anthropicTool struct {
  Name        string      `json:"name"`
  Description string      `json:"description"`
  InputSchema interface{} `json:"input_schema"`
}

func (p *AnthropicProvider) StreamChat(ctx context.Context, req *ChatRequest, cb func(event StreamEvent)) error {
  var msgs []anthropicMsg
  for _, m := range req.Messages {
    switch m.Role {
    case "user":
      contentJSON, _ := json.Marshal(m.Content)
      msgs = append(msgs, anthropicMsg{Role: "user", Content: json.RawMessage(contentJSON)})
    case "assistant":
      if len(m.ToolCalls) > 0 {
        blocks := buildAnthropicContentBlocks(m.Content, m.ToolCalls)
        data, _ := json.Marshal(blocks)
        msgs = append(msgs, anthropicMsg{Role: "assistant", Content: data})
      } else {
        contentJSON, _ := json.Marshal(m.Content)
        msgs = append(msgs, anthropicMsg{Role: "assistant", Content: json.RawMessage(contentJSON)})
      }
    case "tool":
      toolResult := []map[string]string{{
        "type":       "tool_result",
        "tool_use_id": m.ToolCallID,
        "content":    m.Content,
      }}
      data, _ := json.Marshal(toolResult)
      msgs = append(msgs, anthropicMsg{Role: "user", Content: json.RawMessage(data)})
    }
  }

  var tools []anthropicTool
  for _, t := range req.Tools {
    tools = append(tools, anthropicTool{
      Name:        t.Name,
      Description: t.Description,
      InputSchema: t.InputSchema,
    })
  }

  maxTokens := req.MaxTokens
  if maxTokens <= 0 {
    maxTokens = 8192
  }
  body := anthropicReq{
    Model:       p.model,
    System:      req.System,
    MaxTokens:   maxTokens,
    Temperature: req.Temperature,
    Messages:    msgs,
    Tools:       tools,
    Stream:      true,
  }

  // 估算 token 用量
  inputTokens := estimateStringTokens(req.System)
  for _, m := range req.Messages {
    inputTokens += estimateStringTokens(m.Content)
  }

  slog.Debug("anthropic.request_start",
    "model", p.model,
    "base_url", p.baseURL,
    "message_count", len(msgs),
    "tool_count", len(tools),
    "max_tokens", req.MaxTokens,
    "temperature", req.Temperature,
    "estimated_input_tokens", inputTokens,
  )

  bodyJSON, _ := json.Marshal(body)
  headers := map[string]string{
    "x-api-key":         p.apiKey,
    "anthropic-version": "2023-06-01",
    "content-type":      "application/json",
  }

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

    if resp.StatusCode == http.StatusTooManyRequests {
      return fmt.Errorf("HTTP 429 Too Many Requests")
    }
    if resp.StatusCode >= 500 {
      return fmt.Errorf("HTTP %d %s", resp.StatusCode, resp.Status)
    }

    return parseAnthropicSSE(innerCtx, resp.Body, cb)
  })
}

func parseAnthropicSSE(ctx context.Context, r io.Reader, cb func(event StreamEvent)) error {
  decoder := json.NewDecoder(r)
  for {
    var raw json.RawMessage
    if err := decoder.Decode(&raw); err != nil {
      if err == io.EOF {
        return nil
      }
      return err
    }

    var base struct {
      Type string `json:"type"`
    }
    json.Unmarshal(raw, &base)

    switch base.Type {
    case "content_block_start":
      var ev struct {
        Index int `json:"index"`
        Block struct {
          Type  string          `json:"type"`
          ID    string          `json:"id"`
          Name  string          `json:"name"`
          Input json.RawMessage `json:"input"`
        } `json:"content_block"`
      }
      json.Unmarshal(raw, &ev)
      if ev.Block.Type == "tool_use" {
        cb(StreamEvent{
          Type: "tool_call_start",
          Data: mustJSON(ToolCallData{ID: ev.Block.ID, Name: ev.Block.Name, Input: ev.Block.Input}),
        })
      }

    case "content_block_delta":
      var ev struct {
        Index int `json:"index"`
        Delta struct {
          Type string `json:"type"`
          Text string `json:"text"`
        } `json:"delta"`
      }
      json.Unmarshal(raw, &ev)
      switch ev.Delta.Type {
      case "text_delta":
        if ev.Delta.Text != "" {
          cb(TextDelta(ev.Delta.Text))
        }
      case "thinking_delta":
        if ev.Delta.Text != "" {
          cb(reasoningEvent(ev.Delta.Text))
        }
      }

    case "message_stop":
      return nil

    case "error":
      var ev struct {
        Error struct {
          Type    string `json:"type"`
          Message string `json:"message"`
        } `json:"error"`
      }
      json.Unmarshal(raw, &ev)
      cb(ErrorEvent(ev.Error.Message))
    }
  }
}

func buildAnthropicContentBlocks(text string, toolCalls []ToolCall) []map[string]interface{} {
  var blocks []map[string]interface{}
  if text != "" {
    blocks = append(blocks, map[string]interface{}{"type": "text", "text": text})
  }
  for _, tc := range toolCalls {
    var args interface{}
    json.Unmarshal([]byte(tc.Function.Arguments), &args)
    blocks = append(blocks, map[string]interface{}{
      "type":  "tool_use",
      "id":    tc.ID,
      "name":  tc.Function.Name,
      "input": args,
    })
  }
  return blocks
}

// ============================================================
// OpenAI 兼容实现（使用官方 Go SDK）
// ============================================================

type OpenAIProvider struct {
  apiKey       string
  baseURL      string
  model        string
  providerType string
}

func NewOpenAIProvider(apiKey, baseURL, model string) *OpenAIProvider {
  if baseURL == "" {
    baseURL = "https://api.openai.com/v1"
  }
  return &OpenAIProvider{apiKey: apiKey, baseURL: baseURL, model: model}
}

func NewDeepSeekProvider(apiKey, baseURL, model string) *OpenAIProvider {
  if baseURL == "" {
    baseURL = "https://api.deepseek.com"
  }
  return &OpenAIProvider{apiKey: apiKey, baseURL: baseURL, model: model, providerType: "deepseek"}
}

func (p *OpenAIProvider) StreamChat(ctx context.Context, req *ChatRequest, cb func(event StreamEvent)) error {
  client := openai.NewClient(
    option.WithAPIKey(p.apiKey),
    option.WithBaseURL(p.baseURL),
  )

  msgs := buildOpenAIMessagesV2(req.System, req.Messages, p.providerType)

  params := openai.ChatCompletionNewParams{
    Model:       p.model,
    Messages:    msgs,
    Temperature: openai.Opt(req.Temperature),
  }

  if p.providerType == "deepseek" && req.UserID != "" {
    params.SetExtraFields(map[string]any{"user_id": req.UserID})
  }
  if req.MaxTokens > 0 {
    params.MaxTokens = openai.Opt(int64(req.MaxTokens))
  }

  if len(req.Tools) > 0 {
    tools := make([]openai.ChatCompletionToolUnionParam, 0, len(req.Tools))
    for _, t := range req.Tools {
      tools = append(tools, openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
        Name:        t.Name,
        Description: openai.Opt(t.Description),
        Parameters:  shared.FunctionParameters(t.InputSchema),
      }))
    }
    params.Tools = tools
  }

  // 估算 token 用量
  inputTokens := estimateStringTokens(req.System)
  for _, m := range req.Messages {
    inputTokens += estimateStringTokens(m.Content)
  }

  slog.Debug("openai.request_start",
    "model", p.model,
    "base_url", p.baseURL,
    "message_count", len(msgs),
    "tool_count", len(req.Tools),
    "max_tokens", req.MaxTokens,
    "temperature", req.Temperature,
    "estimated_input_tokens", inputTokens,
  )

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

      // 推理/思考内容（OpenAI-compatible reasoning_content）
      reasoningRaw := choice.RawJSON()
      if reasoningRaw != "" {
        var rcChoice struct {
          Delta map[string]json.RawMessage `json:"delta"`
        }
        if json.Unmarshal([]byte(reasoningRaw), &rcChoice) == nil {
          if rcData, ok := rcChoice.Delta["reasoning_content"]; ok && len(rcData) > 0 && !bytes.Equal(rcData, []byte("null")) && !bytes.Equal(rcData, []byte(`""`)) {
            var rcStr string
            if json.Unmarshal(rcData, &rcStr) == nil && rcStr != "" {
              cb(reasoningEvent(rcStr))
            }
          }
        }
      }

      // 内容增量
      if choice.Delta.Content != "" {
        textDeltaCount++
        cb(TextDelta(choice.Delta.Content))
      }

      // 完成事件（tool_calls 不做完成处理，等后续积累）
      if choice.FinishReason != "" && choice.FinishReason != "tool_calls" {
        slog.Debug("openai.stream_finish", "finish_reason", string(choice.FinishReason), "chunk_count", chunkCount)
        cb(FinishEvent("", map[string]int{}))
      }
    }

    if err := stream.Err(); err != nil {
      return err
    }

    // 积累完成后检查是否有工具调用
    toolCallCount := 0
    if len(acc.Choices) > 0 {
      for _, tc := range acc.Choices[0].Message.ToolCalls {
        toolCallCount++
        cb(StreamEvent{
          Type: "tool_call_start",
          Data: mustJSON(ToolCallData{
            ID:    tc.ID,
            Name:  tc.Function.Name,
            Input: json.RawMessage(tc.Function.Arguments),
          }),
        })
      }
    }

    // 估算输出 token
    outputTokens := 0
    if len(acc.Choices) > 0 {
      outputTokens = estimateStringTokens(acc.Choices[0].Message.Content)
    }

    slog.Debug("openai.request_complete",
      "model", p.model,
      "chunk_count", chunkCount,
      "text_deltas", textDeltaCount,
      "tool_calls", toolCallCount,
      "estimated_output_tokens", outputTokens,
      "total_estimated_tokens", inputTokens+outputTokens,
    )

    return nil
  })
}

func buildOpenAIMessagesV2(system string, messages []LLMMessage, providerType string) []openai.ChatCompletionMessageParamUnion {
  var msgs []openai.ChatCompletionMessageParamUnion
  if system != "" {
    msgs = append(msgs, openai.SystemMessage(system))
  }
  for _, m := range messages {
    switch m.Role {
    case "user":
      msgs = append(msgs, openai.UserMessage(m.Content))
    case "assistant":
      if len(m.ToolCalls) > 0 {
        tcs := make([]openai.ChatCompletionMessageToolCallUnionParam, 0, len(m.ToolCalls))
        for _, tc := range m.ToolCalls {
          tcs = append(tcs, openai.ChatCompletionMessageToolCallUnionParam{
            OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
              ID:   tc.ID,
              Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
                Name:      tc.Function.Name,
                Arguments: tc.Function.Arguments,
              },
            },
          })
        }
        asst := &openai.ChatCompletionAssistantMessageParam{
          Content:   openai.ChatCompletionAssistantMessageParamContentUnion{OfString: openai.Opt(m.Content)},
          ToolCalls: tcs,
        }
        if providerType == "deepseek" && m.ReasoningContent != "" {
          asst.SetExtraFields(map[string]any{"reasoning_content": m.ReasoningContent})
        }
        msgs = append(msgs, openai.ChatCompletionMessageParamUnion{OfAssistant: asst})
      } else {
        if providerType == "deepseek" && m.ReasoningContent != "" {
          asst := &openai.ChatCompletionAssistantMessageParam{
            Content: openai.ChatCompletionAssistantMessageParamContentUnion{OfString: openai.Opt(m.Content)},
          }
          asst.SetExtraFields(map[string]any{"reasoning_content": m.ReasoningContent})
          msgs = append(msgs, openai.ChatCompletionMessageParamUnion{OfAssistant: asst})
        } else {
          msgs = append(msgs, openai.AssistantMessage(m.Content))
        }
      }
    case "tool":
      msgs = append(msgs, openai.ToolMessage(m.Content, m.ToolCallID))
    }
  }
  return msgs
}

func buildDeepSeekMessages(messages []LLMMessage) []map[string]interface{} {
  raw := make([]map[string]interface{}, 0, len(messages))
  for _, m := range messages {
    msg := map[string]interface{}{
      "role": m.Role,
    }
    if m.Content != "" {
      msg["content"] = m.Content
    }
    if m.ReasoningContent != "" {
      msg["reasoning_content"] = m.ReasoningContent
    }
    if m.ToolCallID != "" {
      msg["tool_call_id"] = m.ToolCallID
    }
    if len(m.ToolCalls) > 0 {
      tcs := make([]map[string]interface{}, 0, len(m.ToolCalls))
      for _, tc := range m.ToolCalls {
        var args interface{}
        json.Unmarshal([]byte(tc.Function.Arguments), &args)
        tcs = append(tcs, map[string]interface{}{
          "id":   tc.ID,
          "type": tc.Type,
          "function": map[string]interface{}{
            "name":      tc.Function.Name,
            "arguments": tc.Function.Arguments,
          },
        })
      }
      msg["tool_calls"] = tcs
    }
    raw = append(raw, msg)
  }
  return raw
}


