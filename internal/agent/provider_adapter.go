package agent

import (
  "context"
  "encoding/json"
  "fmt"
  "iter"
  "log/slog"
  "strings"

  "google.golang.org/adk/v2/model"
  "google.golang.org/genai"
)

// ADKProviderAdapter wraps a picoagent Provider as ADK model.LLM.
// This is the bridge between our existing LLM providers and ADK's agent framework.
type ADKProviderAdapter struct {
  inner          Provider
  name           string
  disableTools   bool
  requestTimeout int
}

func NewADKProviderAdapter(provider Provider, name string) *ADKProviderAdapter {
  return &ADKProviderAdapter{inner: provider, name: name}
}

func (a *ADKProviderAdapter) SetDisableTools(v bool) { a.disableTools = v }
func (a *ADKProviderAdapter) SetRequestTimeout(s int) { a.requestTimeout = s }

func (a *ADKProviderAdapter) Name() string { return a.name }

func (a *ADKProviderAdapter) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
  return func(yield func(*model.LLMResponse, error) bool) {
    slog.Debug("adapter.generate_content", "model", req.Model, "stream", stream, "contents", len(req.Contents))

    chatReq := a.buildChatReqFromLLM(req)
    if chatReq == nil {
      yield(nil, fmt.Errorf("构建 LLM 请求失败"))
      return
    }

    if stream {
      a.streamGenerate(ctx, chatReq, yield)
    } else {
      a.syncGenerate(ctx, chatReq, yield)
    }
  }
}

func (a *ADKProviderAdapter) syncGenerate(ctx context.Context, chatReq *ChatRequest, yield func(*model.LLMResponse, error) bool) {
  var textParts []string
  var toolCalls []*genai.FunctionCall

  err := a.inner.StreamChat(ctx, chatReq, func(event StreamEvent) {
    switch event.Type {
    case "text_delta":
      var text string
      if json.Unmarshal(event.Data, &text) == nil {
        textParts = append(textParts, text)
      }
    case "tool_call_start":
      var tc ToolCallData
      if json.Unmarshal(event.Data, &tc) == nil {
        var args map[string]any
        json.Unmarshal(tc.Input, &args)
        toolCalls = append(toolCalls, &genai.FunctionCall{
          Name: tc.Name,
          Args: args,
          ID:   tc.ID,
        })
      }
    }
  })

  if err != nil {
    yield(nil, err)
    return
  }

  resp := buildLLMResponse(strings.Join(textParts, ""), toolCalls, false)
  yield(resp, nil)
}

func (a *ADKProviderAdapter) streamGenerate(ctx context.Context, chatReq *ChatRequest, yield func(*model.LLMResponse, error) bool) {
  type item struct {
    resp *model.LLMResponse
    err  error
  }
  ch := make(chan item, 64)

  go func() {
    defer close(ch)

    var toolCalls []*genai.FunctionCall
    var textParts []string

    err := a.inner.StreamChat(ctx, chatReq, func(event StreamEvent) {
      switch event.Type {
      case "text_delta":
        var text string
        if json.Unmarshal(event.Data, &text) == nil {
          textParts = append(textParts, text)
          resp := buildLLMResponse(text, nil, true)
          ch <- item{resp: resp}
        }
      case "tool_call_start":
        var tc ToolCallData
        if json.Unmarshal(event.Data, &tc) == nil {
          var args map[string]any
          json.Unmarshal(tc.Input, &args)
          toolCalls = append(toolCalls, &genai.FunctionCall{
            Name: tc.Name,
            Args: args,
            ID:   tc.ID,
          })
        }
      case "error":
        var errMsg string
        json.Unmarshal(event.Data, &errMsg)
        ch <- item{err: fmt.Errorf("%s", errMsg)}
      }
    })

    if err != nil {
      ch <- item{err: err}
      return
    }

    fullText := strings.Join(textParts, "")
    resp := buildLLMResponse(fullText, toolCalls, false)
    resp.TurnComplete = true
    ch <- item{resp: resp}
  }()

  for item := range ch {
    if !yield(item.resp, item.err) {
      return
    }
  }
}

// buildChatReqFromLLM converts ADK's model.LLMRequest to our ChatRequest.
func (a *ADKProviderAdapter) buildChatReqFromLLM(req *model.LLMRequest) *ChatRequest {
  system := ""
  temp := 0.7
  maxTokens := 0

  if req.Config != nil {
    if req.Config.SystemInstruction != nil {
      system = extractTextContent(req.Config.SystemInstruction)
    }
    if req.Config.Temperature != nil {
      temp = float64(*req.Config.Temperature)
    }
    if req.Config.MaxOutputTokens > 0 {
      maxTokens = int(req.Config.MaxOutputTokens)
    }
  }

  messages := convertGenaiContents(req.Contents)
  toolDefs := convertGenaiTools(req.Config)

  return &ChatRequest{
    Model:          req.Model,
    System:         system,
    Messages:       messages,
    Tools:          toolDefs,
    MaxTokens:      maxTokens,
    Temperature:    temp,
    DisableTools:   a.disableTools,
    RequestTimeout: a.requestTimeout,
  }
}

// extractTextContent concatenates Text parts from a genai Content.
func extractTextContent(c *genai.Content) string {
  if c == nil {
    return ""
  }
  var parts []string
  for _, p := range c.Parts {
    if p.Text != "" {
      parts = append(parts, p.Text)
    }
  }
  return strings.Join(parts, "\n")
}

// convertGenaiContents converts []*genai.Content to our []LLMMessage.
func convertGenaiContents(contents []*genai.Content) []LLMMessage {
  var msgs []LLMMessage
  for _, c := range contents {
    if c == nil {
      continue
    }
    msgs = append(msgs, genaiContentToMsg(c))
  }
  return msgs
}

// genaiContentToMsg converts a single genai.Content to an LLMMessage.
func genaiContentToMsg(c *genai.Content) LLMMessage {
  // tool responses have role "user" in genai, handle via FunctionResponse first
  for _, p := range c.Parts {
    if p.FunctionResponse != nil {
      respJSON, _ := json.Marshal(p.FunctionResponse.Response)
      return LLMMessage{
        Role:       "tool",
        ToolCallID: p.FunctionResponse.ID,
        Content:    string(respJSON),
      }
    }
  }

  switch c.Role {
  case "user":
    return LLMMessage{
      Role:    "user",
      Content: extractTextContent(c),
    }
  case "model":
    msg := LLMMessage{Role: "assistant"}
    var textParts []string
    var reasoningParts []string
    for _, p := range c.Parts {
      if p.Thought {
        if p.Text != "" {
          reasoningParts = append(reasoningParts, p.Text)
        }
      } else if p.Text != "" {
        textParts = append(textParts, p.Text)
      }
      if p.FunctionCall != nil {
        args, _ := json.Marshal(p.FunctionCall.Args)
        msg.ToolCalls = append(msg.ToolCalls, ToolCall{
          ID:   p.FunctionCall.ID,
          Type: "function",
          Function: ToolFunction{
            Name:      p.FunctionCall.Name,
            Arguments: string(args),
          },
        })
      }
    }
    msg.Content = strings.Join(textParts, "")
    msg.ReasoningContent = strings.Join(reasoningParts, "")
    return msg
  default:
    return LLMMessage{
      Role:    "user",
      Content: extractTextContent(c),
    }
  }
}

// convertGenaiTools extracts tool definitions from genai config.
func convertGenaiTools(config *genai.GenerateContentConfig) []ToolDef {
  if config == nil || len(config.Tools) == 0 {
    return nil
  }
  var defs []ToolDef
  for _, t := range config.Tools {
    for _, fd := range t.FunctionDeclarations {
      var schema map[string]interface{}
      if fd.Parameters != nil {
        schema = genaiSchemaToMap(fd.Parameters)
      } else {
        schema = map[string]interface{}{"type": "object"}
      }
      defs = append(defs, ToolDef{
        Name:        fd.Name,
        Description: fd.Description,
        InputSchema: schema,
      })
    }
  }
  return defs
}

// genaiSchemaToMap converts genai.Schema to our map format.
func genaiSchemaToMap(s *genai.Schema) map[string]interface{} {
  if s == nil {
    return map[string]interface{}{"type": "object"}
  }
  m := map[string]interface{}{
    "type": schemaTypeString(s.Type),
  }
  if s.Description != "" {
    m["description"] = s.Description
  }
  if len(s.Enum) > 0 {
    m["enum"] = s.Enum
  }
  if len(s.Properties) > 0 {
    props := map[string]interface{}{}
    for k, v := range s.Properties {
      props[k] = genaiSchemaToMap(v)
    }
    m["properties"] = props
  }
  if len(s.Required) > 0 {
    m["required"] = s.Required
  }
  return m
}

func schemaTypeString(t genai.Type) string {
  switch t {
  case genai.TypeString:
    return "string"
  case genai.TypeInteger:
    return "integer"
  case genai.TypeNumber:
    return "number"
  case genai.TypeBoolean:
    return "boolean"
  case genai.TypeArray:
    return "array"
  case genai.TypeObject:
    return "object"
  default:
    return "string"
  }
}

// buildLLMResponse creates an ADK LLMResponse from text and tool calls.
func buildLLMResponse(text string, toolCalls []*genai.FunctionCall, partial bool) *model.LLMResponse {
  var parts []*genai.Part
  if text != "" {
    parts = append(parts, &genai.Part{Text: text})
  }
  for _, tc := range toolCalls {
    parts = append(parts, &genai.Part{FunctionCall: tc})
  }
  if len(parts) == 0 {
    return &model.LLMResponse{
      Content: &genai.Content{Role: "model"},
      Partial: partial,
    }
  }
  return &model.LLMResponse{
    Content: &genai.Content{
      Role:  "model",
      Parts: parts,
    },
    Partial: partial,
  }
}
