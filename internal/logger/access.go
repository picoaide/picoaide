package logger

import (
  "bufio"
  "log/slog"
  "net"
  "net/http"
  "time"
)

type responseRecorder struct {
  http.ResponseWriter
  statusCode int
}

func (r *responseRecorder) WriteHeader(code int) {
  r.statusCode = code
  r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Flush() {
  if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
    flusher.Flush()
  }
}

// Hijack 实现 http.Hijacker 接口，使 WebSocket 升级能透传到底层连接
func (r *responseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
  return r.ResponseWriter.(http.Hijacker).Hijack()
}

// AccessMiddleware HTTP 请求日志中间件
func AccessMiddleware(next http.Handler) http.Handler {
  return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    start := time.Now()
    rec := &responseRecorder{ResponseWriter: w, statusCode: 200}

    next.ServeHTTP(rec, r)

    duration := time.Since(start)

    // 跳过健康检查等高频低价值路径
    path := r.URL.Path
    if path == "/favicon.ico" || path == "/robots.txt" {
      return
    }

    // 获取当前用户（从 context 或 header 中）
    user := extractUser(r)

    slog.Info("access",
      "type", "ACCESS",
      "method", r.Method,
      "path", path,
      "status", rec.statusCode,
      "duration", duration.String(),
      "ip", clientIP(r),
      "user", user,
    )
  })
}

func extractUser(r *http.Request) string {
  // 尝试从 query 获取 token 中的用户名
  if t := r.URL.Query().Get("token"); t != "" {
    if idx := stringsIndex(t, ":"); idx > 0 {
      return t[:idx]
    }
  }
  // 尝试从 Authorization header 获取
  if auth := r.Header.Get("Authorization"); len(auth) > 7 && auth[:7] == "Bearer " {
    token := auth[7:]
    if idx := stringsIndex(token, ":"); idx > 0 {
      return token[:idx]
    }
  }
  return "-"
}

func clientIP(r *http.Request) string {
  if ip := r.Header.Get("X-Real-IP"); ip != "" {
    return ip
  }
  if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
    return ip
  }
  return r.RemoteAddr
}

func stringsIndex(s, substr string) int {
  for i := 0; i <= len(s)-len(substr); i++ {
    if s[i:i+len(substr)] == substr {
      return i
    }
  }
  return -1
}
