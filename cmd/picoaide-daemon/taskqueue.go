package main

import (
  "context"
  "encoding/json"
  "errors"
  "os"
  "path/filepath"
  "sync"
  "sync/atomic"
  "time"

  "log/slog"

  "github.com/picoaide/picoaide/internal/agent"
  "github.com/picoaide/picoaide/internal/daemon/store"
)

type TaskQueue struct {
  mu      sync.Mutex
  queue   []*Task
  current *Task
  cond    *sync.Cond
  engine  *agent.Engine
  store   *store.TaskStore

  daemonDir string
  workspace string

  sysPrompt string

  eventCB func(typ string, data json.RawMessage)

  pauseRequested atomic.Bool
  pauseReady     chan struct{}

  ctx    context.Context
  cancel context.CancelFunc
}

type Task struct {
  ID        string
  Source    string
  Priority  int
  Status    string
  Cancel    context.CancelFunc
  CreatedAt time.Time
  Message   string
  IterCount int
  StartedAt string

  mu           sync.Mutex
  intervention string
}

func NewTaskQueue(engine *agent.Engine, taskStore *store.TaskStore, daemonDir, workspace, sysPrompt string, eventCB func(typ string, data json.RawMessage)) *TaskQueue {
  q := &TaskQueue{
    engine:    engine,
    store:     taskStore,
    daemonDir: daemonDir,
    workspace: workspace,
    sysPrompt: sysPrompt,
    eventCB:   eventCB,
  }
  q.cond = sync.NewCond(&q.mu)
  q.pauseReady = make(chan struct{}, 1)
  engine.SetPauseChecker(func() bool { return q.pauseRequested.Load() })
  engine.SetOnPause(q.onPause)
  return q
}

func (q *TaskQueue) Submit(id, source string, priority int, message string) *Task {
  q.mu.Lock()
  defer q.mu.Unlock()

  task := &Task{
    ID:        id,
    Source:    source,
    Priority:  priority,
    Status:    "pending",
    Message:   message,
    CreatedAt: time.Now().UTC(),
  }
  q.queue = append(q.queue, task)
  q.saveTaskMeta(task)
  q.cond.Signal()
  slog.Debug("taskqueue.submitted", "task_id", id, "source", source)
  return task
}

func (q *TaskQueue) Pause(id string) error {
  q.mu.Lock()
  if q.current == nil || q.current.ID != id {
    q.mu.Unlock()
    return errors.New("任务未在运行中或不存在")
  }
  if q.current.Status != "running" {
    q.mu.Unlock()
    return errors.New("任务不在运行中状态")
  }
  q.mu.Unlock()

  q.pauseRequested.Store(true)
  slog.Debug("taskqueue.pause_requested", "task_id", id)

  select {
  case <-q.pauseReady:
    slog.Debug("taskqueue.paused", "task_id", id)
    return nil
  case <-time.After(60 * time.Second):
    slog.Debug("taskqueue.pause_timeout", "task_id", id)
    return errors.New("暂停超时：引擎未在 60 秒内到达安全点")
  }
}

func (q *TaskQueue) Resume(id string) error {
  q.mu.Lock()
  task := q.findTaskLocked(id)
  if task == nil {
    q.mu.Unlock()
    return errors.New("任务不存在")
  }
  if task.Status != "paused" {
    q.mu.Unlock()
    return errors.New("任务不在暂停状态")
  }
  task.Status = "pending"
  q.mu.Unlock()

  if err := q.restoreEngineFromSnapshot(id); err != nil {
    slog.Debug("taskqueue.resume_restore_failed", "task_id", id, "error", err.Error())
    q.mu.Lock()
    task.Status = "paused"
    q.mu.Unlock()
    return err
  }

  q.mu.Lock()
  q.queue = append(q.queue, task)
  q.cond.Signal()
  q.mu.Unlock()

  slog.Debug("taskqueue.resumed", "task_id", id)
  return nil
}

func (q *TaskQueue) Cancel(id string) {
  q.mu.Lock()
  defer q.mu.Unlock()

  if q.current != nil && q.current.ID == id {
    if q.current.Cancel != nil {
      q.current.Cancel()
    }
    q.current.Status = "cancelled"
    q.saveTaskMeta(q.current)
    slog.Debug("taskqueue.cancelled_running", "task_id", id)
    q.current = nil
    return
  }

  for i, t := range q.queue {
    if t.ID == id && t.Status == "pending" {
      t.Status = "cancelled"
      q.saveTaskMeta(t)
      q.queue = append(q.queue[:i], q.queue[i+1:]...)
      slog.Debug("taskqueue.cancelled_pending", "task_id", id)
      return
    }
  }

  task := q.findTaskLocked(id)
  if task != nil && task.Status == "paused" {
    task.Status = "cancelled"
    q.saveTaskMeta(task)
    slog.Debug("taskqueue.cancelled_paused", "task_id", id)
  }
}

func (q *TaskQueue) SendMessage(id, content string) error {
  q.mu.Lock()
  defer q.mu.Unlock()

  if q.current != nil && q.current.ID == id {
    q.current.mu.Lock()
    q.current.intervention = content
    q.current.mu.Unlock()
    slog.Debug("taskqueue.intervention", "task_id", id)
    return nil
  }
  return errors.New("任务未在运行中或不存在")
}

func (q *TaskQueue) Run(ctx context.Context) {
  q.ctx = ctx
  for ctx.Err() == nil {
    task := q.dequeue()
    if task == nil {
      time.Sleep(500 * time.Millisecond)
      continue
    }
    q.executeTask(ctx, task)
  }
}

func (q *TaskQueue) dequeue() *Task {
  q.mu.Lock()
  defer q.mu.Unlock()

  for len(q.queue) == 0 {
    q.cond.Wait()
  }

  var best *Task
  var bestIdx int
  for i, t := range q.queue {
    if t.Status != "pending" {
      continue
    }
    if best == nil || t.Priority > best.Priority {
      best = t
      bestIdx = i
    }
  }
  if best == nil {
    return nil
  }
  q.queue = append(q.queue[:bestIdx], q.queue[bestIdx+1:]...)
  q.current = best
  return best
}

func (q *TaskQueue) executeTask(ctx context.Context, task *Task) {
  taskCtx, cancel := context.WithCancel(ctx)
  task.Cancel = cancel
  defer cancel()

  task.mu.Lock()
  task.Status = "running"
  task.StartedAt = time.Now().UTC().Format(time.RFC3339)
  task.mu.Unlock()
  q.saveTaskMeta(task)

  q.eventCB("task_started", mustRawJSON(map[string]string{"task_id": task.ID}))
  slog.Debug("taskqueue.task_started", "task_id", task.ID)

  taskDir := filepath.Join(q.daemonDir, "tasks", task.ID)
  var es *store.EventStore
  var seq int64

  {
    var err error
    es, err = store.NewEventStore(taskDir)
    if err == nil {
      defer func() {
        es.Close()
      }()
    }
  }

  takeFileSnapshot(taskDir, q.workspace)

  var responseText string

  msg := &agent.Message{
    Role:    agent.RoleUser,
    Content: task.Message,
  }

  q.pauseRequested.Store(false)

  err := q.engine.Process(taskCtx, q.sysPrompt, nil, msg, func(evt agent.StreamEvent) {
    q.eventCB(evt.Type, evt.Data)

    if es != nil {
      seq++
      es.Append(&store.Event{
        TaskID: task.ID,
        Seq:    seq,
        Type:   evt.Type,
        Data:   evt.Data,
        Time:   time.Now().UTC().Format(time.RFC3339),
      })
    }

    if evt.Type == "text_delta" {
      var text string
      if json.Unmarshal(evt.Data, &text) == nil {
        responseText += text
      }
    }
    if evt.Type == "finish" {
      var finish struct {
        Content string `json:"content"`
      }
      if json.Unmarshal(evt.Data, &finish) == nil && finish.Content != "" {
        responseText = finish.Content
      }
    }
  })

  q.mu.Lock()
  if q.current == task {
    q.current = nil
  }
  q.mu.Unlock()

  if errors.Is(err, agent.ErrPaused) {
    task.mu.Lock()
    task.Status = "paused"
    task.mu.Unlock()
    q.saveTaskMeta(task)

    q.eventCB("task_paused", mustRawJSON(map[string]interface{}{
      "task_id":    task.ID,
      "iter_count": q.engine.IterCount(),
    }))
    slog.Debug("taskqueue.task_paused", "task_id", task.ID)
    return
  }

  if err != nil {
    task.mu.Lock()
    task.Status = "failed"
    task.mu.Unlock()
    q.saveTaskMeta(task)

    q.eventCB("task_failed", mustRawJSON(map[string]string{
      "task_id": task.ID,
      "error":   err.Error(),
    }))
    slog.Debug("taskqueue.task_failed", "task_id", task.ID, "error", err.Error())
    return
  }

  task.mu.Lock()
  task.Status = "completed"
  task.mu.Unlock()
  q.saveTaskMeta(task)

  q.eventCB("task_completed", mustRawJSON(map[string]interface{}{
    "task_id":  task.ID,
    "response": responseText,
    "iters":    q.engine.IterCount(),
  }))
  slog.Debug("taskqueue.task_completed", "task_id", task.ID, "response_len", len(responseText))
}

func (q *TaskQueue) onPause(snap *agent.EngineSnapshot) {
  q.mu.Lock()
  task := q.current
  q.mu.Unlock()

  if task == nil {
    return
  }

  taskDir := filepath.Join(q.daemonDir, "tasks", task.ID)
  ss := store.NewSnapshotStore(taskDir)
  taskSnap := &store.TaskSnapshot{
    SessionKey: snap.SessionKey,
    IterCount:  snap.IterCount,
    Skills:     snap.Skills,
  }
  if err := ss.Save(taskSnap); err != nil {
    slog.Debug("taskqueue.snapshot_save_failed", "task_id", task.ID, "error", err.Error())
  }

  fullSnapPath := filepath.Join(taskDir, "engine_snapshot.json")
  snapData, _ := json.MarshalIndent(snap, "", "  ")
  os.WriteFile(fullSnapPath, snapData, 0600)

  slog.Debug("taskqueue.snapshot_saved", "task_id", task.ID)
  select {
  case q.pauseReady <- struct{}{}:
  default:
  }
}

func (q *TaskQueue) restoreEngineFromSnapshot(id string) error {
  taskDir := filepath.Join(q.daemonDir, "tasks", id)
  snapPath := filepath.Join(taskDir, "engine_snapshot.json")

  data, err := os.ReadFile(snapPath)
  if err != nil {
    return err
  }
  var snap agent.EngineSnapshot
  if err := json.Unmarshal(data, &snap); err != nil {
    return err
  }

  q.engine.Restore(&snap)
  slog.Debug("taskqueue.engine_restored", "task_id", id, "iter_count", snap.IterCount)
  return nil
}

func (q *TaskQueue) FlushAll() {
  q.mu.Lock()
  defer q.mu.Unlock()

  for _, t := range q.queue {
    if t.Cancel != nil {
      t.Cancel()
    }
    if t.Status == "pending" {
      t.Status = "cancelled"
      q.saveTaskMeta(t)
    }
  }
  if q.current != nil {
    if q.current.Cancel != nil {
      q.current.Cancel()
    }
  }
}

func (q *TaskQueue) findTaskLocked(id string) *Task {
  if q.current != nil && q.current.ID == id {
    return q.current
  }
  for _, t := range q.queue {
    if t.ID == id {
      return t
    }
  }
  return nil
}

func (q *TaskQueue) saveTaskMeta(t *Task) {
  if q.store == nil {
    return
  }
  q.store.Save(store.TaskMeta{
    ID:        t.ID,
    Status:    t.Status,
    Source:    t.Source,
    CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339),
    StartedAt: t.StartedAt,
  })
}

