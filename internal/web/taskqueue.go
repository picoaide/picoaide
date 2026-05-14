package web

import (
  "fmt"
  "log/slog"
  "sync"
  "sync/atomic"
  "time"

  "github.com/picoaide/picoaide/internal/auth"
)

// ============================================================
// 镜像拉取任务状态跟踪
// ============================================================

type ImagePullTask struct {
  Running   bool   `json:"running"`
  Tag       string `json:"tag"`
  Message   string `json:"message"`
  Error     string `json:"error,omitempty"`
  StartedAt string `json:"started_at,omitempty"`
}

var imagePullStatus struct {
  mu     sync.Mutex
  status ImagePullTask
}

func startImagePull(tag string) {
  imagePullStatus.mu.Lock()
  defer imagePullStatus.mu.Unlock()
  imagePullStatus.status = ImagePullTask{
    Running:   true,
    Tag:       tag,
    Message:   "正在拉取...",
    StartedAt: time.Now().Format("2006-01-02 15:04:05"),
  }
}

func updateImagePull(msg string) {
  imagePullStatus.mu.Lock()
  defer imagePullStatus.mu.Unlock()
  if imagePullStatus.status.Running {
    imagePullStatus.status.Message = msg
  }
}

func finishImagePull() {
  imagePullStatus.mu.Lock()
  defer imagePullStatus.mu.Unlock()
  imagePullStatus.status.Running = false
  imagePullStatus.status.Message = "拉取完成"
  imagePullStatus.status.Error = ""
}

func failImagePull(errMsg string) {
  imagePullStatus.mu.Lock()
  defer imagePullStatus.mu.Unlock()
  imagePullStatus.status.Running = false
  imagePullStatus.status.Error = errMsg
  imagePullStatus.status.Message = "拉取失败: " + errMsg
}

func getImagePullStatus() ImagePullTask {
  imagePullStatus.mu.Lock()
  defer imagePullStatus.mu.Unlock()
  return imagePullStatus.status
}

// ============================================================
// 批量任务队列（支持排队）
// ============================================================

const (
  taskInterval = 2 * time.Second
  taskQueueSize = 64
)

type TaskItem struct {
  Username string
  Fn       func(username string) error
}

type pendingTask struct {
  taskType string
  users    []string
  fn       func(username string) error
}

type TaskStatus struct {
  ID        string `json:"id"`
  Type      string `json:"type"`
  Total     int    `json:"total"`
  Done      int    `json:"done"`
  Failed    int    `json:"failed"`
  Running   bool   `json:"running"`
  Message   string `json:"message"`
  StartedAt string `json:"started_at,omitempty"`
  Pending   int    `json:"pending"`
}

var taskQueue struct {
  mu      sync.Mutex
  items   chan TaskItem
  pending []pendingTask
  status  *TaskStatus
  active  int32
  done    int32
  failed  int32
  total   int32
}

func init() {
  taskQueue.items = make(chan TaskItem, taskQueueSize)
}

func enqueueTask(taskType string, users []string, fn func(username string) error) (string, error) {
  var filtered []string
  for _, u := range users {
    if auth.IsSuperadmin(u) {
      continue
    }
    filtered = append(filtered, u)
  }
  if len(filtered) == 0 {
    return "", fmt.Errorf("没有可操作的用户")
  }

  taskQueue.mu.Lock()

  if atomic.LoadInt32(&taskQueue.active) == 1 {
    taskQueue.pending = append(taskQueue.pending, pendingTask{
      taskType: taskType,
      users:    filtered,
      fn:       fn,
    })
    taskID := fmt.Sprintf("%s-%d", taskType, time.Now().Unix())
    slog.Info("批量任务已排队", "task_id", taskID, "total", len(filtered))
    taskQueue.mu.Unlock()
    return taskID, nil
  }

  taskID := fmt.Sprintf("%s-%d", taskType, time.Now().Unix())
  atomic.StoreInt32(&taskQueue.total, int32(len(filtered)))
  atomic.StoreInt32(&taskQueue.done, 0)
  atomic.StoreInt32(&taskQueue.failed, 0)
  atomic.StoreInt32(&taskQueue.active, 1)

  taskQueue.status = &TaskStatus{
    ID:        taskID,
    Type:      taskType,
    Total:     len(filtered),
    Running:   true,
    StartedAt: time.Now().Format("2006-01-02 15:04:05"),
    Pending:   len(taskQueue.pending),
  }
  taskQueue.mu.Unlock()

  go processQueue(fn)
  go func() {
    for _, u := range filtered {
      taskQueue.items <- TaskItem{Username: u, Fn: fn}
    }
  }()

  slog.Info("批量任务已提交", "task_id", taskID, "total", len(filtered))
  return taskID, nil
}

func processQueue(fn func(username string) error) {
  total := atomic.LoadInt32(&taskQueue.total)
  for {
    item, ok := <-taskQueue.items
    if !ok {
      break
    }

    taskErr := item.Fn(item.Username)
    if taskErr != nil {
      atomic.AddInt32(&taskQueue.failed, 1)
      slog.Error("任务执行失败", "username", item.Username, "error", taskErr)
    } else {
      atomic.AddInt32(&taskQueue.done, 1)
    }

    done := atomic.LoadInt32(&taskQueue.done)
    failed := atomic.LoadInt32(&taskQueue.failed)
    processed := done + failed

    taskQueue.mu.Lock()
    if taskQueue.status != nil {
      taskQueue.status.Done = int(done)
      taskQueue.status.Failed = int(failed)
      if taskErr != nil {
        taskQueue.status.Message = fmt.Sprintf("%s: %s", item.Username, taskErr.Error())
      }
    }
    taskQueue.mu.Unlock()

    if processed >= total {
      break
    }
    time.Sleep(taskInterval)
  }

  taskQueue.mu.Lock()
  atomic.StoreInt32(&taskQueue.active, 0)
  if taskQueue.status != nil {
    taskQueue.status.Running = false
    done := atomic.LoadInt32(&taskQueue.done)
    failed := atomic.LoadInt32(&taskQueue.failed)
    taskQueue.status.Done = int(done)
    taskQueue.status.Failed = int(failed)
    taskQueue.status.Message = fmt.Sprintf("完成：%d 成功，%d 失败", done, failed)
    taskQueue.status.Pending = len(taskQueue.pending)
  }

  if len(taskQueue.pending) > 0 {
    next := taskQueue.pending[0]
    taskQueue.pending = taskQueue.pending[1:]

    atomic.StoreInt32(&taskQueue.total, int32(len(next.users)))
    atomic.StoreInt32(&taskQueue.done, 0)
    atomic.StoreInt32(&taskQueue.failed, 0)
    atomic.StoreInt32(&taskQueue.active, 1)

    taskQueue.status = &TaskStatus{
      ID:        fmt.Sprintf("%s-%d", next.taskType, time.Now().Unix()),
      Type:      next.taskType,
      Total:     len(next.users),
      Running:   true,
      StartedAt: time.Now().Format("2006-01-02 15:04:05"),
      Pending:   len(taskQueue.pending),
    }
    taskQueue.mu.Unlock()

    go processQueue(next.fn)
    go func() {
      for _, u := range next.users {
        taskQueue.items <- TaskItem{Username: u, Fn: next.fn}
      }
    }()
    return
  }

  taskQueue.mu.Unlock()
  slog.Info("批量任务完成", "done", taskQueue.done, "failed", taskQueue.failed)
}

func getTaskStatus() *TaskStatus {
  taskQueue.mu.Lock()
  defer taskQueue.mu.Unlock()
  if taskQueue.status == nil {
    return &TaskStatus{Running: false}
  }
  s := *taskQueue.status
  s.Done = int(atomic.LoadInt32(&taskQueue.done))
  s.Failed = int(atomic.LoadInt32(&taskQueue.failed))
  s.Pending = len(taskQueue.pending)
  return &s
}
