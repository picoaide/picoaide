package store

import (
  "encoding/json"
  "os"
  "path/filepath"
  "testing"
)

// tasks_test.go — 测试 daemon/tasks.json 的文件存储

func TestTaskStore_CreateAndList(t *testing.T) {
  dir := t.TempDir()
  s := NewTaskStore(dir)

  task := TaskMeta{
    ID:       "task_001",
    Status:   "completed",
    Source:   "web",
    Title:    "test task",
    Response: "hello world",
    IterCount: 5,
    ToolCount: 3,
    CreatedAt: "2026-01-01T00:00:00Z",
  }

  err := s.Save(task)
  if err != nil {
    t.Fatalf("Save failed: %v", err)
  }

  list, err := s.List()
  if err != nil {
    t.Fatal(err)
  }
  if len(list) != 1 {
    t.Fatalf("List length = %d, want 1", len(list))
  }
  if list[0].ID != "task_001" {
    t.Errorf("ID = %q, want task_001", list[0].ID)
  }
  if list[0].Status != "completed" {
    t.Errorf("Status = %q, want completed", list[0].Status)
  }
}

func TestTaskStore_UpdateStatus(t *testing.T) {
  dir := t.TempDir()
  s := NewTaskStore(dir)

  s.Save(TaskMeta{ID: "t1", Status: "pending", CreatedAt: "a"})
  s.Save(TaskMeta{ID: "t2", Status: "pending", CreatedAt: "b"})

  err := s.UpdateStatus("t1", "running")
  if err != nil {
    t.Fatal(err)
  }

  list, _ := s.List()
  for _, m := range list {
    if m.ID == "t1" && m.Status != "running" {
      t.Errorf("t1 status = %q, want running", m.Status)
    }
    if m.ID == "t2" && m.Status != "pending" {
      t.Errorf("t2 status should still be pending, got %q", m.Status)
    }
  }
}

func TestTaskStore_EmptyDir(t *testing.T) {
  dir := t.TempDir()
  s := NewTaskStore(dir)

  list, err := s.List()
  if err != nil {
    t.Fatal(err)
  }
  if len(list) != 0 {
    t.Errorf("expected empty list, got %d items", len(list))
  }
}

func TestTaskStore_NonExistentDir(t *testing.T) {
  s := NewTaskStore("/nonexistent/path")

  _, err := s.List()
  if err != nil {
    t.Errorf("List on nonexistent dir should return empty, not error: %v", err)
  }
}

func TestTaskStore_SaveThenReload(t *testing.T) {
  dir := t.TempDir()
  s1 := NewTaskStore(dir)

  s1.Save(TaskMeta{ID: "a", Status: "pending", CreatedAt: "t1"})
  s1.Save(TaskMeta{ID: "b", Status: "completed", CreatedAt: "t2"})

  // 模拟进程重启：创建新 TaskStore 并读取
  s2 := NewTaskStore(dir)
  list, err := s2.List()
  if err != nil {
    t.Fatal(err)
  }
  if len(list) != 2 {
    t.Errorf("reloaded list length = %d, want 2", len(list))
  }
}

func TestTaskStore_ConcurrentAccess(t *testing.T) {
  dir := t.TempDir()
  s := NewTaskStore(dir)

  done := make(chan struct{})
  for i := 0; i < 10; i++ {
    go func(n int) {
      id := "task_" + string(rune('0'+n))
      s.Save(TaskMeta{ID: id, Status: "pending", CreatedAt: "t"})
      done <- struct{}{}
    }(i)
  }
  for i := 0; i < 10; i++ {
    <-done
  }

  list, _ := s.List()
  if len(list) != 10 {
    t.Errorf("concurrent: got %d tasks, want 10", len(list))
  }
}

// ============================================================
// EventStore — events.jsonl.gz
// ============================================================

func TestEventStore_WriteAndReadBack(t *testing.T) {
  dir := t.TempDir()
  taskDir := filepath.Join(dir, "task_001")
  os.MkdirAll(taskDir, 0755)

  es, err := NewEventStore(taskDir)
  if err != nil {
    t.Fatal(err)
  }
  defer es.Close()

  for i := 0; i < 5; i++ {
    es.Append(&Event{
      TaskID: "task_001", Seq: int64(i + 1), Type: "text_delta", Data: json.RawMessage(`"hello"`),
    })
  }
  if err := es.Flush(); err != nil {
    t.Fatal(err)
  }

  events, err := es.ReadAll()
  if err != nil {
    t.Fatal(err)
  }
  if len(events) != 5 {
    t.Fatalf("read %d events, want 5", len(events))
  }
  for i, e := range events {
    if e.Seq != int64(i+1) {
      t.Errorf("event[%d].Seq = %d, want %d", i, e.Seq, i+1)
    }
  }
}

func TestEventStore_ReplayFromSeq(t *testing.T) {
  dir := t.TempDir()
  taskDir := filepath.Join(dir, "task_001")
  os.MkdirAll(taskDir, 0755)

  es, _ := NewEventStore(taskDir)
  for i := 0; i < 10; i++ {
    es.Append(&Event{TaskID: "task_001", Seq: int64(i + 1), Type: "text_delta", Data: json.RawMessage(`"x"`)})
  }
  es.Flush()
  es.Close()

  es2, _ := NewEventStore(taskDir)
  defer es2.Close()
  events, err := es2.ReadFromSeq(5)
  if err != nil {
    t.Fatal(err)
  }
  if len(events) != 5 {
    t.Fatalf("replay from seq 5: got %d events, want 5", len(events))
  }
  for i, e := range events {
    wantSeq := int64(i + 6)
    if e.Seq != wantSeq {
      t.Errorf("event[%d].Seq = %d, want %d", i, e.Seq, wantSeq)
    }
  }
}

// ============================================================
// SnapshotStore — snapshot.json + files.json
// ============================================================

func TestSnapshotStore_SaveAndLoad(t *testing.T) {
  dir := t.TempDir()
  taskDir := filepath.Join(dir, "task_001")
  os.MkdirAll(taskDir, 0755)

  ss := NewSnapshotStore(taskDir)

  snap := &TaskSnapshot{
    SessionKey: "key1",
    IterCount:  5,
    Skills:     []string{"s1"},
  }
  err := ss.Save(snap)
  if err != nil {
    t.Fatal(err)
  }

  loaded, err := ss.Load()
  if err != nil {
    t.Fatal(err)
  }
  if loaded.SessionKey != "key1" {
    t.Errorf("SessionKey = %q", loaded.SessionKey)
  }
  if loaded.IterCount != 5 {
    t.Errorf("IterCount = %d", loaded.IterCount)
  }
  if len(loaded.Skills) != 1 || loaded.Skills[0] != "s1" {
    t.Errorf("Skills = %v", loaded.Skills)
  }
}

func TestSnapshotStore_LoadNonExistent(t *testing.T) {
  dir := t.TempDir()
  taskDir := filepath.Join(dir, "task_001")
  os.MkdirAll(taskDir, 0755)

  ss := NewSnapshotStore(taskDir)
  _, err := ss.Load()
  if err != ErrNotFound {
    t.Errorf("expected ErrNotFound, got %v", err)
  }
}

func TestSnapshotStore_FilesSaveAndLoad(t *testing.T) {
  dir := t.TempDir()
  taskDir := filepath.Join(dir, "task_001")
  os.MkdirAll(taskDir, 0755)

  ss := NewSnapshotStore(taskDir)
  files := []FileEntry{
    {Path: "main.py", SHA256: "abc123", Size: 100, ModTime: "t1"},
    {Path: "readme.md", SHA256: "def456", Size: 200, ModTime: "t2"},
  }
  err := ss.SaveFiles("before_task", files)
  if err != nil {
    t.Fatal(err)
  }

  loaded, err := ss.LoadFiles("before_task")
  if err != nil {
    t.Fatal(err)
  }
  if len(loaded) != 2 {
    t.Fatalf("files length = %d, want 2", len(loaded))
  }
  if loaded[0].Path != "main.py" {
    t.Errorf("first file path = %q", loaded[0].Path)
  }
}
