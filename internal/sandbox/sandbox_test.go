package sandbox

import (
  "context"
  "encoding/json"
  "testing"
  "time"
)

func TestNewManager(t *testing.T) {
  m := NewManager("/tmp/rootfs", "/tmp/workdir")
  if m == nil {
    t.Fatal("NewManager returned nil")
  }
  if m.rootfs != "/tmp/rootfs" {
    t.Errorf("rootfs = %q, want /tmp/rootfs", m.rootfs)
  }
  if m.workDir != "/tmp/workdir" {
    t.Errorf("workDir = %q, want /tmp/workdir", m.workDir)
  }
}

func TestRunResult_Structure(t *testing.T) {
  r := &RunResult{
    Events: []StreamEvent{
      {Type: "start", Data: json.RawMessage(`{}`)},
      {Type: "finish", Data: json.RawMessage(`{"status":"ok"}`)},
    },
    Error: "",
  }
  if len(r.Events) != 2 {
    t.Errorf("len(Events) = %d, want 2", len(r.Events))
  }
  if r.Events[0].Type != "start" {
    t.Errorf("Events[0].Type = %q, want start", r.Events[0].Type)
  }
  if r.Events[1].Type != "finish" {
    t.Errorf("Events[1].Type = %q, want finish", r.Events[1].Type)
  }
}

func TestRunResult_WithError(t *testing.T) {
  r := &RunResult{
    Events: []StreamEvent{
      {Type: "error", Data: json.RawMessage(`"something went wrong"`)},
    },
    Error: "something went wrong",
  }
  if r.Error != "something went wrong" {
    t.Errorf("Error = %q, want something went wrong", r.Error)
  }
}

func TestUserLock_ReleasedOnPanic(t *testing.T) {
  m := NewManager("/tmp/nonexistent", t.TempDir())
  username := "panic-test-user"

  // Acquire the lock normally
  err := m.acquireUser(context.Background(), username)
  if err != nil {
    t.Fatal(err)
  }

  // Simulate a function that panics after acquiring (like prepareSandbox)
  func() {
    defer func() { recover() }()
    var released bool
    defer func() {
      if !released {
        m.releaseUser(username)
      }
    }()
    panic("simulated panic")
    // released = true never reached
  }()

  // Lock should be released by the defer
  ctx, cancel := context.WithTimeout(context.Background(), time.Second)
  defer cancel()
  err = m.acquireUser(ctx, username)
  if err != nil {
    t.Fatal("lock not released after panic, acquire blocked")
  }
  m.releaseUser(username)
}
