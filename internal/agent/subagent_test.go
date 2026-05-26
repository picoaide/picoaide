package agent

import (
  "context"
  "encoding/json"
  "strings"
  "testing"
  "time"
)

// ============================================================
// SubAgent — 子代理类型
// ============================================================

func TestSubAgent_RunsTask(t *testing.T) {
  agent := NewSubAgent("test-agent", func(ctx context.Context) (string, error) {
    return "hello from subagent", nil
  })

  ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
  defer cancel()

  result := agent.Run(ctx)
  if !result.Success {
    t.Fatalf("expected success, got error: %s", result.Data)
  }
  if result.Data != "hello from subagent" {
    t.Errorf("Data = %q, want %q", result.Data, "hello from subagent")
  }
  if result.Name != "test-agent" {
    t.Errorf("Name = %q, want test-agent", result.Name)
  }
}

func TestSubAgent_Timeout(t *testing.T) {
  agent := NewSubAgent("slow-agent", func(ctx context.Context) (string, error) {
    select {
    case <-ctx.Done():
      return "", ctx.Err()
    case <-time.After(10 * time.Second):
      return "too late", nil
    }
  })

  ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
  defer cancel()

  result := agent.Run(ctx)
  if result.Success {
    t.Errorf("expected failure for timeout, got success: %s", result.Data)
  }
  if !strings.Contains(result.Data, "context deadline exceeded") && !strings.Contains(result.Data, "超时") {
    t.Errorf("expected timeout message, got: %s", result.Data)
  }
}

// ============================================================
// SubAgentManager — 管理多个子代理
// ============================================================

func TestSubAgentManager_SpawnAndCollect(t *testing.T) {
  mgr := NewSubAgentManager()
  agent := NewSubAgent("worker", func(ctx context.Context) (string, error) {
    return "done", nil
  })

  err := mgr.Spawn(context.Background(), agent)
  if err != nil {
    t.Fatal(err)
  }

  result := mgr.Collect("worker", 5*time.Second)
  if result == nil {
    t.Fatal("expected result, got nil")
  }
  if !result.Success {
    t.Fatalf("expected success, got: %s", result.Data)
  }
  if result.Data != "done" {
    t.Errorf("Data = %q, want %q", result.Data, "done")
  }
}

func TestSubAgentManager_CollectTimeout(t *testing.T) {
  mgr := NewSubAgentManager()
  agent := NewSubAgent("slow", func(ctx context.Context) (string, error) {
    select {
    case <-ctx.Done():
      return "", ctx.Err()
    case <-time.After(10 * time.Second):
      return "done", nil
    }
  })

  mgr.Spawn(context.Background(), agent)
  result := mgr.Collect("slow", 20*time.Millisecond)

  if result == nil {
    t.Fatal("expected result, got nil")
  }
  if result.Success {
    t.Errorf("expected timeout failure, got success: %s", result.Data)
  }
}

func TestSubAgentManager_List(t *testing.T) {
  mgr := NewSubAgentManager()
  mgr.Spawn(context.Background(), NewSubAgent("a", func(ctx context.Context) (string, error) { return "a", nil }))
  mgr.Spawn(context.Background(), NewSubAgent("b", func(ctx context.Context) (string, error) { return "b", nil }))

  // wait briefly for spawn to register
  time.Sleep(5 * time.Millisecond)

  names := mgr.List()
  if len(names) != 2 {
    t.Fatalf("expected 2 agents, got %d: %v", len(names), names)
  }
  has := func(name string) bool {
    for _, n := range names {
      if n == name {
        return true
      }
    }
    return false
  }
  if !has("a") || !has("b") {
    t.Errorf("missing names: got %v", names)
  }
}

func TestSubAgentManager_CancelAll(t *testing.T) {
  mgr := NewSubAgentManager()
  mgr.Spawn(context.Background(), NewSubAgent("slow1", func(ctx context.Context) (string, error) {
    select {
    case <-ctx.Done():
      return "", ctx.Err()
    case <-time.After(10 * time.Second):
      return "done", nil
    }
  }))
  mgr.Spawn(context.Background(), NewSubAgent("slow2", func(ctx context.Context) (string, error) {
    select {
    case <-ctx.Done():
      return "", ctx.Err()
    case <-time.After(10 * time.Second):
      return "done", nil
    }
  }))

  mgr.CancelAll()

  r1 := mgr.Collect("slow1", 1*time.Second)
  r2 := mgr.Collect("slow2", 1*time.Second)

  if r1.Success {
    t.Errorf("slow1 should be cancelled, got success")
  }
  if r2.Success {
    t.Errorf("slow2 should be cancelled, got success")
  }
}

func TestSubAgentManager_SpawnDuplicate(t *testing.T) {
  mgr := NewSubAgentManager()
  agent := NewSubAgent("dup", func(ctx context.Context) (string, error) { return "first", nil })

  if err := mgr.Spawn(context.Background(), agent); err != nil {
    t.Fatal(err)
  }
  if err := mgr.Spawn(context.Background(), agent); err == nil {
    t.Error("expected error for duplicate spawn, got nil")
  }
}

func TestSubAgentManager_CollectNonExistent(t *testing.T) {
  mgr := NewSubAgentManager()
  result := mgr.Collect("nonexistent", 1*time.Second)
  if result == nil {
    t.Fatal("expected non-nil result for non-existent agent")
  }
  if result.Success {
    t.Errorf("expected failure for non-existent agent")
  }
}

// ============================================================
// SubAgentManager + ToolRegistry 集成
// ============================================================

func TestSubAgentTool_SpawnAndCollect(t *testing.T) {
  mgr := NewSubAgentManager()
  registry := NewToolRegistry()

  tool := &SubAgentTool{Manager: mgr}
  registry.Register(tool)

  args, _ := json.Marshal(map[string]interface{}{
    "name":     "searcher",
    "command":  "echo hello from subagent",
    "timeout":  5,
  })

  result, err := registry.Execute(context.Background(), "subagent_task", args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("expected success, got: %s", result.Data)
  }
  if !strings.Contains(result.Data, "hello from subagent") {
    t.Errorf("expected 'hello from subagent' in result, got: %s", result.Data)
  }
}

func TestSubAgentTool_Timeout(t *testing.T) {
  mgr := NewSubAgentManager()
  registry := NewToolRegistry()
  tool := &SubAgentTool{Manager: mgr}
  registry.Register(tool)

  args, _ := json.Marshal(map[string]interface{}{
    "name":    "sleeper",
    "command": "sleep 10",
    "timeout": 1,
  })

  result, err := registry.Execute(context.Background(), "subagent_task", args)
  if err != nil {
    t.Fatal(err)
  }
  if result.Success {
    t.Errorf("expected timeout failure, got success: %s", result.Data)
  }
  if !strings.Contains(result.Data, "超时") {
    t.Errorf("expected '超时' in message, got: %s", result.Data)
  }
}

func TestSubAgentTool_ListAgents(t *testing.T) {
  mgr := NewSubAgentManager()
  registry := NewToolRegistry()
  tool := &SubAgentTool{Manager: mgr}
  registry.Register(tool)

  args, _ := json.Marshal(map[string]interface{}{
    "name":    "list-test",
    "command": "echo ok",
    "timeout": 5,
  })

  result, err := registry.Execute(context.Background(), "subagent_task", args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("expected success, got: %s", result.Data)
  }

  // Verify the agent is cleaned up after collect
  names := mgr.List()
  for _, n := range names {
    if n == "list-test" {
      t.Error("agent should be cleaned up after collect")
    }
  }
}

func TestSubAgent_TaskError(t *testing.T) {
  agent := NewSubAgent("failing-agent", func(ctx context.Context) (string, error) {
    return "", nil
  })

  ctx := context.Background()
  result := agent.Run(ctx)
  if result.Success {
    t.Errorf("expected failure for empty result, got success")
  }
  if !strings.Contains(result.Data, "空") && !strings.Contains(result.Data, "empty") {
    t.Errorf("expected empty result message, got: %s", result.Data)
  }
}
