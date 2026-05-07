package web

import (
  "net"
  "net/http"
  "sync"
  "time"
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

func clientIP(r *http.Request) string {
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

func (s *Server) rateLimitLogin(next http.HandlerFunc) http.HandlerFunc {
  return func(w http.ResponseWriter, r *http.Request) {
    if s.loginLimiter != nil {
      ip := clientIP(r)
      if !s.loginLimiter.allow(ip) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusTooManyRequests)
        w.Write([]byte(`{"success":false,"error":"请求过于频繁，请稍后再试"}`))
        return
      }
    }
    next(w, r)
  }
}
