package agent

import (
  "context"
  "errors"
  "net"
  "strings"
  "testing"
  "time"
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
