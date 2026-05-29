package web

import (
  "bytes"
  "encoding/json"
  "net/http"
  "strings"
  "testing"

  "github.com/picoaide/picoaide/internal/auth"
)

// ============================================================
// 记忆进化审计端点集成测试
// ============================================================

func TestPicoAgentAudit_Success(t *testing.T) {
  env := setupTestServer(t)

  // 生成 MCP token
  token, err := auth.GenerateMCPToken("testuser")
  if err != nil {
    t.Fatalf("GenerateMCPToken: %v", err)
  }

  body := map[string]string{
    "username":        "testuser",
    "session_key":     "test-session-001",
    "changes_summary": "decisions=2,knowledge=1,preferences=0",
    "files_modified":  "MEMORY.md",
  }
  bodyJSON, _ := json.Marshal(body)

  req, err := http.NewRequest("POST", env.HTTP.URL+"/api/picoagent/audit", bytes.NewReader(bodyJSON))
  if err != nil {
    t.Fatalf("创建请求失败: %v", err)
  }
  req.Header.Set("Authorization", "Bearer "+token)
  req.Header.Set("Content-Type", "application/json")

  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    t.Fatalf("发送请求失败: %v", err)
  }
  assertStatus(t, resp, http.StatusOK)

  result := getJSON(t, resp)
  if result["status"] != "ok" {
    t.Errorf("status = %v, want ok", result["status"])
  }

  // 验证审计记录已写入 DB
  engine, err := auth.GetEngine()
  if err != nil {
    t.Fatal(err)
  }
  var logs []auth.MemoryEvolutionLog
  if err := engine.Where("session_key = ?", "test-session-001").Find(&logs); err != nil {
    t.Fatal(err)
  }
  if len(logs) == 0 {
    t.Fatal("memory_evolution_log 应包含审计记录")
  }
  if logs[0].Username != "testuser" {
    t.Errorf("username = %q, want testuser", logs[0].Username)
  }
  if logs[0].ChangesSummary != "decisions=2,knowledge=1,preferences=0" {
    t.Errorf("changes_summary = %q, want decisions=2,...", logs[0].ChangesSummary)
  }
  if logs[0].FilesModified != "MEMORY.md" {
    t.Errorf("files_modified = %q, want MEMORY.md", logs[0].FilesModified)
  }
}

func TestPicoAgentAudit_Unauthorized(t *testing.T) {
  env := setupTestServer(t)

  body := map[string]string{
    "username":        "testuser",
    "session_key":     "test-session-002",
    "changes_summary": "test",
    "files_modified":  "MEMORY.md",
  }
  bodyJSON, _ := json.Marshal(body)

  // 不传 Bearer token
  req, err := http.NewRequest("POST", env.HTTP.URL+"/api/picoagent/audit", bytes.NewReader(bodyJSON))
  if err != nil {
    t.Fatalf("创建请求失败: %v", err)
  }
  req.Header.Set("Content-Type", "application/json")

  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    t.Fatalf("发送请求失败: %v", err)
  }
  assertStatus(t, resp, http.StatusUnauthorized)
}

func TestPicoAgentAudit_TokenMismatch(t *testing.T) {
  env := setupTestServer(t)

  token, err := auth.GenerateMCPToken("testuser")
  if err != nil {
    t.Fatalf("GenerateMCPToken: %v", err)
  }

  // username 与 token 不匹配
  body := map[string]string{
    "username":        "otheruser",
    "session_key":     "test-session-003",
    "changes_summary": "test",
    "files_modified":  "MEMORY.md",
  }
  bodyJSON, _ := json.Marshal(body)

  req, err := http.NewRequest("POST", env.HTTP.URL+"/api/picoagent/audit", bytes.NewReader(bodyJSON))
  if err != nil {
    t.Fatalf("创建请求失败: %v", err)
  }
  req.Header.Set("Authorization", "Bearer "+token)
  req.Header.Set("Content-Type", "application/json")

  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    t.Fatalf("发送请求失败: %v", err)
  }
  assertStatus(t, resp, http.StatusForbidden)
}

func TestPicoAgentAudit_EmptyBody(t *testing.T) {
  env := setupTestServer(t)

  token, err := auth.GenerateMCPToken("testuser")
  if err != nil {
    t.Fatalf("GenerateMCPToken: %v", err)
  }

  req, err := http.NewRequest("POST", env.HTTP.URL+"/api/picoagent/audit", strings.NewReader("{}"))
  if err != nil {
    t.Fatalf("创建请求失败: %v", err)
  }
  req.Header.Set("Authorization", "Bearer "+token)
  req.Header.Set("Content-Type", "application/json")

  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    t.Fatalf("发送请求失败: %v", err)
  }
  // username 和 session_key 为空 → 400
  assertStatus(t, resp, http.StatusBadRequest)
}
