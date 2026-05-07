package util

import (
  "os"
  "path/filepath"
  "testing"
)

func TestFormatSize(t *testing.T) {
  tests := []struct {
    input    int64
    expected string
  }{
    {0, "0 B"},
    {512, "512 B"},
    {1023, "1023 B"},
    {1024, "1.0 KB"},
    {1536, "1.5 KB"},
    {1048576, "1.0 MB"},
    {1572864, "1.5 MB"},
    {1073741824, "1024.0 MB"},
  }
  for _, tt := range tests {
    got := FormatSize(tt.input)
    if got != tt.expected {
      t.Errorf("FormatSize(%d) = %q, want %q", tt.input, got, tt.expected)
    }
  }
}

func TestIsTextFile(t *testing.T) {
  textFiles := []string{
    "readme.md", "config.yaml", "data.json", "main.go",
    "script.sh", "style.css", "page.html", "query.sql",
    "app.py", "index.js", "types.ts", "config.toml",
    "data.csv", "notes.txt", "setup.cfg", ".env",
    ".gitignore", // 无扩展名但常见
  }
  for _, f := range textFiles {
    if !IsTextFile(f) {
      t.Errorf("IsTextFile(%q) = false, want true", f)
    }
  }

  binaryFiles := []string{
    "image.png", "data.bin", "archive.zip", "video.mp4",
    "photo.jpg", "font.ttf", "executable.exe",
  }
  for _, f := range binaryFiles {
    if IsTextFile(f) {
      t.Errorf("IsTextFile(%q) = true, want false", f)
    }
  }
}

func TestSafePathSegment(t *testing.T) {
  validNames := []string{
    "hello", "my-file", "my_file", "file.txt",
    "user123", "HelloWorld", "中文", "用户名",
    "a", "A", "0",
  }
  for _, name := range validNames {
    if err := SafePathSegment(name); err != nil {
      t.Errorf("SafePathSegment(%q) = %v, want nil", name, err)
    }
  }

  invalidNames := []struct {
    name string
    msg  string
  }{
    {"", "空字符串"},
    {".", "单点"},
    {"..", "双点"},
    {"a/b", "含斜杠"},
    {"a\\b", "含反斜杠"},
    {"a..b", "含连续点"},
    {"../etc/passwd", "路径遍历"},
  }
  for _, tt := range invalidNames {
    if err := SafePathSegment(tt.name); err == nil {
      t.Errorf("SafePathSegment(%q) = nil, want error (%s)", tt.name, tt.msg)
    }
  }
}

func TestSafeRelPath(t *testing.T) {
  baseDir := t.TempDir()

  // 创建子目录和文件
  os.MkdirAll(filepath.Join(baseDir, "sub"), 0755)
  os.WriteFile(filepath.Join(baseDir, "sub", "file.txt"), []byte("ok"), 0644)

  validPaths := []struct {
    relPath string
    want    string
  }{
    {"sub", filepath.Join(baseDir, "sub")},
    {"sub/file.txt", filepath.Join(baseDir, "sub", "file.txt")},
    {"", baseDir},
    {".", baseDir},
  }
  for _, tt := range validPaths {
    got, err := SafeRelPath(baseDir, tt.relPath)
    if err != nil {
      t.Errorf("SafeRelPath(%q) error: %v", tt.relPath, err)
      continue
    }
    if got != tt.want {
      t.Errorf("SafeRelPath(%q) = %q, want %q", tt.relPath, got, tt.want)
    }
  }

  // 不存在的路径：先创建父目录才能验证
  os.MkdirAll(filepath.Join(baseDir, "newdir"), 0755)
  absPath, err := SafeRelPath(baseDir, "newdir/file.txt")
  if err != nil {
    t.Errorf("SafeRelPath(newdir/file.txt) error: %v", err)
  }
  expected := filepath.Join(baseDir, "newdir", "file.txt")
  if absPath != expected {
    t.Errorf("SafeRelPath(newdir/file.txt) = %q, want %q", absPath, expected)
  }
}

func TestParseFlags(t *testing.T) {
  tests := []struct {
    args         []string
    wantFlags    map[string]string
    wantPositional []string
  }{
    {
      args:         []string{"serve", "-listen", ":80"},
      wantFlags:    map[string]string{"-listen": ":80"},
      wantPositional: []string{"serve"},
    },
    {
      // "-dev" 后跟 "serve"（不以 - 开头），所以 "serve" 被当作 -dev 的值
      args:         []string{"-dev", "serve"},
      wantFlags:    map[string]string{"-dev": "serve"},
      wantPositional: []string{},
    },
    {
      args:         []string{"help"},
      wantFlags:    map[string]string{},
      wantPositional: []string{"help"},
    },
    {
      args:         []string{},
      wantFlags:    map[string]string{},
    },
    {
      args:         []string{"-a", "1", "-b", "2", "cmd"},
      wantFlags:    map[string]string{"-a": "1", "-b": "2"},
      wantPositional: []string{"cmd"},
    },
  }

  for _, tt := range tests {
    flags, positional := ParseFlags(tt.args)
    for k, v := range tt.wantFlags {
      if flags[k] != v {
        t.Errorf("ParseFlags(%v) flags[%q] = %q, want %q", tt.args, k, flags[k], v)
      }
    }
    for k := range flags {
      if _, ok := tt.wantFlags[k]; !ok {
        t.Errorf("ParseFlags(%v) unexpected flag %q=%q", tt.args, k, flags[k])
      }
    }
    if len(positional) != len(tt.wantPositional) {
      t.Errorf("ParseFlags(%v) positional = %v, want %v", tt.args, positional, tt.wantPositional)
    }
  }
}

func TestDeepCopyMap(t *testing.T) {
  original := map[string]interface{}{
    "name": "test",
    "nested": map[string]interface{}{
      "key": "value",
      "list": []interface{}{1, 2, 3},
    },
  }

  copied := DeepCopyMap(original)

  // 修改副本不影响原始
  copied["name"] = "changed"
  copied["nested"].(map[string]interface{})["key"] = "changed"

  if original["name"] != "test" {
    t.Error("DeepCopyMap: modifying copy affected original (top-level)")
  }
  if original["nested"].(map[string]interface{})["key"] != "value" {
    t.Error("DeepCopyMap: modifying copy affected original (nested)")
  }
}

func TestMergeMap(t *testing.T) {
  base := map[string]interface{}{
    "name": "base",
    "nested": map[string]interface{}{
      "a": 1,
      "b": 2,
    },
  }

  overlay := map[string]interface{}{
    "age": 10,
    "nested": map[string]interface{}{
      "b": 20,
      "c": 30,
    },
  }

  result := MergeMap(base, overlay)

  if result["name"] != "base" {
    t.Error("MergeMap: overlay should not overwrite existing top-level key")
  }
  if result["age"] != 10 {
    t.Error("MergeMap: overlay should add new key")
  }
  nested := result["nested"].(map[string]interface{})
  if nested["a"] != 1 {
    t.Error("MergeMap: should preserve base nested value")
  }
  if nested["b"] != 20 {
    t.Error("MergeMap: src (overlay) non-map values overwrite dst (base)")
  }
  if nested["c"] != 30 {
    t.Error("MergeMap: should add new nested key from overlay")
  }
}

func TestMergeMapEmptyOverlay(t *testing.T) {
  base := map[string]interface{}{
    "key": "value",
  }
  result := MergeMap(base, map[string]interface{}{})
  if result["key"] != "value" {
    t.Error("MergeMap with empty overlay should preserve base")
  }
}

func TestDeepCopySlice(t *testing.T) {
  original := []interface{}{1, "hello", map[string]interface{}{"k": "v"}}
  copied := DeepCopySlice(original)

  copied[0] = 99
  copied[2].(map[string]interface{})["k"] = "changed"

  if original[0] != 1 {
    t.Error("DeepCopySlice: modifying copy affected original")
  }
  if original[2].(map[string]interface{})["k"] != "v" {
    t.Error("DeepCopySlice: modifying copy affected original nested map")
  }
}
