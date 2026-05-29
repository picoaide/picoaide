package agent

import (
  "context"
  "encoding/json"
  "os"
  "path/filepath"
  "strings"
  "testing"
)

func TestUpdateMemoryToolName(t *testing.T) {
  tool := &UpdateMemoryTool{Workspace: t.TempDir()}
  if tool.Name() != "update_memory" {
    t.Errorf("Name() = %q, want %q", tool.Name(), "update_memory")
  }
}

func TestUpdateMemoryToolSchema(t *testing.T) {
  tool := &UpdateMemoryTool{Workspace: t.TempDir()}
  schema := tool.Schema()
  if schema == nil {
    t.Fatal("Schema() should not be nil")
  }
  props, ok := schema["properties"].(map[string]interface{})
  if !ok {
    t.Fatal("Schema should have properties")
  }
  for _, key := range []string{"section", "action", "entries"} {
    if _, exists := props[key]; !exists {
      t.Errorf("Schema should have property %q", key)
    }
  }
}

func TestUpdateMemoryToolAddDecision(t *testing.T) {
  dir := t.TempDir()
  memoryDir := filepath.Join(dir, "memory")
  os.MkdirAll(memoryDir, 0755)

  // Create initial MEMORY.md
  initialContent := `# 长期记忆

## 关键决策
- 已有决策：旧内容
`
  os.WriteFile(filepath.Join(memoryDir, "MEMORY.md"), []byte(initialContent), 0644)

  tool := &UpdateMemoryTool{Workspace: dir}
  params := map[string]interface{}{
    "section": "decisions",
    "action":  "add",
    "entries": []map[string]interface{}{
      {"topic": "技术选型", "content": "使用 Go 1.22", "rationale": "更好的性能"},
    },
  }
  args, _ := json.Marshal(params)

  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("Execute failed: %s", result.Data)
  }

  // Verify file was updated
  content, _ := os.ReadFile(filepath.Join(memoryDir, "MEMORY.md"))
  if !strings.Contains(string(content), "技术选型") {
    t.Error("MEMORY.md should contain new decision")
  }
  if !strings.Contains(string(content), "已有决策") {
    t.Error("MEMORY.md should still contain existing decision")
  }
}

func TestUpdateMemoryToolAddPreference(t *testing.T) {
  dir := t.TempDir()

  initialContent := `# 用户信息

## 工作偏好
- 沟通风格：简洁
`
  os.WriteFile(filepath.Join(dir, "USER.md"), []byte(initialContent), 0644)

  tool := &UpdateMemoryTool{Workspace: dir}
  params := map[string]interface{}{
    "section": "preferences",
    "action":  "add",
    "entries": []map[string]interface{}{
      {"topic": "技术偏好", "content": "优先可维护性"},
    },
  }
  args, _ := json.Marshal(params)

  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("Execute failed: %s", result.Data)
  }

  content, _ := os.ReadFile(filepath.Join(dir, "USER.md"))
  if !strings.Contains(string(content), "技术偏好") {
    t.Error("USER.md should contain new preference")
  }
}

func TestUpdateMemoryToolUpdateDecision(t *testing.T) {
  dir := t.TempDir()
  memoryDir := filepath.Join(dir, "memory")
  os.MkdirAll(memoryDir, 0755)

  initialContent := `# 长期记忆

## 关键决策
- 技术选型：旧版本
`
  os.WriteFile(filepath.Join(memoryDir, "MEMORY.md"), []byte(initialContent), 0644)

  tool := &UpdateMemoryTool{Workspace: dir}
  params := map[string]interface{}{
    "section": "decisions",
    "action":  "update",
    "entries": []map[string]interface{}{
      {"topic": "技术选型", "content": "Go 1.22", "rationale": "更好的性能"},
    },
  }
  args, _ := json.Marshal(params)

  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("Execute failed: %s", result.Data)
  }

  content, _ := os.ReadFile(filepath.Join(memoryDir, "MEMORY.md"))
  if !strings.Contains(string(content), "Go 1.22") {
    t.Error("MEMORY.md should contain updated decision content")
  }
  if strings.Contains(string(content), "旧版本") {
    t.Error("MEMORY.md should NOT contain old decision content")
  }
}

func TestUpdateMemoryToolDeleteDecision(t *testing.T) {
  dir := t.TempDir()
  memoryDir := filepath.Join(dir, "memory")
  os.MkdirAll(memoryDir, 0755)

  initialContent := `# 长期记忆

## 关键决策
- 技术选型：Go 1.22
- 架构设计：微服务
`
  os.WriteFile(filepath.Join(memoryDir, "MEMORY.md"), []byte(initialContent), 0644)

  tool := &UpdateMemoryTool{Workspace: dir}
  params := map[string]interface{}{
    "section": "decisions",
    "action":  "delete",
    "entries": []map[string]interface{}{
      {"topic": "技术选型"},
    },
  }
  args, _ := json.Marshal(params)

  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("Execute failed: %s", result.Data)
  }

  content, _ := os.ReadFile(filepath.Join(memoryDir, "MEMORY.md"))
  if strings.Contains(string(content), "技术选型") {
    t.Error("MEMORY.md should NOT contain deleted decision")
  }
  if !strings.Contains(string(content), "架构设计") {
    t.Error("MEMORY.md should still contain other decisions")
  }
}

func TestUpdateMemoryToolInvalidSection(t *testing.T) {
  tool := &UpdateMemoryTool{Workspace: t.TempDir()}
  params := map[string]interface{}{
    "section": "invalid_section",
    "action":  "add",
    "entries": []map[string]interface{}{
      {"topic": "test", "content": "test"},
    },
  }
  args, _ := json.Marshal(params)

  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if result.Success {
    t.Fatal("should fail with invalid section")
  }
}

func TestUpdateMemoryToolCreatesBackup(t *testing.T) {
  dir := t.TempDir()
  memoryDir := filepath.Join(dir, "memory")
  os.MkdirAll(memoryDir, 0755)
  os.WriteFile(filepath.Join(memoryDir, "MEMORY.md"), []byte("# 长期记忆\n\n## 关键决策\n- 旧决策\n"), 0644)

  tool := &UpdateMemoryTool{Workspace: dir}
  params := map[string]interface{}{
    "section": "decisions",
    "action":  "add",
    "entries": []map[string]interface{}{
      {"topic": "新决策", "content": "新内容", "rationale": ""},
    },
  }
  args, _ := json.Marshal(params)

  _, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }

  // Check backup was created
  archiveDir := filepath.Join(memoryDir, "archive")
  entries, _ := os.ReadDir(archiveDir)
  if len(entries) == 0 {
    t.Error("backup should have been created")
  }
}

func TestUpdateMemoryToolInvalidAction(t *testing.T) {
  tool := &UpdateMemoryTool{Workspace: t.TempDir()}
  params := map[string]interface{}{
    "section": "decisions",
    "action":  "invalid",
    "entries": []map[string]interface{}{
      {"topic": "test", "content": "test"},
    },
  }
  args, _ := json.Marshal(params)
  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if result.Success {
    t.Fatal("should fail with invalid action")
  }
}

func TestUpdateMemoryToolEmptyEntries(t *testing.T) {
  tool := &UpdateMemoryTool{Workspace: t.TempDir()}
  params := map[string]interface{}{
    "section": "decisions",
    "action":  "add",
    "entries": []map[string]interface{}{},
  }
  args, _ := json.Marshal(params)
  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if result.Success {
    t.Fatal("should fail with empty entries")
  }
}

func TestUpdateMemoryToolAddKnowledge(t *testing.T) {
  dir := t.TempDir()
  memoryDir := filepath.Join(dir, "memory")
  os.MkdirAll(memoryDir, 0755)
  os.WriteFile(filepath.Join(memoryDir, "MEMORY.md"), []byte("# 长期记忆\n\n## 项目知识\n- 已有知识：旧\n"), 0644)

  tool := &UpdateMemoryTool{Workspace: dir}
  params := map[string]interface{}{
    "section": "knowledge",
    "action":  "add",
    "entries": []map[string]interface{}{
      {"topic": "新知识", "content": "新事实"},
    },
  }
  args, _ := json.Marshal(params)
  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("Execute failed: %s", result.Data)
  }

  content, _ := os.ReadFile(filepath.Join(memoryDir, "MEMORY.md"))
  if !strings.Contains(string(content), "新事实") {
    t.Error("MEMORY.md should contain new knowledge")
  }
}

func TestUpdateMemoryToolAddProgress(t *testing.T) {
  dir := t.TempDir()
  memoryDir := filepath.Join(dir, "memory")
  os.MkdirAll(memoryDir, 0755)
  os.WriteFile(filepath.Join(memoryDir, "MEMORY.md"), []byte("# 长期记忆\n"), 0644)

  tool := &UpdateMemoryTool{Workspace: dir}
  params := map[string]interface{}{
    "section": "progress",
    "action":  "add",
    "entries": []map[string]interface{}{
      {"topic": "进行中", "content": "正在开发用户模块"},
    },
  }
  args, _ := json.Marshal(params)
  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("Execute failed: %s", result.Data)
  }

  content, _ := os.ReadFile(filepath.Join(memoryDir, "MEMORY.md"))
  if !strings.Contains(string(content), "正在开发用户模块") {
    t.Error("MEMORY.md should contain progress item")
  }
}

func TestUpdateMemoryToolUpdatePreference(t *testing.T) {
  dir := t.TempDir()
  os.WriteFile(filepath.Join(dir, "USER.md"), []byte("# 用户信息\n\n## 工作偏好\n- 沟通风格：详细\n"), 0644)

  tool := &UpdateMemoryTool{Workspace: dir}
  params := map[string]interface{}{
    "section": "preferences",
    "action":  "update",
    "entries": []map[string]interface{}{
      {"topic": "沟通风格", "content": "简洁直接"},
    },
  }
  args, _ := json.Marshal(params)
  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("Execute failed: %s", result.Data)
  }

  content, _ := os.ReadFile(filepath.Join(dir, "USER.md"))
  if !strings.Contains(string(content), "简洁直接") {
    t.Error("USER.md should contain updated preference")
  }
  if strings.Contains(string(content), "详细") {
    t.Error("USER.md should NOT contain old preference")
  }
}

func TestUpdateMemoryToolDeletePreference(t *testing.T) {
  dir := t.TempDir()
  os.WriteFile(filepath.Join(dir, "USER.md"), []byte("# 用户信息\n\n## 工作偏好\n- 沟通风格：简洁\n- 技术偏好：Go\n"), 0644)

  tool := &UpdateMemoryTool{Workspace: dir}
  params := map[string]interface{}{
    "section": "preferences",
    "action":  "delete",
    "entries": []map[string]interface{}{
      {"topic": "沟通风格"},
    },
  }
  args, _ := json.Marshal(params)
  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("Execute failed: %s", result.Data)
  }

  content, _ := os.ReadFile(filepath.Join(dir, "USER.md"))
  if strings.Contains(string(content), "沟通风格") {
    t.Error("USER.md should NOT contain deleted preference")
  }
  if !strings.Contains(string(content), "技术偏好") {
    t.Error("USER.md should still contain other preferences")
  }
}

func TestUpdateMemoryToolUpdateNonexistentTopic(t *testing.T) {
  dir := t.TempDir()
  memoryDir := filepath.Join(dir, "memory")
  os.MkdirAll(memoryDir, 0755)
  os.WriteFile(filepath.Join(memoryDir, "MEMORY.md"), []byte("# 长期记忆\n\n## 关键决策\n- 已有决策：旧\n"), 0644)

  tool := &UpdateMemoryTool{Workspace: dir}
  params := map[string]interface{}{
    "section": "decisions",
    "action":  "update",
    "entries": []map[string]interface{}{
      {"topic": "不存在的主题", "content": "新值"},
    },
  }
  args, _ := json.Marshal(params)
  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("should succeed silently: %s", result.Data)
  }

  content, _ := os.ReadFile(filepath.Join(memoryDir, "MEMORY.md"))
  if strings.Contains(string(content), "不存在的主题") {
    t.Error("should NOT add nonexistent topic as new entry via update")
  }
}

func TestUpdateMemoryToolDeleteEmptyFile(t *testing.T) {
  dir := t.TempDir()
  tool := &UpdateMemoryTool{Workspace: dir}
  params := map[string]interface{}{
    "section": "preferences",
    "action":  "delete",
    "entries": []map[string]interface{}{
      {"topic": "something"},
    },
  }
  args, _ := json.Marshal(params)
  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("should handle empty file gracefully: %s", result.Data)
  }

  // Verify USER.md was created with default header
  content, _ := os.ReadFile(filepath.Join(dir, "USER.md"))
  if !strings.Contains(string(content), "# 用户信息") {
    t.Error("should create USER.md with default header")
  }
}
