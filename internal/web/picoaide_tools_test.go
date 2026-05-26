package web

import (
  "encoding/json"
  "net/http/httptest"
  "testing"

  "github.com/gin-gonic/gin"
)

// ============================================================
// ToolDef 数量与名称检查
// ============================================================

func TestPicoaideToolDefs_Count(t *testing.T) {
  if got := len(picoaideToolDefs); got != 6 {
    t.Errorf("picoaideToolDefs len = %d, want 6", got)
  }
}

func TestPicoaideToolDefs_Names(t *testing.T) {
  expected := []string{
    "picoaide_user_info",
    "picoaide_skills_list",
    "picoaide_shared_folders",
    "picoaide_cron_create",
    "picoaide_cron_list",
    "picoaide_cron_delete",
  }
  for i, name := range expected {
    if picoaideToolDefs[i].Name != name {
      t.Errorf("picoaideToolDefs[%d].Name = %q, want %q", i, picoaideToolDefs[i].Name, name)
    }
  }
}

// ============================================================
// Handler map 检查
// ============================================================

func TestPicoaideHandlersMap_Count(t *testing.T) {
  if got := len(picoaideHandlers); got != 6 {
    t.Errorf("picoaideHandlers len = %d, want 6", got)
  }
}

func TestPicoaideHandlersMap_Keys(t *testing.T) {
  expected := []string{
    "picoaide_user_info",
    "picoaide_skills_list",
    "picoaide_shared_folders",
    "picoaide_cron_create",
    "picoaide_cron_list",
    "picoaide_cron_delete",
  }
  for _, k := range expected {
    if _, ok := picoaideHandlers[k]; !ok {
      t.Errorf("picoaideHandlers missing key %q", k)
    }
  }
}

// ============================================================
// Handler dispatch 测试（不依赖数据库）
// ============================================================

func TestPicoaideTools_UnknownTool(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  w := httptest.NewRecorder()
  ctx, _ := gin.CreateTestContext(w)
  picoaideHandleMCPToolCall(s, ctx, json.Number("1"), "nonexistent_tool", nil, "testadmin")

  var resp map[string]interface{}
  if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
    t.Fatalf("JSON 解码失败: %v", err)
  }
  if resp["jsonrpc"] != "2.0" {
    t.Errorf("jsonrpc = %v, want 2.0", resp["jsonrpc"])
  }
  content, ok := resp["result"].(map[string]interface{})
  if !ok {
    t.Fatalf("result 不是 map: %v", resp["result"])
  }
  if content["isError"] != true {
    t.Errorf("isError = %v, want true", content["isError"])
  }
}

// ============================================================
// agent 服务注册测试
// ============================================================

func TestAgentServiceRegistered(t *testing.T) {
  info, ok := serviceRegistry["agent"]
  if !ok {
    t.Fatal("agent 服务未注册到 serviceRegistry")
  }
  if info.Hub != nil {
    t.Errorf("agent service Hub 应为 nil")
  }
  if info.ServerName != "picoaide-agent" {
    t.Errorf("ServerName = %q, want picoaide-agent", info.ServerName)
  }
  if info.Version != "1.0.0" {
    t.Errorf("Version = %q, want 1.0.0", info.Version)
  }
  if len(info.Tools) != 6 {
    t.Errorf("Tools len = %d, want 6", len(info.Tools))
  }
}
