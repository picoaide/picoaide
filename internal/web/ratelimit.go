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
)

type rateLimiter struct {
  mu       sync.Mutex
  attempts map[string][]time.Time
  limit    int
  window   time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
  rl := &rateLimiter{
    attempts: make(map[string][]time.Time),
    limit:    limit,
    window:   window,
  }
  go rl.cleanup()
  return rl
}

func newLoginRateLimiter() *rateLimiter {
  return newRateLimiter(loginRateLimitAttempts, loginRateLimitWindow)
}

func (rl *rateLimiter) cleanup() {
  ticker := time.NewTicker(time.Minute)
  defer ticker.Stop()
  for range ticker.C {
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
  if ip := r.Header.Get("X-Real-IP"); ip != "" {
    return ip
  }
  if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
    if idx := stringsIndex(ip, ","); idx > 0 {
      return ip[:idx]
    }
    return ip
  }
  host, _, err := net.SplitHostPort(r.RemoteAddr)
  if err != nil {
    return r.RemoteAddr
  }
  return host
}

func stringsIndex(s, substr string) int {
  for i := 0; i <= len(s)-len(substr); i++ {
    if s[i:i+len(substr)] == substr {
      return i
    }
  }
  return -1
}

// rateLimitLogin 返回 Gin 中间件用于登录限流
func (s *Server) rateLimitLogin() gin.HandlerFunc {
  return func(c *gin.Context) {
    if s.loginLimiter != nil {
      ip := clientIPFromRequest(c.Request)
      if !s.loginLimiter.allow(ip) {
        c.JSON(http.StatusTooManyRequests, gin.H{
          "success": false,
          "error":   "请求过于频繁，请稍后再试",
        })
        c.Abort()
        return
      }
    }
    c.Next()
  }
}
