package web

import (
  "context"
  "crypto/rand"
  "encoding/hex"
  "encoding/json"
  "errors"
  "fmt"
  "log/slog"
  "net/http"
  "path/filepath"
  "sync"
  "time"

  "github.com/gin-gonic/gin"

  "github.com/picoaide/picoaide/internal/agent"
  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/logger"
  "github.com/picoaide/picoaide/internal/user"
)

const maxEventsPerRun = 10000

// ============================================================
// 可重连聊天流 — 沙盒事件持久化存储，SSE 断线重连
// ============================================================

type streamEvent struct {
  Type string          `json:"type"`
  Data json.RawMessage `json:"data,omitempty"`
}

type chatRun struct {
  runID    string
  username string
  createdAt time.Time
  cancel   context.CancelFunc

  mu     sync.Mutex
  events []streamEvent
  subs   map[chan struct{}]bool
  done   bool
}

var activeRuns sync.Map // map[string]*chatRun
var userRun sync.Map    // map[string]*chatRun  username→当前活跃run

const staleRunTimeout = 30 * time.Minute

func init() {
  go cleanStaleRuns()
}

// cleanStaleRuns 定期清理超过 30 分钟未完成的僵死会话
func cleanStaleRuns() {
  ticker := time.NewTicker(5 * time.Minute)
  defer ticker.Stop()
  for range ticker.C {
    activeRuns.Range(func(key, value interface{}) bool {
      run := value.(*chatRun)
      run.mu.Lock()
      stale := !run.done && time.Since(run.createdAt) > staleRunTimeout
      run.mu.Unlock()
      if stale {
        run.finish()
        activeRuns.Delete(key)
      }
      return true
    })
  }
}

func generateRunID() string {
  b := make([]byte, 16)
  if _, err := rand.Read(b); err != nil {
    slog.Error("chat.generate_run_id_failed", "error", err.Error())
    return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
  }
  return hex.EncodeToString(b)
}

func newChatRun(username string) *chatRun {
  return &chatRun{
    runID:     generateRunID(),
    username:  username,
    createdAt: time.Now(),
    subs:      make(map[chan struct{}]bool),
  }
}

// append 追加事件并通知所有订阅者
func (r *chatRun) append(evt streamEvent) {
  r.mu.Lock()
  if len(r.events) < maxEventsPerRun {
    r.events = append(r.events, evt)
  }
  for ch := range r.subs {
    select {
    case ch <- struct{}{}:
    default:
    }
  }
  r.mu.Unlock()
}

// finish 标记完成并关闭所有订阅者通道
func (r *chatRun) finish() {
  r.mu.Lock()
  r.done = true
  for ch := range r.subs {
    close(ch)
  }
  r.subs = make(map[chan struct{}]bool)
  r.mu.Unlock()

  userRun.Delete(r.username)

  // 延迟清理
  go func() {
    time.Sleep(5 * time.Minute)
    activeRuns.Delete(r.runID)
  }()
}

// subscribe 注册订阅者，返回通知通道和现有事件
func (r *chatRun) subscribe() (chan struct{}, []streamEvent) {
  r.mu.Lock()
  defer r.mu.Unlock()
  ch := make(chan struct{}, 64)
  r.subs[ch] = true
  events := make([]streamEvent, len(r.events))
  copy(events, r.events)
  if r.done {
    close(ch)
  } else if len(r.events) > 0 {
    ch <- struct{}{}
  }
  return ch, events
}

// unsubscribe 移除订阅者
func (r *chatRun) unsubscribe(ch chan struct{}) {
  r.mu.Lock()
  delete(r.subs, ch)
  r.mu.Unlock()
}

// startChatSandbox 创建聊天运行并启动沙箱，Web 和 IM 共用
func (s *Server) startChatSandbox(username, message string, inputJSON []byte) *chatRun {
  // 同一用户发新消息时，取消上一个正在运行的沙箱（通过 context 优雅退出）
  // 上一个沙箱退出后会 releaseUser，当前消息的 acquireUser 再获取 token 启动
  if v, ok := userRun.Load(username); ok {
    v.(*chatRun).cancel()
  }

  runCtx, runCancel := context.WithCancel(context.Background())
  run := newChatRun(username)
  run.cancel = runCancel
  activeRuns.Store(run.runID, run)
  userRun.Store(username, run)
  run.append(streamEvent{Type: "user_message", Data: mustMarshal(message)})

  mcpToken, err := auth.GetMCPToken(username)
  if err != nil {
    mcpToken, _ = auth.GenerateMCPToken(username)
  }
  workspace := filepath.Join(config.WorkDir(), "users", username)
  apiKeys := s.loadAPIKeys()

  slog.Debug("chat.sandbox_start", "run_id", run.runID, "username", username)

  go func() {
    defer runCancel()
    events, err := s.agentIntegration.sandbox.Run(
      runCtx, mcpToken, inputJSON, workspace, apiKeys,
      buildSkillMounts(username), username,
    )
    if err != nil {
      if errors.Is(err, context.Canceled) {
        slog.Debug("chat.sandbox_cancelled", "run_id", run.runID)
      } else {
        slog.Debug("chat.sandbox_error", "run_id", run.runID, "error", err.Error())
        run.append(streamEvent{Type: "error", Data: mustMarshal(err.Error())})
      }
      run.finish()
      return
    }
    var eventCount int
    for evt := range events {
      eventCount++
      run.append(streamEvent{Type: evt.Type, Data: evt.Data})
    }
    slog.Debug("chat.sandbox_complete", "run_id", run.runID, "event_count", eventCount)
    run.finish()
  }()

  return run
}

// ============================================================
// POST /api/user/chat/send — 提交消息，立即返回 run_id
// ============================================================

func (s *Server) handleChatSend(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  message := c.PostForm("message")
  logger.DebugRecv("POST", "/api/user/chat/send", "username", username, "message_length", len(message))
  if message == "" {
    writeError(c, http.StatusBadRequest, "请输入消息")
    return
  }

  if s.agentIntegration == nil || s.agentIntegration.sandbox == nil {
    writeError(c, http.StatusServiceUnavailable, "沙箱未就绪")
    return
  }

  user.InitializeUser("", filepath.Join(config.WorkDir(), "users"), username)

  input := agent.Message{Role: agent.RoleUser, Content: message}
  inputJSON, _ := json.Marshal(input)

  run := s.startChatSandbox(username, message, inputJSON)

  logger.DebugSend("POST", "/api/user/chat/send", http.StatusOK, "run_id", run.runID)
  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "run_id":  run.runID,
    "message": "消息已提交，AI 正在处理",
  })
}

// ============================================================
// GET /api/user/chat/stream — SSE 流式输出，支持断线重连
// ============================================================

func (s *Server) handleChatStream(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  runID := c.Query("run_id")
  if runID == "" {
    writeError(c, http.StatusBadRequest, "缺少 run_id")
    return
  }

  v, ok := activeRuns.Load(runID)
  if !ok {
    writeError(c, http.StatusNotFound, "找不到该会话")
    return
  }
  run := v.(*chatRun)
  if run.username != username {
    writeError(c, http.StatusForbidden, "无权访问此会话")
    return
  }

  notifCh, events := run.subscribe()
  defer run.unsubscribe(notifCh)

  c.Header("Content-Type", "text/event-stream")
  c.Header("Cache-Control", "no-cache")
  c.Header("X-Accel-Buffering", "no")
  c.Writer.WriteHeader(http.StatusOK)

  flusher, ok := c.Writer.(http.Flusher)
  if !ok {
    return
  }
  flusher.Flush()

  clientGone := c.Request.Context().Done()
  cursor := len(events)

  // 先发已有事件
  for i := 0; i < cursor; i++ {
    writeSSE(c.Writer, flusher, clientGone, events[i])
  }

  // 等新事件
  for {
    select {
    case <-clientGone:
      return
    case _, ok := <-notifCh:
      if !ok {
        // 通道已关闭，run 已结束，发完剩余事件后退出
        run.mu.Lock()
        remaining := run.events[cursor:]
        run.mu.Unlock()
        for _, evt := range remaining {
          if !writeSSE(c.Writer, flusher, clientGone, evt) {
            return
          }
        }
        return
      }
      run.mu.Lock()
      newEvents := run.events[cursor:]
      cursor = len(run.events)
      run.mu.Unlock()
      for _, evt := range newEvents {
        if !writeSSE(c.Writer, flusher, clientGone, evt) {
          return
        }
      }
    }
  }
}

// ============================================================
// POST /api/user/chat/stop — 停止当前对话
// ============================================================

func (s *Server) handleChatStop(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  v, ok := userRun.Load(username)
  if !ok {
    writeJSON(c, http.StatusOK, map[string]interface{}{
      "success": true,
      "message": "没有正在运行的对话",
    })
    return
  }
  run := v.(*chatRun)
  run.cancel()

  slog.Debug("chat.user_stopped", "username", username, "run_id", run.runID)
  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "message": "对话已停止",
  })
}

// writeSSE 写一条 SSE 事件，客户端断开时返回 false
func writeSSE(w gin.ResponseWriter, flusher http.Flusher, clientGone <-chan struct{}, evt streamEvent) bool {
  // stderr 不发给前端
  if evt.Type == "stderr" {
    return true
  }

  select {
  case <-clientGone:
    return false
  default:
    data, _ := json.Marshal(evt)
    fmt.Fprintf(w, "data: %s\n\n", data)
    flusher.Flush()
    return true
  }
}

func mustMarshal(v interface{}) json.RawMessage {
  data, _ := json.Marshal(v)
  return data
}
