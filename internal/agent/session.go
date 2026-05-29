package agent

import (
  "crypto/sha256"
  "encoding/hex"
  "encoding/json"
  "fmt"
  "strings"
)

// ============================================================
// 消息类型（兼容 PicoClaw JSONL 格式）
// ============================================================

type MessageRole string

const (
  RoleUser      MessageRole = "user"
  RoleAssistant MessageRole = "assistant"
  RoleTool      MessageRole = "tool"
  RoleSystem    MessageRole = "system"
)

type Attachment struct {
  Type        string `json:"type,omitempty"`
  Ref         string `json:"ref,omitempty"`
  URL         string `json:"url,omitempty"`
  Filename    string `json:"filename,omitempty"`
  ContentType string `json:"content_type,omitempty"`
}

type ToolCall struct {
  ID       string       `json:"id"`
  Type     string       `json:"type"`
  Function ToolFunction `json:"function"`
}

type ToolFunction struct {
  Name      string `json:"name"`
  Arguments string `json:"arguments"`
}

type Message struct {
  Role             MessageRole  `json:"role"`
  Content          string       `json:"content,omitempty"`
  Media            []string     `json:"media,omitempty"`
  Attachments      []Attachment `json:"attachments,omitempty"`
  ReasoningContent string       `json:"reasoning_content,omitempty"`
  ToolCalls        []ToolCall   `json:"tool_calls,omitempty"`
  ToolCallID       string       `json:"tool_call_id,omitempty"`
}

// ============================================================
// Session 会话（兼容 PicoClaw JSONL + meta）
// ============================================================

type SessionScope struct {
  Version    int               `json:"version"`
  AgentID    string            `json:"agent_id"`
  Channel    string            `json:"channel"`
  Account    string            `json:"account"`
  Dimensions []string          `json:"dimensions"`
  Values     map[string]string `json:"values"`
}

type SessionMeta struct {
  Key       string        `json:"key"`
  Summary   string        `json:"summary"`
  Skip      int           `json:"skip"`
  Count     int           `json:"count"`
  CreatedAt string        `json:"created_at"`
  UpdatedAt string        `json:"updated_at"`
  Scope     *SessionScope `json:"scope,omitempty"`
  Aliases   []string      `json:"aliases,omitempty"`
}

// ============================================================
// Session Key 生成（统一按用户，跨渠道）
// ============================================================

// BuildSessionKey 生成统一 session key
// 同一个人不管从钉钉/飞书/企微/Web UI 进来，都是同一个 session
// 格式: sk_v1_{sha256(agent:user:{userID})}
func BuildSessionKey(scope SessionScope) string {
  userID := ""
  for _, dim := range scope.Dimensions {
    if v, ok := scope.Values[dim]; ok && v != "" {
      userID = v
      break
    }
  }
  if userID == "" && scope.Account != "" {
    userID = scope.Account
  }
  sig := fmt.Sprintf("agent:user:%s", userID)
  if scope.AgentID != "" && scope.AgentID != "pico" {
    sig = fmt.Sprintf("agent:%s:user:%s", scope.AgentID, userID)
  }
  sum := sha256.Sum256([]byte(sig))
  return "sk_v1_" + hex.EncodeToString(sum[:])
}

func SanitizeKey(key string) string {
  r := strings.NewReplacer(":", "_", "/", "_", "\\", "_")
  return r.Replace(key)
}

// ============================================================
// LLM 消息格式（最终发送给 SDK）
// ============================================================

type LLMMessage struct {
  Role       string     `json:"role"`
  Content    string     `json:"content,omitempty"`
  ToolCallID string     `json:"tool_call_id,omitempty"`
  ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

func (m *Message) ToLLMMessage() LLMMessage {
  return LLMMessage{
    Role:       string(m.Role),
    Content:    m.Content,
    ToolCallID: m.ToolCallID,
    ToolCalls:  m.ToolCalls,
  }
}

// ============================================================
// 配置（从宿主办 API 获取）
// ============================================================

type AgentConfig struct {
  UserID    string `json:"user_id"`
  Workspace string `json:"workspace"`

  Model ModelConfig `json:"model"`

  Tools      map[string]ToolConfig  `json:"tools"`
  MCPServers map[string]MCPServer   `json:"mcp_servers"`

  MaxIter        int `json:"max_iter,omitempty"`        // ReAct 最大迭代次数，默认 20
  MaxTokens      int `json:"max_tokens,omitempty"`      // 每次 LLM 调用的最大 token 数（0 表示引擎内置 100000，不硬截断）
  RequestTimeout int `json:"request_timeout,omitempty"` // LLM API 请求超时秒数，默认 120
}

type ModelConfig struct {
  Provider       string  `json:"provider"`
  ModelID        string  `json:"model_id"`
  BaseURL        string  `json:"base_url,omitempty"`
  MaxTokens      int     `json:"max_tokens,omitempty"`      // 每次 LLM 调用最大 token 数（0 表示引擎内置 100000，不硬截断）
  MaxIter        int     `json:"max_iter,omitempty"`        // ReAct 最大迭代次数，默认 20
  Temperature    float64 `json:"temperature,omitempty"`     // LLM 温度，默认 0.7
  ContextWindow  int     `json:"context_window,omitempty"`  // 上下文窗口大小，默认 200000
  RequestTimeout int     `json:"request_timeout,omitempty"` // 请求超时秒数，默认 120
}

// Secrets 从宿主办注入的密钥文件（非 API 返回）
type Secrets struct {
  APIKey string `json:"api_key"`
}

type ToolConfig struct {
  Enabled bool `json:"enabled"`
}

type MCPServer struct {
  Socket string `json:"socket"` // Unix socket 路径
}

// ============================================================
// 流式输出事件（通过 stdout JSON Lines 发给宿主办）
// ============================================================

type StreamEvent struct {
  Type string          `json:"type"`
  Data json.RawMessage `json:"data,omitempty"`
}

func TextDelta(text string) StreamEvent {
  return StreamEvent{Type: "text_delta", Data: json.RawMessage(jsonString(text))}
}

func ToolCallEvent(name string, args interface{}) StreamEvent {
  argsJSON, _ := json.Marshal(args)
  return StreamEvent{Type: "tool_call", Data: argsJSON}
}

func FinishEvent(content string, usage map[string]int) StreamEvent {
  data, _ := json.Marshal(map[string]interface{}{
    "content": content,
    "usage":   usage,
  })
  return StreamEvent{Type: "finish", Data: data}
}

func ErrorEvent(err string) StreamEvent {
  return StreamEvent{Type: "error", Data: json.RawMessage(jsonString(err))}
}

// ============================================================
// 工具定义
// ============================================================

type ToolDef struct {
  Name        string                 `json:"name"`
  Description string                 `json:"description"`
  InputSchema map[string]interface{} `json:"inputSchema"`
}

func jsonString(s string) string {
  b, _ := json.Marshal(s)
  return string(b)
}
