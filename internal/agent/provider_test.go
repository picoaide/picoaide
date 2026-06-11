package agent

import (
  "context"
  "encoding/json"
  "errors"
  "net"
  "strings"
  "testing"
  "time"

  "github.com/openai/openai-go/v3"
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

func fakeDeepSeekParams(model string, messages []LLMMessage, userID string) openai.ChatCompletionNewParams {
  msgs := buildOpenAIMessagesV2("system", messages, "deepseek")
  p := openai.ChatCompletionNewParams{
    Model:    model,
    Messages: msgs,
  }
  if userID != "" {
    p.SetExtraFields(map[string]any{"user_id": userID})
  }
  return p
}

func TestBuildDeepSeekMessages_IncludesReasoningContent(t *testing.T) {
  msgs := []LLMMessage{
    {Role: "user", Content: "hello"},
    {Role: "assistant", Content: "visible", ReasoningContent: "thinking", ToolCalls: []ToolCall{{ID: "t1", Type: "function", Function: ToolFunction{Name: "fn", Arguments: "{}"}}}},
    {Role: "tool", Content: "result", ToolCallID: "t1"},
  }
  raw := buildDeepSeekMessages(msgs)
  data, _ := json.Marshal(raw)
  var parsed []map[string]interface{}
  json.Unmarshal(data, &parsed)

  if len(parsed) != 3 {
    t.Fatalf("expected 3 messages, got %d", len(parsed))
  }

  // assistant message should have reasoning_content and content
  assistant := parsed[1]
  if assistant["role"] != "assistant" {
    t.Errorf("expected role=assistant, got %v", assistant["role"])
  }
  if assistant["content"] != "visible" {
    t.Errorf("expected content=visible, got %v", assistant["content"])
  }
  if assistant["reasoning_content"] != "thinking" {
    t.Errorf("expected reasoning_content=thinking, got %v", assistant["reasoning_content"])
  }

  // user message should NOT have reasoning_content
  user := parsed[0]
  if _, ok := user["reasoning_content"]; ok {
    t.Errorf("user message should not have reasoning_content")
  }
}

func TestBuildDeepSeekMessages_SkipsEmptyReasoningContent(t *testing.T) {
  msgs := []LLMMessage{
    {Role: "assistant", Content: "no thinking"},
  }
  raw := buildDeepSeekMessages(msgs)
  data, _ := json.Marshal(raw)
  var parsed []map[string]interface{}
  json.Unmarshal(data, &parsed)

  if _, ok := parsed[0]["reasoning_content"]; ok {
    t.Errorf("should not include reasoning_content when empty")
  }
}

func TestSetExtraFields_UserID(t *testing.T) {
  p := fakeDeepSeekParams("deepseek-v4-flash", []LLMMessage{{Role: "user", Content: "hi"}}, "yangting")
  data, _ := p.MarshalJSON()
  var parsed map[string]interface{}
  json.Unmarshal(data, &parsed)
  if parsed["user_id"] != "yangting" {
    t.Errorf("expected user_id=yangting, got %v", parsed["user_id"])
  }
}

func TestSetExtraFields_EmptyUserID(t *testing.T) {
  p := fakeDeepSeekParams("deepseek-v4-flash", []LLMMessage{{Role: "user", Content: "hi"}}, "")
  data, _ := p.MarshalJSON()
  var parsed map[string]interface{}
  json.Unmarshal(data, &parsed)
  if _, ok := parsed["user_id"]; ok {
    t.Errorf("should not include user_id when empty")
  }
}

func TestBuildOpenAIMessagesV2_ReasoningContent(t *testing.T) {
  msgs := []LLMMessage{
    {Role: "user", Content: "hello"},
    {Role: "assistant", Content: "visible", ReasoningContent: "thinking", ToolCalls: []ToolCall{{ID: "t1", Type: "function", Function: ToolFunction{Name: "fn", Arguments: "{}"}}}},
  }
  result := buildOpenAIMessagesV2("system", msgs, "deepseek")
  data, _ := json.Marshal(result)
  t.Logf("raw JSON: %s", string(data))
  var parsed []map[string]interface{}
  json.Unmarshal(data, &parsed)
  if len(parsed) != 3 {
    t.Fatalf("expected 3 messages, got %d", len(parsed))
  }
  asst := parsed[2] // system 0, user 1, assistant 2
  if asst["reasoning_content"] != "thinking" {
    t.Errorf("expected reasoning_content=thinking, got %v", asst["reasoning_content"])
  }
}

func TestBuildOpenAIMessagesV2_NoReasoningContentForNonDeepSeek(t *testing.T) {
  msgs := []LLMMessage{
    {Role: "user", Content: "hello"},
    {Role: "assistant", Content: "visible", ReasoningContent: "thinking"},
  }
  result := buildOpenAIMessagesV2("system", msgs, "openai")
  data, _ := json.Marshal(result)
  var parsed []map[string]interface{}
  json.Unmarshal(data, &parsed)
  if len(parsed) < 3 {
    t.Fatalf("expected >=3 messages, got %d", len(parsed))
  }
  if _, ok := parsed[2]["reasoning_content"]; ok {
    t.Errorf("non-deepseek should not include reasoning_content")
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
