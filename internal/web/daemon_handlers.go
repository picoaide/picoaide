package web

import (
  "encoding/json"
  "fmt"
  "log/slog"
  "net/http"
  "os"
  "path/filepath"
  "time"

  "github.com/gin-gonic/gin"
  "github.com/google/uuid"

  "github.com/picoaide/picoaide/internal/config"
  daemonStore "github.com/picoaide/picoaide/internal/daemon/store"
  "github.com/picoaide/picoaide/internal/logger"
)

// ============================================================
// 任务提交流程 — 用户提交任务、查询任务、管理任务生命周期
// ============================================================

type taskSubmitReq struct {
  Message  string `json:"message"`
  Priority int    `json:"priority"`
}

// handleTaskSubmit 提交任务
// POST /api/user/task/submit (form-encoded: message, priority)
func (s *Server) handleTaskSubmit(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  message := c.PostForm("message")
  if message == "" {
    writeError(c, http.StatusBadRequest, "消息不能为空")
    return
  }

  taskID := uuid.New().String()
  now := time.Now().UTC().Format(time.RFC3339)
  meta := daemonStore.TaskMeta{
    ID:        taskID,
    Status:    "pending",
    Source:    "web",
    Title:     truncate(message, 100),
    CreatedAt: now,
  }

  // 持久化任务
  ts := daemonStore.NewTaskStore(filepath.Join(config.WorkDir(), "users", username, "daemon"))
  if err := ts.Save(meta); err != nil {
    slog.Error("daemon.task_save_failed", "username", username, "task_id", taskID, "error", err)
    writeError(c, http.StatusInternalServerError, "保存任务失败")
    return
  }

  // 初始化事件存储并写入 task_submitted 事件
  eventData, _ := json.Marshal(map[string]interface{}{
    "message": message,
  })
  taskDir := filepath.Join(config.WorkDir(), "users", username, "daemon", "tasks", taskID)
  es, err := daemonStore.NewEventStore(taskDir)
  if err == nil {
    evt := &daemonStore.Event{
      TaskID: taskID,
      Seq:    1,
      Type:   "task_submitted",
      Data:   eventData,
      Time:   now,
    }
    es.Append(evt)
    es.Close()
  }

  // 通过 Hub 广播
  s.broadcastDaemonEvent(username, taskID, "task_submitted", eventData)

  logger.Audit("daemon.task_submit", "username", username, "task_id", taskID)
  writeJSON(c, http.StatusOK, gin.H{
    "success": true,
    "task_id": taskID,
    "status":  "pending",
  })
}

// handleTaskPause 暂停任务
// POST /api/user/task/pause
func (s *Server) handleTaskPause(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  taskID := c.PostForm("task_id")
  if taskID == "" {
    writeError(c, http.StatusBadRequest, "缺少 task_id")
    return
  }

  ts := daemonStore.NewTaskStore(filepath.Join(config.WorkDir(), "users", username, "daemon"))
  if err := ts.UpdateStatus(taskID, "paused"); err != nil {
    if err == daemonStore.ErrNotFound {
      writeError(c, http.StatusNotFound, "任务不存在")
    } else {
      writeError(c, http.StatusInternalServerError, "暂停任务失败")
    }
    return
  }

  now := time.Now().UTC().Format(time.RFC3339)
  s.broadcastDaemonEvent(username, taskID, "task_paused", json.RawMessage(fmt.Sprintf(`{"paused_at":"%s"}`, now)))

  logger.Audit("daemon.task_pause", "username", username, "task_id", taskID)
  writeSuccess(c, "任务已暂停")
}

// handleTaskResume 恢复任务
// POST /api/user/task/resume
func (s *Server) handleTaskResume(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  taskID := c.PostForm("task_id")
  if taskID == "" {
    writeError(c, http.StatusBadRequest, "缺少 task_id")
    return
  }

  ts := daemonStore.NewTaskStore(filepath.Join(config.WorkDir(), "users", username, "daemon"))
  if err := ts.UpdateStatus(taskID, "pending"); err != nil {
    if err == daemonStore.ErrNotFound {
      writeError(c, http.StatusNotFound, "任务不存在")
    } else {
      writeError(c, http.StatusInternalServerError, "恢复任务失败")
    }
    return
  }

  now := time.Now().UTC().Format(time.RFC3339)
  s.broadcastDaemonEvent(username, taskID, "task_resumed", json.RawMessage(fmt.Sprintf(`{"resumed_at":"%s"}`, now)))

  logger.Audit("daemon.task_resume", "username", username, "task_id", taskID)
  writeSuccess(c, "任务已恢复")
}

// handleTaskCancel 取消任务
// POST /api/user/task/cancel
func (s *Server) handleTaskCancel(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  taskID := c.PostForm("task_id")
  if taskID == "" {
    writeError(c, http.StatusBadRequest, "缺少 task_id")
    return
  }

  ts := daemonStore.NewTaskStore(filepath.Join(config.WorkDir(), "users", username, "daemon"))
  if err := ts.UpdateStatus(taskID, "cancelled"); err != nil {
    if err == daemonStore.ErrNotFound {
      writeError(c, http.StatusNotFound, "任务不存在")
    } else {
      writeError(c, http.StatusInternalServerError, "取消任务失败")
    }
    return
  }

  now := time.Now().UTC().Format(time.RFC3339)
  eventData := json.RawMessage(fmt.Sprintf(`{"cancelled_at":"%s"}`, now))
  s.broadcastDaemonEvent(username, taskID, "task_cancelled", eventData)

  logger.Audit("daemon.task_cancel", "username", username, "task_id", taskID)
  writeSuccess(c, "任务已取消")
}

// handleTaskMessage 向正在执行的任务注入消息
// POST /api/user/task/message
func (s *Server) handleTaskMessage(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  var req struct {
    TaskID  string `json:"task_id"`
    Message string `json:"message"`
  }
  if err := c.ShouldBindJSON(&req); err != nil {
    writeError(c, http.StatusBadRequest, "请求体格式错误")
    return
  }
  if req.TaskID == "" || req.Message == "" {
    writeError(c, http.StatusBadRequest, "task_id 和 message 不能为空")
    return
  }

  // 校验任务存在且属于当前用户
  tasks, err := s.taskManager.ListTasks(username)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "查询任务失败")
    return
  }
  found := false
  for _, t := range tasks {
    if t.ID == req.TaskID {
      found = true
      break
    }
  }
  if !found {
    writeError(c, http.StatusNotFound, "任务不存在")
    return
  }

  now := time.Now().UTC().Format(time.RFC3339)
  eventData, _ := json.Marshal(map[string]string{
    "message": req.Message,
    "time":    now,
  })
  s.broadcastDaemonEvent(username, req.TaskID, "user_message", eventData)

  logger.Audit("daemon.task_message", "username", username, "task_id", req.TaskID)
  writeSuccess(c, "消息已发送")
}

// handleTaskDetail 获取任务详情（含最近事件）
// GET /api/user/task/detail?task_id=xxx
func (s *Server) handleTaskDetail(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  taskID := c.Query("task_id")
  if taskID == "" {
    writeError(c, http.StatusBadRequest, "缺少 task_id")
    return
  }

  // 查找任务元数据
  tasks, err := s.taskManager.ListTasks(username)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "查询任务失败")
    return
  }
  var meta *daemonStore.TaskMeta
  for i, t := range tasks {
    if t.ID == taskID {
      meta = &tasks[i]
      break
    }
  }
  if meta == nil {
    writeError(c, http.StatusNotFound, "任务不存在")
    return
  }

  // 获取最近事件
  events, _ := s.taskManager.GetTaskEvents(username, taskID, 0)

  writeJSON(c, http.StatusOK, gin.H{
    "success": true,
    "task":    meta,
    "events":  events,
  })
}

// handleTaskEvents 获取任务的事件流（JSON 数组，用于历史回放）
// GET /api/user/task/events?task_id=xxx&since_seq=N
func (s *Server) handleTaskEvents(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  taskID := c.Query("task_id")
  if taskID == "" {
    writeError(c, http.StatusBadRequest, "缺少 task_id")
    return
  }

  var sinceSeq int64
  if s := c.Query("since_seq"); s != "" {
    fmt.Sscanf(s, "%d", &sinceSeq)
  }

  events, err := s.taskManager.GetTaskEvents(username, taskID, sinceSeq)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "查询任务事件失败")
    return
  }
  if events == nil {
    events = []*daemonStore.Event{}
  }

  writeJSON(c, http.StatusOK, gin.H{
    "success": true,
    "task_id": taskID,
    "events":  events,
  })
}

// handleTaskList 列出当前用户的任务
// GET /api/user/task/list?limit=50&offset=0&status=
func (s *Server) handleTaskList(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  statusFilter := c.Query("status")
  limit := 50
  offset := 0
  if v := c.Query("limit"); v != "" {
    if n, err := parseInt(v); err == nil && n > 0 {
      limit = n
    }
  }
  if v := c.Query("offset"); v != "" {
    if n, err := parseInt(v); err == nil && n >= 0 {
      offset = n
    }
  }

  tasks, err := s.taskManager.ListTasks(username)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "查询任务失败")
    return
  }

  // 按创建时间降序排列
  sortTasksByTimeDesc(tasks)

  // 过滤
  var filtered []daemonStore.TaskMeta
  for _, t := range tasks {
    if statusFilter != "" && t.Status != statusFilter {
      continue
    }
    filtered = append(filtered, t)
  }
  if filtered == nil {
    filtered = []daemonStore.TaskMeta{}
  }

  total := len(filtered)
  // 分页
  if offset >= len(filtered) {
    filtered = []daemonStore.TaskMeta{}
  } else {
    end := offset + limit
    if end > len(filtered) {
      end = len(filtered)
    }
    filtered = filtered[offset:end]
  }

  writeJSON(c, http.StatusOK, gin.H{
    "success": true,
    "total":   total,
    "tasks":   filtered,
  })
}

// ============================================================
// Daemon 生命周期 — 状态查询与启停控制
// ============================================================

// handleDaemonStatus 查询当前用户 daemon 状态
// GET /api/user/daemon/status
func (s *Server) handleDaemonStatus(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }
  status := s.daemonManager.GetStatus(username)
  status["success"] = true
  writeJSON(c, http.StatusOK, status)
}

// handleDaemonRestart 重启 daemon
// POST /api/user/daemon/restart
func (s *Server) handleDaemonRestart(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  // 标记停止再重新标记运行（实际 daemon 进程由 agent 端管理）
  s.daemonManager.MarkStopped(username)
  s.daemonManager.UpdateHeartbeat(username, "")

  now := time.Now().UTC().Format(time.RFC3339)
  eventData := json.RawMessage(fmt.Sprintf(`{"restarted_at":"%s"}`, now))
  s.broadcastDaemonEvent(username, "", "daemon_restarted", eventData)

  logger.Audit("daemon.restart", "username", username)
  writeSuccess(c, "daemon 正在重启")
}

// handleDaemonStop 停止 daemon
// POST /api/user/daemon/stop
func (s *Server) handleDaemonStop(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  s.daemonManager.MarkStopped(username)

  now := time.Now().UTC().Format(time.RFC3339)
  eventData := json.RawMessage(fmt.Sprintf(`{"stopped_at":"%s"}`, now))
  s.broadcastDaemonEvent(username, "", "daemon_stopped", eventData)

  logger.Audit("daemon.stop", "username", username)
  writeSuccess(c, "daemon 已停止")
}

// ============================================================
// 超管端点
// ============================================================

// handleAdminListDaemons 列出所有用户 daemon 状态
// GET /api/admin/daemons
func (s *Server) handleAdminListDaemons(c *gin.Context) {
  list, err := s.daemonManager.ListAll()
  if err != nil {
    writeError(c, http.StatusInternalServerError, "查询 daemon 列表失败")
    return
  }
  writeJSON(c, http.StatusOK, gin.H{
    "success": true,
    "daemons": list,
  })
}

// handleAdminListTasks 列出所有用户的任务
// GET /api/admin/tasks?limit=100&offset=0&status=
func (s *Server) handleAdminListTasks(c *gin.Context) {
  statusFilter := c.Query("status")
  limit := 100
  offset := 0
  if v := c.Query("limit"); v != "" {
    if n, err := parseInt(v); err == nil && n > 0 {
      limit = n
    }
  }
  if v := c.Query("offset"); v != "" {
    if n, err := parseInt(v); err == nil && n >= 0 {
      offset = n
    }
  }

  // 遍历所有用户目录收集任务
  var allTasks []daemonStore.TaskMeta
  usersDir := filepath.Join(config.WorkDir(), "users")
  if entries, err := readDirNames(usersDir); err == nil {
    for _, entry := range entries {
      daemonDir := filepath.Join(usersDir, entry, "daemon")
      ts := daemonStore.NewTaskStore(daemonDir)
      if tasks, err := ts.List(); err == nil {
        for _, t := range tasks {
          if statusFilter != "" && t.Status != statusFilter {
            continue
          }
          allTasks = append(allTasks, t)
        }
      }
    }
  }
  if allTasks == nil {
    allTasks = []daemonStore.TaskMeta{}
  }

  sortTasksByTimeDesc(allTasks)
  total := len(allTasks)
  if offset >= len(allTasks) {
    allTasks = []daemonStore.TaskMeta{}
  } else {
    end := offset + limit
    if end > len(allTasks) {
      end = len(allTasks)
    }
    allTasks = allTasks[offset:end]
  }

  writeJSON(c, http.StatusOK, gin.H{
    "success": true,
    "total":   total,
    "tasks":   allTasks,
  })
}

// handleAdminTaskStats 任务统计
// GET /api/admin/tasks/stats
func (s *Server) handleAdminTaskStats(c *gin.Context) {
  stats := map[string]int{
    "pending":   0,
    "running":   0,
    "paused":    0,
    "completed": 0,
    "cancelled": 0,
    "failed":    0,
  }

  usersDir := filepath.Join(config.WorkDir(), "users")
  if entries, err := readDirNames(usersDir); err == nil {
    for _, entry := range entries {
      daemonDir := filepath.Join(usersDir, entry, "daemon")
      ts := daemonStore.NewTaskStore(daemonDir)
      if tasks, err := ts.List(); err == nil {
        for _, t := range tasks {
          stats[t.Status]++
        }
      }
    }
  }

  writeJSON(c, http.StatusOK, gin.H{
    "success": true,
    "stats":   stats,
  })
}

// ============================================================
// 工具函数
// ============================================================

// broadcastDaemonEvent 将事件持久化到 store 并通过 Hub 广播
func (s *Server) broadcastDaemonEvent(username, taskID, eventType string, data json.RawMessage) {
  if s.daemonHub == nil {
    return
  }

  evt := &daemonStore.Event{
    TaskID: taskID,
    Type:   eventType,
    Data:   data,
    Time:   time.Now().UTC().Format(time.RFC3339),
  }

  // 通过 Hub 广播到该用户的所有 SSE 订阅者
  if eventBytes, err := json.Marshal(evt); err == nil {
    s.daemonHub.Broadcast(username, eventBytes)
  }
}

// sortTasksByTimeDesc 按创建时间降序排列任务
func sortTasksByTimeDesc(tasks []daemonStore.TaskMeta) {
  for i := 0; i < len(tasks); i++ {
    for j := i + 1; j < len(tasks); j++ {
      if tasks[i].CreatedAt < tasks[j].CreatedAt {
        tasks[i], tasks[j] = tasks[j], tasks[i]
      }
    }
  }
}

// readDirNames 读取目录中的条目名（只取目录名，不含路径）
func readDirNames(dir string) ([]string, error) {
  entries, err := os.ReadDir(dir)
  if err != nil {
    return nil, err
  }
  names := make([]string, 0, len(entries))
  for _, e := range entries {
    if e.IsDir() {
      names = append(names, e.Name())
    }
  }
  return names, nil
}

// parseInt 将字符串解析为 int
func parseInt(s string) (int, error) {
  var n int
  for _, c := range s {
    if c < '0' || c > '9' {
      return 0, fmt.Errorf("非数字")
    }
    n = n*10 + int(c-'0')
  }
  return n, nil
}

// truncate 截断字符串到指定长度
func truncate(s string, maxLen int) string {
  runes := []rune(s)
  if len(runes) <= maxLen {
    return s
  }
  return string(runes[:maxLen]) + "..."
}
