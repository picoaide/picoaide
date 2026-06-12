package daemon

import (
  "encoding/json"
  "testing"
  "time"

  "github.com/picoaide/picoaide/internal/daemon/store"
)

func TestEventBus_EmitAndSubscribe(t *testing.T) {
  dir := t.TempDir()
  ts := store.NewTaskStore(dir)
  es, err := store.NewEventStore(dir)
  if err != nil {
    t.Fatal(err)
  }
  defer es.Close()

  bus := NewEventBus("task_001", es, ts)

  done := make(chan struct{})
  var received []*store.Event
  subCh := bus.Subscribe("client1")
  go func() {
    for e := range subCh {
      received = append(received, e)
      if len(received) >= 2 {
        close(done)
        return
      }
    }
  }()

  bus.Emit("text_delta", json.RawMessage(`"hello"`))
  bus.Emit("tool_call_start", json.RawMessage(`{"name":"echo"}`))
  bus.Close()

  <-done
  if len(received) != 2 {
    t.Fatalf("received %d events, want 2", len(received))
  }
  if received[0].Type != "text_delta" {
    t.Errorf("event[0].Type = %q", received[0].Type)
  }
}

func TestEventBus_ReplayFromSeq(t *testing.T) {
  dir := t.TempDir()
  ts := store.NewTaskStore(dir)
  es, err := store.NewEventStore(dir)
  if err != nil {
    t.Fatal(err)
  }
  defer es.Close()

  bus := NewEventBus("task_001", es, ts)
  for i := 0; i < 5; i++ {
    bus.Emit("text_delta", json.RawMessage(`"x"`))
  }
  bus.Close()

  replayed, err := bus.ReplayFromSeq(2)
  if err != nil {
    t.Fatal(err)
  }
  if len(replayed) != 3 {
    t.Fatalf("replay from seq 2: got %d events, want 3", len(replayed))
  }
  for i, e := range replayed {
    if e.Seq != int64(i+3) {
      t.Errorf("event[%d].Seq = %d, want %d", i, e.Seq, i+3)
    }
  }
}

func TestEventBus_Unsubscribe(t *testing.T) {
  dir := t.TempDir()
  ts := store.NewTaskStore(dir)
  es, _ := store.NewEventStore(dir)
  defer es.Close()

  bus := NewEventBus("task_001", es, ts)

  subCh := bus.Subscribe("client1")
  bus.Unsubscribe("client1")

  bus.Emit("text_delta", json.RawMessage(`"x"`))
  bus.Close()

  // 确保 channel 不会阻塞
  select {
  case <-subCh:
    t.Error("should not receive event after unsubscribe")
  default:
  }
}

func TestEventBus_RingBuffer(t *testing.T) {
  dir := t.TempDir()
  ts := store.NewTaskStore(dir)
  es, _ := store.NewEventStore(dir)
  defer es.Close()

  bus := NewEventBus("task_001", es, ts)

  subCh := bus.Subscribe("client1")
  done := make(chan struct{})
  var count int
  go func() {
    for range subCh {
      count++
      if count >= 5 {
        close(done)
        return
      }
    }
  }()

  // 发送大量事件，环缓冲能容纳
  for i := 0; i < 10; i++ {
    bus.Emit("text_delta", json.RawMessage(`"x"`))
  }
  bus.Close()

  select {
  case <-done:
  case <-time.After(time.Second):
  }
}
