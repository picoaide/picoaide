package web

import (
  "crypto/rand"
  "encoding/hex"
  "encoding/json"
  "fmt"
  "net/http"
  "strconv"
  "time"

  "github.com/gin-gonic/gin"

  daemonStore "github.com/picoaide/picoaide/internal/daemon/store"
  "github.com/picoaide/picoaide/internal/util"
)

// ============================================================
// 任务事件流 — SSE 连接，支持 seq 断点续传
// ============================================================

// handleTaskEventStream 建立 SSE 连接，先回放历史事件，再推送实时事件
// GET /api/user/events/stream?task_id=xxx&since_seq=N
func (s *Server) handleTaskEventStream(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  taskID := c.Query("task_id")
  if taskID == "" {
    writeError(c, http.StatusBadRequest, "缺少 task_id")
    return
  }
  if err := util.SafePathSegment(taskID); err != nil {
    writeError(c, http.StatusBadRequest, "task_id 无效")
    return
  }

  var sinceSeq int64
  if v := c.Query("since_seq"); v != "" {
    if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
      sinceSeq = n
    }
  }

  // ---- Phase 1: 回放历史事件 ----
  missed, _ := s.taskManager.GetTaskEvents(username, taskID, sinceSeq)

  // ---- Phase 2: 订阅 Hub 获取实时事件 ----
  clientID := generateDaemonClientID()
  ch := s.daemonHub.Subscribe(username, clientID)
  defer s.daemonHub.Unsubscribe(username, clientID)

  // ---- 设置 SSE 响应头 ----
  c.Header("Content-Type", "text/event-stream")
  c.Header("Cache-Control", "no-cache")
  c.Header("Connection", "keep-alive")
  c.Header("X-Accel-Buffering", "no")
  c.Writer.WriteHeader(http.StatusOK)

  flusher, ok := c.Writer.(http.Flusher)
  if !ok {
    return
  }
  flusher.Flush()

  clientGone := c.Request.Context().Done()

  // ---- 发送回放事件 ----
  var lastSeq int64
  for _, evt := range missed {
    if !writeDaemonSSE(c.Writer, flusher, clientGone, evt) {
      return
    }
    if evt.Seq > lastSeq {
      lastSeq = evt.Seq
    }
  }

  // ---- Phase 3: 实时事件循环 ----
  for {
    select {
    case <-clientGone:
      return
    case raw, ok := <-ch:
      if !ok {
        return
      }
      var evt daemonStore.Event
      if err := json.Unmarshal(raw, &evt); err != nil {
        continue
      }
      // 过滤：仅发送匹配 task_id 且 seq > lastSeq 的事件
      if evt.TaskID != taskID {
        continue
      }
      if evt.Seq <= lastSeq {
        continue
      }
      if !writeDaemonSSE(c.Writer, flusher, clientGone, &evt) {
        return
      }
      lastSeq = evt.Seq
    }
  }
}

// writeDaemonSSE 以 "event: <type>\ndata: <json>\n\n" 格式写入 SSE 事件
func writeDaemonSSE(w gin.ResponseWriter, flusher http.Flusher, clientGone <-chan struct{}, evt *daemonStore.Event) bool {
  select {
  case <-clientGone:
    return false
  default:
    data, err := json.Marshal(evt)
    if err != nil {
      return true
    }
    fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, data)
    flusher.Flush()
    return true
  }
}

// generateDaemonClientID 为每个 SSE 连接生成唯一 client ID
func generateDaemonClientID() string {
  b := make([]byte, 8)
  if _, err := rand.Read(b); err != nil {
    return fmt.Sprintf("sse-%d", time.Now().UnixNano())
  }
  return hex.EncodeToString(b)
}
