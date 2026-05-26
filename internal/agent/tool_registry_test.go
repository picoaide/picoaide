package agent

import (
  "context"
  "encoding/json"
  "os"
  "path/filepath"
  "strings"
  "testing"
)

// ============================================================
// WriteFileTool
// ============================================================

func TestWriteFileTool_Create(t *testing.T) {
  dir := t.TempDir()
  path := filepath.Join(dir, "test.txt")
  tool := &WriteFileTool{}

  args, _ := json.Marshal(map[string]interface{}{
    "path":    path,
    "content": "hello world",
  })

  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("expected success, got: %s", result.Data)
  }

  data, _ := os.ReadFile(path)
  if string(data) != "hello world" {
    t.Errorf("content = %q, want %q", string(data), "hello world")
  }
}

func TestWriteFileTool_OverwriteDefault(t *testing.T) {
  dir := t.TempDir()
  path := filepath.Join(dir, "existing.txt")
  os.WriteFile(path, []byte("original"), 0644)

  tool := &WriteFileTool{}
  args, _ := json.Marshal(map[string]interface{}{
    "path":    path,
    "content": "new content",
  })

  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if result.Success {
    t.Fatalf("expected failure, got success: %s", result.Data)
  }
  if !strings.Contains(result.Data, "已存在") {
    t.Errorf("expected '已存在' message, got: %s", result.Data)
  }

  data, _ := os.ReadFile(path)
  if string(data) != "original" {
    t.Errorf("file was modified: %q", string(data))
  }
}

func TestWriteFileTool_OverwriteExplicit(t *testing.T) {
  dir := t.TempDir()
  path := filepath.Join(dir, "existing.txt")
  os.WriteFile(path, []byte("original"), 0644)

  tool := &WriteFileTool{}
  args, _ := json.Marshal(map[string]interface{}{
    "path":      path,
    "content":   "new content",
    "overwrite": true,
  })

  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("expected success, got: %s", result.Data)
  }

  data, _ := os.ReadFile(path)
  if string(data) != "new content" {
    t.Errorf("content = %q, want %q", string(data), "new content")
  }
}

func TestWriteFileTool_EmptyPath(t *testing.T) {
  tool := &WriteFileTool{}
  args, _ := json.Marshal(map[string]interface{}{
    "path":    "",
    "content": "test",
  })

  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if result.Success {
    t.Errorf("expected failure for empty path")
  }
}

// ============================================================
// EditFileTool
// ============================================================

func TestEditFileTool_Replace(t *testing.T) {
  dir := t.TempDir()
  path := filepath.Join(dir, "test.txt")
  os.WriteFile(path, []byte("hello world foo"), 0644)

  tool := &EditFileTool{}
  args, _ := json.Marshal(map[string]interface{}{
    "path":     path,
    "old_text": "world",
    "new_text": "there",
  })

  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("expected success, got: %s", result.Data)
  }

  data, _ := os.ReadFile(path)
  if string(data) != "hello there foo" {
    t.Errorf("content = %q, want %q", string(data), "hello there foo")
  }
}

func TestEditFileTool_NotFound(t *testing.T) {
  dir := t.TempDir()
  path := filepath.Join(dir, "test.txt")
  os.WriteFile(path, []byte("hello world"), 0644)

  tool := &EditFileTool{}
  args, _ := json.Marshal(map[string]interface{}{
    "path":     path,
    "old_text": "nonexistent",
    "new_text": "replacement",
  })

  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if result.Success {
    t.Fatalf("expected failure, got success: %s", result.Data)
  }
  if !strings.Contains(result.Data, "未找到") {
    t.Errorf("expected '未找到' message, got: %s", result.Data)
  }
}

func TestEditFileTool_Ambiguous(t *testing.T) {
  dir := t.TempDir()
  path := filepath.Join(dir, "test.txt")
  os.WriteFile(path, []byte("foo foo foo"), 0644)

  tool := &EditFileTool{}
  args, _ := json.Marshal(map[string]interface{}{
    "path":     path,
    "old_text": "foo",
    "new_text": "bar",
  })

  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if result.Success {
    t.Fatalf("expected failure, got success: %s", result.Data)
  }
  if !strings.Contains(result.Data, "个匹配") {
    t.Errorf("expected '个匹配' message, got: %s", result.Data)
  }
}

func TestEditFileTool_FileNotExist(t *testing.T) {
  tool := &EditFileTool{}
  args, _ := json.Marshal(map[string]interface{}{
    "path":     "/nonexistent/path.txt",
    "old_text": "foo",
    "new_text": "bar",
  })

  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !strings.Contains(result.Data, "读取失败") {
    t.Errorf("expected '读取失败' in message, got: %s", result.Data)
  }
}

// ============================================================
// AppendFileTool
// ============================================================

func TestAppendFileTool_Append(t *testing.T) {
  dir := t.TempDir()
  path := filepath.Join(dir, "test.txt")
  os.WriteFile(path, []byte("hello"), 0644)

  tool := &AppendFileTool{}
  args, _ := json.Marshal(map[string]interface{}{
    "path":    path,
    "content": " world",
  })

  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("expected success, got: %s", result.Data)
  }

  data, _ := os.ReadFile(path)
  if string(data) != "hello world" {
    t.Errorf("content = %q, want %q", string(data), "hello world")
  }
}

func TestAppendFileTool_CreateNew(t *testing.T) {
  dir := t.TempDir()
  path := filepath.Join(dir, "new.txt")

  tool := &AppendFileTool{}
  args, _ := json.Marshal(map[string]interface{}{
    "path":    path,
    "content": "fresh",
  })

  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("expected success, got: %s", result.Data)
  }

  data, _ := os.ReadFile(path)
  if string(data) != "fresh" {
    t.Errorf("content = %q, want %q", string(data), "fresh")
  }
}

// ============================================================
// ListDirTool
// ============================================================

func TestListDirTool_Basic(t *testing.T) {
  dir := t.TempDir()
  os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644)
  os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bb"), 0644)
  os.MkdirAll(filepath.Join(dir, "sub"), 0755)

  tool := &ListDirTool{}
  args, _ := json.Marshal(map[string]interface{}{
    "path": dir,
  })

  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("expected success, got: %s", result.Data)
  }

  if !strings.Contains(result.Data, "FILE: a.txt") {
    t.Errorf("missing a.txt in: %s", result.Data)
  }
  if !strings.Contains(result.Data, "FILE: b.txt (2") {
    t.Errorf("missing or wrong size for b.txt in: %s", result.Data)
  }
  if !strings.Contains(result.Data, "DIR: sub") {
    t.Errorf("missing sub dir in: %s", result.Data)
  }
}

func TestListDirTool_DefaultPath(t *testing.T) {
  tool := &ListDirTool{}
  args, _ := json.Marshal(map[string]interface{}{})

  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  // Should not crash regardless of whether /workspace exists
  if result.Success && result.Data == "" {
    t.Errorf("expected output for default path")
  }
}

func TestListDirTool_NotExist(t *testing.T) {
  tool := &ListDirTool{}
  args, _ := json.Marshal(map[string]interface{}{
    "path": "/nonexistent_dir_12345",
  })

  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if result.Success {
    t.Fatalf("expected failure, got success: %s", result.Data)
  }
  if !strings.Contains(result.Data, "失败") {
    t.Errorf("expected '失败' message, got: %s", result.Data)
  }
}

// ============================================================
// GlobTool
// ============================================================

func TestGlobTool_Basic(t *testing.T) {
  dir := t.TempDir()
  os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644)
  os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0644)
  os.WriteFile(filepath.Join(dir, "c.go"), []byte("c"), 0644)

  tool := &GlobTool{}
  args, _ := json.Marshal(map[string]interface{}{
    "pattern": filepath.Join(dir, "*.txt"),
  })

  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("expected success, got: %s", result.Data)
  }

  lines := strings.Split(strings.TrimSpace(result.Data), "\n")
  if len(lines) != 2 {
    t.Fatalf("expected 2 matches, got %d: %s", len(lines), result.Data)
  }
}

func TestGlobTool_NoMatches(t *testing.T) {
  dir := t.TempDir()

  tool := &GlobTool{}
  args, _ := json.Marshal(map[string]interface{}{
    "pattern": filepath.Join(dir, "*.xyz"),
  })

  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("expected success, got: %s", result.Data)
  }
  if !strings.Contains(result.Data, "无匹配") {
    t.Errorf("expected '无匹配' message, got: %s", result.Data)
  }
}

// ============================================================
// DeleteFileTool
// ============================================================

func TestDeleteFileTool_DeleteFile(t *testing.T) {
  dir := t.TempDir()
  path := filepath.Join(dir, "test.txt")
  os.WriteFile(path, []byte("hello"), 0644)

  tool := &DeleteFileTool{}
  args, _ := json.Marshal(map[string]interface{}{
    "path": path,
  })

  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("expected success, got: %s", result.Data)
  }

  if _, err := os.Stat(path); !os.IsNotExist(err) {
    t.Errorf("file still exists")
  }
}

func TestDeleteFileTool_DeleteDir(t *testing.T) {
  dir := t.TempDir()
  subdir := filepath.Join(dir, "subdir")
  os.MkdirAll(subdir, 0755)
  os.WriteFile(filepath.Join(subdir, "nested.txt"), []byte("nested"), 0644)

  tool := &DeleteFileTool{}
  args, _ := json.Marshal(map[string]interface{}{
    "path": subdir,
  })

  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("expected success, got: %s", result.Data)
  }

  if _, err := os.Stat(subdir); !os.IsNotExist(err) {
    t.Errorf("directory still exists")
  }
}

func TestDeleteFileTool_NotExist(t *testing.T) {
  tool := &DeleteFileTool{}
  args, _ := json.Marshal(map[string]interface{}{
    "path": "/nonexistent_file_xyz",
  })

  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("expected success even for non-existent, got: %s", result.Data)
  }
}

func TestDeleteFileTool_EmptyPath(t *testing.T) {
  tool := &DeleteFileTool{}
  args, _ := json.Marshal(map[string]interface{}{
    "path": "",
  })

  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if result.Success {
    t.Errorf("expected failure for empty path")
  }
}
