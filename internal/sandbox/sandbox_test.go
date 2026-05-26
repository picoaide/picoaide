package sandbox

import (
  "context"
  "encoding/json"
  "io"
  "strings"
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

func TestStreamEvents_ReadsJSONLines(t *testing.T) {
  input := `{"type":"start","data":{"msg":"hello"}}
{"type":"progress","data":{"pct":50}}
{"type":"finish","data":{"status":"done"}}
`
  r := strings.NewReader(input)
  ctx := context.Background()

  events, err := StreamEvents(ctx, r)
  if err != nil {
    t.Fatalf("StreamEvents returned error: %v", err)
  }

  var collected []StreamEvent
  for ev := range events {
    collected = append(collected, ev)
  }

  if len(collected) != 3 {
    t.Fatalf("got %d events, want 3", len(collected))
  }

  tests := []struct {
    index int
    want  string
  }{
    {0, "start"},
    {1, "progress"},
    {2, "finish"},
  }
  for _, tt := range tests {
    if collected[tt.index].Type != tt.want {
      t.Errorf("collected[%d].Type = %q, want %q", tt.index, collected[tt.index].Type, tt.want)
    }
  }
}

func TestStreamEvents_SkipsEmptyLines(t *testing.T) {
  input := `{"type":"a","data":{}}

{"type":"b","data":{}}

`
  r := strings.NewReader(input)
  ctx := context.Background()

  events, err := StreamEvents(ctx, r)
  if err != nil {
    t.Fatalf("StreamEvents returned error: %v", err)
  }

  var collected []StreamEvent
  for ev := range events {
    collected = append(collected, ev)
  }

  if len(collected) != 2 {
    t.Errorf("got %d events, want 2 (empty lines should be skipped)", len(collected))
  }
}

func TestStreamEvents_SkipsInvalidJSON(t *testing.T) {
  input := `{"type":"valid","data":{}}
not valid json
{"type":"also-valid","data":{}}
`
  r := strings.NewReader(input)
  ctx := context.Background()

  events, err := StreamEvents(ctx, r)
  if err != nil {
    t.Fatalf("StreamEvents returned error: %v", err)
  }

  var collected []StreamEvent
  for ev := range events {
    collected = append(collected, ev)
  }

  if len(collected) != 2 {
    t.Errorf("got %d events, want 2 (invalid JSON lines should be skipped)", len(collected))
  }
}

func TestStreamEvents_PreservesRawData(t *testing.T) {
  rawData := `{"msg":"test","count":42}`
  input := `{"type":"data","data":` + rawData + `}
`
  r := strings.NewReader(input)
  ctx := context.Background()

  events, err := StreamEvents(ctx, r)
  if err != nil {
    t.Fatalf("StreamEvents returned error: %v", err)
  }

  var collected []StreamEvent
  for ev := range events {
    collected = append(collected, ev)
  }

  if len(collected) != 1 {
    t.Fatalf("got %d events, want 1", len(collected))
  }

  if string(collected[0].Data) != rawData {
    t.Errorf("Data = %q, want %q", string(collected[0].Data), rawData)
  }
}

func TestStreamEvents_ContextCancellation(t *testing.T) {
  pr, pw := io.Pipe()
  ctx, cancel := context.WithCancel(context.Background())

  events, err := StreamEvents(ctx, pr)
  if err != nil {
    t.Fatalf("StreamEvents returned error: %v", err)
  }

  // Fill the buffer so the next send blocks and ctx.Done() wins the select
  for i := 0; i < 100; i++ {
    pw.Write([]byte(`{"type":"fill","data":{}}` + "\n"))
  }
  // Give the goroutine time to fill the buffer with the first 100 events
  time.Sleep(50 * time.Millisecond)

  // Cancel context and write one more line
  cancel()
  pw.Write([]byte(`{"type":"extra","data":{}}` + "\n"))
  pw.Close()

  done := make(chan struct{})
  go func() {
    for range events {
    }
    close(done)
  }()

  select {
  case <-done:
  case <-time.After(3 * time.Second):
    t.Fatal("channel did not close after context cancellation")
  }
}

func TestStreamEvents_ContextAlreadyCancelled(t *testing.T) {
  ctx, cancel := context.WithCancel(context.Background())
  cancel()

  r := strings.NewReader(`{"type":"a","data":{}}` + "\n")
  events, err := StreamEvents(ctx, r)
  if err != nil {
    t.Fatalf("StreamEvents returned error: %v", err)
  }

  // Channel must close (goroutine must exit) — select may deliver event
  // due to buffered channel racing with ctx.Done(), but close is guaranteed.
  done := make(chan struct{})
  go func() {
    for range events {
    }
    close(done)
  }()

  select {
  case <-done:
  case <-time.After(3 * time.Second):
    t.Fatal("goroutine did not exit with already-cancelled context")
  }
}

func TestStreamEvents_LargeInput(t *testing.T) {
  var lines []string
  for i := 0; i < 1000; i++ {
    lines = append(lines, `{"type":"event","data":{"index":`+itoa(i)+`}}`)
  }
  input := strings.Join(lines, "\n") + "\n"
  r := strings.NewReader(input)
  ctx := context.Background()

  events, err := StreamEvents(ctx, r)
  if err != nil {
    t.Fatalf("StreamEvents returned error: %v", err)
  }

  count := 0
  for range events {
    count++
  }
  if count != 1000 {
    t.Errorf("got %d events, want 1000", count)
  }
}

func itoa(i int) string {
  if i == 0 {
    return "0"
  }
  var buf [20]byte
  pos := len(buf)
  neg := i < 0
  if neg {
    i = -i
  }
  for i > 0 {
    pos--
    buf[pos] = byte('0' + i%10)
    i /= 10
  }
  if neg {
    pos--
    buf[pos] = '-'
  }
  return string(buf[pos:])
}

func TestStreamEvents_PartialJSONLine(t *testing.T) {
  input := `{"type":"valid","data":{}}
{"type":"truncated","data":{`
  r := strings.NewReader(input)
  ctx := context.Background()

  events, err := StreamEvents(ctx, r)
  if err != nil {
    t.Fatalf("StreamEvents returned error: %v", err)
  }

  var collected []StreamEvent
  for ev := range events {
    collected = append(collected, ev)
  }

  if len(collected) != 1 {
    t.Errorf("got %d events, want 1 (partial JSON should be skipped)", len(collected))
  }
}

func TestStreamEvents_MultipleTypes(t *testing.T) {
  input := `{"type":"start","data":{"ts":1}}
{"type":"intermediate","data":{"msg":"step1"}}
{"type":"intermediate","data":{"msg":"step2"}}
{"type":"error","data":"something failed"}
{"type":"finish","data":{"status":"err"}}
`
  r := strings.NewReader(input)
  ctx := context.Background()

  events, err := StreamEvents(ctx, r)
  if err != nil {
    t.Fatalf("StreamEvents returned error: %v", err)
  }

  var collected []StreamEvent
  for ev := range events {
    collected = append(collected, ev)
  }

  if len(collected) != 5 {
    t.Fatalf("got %d events, want 5", len(collected))
  }

  expected := []string{"start", "intermediate", "intermediate", "error", "finish"}
  for i, typ := range expected {
    if collected[i].Type != typ {
      t.Errorf("collected[%d].Type = %q, want %q", i, collected[i].Type, typ)
    }
  }
}

func TestStreamEvents_EmptyReader(t *testing.T) {
  r := strings.NewReader("")
  ctx := context.Background()

  events, err := StreamEvents(ctx, r)
  if err != nil {
    t.Fatalf("StreamEvents returned error: %v", err)
  }

  _, ok := <-events
  if ok {
    t.Error("expected channel to be closed immediately for empty reader")
  }
}

func TestStreamEvents_OnlyEmptyLines(t *testing.T) {
  r := strings.NewReader("\n\n\n\n")
  ctx := context.Background()

  events, err := StreamEvents(ctx, r)
  if err != nil {
    t.Fatalf("StreamEvents returned error: %v", err)
  }

  _, ok := <-events
  if ok {
    t.Error("expected channel to be closed immediately for only empty lines")
  }
}
