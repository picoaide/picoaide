package agent

import (
  "math"
  "time"
)

// ============================================================
// 重试退避（参考 OpenCode retry.ts 设计）
// ============================================================

const (
  RetryInitialDelay = 2000 * time.Millisecond
  RetryBackoffFactor = 2
  RetryMaxDelay     = 30 * time.Second
)

type RetryPolicy struct {
  MaxAttempts    int
  InitialDelay   time.Duration
  BackoffFactor  float64
  MaxDelay       time.Duration
}

func DefaultRetryPolicy() *RetryPolicy {
  return &RetryPolicy{
    MaxAttempts:   3,
    InitialDelay:  RetryInitialDelay,
    BackoffFactor: RetryBackoffFactor,
    MaxDelay:      RetryMaxDelay,
  }
}

// Delay 计算第 n 次尝试的延迟时间（n 从 0 开始）
func (p *RetryPolicy) Delay(attempt int) time.Duration {
  if attempt < 0 {
    attempt = 0
  }
  delay := float64(p.InitialDelay) * math.Pow(p.BackoffFactor, float64(attempt))
  if delay > float64(p.MaxDelay) {
    delay = float64(p.MaxDelay)
  }
  return time.Duration(delay)
}
