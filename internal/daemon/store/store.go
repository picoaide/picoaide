package store

import (
  "bufio"
  "compress/gzip"
  "encoding/json"
  "errors"
  "fmt"
  "os"
  "path/filepath"
  "sync"
)

var ErrNotFound = errors.New("not found")

// ============================================================
// TaskMeta — 任务索引
// ============================================================

type TaskMeta struct {
  ID           string `json:"id"`
  Status       string `json:"status"` // pending|running|paused|completed|cancelled|failed
  Source       string `json:"source"` // web|im|cron
  Title        string `json:"title,omitempty"`
  Response     string `json:"response,omitempty"`
  Error        string `json:"error,omitempty"`
  IterCount    int    `json:"iteration_count"`
  ToolCount    int    `json:"tool_call_count"`
  CurrentTool  string `json:"current_tool,omitempty"`
  CreatedAt    string `json:"created_at"`
  StartedAt    string `json:"started_at,omitempty"`
  PausedAt     string `json:"paused_at,omitempty"`
  CompletedAt  string `json:"completed_at,omitempty"`
}

// ============================================================
// TaskStore — tasks.json
// ============================================================

type TaskStore struct {
  dir string
  mu  sync.Mutex
}

func NewTaskStore(dir string) *TaskStore {
  return &TaskStore{dir: dir}
}

func (s *TaskStore) filePath() string {
  return filepath.Join(s.dir, "tasks.json")
}

func (s *TaskStore) List() ([]TaskMeta, error) {
  s.mu.Lock()
  defer s.mu.Unlock()
  return s.listLocked()
}

func (s *TaskStore) listLocked() ([]TaskMeta, error) {
  data, err := os.ReadFile(s.filePath())
  if err != nil {
    if os.IsNotExist(err) {
      return nil, nil
    }
    return nil, err
  }
  var tasks []TaskMeta
  if err := json.Unmarshal(data, &tasks); err != nil {
    return nil, fmt.Errorf("解析 tasks.json 失败: %w", err)
  }
  return tasks, nil
}

func (s *TaskStore) Save(task TaskMeta) error {
  s.mu.Lock()
  defer s.mu.Unlock()

  tasks, _ := s.listLocked()
  if tasks == nil {
    tasks = []TaskMeta{}
  }
  found := false
  for i, t := range tasks {
    if t.ID == task.ID {
      tasks[i] = task
      found = true
      break
    }
  }
  if !found {
    tasks = append(tasks, task)
  }

  return s.writeLocked(tasks)
}

func (s *TaskStore) UpdateStatus(id, status string) error {
  s.mu.Lock()
  defer s.mu.Unlock()

  tasks, err := s.listLocked()
  if err != nil {
    return err
  }
  for i, t := range tasks {
    if t.ID == id {
      tasks[i].Status = status
      return s.writeLocked(tasks)
    }
  }
  return ErrNotFound
}

func (s *TaskStore) writeLocked(tasks []TaskMeta) error {
  if err := os.MkdirAll(s.dir, 0755); err != nil {
    return fmt.Errorf("创建目录失败: %w", err)
  }
  data, err := json.MarshalIndent(tasks, "", "  ")
  if err != nil {
    return err
  }
  tmp := s.filePath() + ".tmp"
  if err := os.WriteFile(tmp, data, 0600); err != nil {
    return err
  }
  return os.Rename(tmp, s.filePath())
}

// ============================================================
// Event — daemon 事件
// ============================================================

type Event struct {
  TaskID string          `json:"task_id"`
  Seq    int64           `json:"seq"`
  Type   string          `json:"type"`
  Data   json.RawMessage `json:"data,omitempty"`
  Time   string          `json:"time,omitempty"`
}

// ============================================================
// EventStore — events.jsonl（活跃写入）; events.jsonl.gz（完成后归档）
// ============================================================

type EventStore struct {
  dir    string
  file   *os.File
  bufW   *bufio.Writer
  mu     sync.Mutex
}

func NewEventStore(taskDir string) (*EventStore, error) {
  os.MkdirAll(taskDir, 0755)
  path := filepath.Join(taskDir, "events.jsonl")
  f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
  if err != nil {
    return nil, err
  }
  return &EventStore{dir: taskDir, file: f, bufW: bufio.NewWriter(f)}, nil
}

func (es *EventStore) Append(evt *Event) error {
  es.mu.Lock()
  defer es.mu.Unlock()
  data, err := json.Marshal(evt)
  if err != nil {
    return err
  }
  _, err = es.bufW.Write(append(data, '\n'))
  return err
}

func (es *EventStore) Flush() error {
  es.mu.Lock()
  defer es.mu.Unlock()
  return es.bufW.Flush()
}

func (es *EventStore) Close() error {
  if err := es.Flush(); err != nil {
    es.file.Close()
    return err
  }
  if err := es.file.Close(); err != nil {
    return err
  }
  return gzipFile(filepath.Join(es.dir, "events.jsonl"))
}

func (es *EventStore) ReadAll() ([]*Event, error) {
  return es.ReadFromSeq(0)
}

func (es *EventStore) ReadFromSeq(fromSeq int64) ([]*Event, error) {
  path := filepath.Join(es.dir, "events.jsonl")
  // 优先读压缩版
  gzPath := path + ".gz"
  if _, err := os.Stat(gzPath); err == nil {
    path = gzPath
  }
  f, err := os.Open(path)
  if err != nil {
    if os.IsNotExist(err) {
      return nil, nil
    }
    return nil, err
  }
  defer f.Close()

  var scanner *bufio.Scanner
  if path == gzPath {
    gzipR, err := gzip.NewReader(f)
    if err != nil {
      return nil, err
    }
    defer gzipR.Close()
    scanner = bufio.NewScanner(gzipR)
  } else {
    scanner = bufio.NewScanner(f)
  }

  var events []*Event
  for scanner.Scan() {
    var evt Event
    if err := json.Unmarshal(scanner.Bytes(), &evt); err != nil {
      continue
    }
    if evt.Seq > fromSeq {
      events = append(events, &evt)
    }
  }
  return events, scanner.Err()
}

func gzipFile(src string) error {
  data, err := os.ReadFile(src)
  if err != nil {
    return err
  }
  dst := src + ".gz"
  tmp := dst + ".tmp"
  f, err := os.Create(tmp)
  if err != nil {
    return err
  }
  w := gzip.NewWriter(f)
  if _, err := w.Write(data); err != nil {
    f.Close()
    os.Remove(tmp)
    return err
  }
  if err := w.Close(); err != nil {
    f.Close()
    os.Remove(tmp)
    return err
  }
  f.Close()
  if err := os.Rename(tmp, dst); err != nil {
    os.Remove(tmp)
    return err
  }
  return os.Remove(src)
}

// ============================================================
// TaskSnapshot — pause 时的引擎状态
// ============================================================

type TaskSnapshot struct {
  SessionKey       string   `json:"session_key"`
  IterCount        int      `json:"iter_count"`
  Skills           []string `json:"skills"`
}

// ============================================================
// FileEntry — 文件状态（用于 diff）
// ============================================================

type FileEntry struct {
  Path    string `json:"path"`
  SHA256  string `json:"sha256"`
  Size    int64  `json:"size"`
  ModTime string `json:"mod_time"`
}

// ============================================================
// SnapshotStore — snapshot.json + files_*.json
// ============================================================

type SnapshotStore struct {
  dir string
  mu  sync.Mutex
}

func NewSnapshotStore(taskDir string) *SnapshotStore {
  return &SnapshotStore{dir: taskDir}
}

func (ss *SnapshotStore) snapPath() string {
  return filepath.Join(ss.dir, "snapshot.json")
}

func (ss *SnapshotStore) filesPath(snapType string) string {
  if snapType == "" {
    snapType = "default"
  }
  return filepath.Join(ss.dir, "files_"+snapType+".json")
}

func (ss *SnapshotStore) Save(snap *TaskSnapshot) error {
  ss.mu.Lock()
  defer ss.mu.Unlock()
  os.MkdirAll(ss.dir, 0755)
  data, err := json.MarshalIndent(snap, "", "  ")
  if err != nil {
    return err
  }
  tmp := ss.snapPath() + ".tmp"
  if err := os.WriteFile(tmp, data, 0600); err != nil {
    return err
  }
  return os.Rename(tmp, ss.snapPath())
}

func (ss *SnapshotStore) SaveFiles(snapType string, files []FileEntry) error {
  ss.mu.Lock()
  defer ss.mu.Unlock()
  os.MkdirAll(ss.dir, 0755)
  data, err := json.MarshalIndent(files, "", "  ")
  if err != nil {
    return err
  }
  tmp := ss.filesPath(snapType) + ".tmp"
  if err := os.WriteFile(tmp, data, 0600); err != nil {
    return err
  }
  return os.Rename(tmp, ss.filesPath(snapType))
}

func (ss *SnapshotStore) Load() (*TaskSnapshot, error) {
  ss.mu.Lock()
  defer ss.mu.Unlock()
  data, err := os.ReadFile(ss.snapPath())
  if err != nil {
    if os.IsNotExist(err) {
      return nil, ErrNotFound
    }
    return nil, err
  }
  var snap TaskSnapshot
  if err := json.Unmarshal(data, &snap); err != nil {
    return nil, err
  }
  return &snap, nil
}

func (ss *SnapshotStore) LoadFiles(snapType string) ([]FileEntry, error) {
  ss.mu.Lock()
  defer ss.mu.Unlock()
  data, err := os.ReadFile(ss.filesPath(snapType))
  if err != nil {
    if os.IsNotExist(err) {
      return nil, ErrNotFound
    }
    return nil, err
  }
  var files []FileEntry
  if err := json.Unmarshal(data, &files); err != nil {
    return nil, err
  }
  return files, nil
}
