package web

import (
  "context"
  "net/http"

  "github.com/PicoAide/PicoAide/internal/config"
)

type contextKey string

const authModeKey contextKey = "auth_mode"

// withAuthMode 将认证模式信息注入请求上下文
func withAuthMode(cfg *config.GlobalConfig, next http.HandlerFunc) http.HandlerFunc {
  return func(w http.ResponseWriter, r *http.Request) {
    ctx := context.WithValue(r.Context(), authModeKey, cfg.AuthMode())
    next(w, r.WithContext(ctx))
  }
}

// getAuthMode 从请求上下文获取认证模式
func getAuthMode(r *http.Request) string {
  if v := r.Context().Value(authModeKey); v != nil {
    return v.(string)
  }
  return "local"
}

// isUnifiedAuth 判断当前是否为统一认证模式
func isUnifiedAuth(r *http.Request) bool {
  return getAuthMode(r) != "local"
}

// requireLocalMode 仅允许本地模式访问（创建用户、修改密码）
func requireLocalMode(s *Server, next http.HandlerFunc) http.HandlerFunc {
  return func(w http.ResponseWriter, r *http.Request) {
    if s.cfg.UnifiedAuthEnabled() {
      writeError(w, http.StatusForbidden, "统一认证模式下不允许此操作，用户由外部认证系统管理")
      return
    }
    next(w, r)
  }
}

// requireUnifiedMode 仅允许统一认证模式访问（白名单管理）
func requireUnifiedMode(s *Server, next http.HandlerFunc) http.HandlerFunc {
  return func(w http.ResponseWriter, r *http.Request) {
    if !s.cfg.UnifiedAuthEnabled() {
      writeError(w, http.StatusForbidden, "本地模式下无需白名单管理")
      return
    }
    next(w, r)
  }
}
