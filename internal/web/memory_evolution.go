package web

import (
  "errors"
  "io"
  "log/slog"
  "net/http"
  "strings"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/store"
)

type auditRequest struct {
  Username       string `json:"username"`
  SessionKey     string `json:"session_key"`
  ChangesSummary string `json:"changes_summary"`
  FilesModified  string `json:"files_modified"`
}

func (s *Server) handlePicoAgentAudit(c *gin.Context) {
  if s.auditLimiter != nil {
    if !s.auditLimiter.allow(clientIPFromRequest(c.Request)) {
      writeError(c, http.StatusTooManyRequests, "请求过于频繁，请稍后再试")
      return
    }
  }

  authHeader := c.GetHeader("Authorization")
  if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
    writeError(c, http.StatusUnauthorized, "缺少 Authorization 头")
    return
  }
  token := strings.TrimPrefix(authHeader, "Bearer ")

  username, ok := store.ValidateMCPToken(token)
  if !ok {
    writeError(c, http.StatusUnauthorized, "无效的 token")
    return
  }

  c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)

  var req auditRequest
  if err := c.ShouldBindJSON(&req); err != nil {
    if err == io.EOF {
      writeError(c, http.StatusBadRequest, "请求体为空")
      return
    }
    var maxBytesErr *http.MaxBytesError
    if errors.As(err, &maxBytesErr) {
      writeError(c, http.StatusRequestEntityTooLarge, "请求体过大")
      return
    }
    writeError(c, http.StatusBadRequest, "请求体格式错误")
    return
  }
  if req.Username == "" || req.SessionKey == "" {
    writeError(c, http.StatusBadRequest, "username 和 session_key 不能为空")
    return
  }

  if req.Username != username {
    writeError(c, http.StatusForbidden, "token 与 username 不匹配")
    return
  }

  engine, err := store.GetEngine()
  if err != nil {
    slog.Error("memory_evolution.audit_db_error", "error", err.Error())
    writeError(c, http.StatusInternalServerError, "数据库连接失败")
    return
  }

  log := store.MemoryEvolutionLog{
    Username:       req.Username,
    SessionKey:     req.SessionKey,
    ChangesSummary: req.ChangesSummary,
    FilesModified:  req.FilesModified,
  }

  if _, err := engine.Insert(&log); err != nil {
    slog.Error("memory_evolution.audit_insert_error", "error", err.Error())
    writeError(c, http.StatusInternalServerError, "写入审计日志失败")
    return
  }

  slog.Info("memory_evolution.audit",
    "username", req.Username,
    "session_key", req.SessionKey,
    "changes", req.ChangesSummary,
    "files", req.FilesModified,
  )

  writeJSON(c, http.StatusOK, map[string]string{"status": "ok"})
}
