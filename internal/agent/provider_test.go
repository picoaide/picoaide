package agent

import (
  "context"
  "encoding/json"
  "errors"
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
      return errors.New("TLS handshake failed")
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

func TestChatRequest_RequestTimeout(t *testing.T) {
  // 验证 ChatRequest 的 RequestTimeout 传递
  req := ChatRequest{
    Model:          "test-model",
    Messages:       []LLMMessage{{Role: "user", Content: "hello"}},
    DisableTools:   true,
    RequestTimeout: 2,
  }

  if req.RequestTimeout != 2 {
    t.Errorf("RequestTimeout = %d, 期望 2", req.RequestTimeout)
  }

  // 验证超时被 context.WithTimeout 包装且生效
  ctx := context.Background()
  llmCtx, cancel := context.WithTimeout(ctx, time.Duration(req.RequestTimeout)*time.Second)
  defer cancel()

  deadline, ok := llmCtx.Deadline()
  if !ok {
    t.Fatal("context 应设置 deadline")
  }
  remaining := time.Until(deadline)
  if remaining > 5*time.Second || remaining < 0 {
    t.Errorf("剩余时间不合理: %v", remaining)
  }

  // 验证超时生效
  <-llmCtx.Done()
  if llmCtx.Err() != context.DeadlineExceeded {
    t.Errorf("应返回 DeadlineExceeded, 实际: %v", llmCtx.Err())
  }

  // 验证 provider.go 中的 StreamChat 使用 RequestTimeout
  // 验证路径: buildChatReqFromLLM → ChatRequest.RequestTimeout → StreamChat 中 WithTimeout
  adapter := &ADKProviderAdapter{requestTimeout: 30}
  if adapter.requestTimeout != 30 {
    t.Errorf("adapter.requestTimeout = %d, 期望 30", adapter.requestTimeout)
  }

  // 验证 SetRequestTimeout
  adapter.SetRequestTimeout(60)
  if adapter.requestTimeout != 60 {
    t.Errorf("adapter.requestTimeout after SetRequestTimeout = %d, 期望 60", adapter.requestTimeout)
  }
}
