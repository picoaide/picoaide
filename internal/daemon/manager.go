package daemon

import (
  "database/sql"
  "encoding/json"
  "log/slog"
  "path/filepath"
  "sync"
  "time"

  "github.com/picoaide/picoaide/internal/daemon/store"
)

// ============================================================
// DaemonManager — 管理所有用户的 daemon 进程
// ============================================================

type DaemonManager struct {
  db       *sql.DB
  workDir  string
  hub      *Hub
}

func NewDaemonManager(db *sql.DB, workDir string, hub *Hub) *DaemonManager {
  dm := &DaemonManager{
    db:      db,
    workDir: workDir,
    hub:     hub,
  }
  dm.ensureTable()
  go dm.monitorLoop()
  return dm
}

func (dm *DaemonManager) ensureTable() {
  dm.db.Exec(`CREATE TABLE IF NOT EXISTS agent_daemons (
    username TEXT PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'stopped',
    current_task_id TEXT DEFAULT NULL,
    last_heartbeat_at TEXT NOT NULL DEFAULT ''
  )`)
}

// GetStatus 查询 daemon 状态
func (dm *DaemonManager) GetStatus(username string) map[string]interface{} {
  var status, currentTaskID, lastHB string
  row := dm.db.QueryRow(
    `SELECT status, current_task_id, last_heartbeat_at FROM agent_daemons WHERE username = ?`,
    username,
  )
  if err := row.Scan(&status, &currentTaskID, &lastHB); err != nil {
    return map[string]interface{}{"running": false}
  }
  return map[string]interface{}{
    "running":          status == "running",
    "status":           status,
    "current_task_id":  currentTaskID,
    "last_heartbeat_at": lastHB,
  }
}

// UpdateHeartbeat 更新心跳
func (dm *DaemonManager) UpdateHeartbeat(username, taskID string) {
  dm.db.Exec(
    `INSERT OR REPLACE INTO agent_daemons (username, status, current_task_id, last_heartbeat_at)
     VALUES (?, 'running', ?, ?)`,
    username, taskID, time.Now().UTC().Format(time.RFC3339),
  )
}

// MarkStopped 标记 daemon 停止
func (dm *DaemonManager) MarkStopped(username string) {
  dm.db.Exec(
    `UPDATE agent_daemons SET status='stopped', current_task_id=NULL WHERE username = ?`,
    username,
  )
}

// MarkCrashed 标记 daemon 崩溃
func (dm *DaemonManager) MarkCrashed(username string) {
  dm.db.Exec(
    `UPDATE agent_daemons SET status='crash' WHERE username = ?`,
    username,
  )
}

// ListAll 列出所有 daemon 状态
func (dm *DaemonManager) ListAll() ([]map[string]interface{}, error) {
  rows, err := dm.db.Query(
    `SELECT username, status, current_task_id, last_heartbeat_at FROM agent_daemons ORDER BY username`,
  )
  if err != nil {
    return nil, err
  }
  defer rows.Close()

  var result []map[string]interface{}
  for rows.Next() {
    var username, status, taskID, lhb string
    rows.Scan(&username, &status, &taskID, &lhb)
    result = append(result, map[string]interface{}{
      "username":          username,
      "status":            status,
      "current_task_id":   taskID,
      "last_heartbeat_at": lhb,
    })
  }
  if result == nil {
    result = []map[string]interface{}{}
  }
  return result, nil
}

// monitorLoop 崩溃检测循环
func (dm *DaemonManager) monitorLoop() {
  ticker := time.NewTicker(10 * time.Second)
  defer ticker.Stop()
  for range ticker.C {
    rows, err := dm.db.Query(
      `SELECT username FROM agent_daemons WHERE status = 'running'
       AND last_heartbeat_at < ?`,
      time.Now().UTC().Add(-30*time.Second).Format(time.RFC3339),
    )
    if err != nil {
      continue
    }
    var crashed []string
    for rows.Next() {
      var u string
      rows.Scan(&u)
      crashed = append(crashed, u)
    }
    rows.Close()
    for _, u := range crashed {
      slog.Error("daemon.health_check_failed", "username", u)
      dm.MarkCrashed(u)
    }
  }
}

// ============================================================
// Hub — per-user 事件广播中心（被 Web API 使用）
// ============================================================

type Hub struct {
  subs map[string]map[string]chan json.RawMessage
  mu   sync.RWMutex
}

func NewHub() *Hub {
  return &Hub{subs: make(map[string]map[string]chan json.RawMessage)}
}

func (h *Hub) Subscribe(username, clientID string) chan json.RawMessage {
  h.mu.Lock()
  defer h.mu.Unlock()
  if h.subs[username] == nil {
    h.subs[username] = make(map[string]chan json.RawMessage)
  }
  ch := make(chan json.RawMessage, 100)
  h.subs[username][clientID] = ch
  return ch
}

func (h *Hub) Unsubscribe(username, clientID string) {
  h.mu.Lock()
  defer h.mu.Unlock()
  if m, ok := h.subs[username]; ok {
    if ch, ok := m[clientID]; ok {
      close(ch)
      delete(m, clientID)
    }
  }
}

func (h *Hub) Broadcast(username string, data json.RawMessage) {
  h.mu.RLock()
  defer h.mu.RUnlock()
  for _, ch := range h.subs[username] {
    select {
    case ch <- data:
    default:
    }
  }
}

// ============================================================
// TaskManager — 任务管理器
// ============================================================

type TaskManager struct {
  workDir string
}

func NewTaskManager(workDir string) *TaskManager {
  return &TaskManager{workDir: workDir}
}

func (tm *TaskManager) userDaemonDir(username string) string {
  return filepath.Join(tm.workDir, "users", username, "daemon")
}

func (tm *TaskManager) ListTasks(username string) ([]store.TaskMeta, error) {
  ts := store.NewTaskStore(tm.userDaemonDir(username))
  return ts.List()
}

func (tm *TaskManager) GetTaskEvents(username, taskID string, sinceSeq int64) ([]*store.Event, error) {
  taskDir := filepath.Join(tm.userDaemonDir(username), "tasks", taskID)
  es, err := store.NewEventStore(taskDir)
  if err != nil {
    return nil, err
  }
  defer es.Close()
  return es.ReadFromSeq(sinceSeq)
}

func (tm *TaskManager) GetTaskSnapshots(username, taskID, snapType string) ([]store.FileEntry, error) {
  snapDir := filepath.Join(tm.userDaemonDir(username), "tasks", taskID)
  ss := store.NewSnapshotStore(snapDir)
  return ss.LoadFiles(snapType)
}
