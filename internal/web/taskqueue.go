package web

import (
  "fmt"
  "log"
  "sync"
  "sync/atomic"
  "time"

  "github.com/picoaide/picoaide/internal/auth"
)

// ============================================================
// 批量任务队列
// ============================================================

const (
  taskInterval = 2 * time.Second // 每个用户操作间隔
  taskQueueSize = 64
)

// TaskItem 队列中的单个任务项
type TaskItem struct {
  Username string
  Fn       func(username string) error
}

// TaskStatus 任务状态
type TaskStatus struct {
  ID        string `json:"id"`
  Type      string `json:"type"`
  Total     int    `json:"total"`
  Done      int    `json:"done"`
  Failed    int    `json:"failed"`
  Running   bool   `json:"running"`
  Message   string `json:"message"`
  StartedAt string `json:"started_at,omitempty"`
}

// taskQueue 全局任务队列
var taskQueue struct {
  mu     sync.Mutex
  items  chan TaskItem
  status *TaskStatus
  active int32 // 原子标记：1=正在处理
  done   int32
  failed int32
  total  int32
}

func init() {
  taskQueue.items = make(chan TaskItem, taskQueueSize)
}

// enqueueTask 提交批量任务到队列，返回任务 ID
// 跳过超管用户
func enqueueTask(taskType string, users []string, fn func(username string) error) (string, error) {
  taskQueue.mu.Lock()
  defer taskQueue.mu.Unlock()

  // 已有任务在运行
  if atomic.LoadInt32(&taskQueue.active) == 1 {
    return "", fmt.Errorf("已有任务正在执行，请等待完成后再试")
  }

  // 过滤超管
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
  }

  // 启动后台处理
  go processQueue(fn)

  // 投递任务项
  go func() {
    for _, u := range filtered {
      taskQueue.items <- TaskItem{Username: u, Fn: fn}
    }
  }()

  log.Printf("[task-queue] 任务 %s 已提交，共 %d 个用户", taskID, len(filtered))
  return taskID, nil
}

// processQueue 消费队列中的任务
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
      log.Printf("[task-queue] %s 失败: %v", item.Username, taskErr)
    } else {
      atomic.AddInt32(&taskQueue.done, 1)
    }

    done := atomic.LoadInt32(&taskQueue.done)
    failed := atomic.LoadInt32(&taskQueue.failed)
    processed := done + failed

    // 更新状态消息
    taskQueue.mu.Lock()
    if taskQueue.status != nil {
      taskQueue.status.Done = int(done)
      taskQueue.status.Failed = int(failed)
      if taskErr != nil {
        taskQueue.status.Message = fmt.Sprintf("%s: %s", item.Username, taskErr.Error())
      }
    }
    taskQueue.mu.Unlock()

    // 全部处理完成
    if processed >= total {
      break
    }

    // 间隔避免拥堵
    time.Sleep(taskInterval)
  }

  // 标记完成
  taskQueue.mu.Lock()
  atomic.StoreInt32(&taskQueue.active, 0)
  if taskQueue.status != nil {
    taskQueue.status.Running = false
    done := atomic.LoadInt32(&taskQueue.done)
    failed := atomic.LoadInt32(&taskQueue.failed)
    taskQueue.status.Done = int(done)
    taskQueue.status.Failed = int(failed)
    taskQueue.status.Message = fmt.Sprintf("完成：%d 成功，%d 失败", done, failed)
  }
  taskQueue.mu.Unlock()

  log.Printf("[task-queue] 任务完成: %d 成功, %d 失败", taskQueue.done, taskQueue.failed)
}

// getTaskStatus 返回当前任务状态
func getTaskStatus() *TaskStatus {
  taskQueue.mu.Lock()
  defer taskQueue.mu.Unlock()
  if taskQueue.status == nil {
    return &TaskStatus{Running: false}
  }
  // 刷新原子计数
  s := *taskQueue.status
  s.Done = int(atomic.LoadInt32(&taskQueue.done))
  s.Failed = int(atomic.LoadInt32(&taskQueue.failed))
  return &s
}
