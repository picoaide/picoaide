package web

import (
  "net"
  "net/http"
  "sync"
  "time"

  "github.com/gin-gonic/gin"
)

const (
  loginRateLimitAttempts = 10
  loginRateLimitWindow   = 5 * time.Minute
  auditRateLimitAttempts = 500
  auditRateLimitWindow   = 1 * time.Minute
)

type rateLimiter struct {
  mu       sync.Mutex
  attempts map[string][]time.Time
  limit    int
  window   time.Duration
  done     chan struct{}
  stopped  bool
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
  rl := &rateLimiter{
    attempts: make(map[string][]time.Time),
    limit:    limit,
    window:   window,
    done:     make(chan struct{}),
  }
  go rl.cleanup()
  return rl
}

func (rl *rateLimiter) Stop() {
  rl.mu.Lock()
  defer rl.mu.Unlock()
  if !rl.stopped {
    rl.stopped = true
    close(rl.done)
  }
}

func newLoginRateLimiter() *rateLimiter {
  return newRateLimiter(loginRateLimitAttempts, loginRateLimitWindow)
}

func newAuditRateLimiter() *rateLimiter {
  return newRateLimiter(auditRateLimitAttempts, auditRateLimitWindow)
}

func (rl *rateLimiter) cleanup() {
  ticker := time.NewTicker(time.Minute)
  defer ticker.Stop()
  for {
    select {
    case <-rl.done:
      return
    case <-ticker.C:
      rl.mu.Lock()
      now := time.Now()
      for ip, times := range rl.attempts {
        var valid []time.Time
        for _, t := range times {
          if now.Sub(t) < rl.window {
            valid = append(valid, t)
          }
        }
        if len(valid) == 0 {
          delete(rl.attempts, ip)
        } else {
          rl.attempts[ip] = valid
        }
      }
      rl.mu.Unlock()
    }
  }
}

func (rl *rateLimiter) allow(ip string) bool {
  rl.mu.Lock()
  defer rl.mu.Unlock()
  now := time.Now()
  times := rl.attempts[ip]
  var valid []time.Time
  for _, t := range times {
    if now.Sub(t) < rl.window {
      valid = append(valid, t)
    }
  }
  if len(valid) >= rl.limit {
    rl.attempts[ip] = valid
    return false
  }
  valid = append(valid, now)
  rl.attempts[ip] = valid
  return true
}

func clientIPFromRequest(r *http.Request) string {
  host, _, err := net.SplitHostPort(r.RemoteAddr)
  if err != nil {
    return r.RemoteAddr
  }
  return host
}

// rateLimitLogin 返回 Gin 中间件用于登录限流
func (s *Server) rateLimitLogin() gin.HandlerFunc {
  return func(c *gin.Context) {
    if s.loginLimiter != nil {
      ip := clientIPFromRequest(c.Request)
      if !s.loginLimiter.allow(ip) {
        writeError(c, http.StatusTooManyRequests, "请求过于频繁，请稍后再试")
        c.Abort()
        return
      }
    }
    c.Next()
  }
}
