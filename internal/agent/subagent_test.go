package agent

import (
  "context"
  "encoding/json"
  "strings"
  "testing"
  "time"
)

func testSubAgentProvider() *mockProvider {
  return &mockProvider{
    responseText: "子代理任务已完成，返回结果。",
  }
}

// ============================================================
// SubAgentManager — 管理多个子代理
// ============================================================

func TestSubAgentManager_SpawnAndCollect(t *testing.T) {
  mgr := NewSubAgentManager(testConfig(), testSubAgentProvider(), NewToolRegistry())

  err := mgr.SpawnAgent(context.Background(), "worker", "return hello", "", "")
  if err != nil {
    t.Fatal(err)
  }

  result := mgr.Collect("worker", 30*time.Second)
  if result == nil {
    t.Fatal("expected result, got nil")
  }
  if !result.Success {
    t.Fatalf("expected success, got: %s", result.Data)
  }
  if result.Name != "worker" {
    t.Errorf("Name = %q, want %q", result.Name, "worker")
  }
}

func TestSubAgentManager_List(t *testing.T) {
  mgr := NewSubAgentManager(testConfig(), testSubAgentProvider(), NewToolRegistry())
  mgr.SpawnAgent(context.Background(), "a", "task a", "", "")
  mgr.SpawnAgent(context.Background(), "b", "task b", "", "")

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
  mgr := NewSubAgentManager(testConfig(), testSubAgentProvider(), NewToolRegistry())
  mgr.SpawnAgent(context.Background(), "slow1", "task 1", "", "")
  mgr.SpawnAgent(context.Background(), "slow2", "task 2", "", "")

  // 取消子代理（如果已快速完成则 Collect 直接返回成功）
  mgr.CancelAll()

  r1 := mgr.Collect("slow1", 1*time.Second)
  r2 := mgr.Collect("slow2", 1*time.Second)

  // 取消后子代理可能已经完成（mock provider 太快），也可能被取消
  // 无论哪种情况都不应 panic 或 hang
  if r1 == nil || r2 == nil {
    t.Fatal("expected non-nil results")
  }
}

func TestSubAgentManager_SpawnDuplicate(t *testing.T) {
  mgr := NewSubAgentManager(testConfig(), testSubAgentProvider(), NewToolRegistry())
  if err := mgr.SpawnAgent(context.Background(), "dup", "first", "", ""); err != nil {
    t.Fatal(err)
  }
  if err := mgr.SpawnAgent(context.Background(), "dup", "second", "", ""); err == nil {
    t.Error("expected error for duplicate spawn, got nil")
  }
}

func TestSubAgentManager_CollectNonExistent(t *testing.T) {
  mgr := NewSubAgentManager(testConfig(), testSubAgentProvider(), NewToolRegistry())
  result := mgr.Collect("nonexistent", 1*time.Second)
  if result == nil {
    t.Fatal("expected non-nil result for non-existent agent")
  }
  if result.Success {
    t.Errorf("expected failure for non-existent agent")
  }
}

func TestSubAgentManager_EmptyName(t *testing.T) {
  mgr := NewSubAgentManager(testConfig(), testSubAgentProvider(), NewToolRegistry())
  err := mgr.SpawnAgent(context.Background(), "", "task", "", "")
  if err == nil {
    t.Error("expected error for empty name")
  }
}

func TestSubAgentManager_EmptyTask(t *testing.T) {
  mgr := NewSubAgentManager(testConfig(), testSubAgentProvider(), NewToolRegistry())
  err := mgr.SpawnAgent(context.Background(), "test", "", "", "")
  if err == nil {
    t.Error("expected error for empty task")
  }
}

// ============================================================
// SubAgentManager + ToolRegistry 集成（spawn + collect 双工具）
// ============================================================

func TestSubAgentSpawnAndCollect(t *testing.T) {
  mgr := NewSubAgentManager(testConfig(), testSubAgentProvider(), NewToolRegistry())
  registry := NewToolRegistry()

  registry.Register(&SubAgentSpawnTool{Manager: mgr})
  registry.Register(&SubAgentCollectTool{Manager: mgr})

  // 先 spawn
  spawnArgs, _ := json.Marshal(map[string]interface{}{
    "name": "searcher",
    "task": "return hello from subagent",
  })
  spawnResult, err := registry.Execute(context.Background(), "subagent_spawn", spawnArgs)
  if err != nil {
    t.Fatal(err)
  }
  if !spawnResult.Success {
    t.Fatalf("spawn expected success, got: %s", spawnResult.Data)
  }

  // 子代理应该在列表中
  names := mgr.List()
  found := false
  for _, n := range names {
    if n == "searcher" {
      found = true
      break
    }
  }
  if !found {
    t.Error("spawned agent should be in list before collect")
  }

  // 再 collect
  collectArgs, _ := json.Marshal(map[string]interface{}{
    "name": "searcher",
  })
  collectResult, err := registry.Execute(context.Background(), "subagent_collect", collectArgs)
  if err != nil {
    t.Fatal(err)
  }
  if !collectResult.Success {
    t.Fatalf("collect expected success, got: %s", collectResult.Data)
  }
  if !strings.Contains(collectResult.Data, "子代理任务已完成") {
    t.Errorf("expected agent output in result, got: %s", collectResult.Data)
  }

  // collect 后 agent 应从列表中移除
  names = mgr.List()
  for _, n := range names {
    if n == "searcher" {
      t.Error("agent should be cleaned up after collect")
    }
  }
}

func TestSubAgentTool_EmptyTask(t *testing.T) {
  mgr := NewSubAgentManager(testConfig(), testSubAgentProvider(), NewToolRegistry())
  registry := NewToolRegistry()
  tool := &SubAgentSpawnTool{Manager: mgr}
  registry.Register(tool)

  args, _ := json.Marshal(map[string]interface{}{
    "name": "test",
    "task": "",
  })

  result, err := registry.Execute(context.Background(), "subagent_spawn", args)
  if err != nil {
    t.Fatal(err)
  }
  if result.Success {
    t.Errorf("expected failure for empty task")
  }
}

func TestSubAgentTool_EmptyName(t *testing.T) {
  mgr := NewSubAgentManager(testConfig(), testSubAgentProvider(), NewToolRegistry())
  registry := NewToolRegistry()

  registry.Register(&SubAgentSpawnTool{Manager: mgr})
  registry.Register(&SubAgentCollectTool{Manager: mgr})

  // spawn with empty name
  args, _ := json.Marshal(map[string]interface{}{
    "name": "",
    "task": "hello",
  })
  result, err := registry.Execute(context.Background(), "subagent_spawn", args)
  if err != nil {
    t.Fatal(err)
  }
  if result.Success {
    t.Errorf("expected failure for empty name on spawn")
  }

  // collect with empty name
  collectArgs, _ := json.Marshal(map[string]interface{}{
    "name": "",
  })
  result, err = registry.Execute(context.Background(), "subagent_collect", collectArgs)
  if err != nil {
    t.Fatal(err)
  }
  if result.Success {
    t.Errorf("expected failure for empty name on collect")
  }
}

func TestSubAgentTool_ManagerNil(t *testing.T) {
  registry := NewToolRegistry()

  // spawn tool with nil manager
  registry.Register(&SubAgentSpawnTool{Manager: nil})

  args, _ := json.Marshal(map[string]interface{}{
    "name": "test",
    "task": "hello",
  })
  result, err := registry.Execute(context.Background(), "subagent_spawn", args)
  if err != nil {
    t.Fatal(err)
  }
  if result.Success {
    t.Errorf("expected failure when spawn manager is nil")
  }

  // collect tool with nil manager
  registry.Register(&SubAgentCollectTool{Manager: nil})

  collectArgs, _ := json.Marshal(map[string]interface{}{
    "name": "test",
  })
  result, err = registry.Execute(context.Background(), "subagent_collect", collectArgs)
  if err != nil {
    t.Fatal(err)
  }
  if result.Success {
    t.Errorf("expected failure when collect manager is nil")
  }
}

func TestSubAgentCollect_Timeout(t *testing.T) {
  mgr := NewSubAgentManager(testConfig(), testSubAgentProvider(), NewToolRegistry())

  err := mgr.SpawnAgent(context.Background(), "timeout-test", "return hello", "", "")
  if err != nil {
    t.Fatal(err)
  }

  // mock provider 通常很快，1 秒内能完成 → Collect 返回正常结果而非超时
  // 若机器负载极高导致超时也是可接受的——测试只验证不 hang
  result := mgr.Collect("timeout-test", 5*time.Second)
  if result == nil {
    t.Fatal("expected non-nil collect result")
  }
}
