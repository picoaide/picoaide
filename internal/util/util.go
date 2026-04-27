package util

import (
  "fmt"
  "os"
  "path/filepath"
  "strings"
)

// ============================================================
// 通用工具函数
// ============================================================

func DeepCopyMap(src map[string]interface{}) map[string]interface{} {
  dst := make(map[string]interface{}, len(src))
  for k, v := range src {
    dst[k] = deepCopyValue(v)
  }
  return dst
}

func DeepCopySlice(src []interface{}) []interface{} {
  dst := make([]interface{}, len(src))
  for i, v := range src {
    dst[i] = deepCopyValue(v)
  }
  return dst
}

func deepCopyValue(v interface{}) interface{} {
  switch val := v.(type) {
  case map[string]interface{}:
    return DeepCopyMap(val)
  case []interface{}:
    return DeepCopySlice(val)
  default:
    return v
  }
}

func MergeMap(dst, src map[string]interface{}) map[string]interface{} {
  for k, sv := range src {
    dv, exists := dst[k]
    if !exists {
      dst[k] = sv
      continue
    }
    srcMap, srcIsMap := sv.(map[string]interface{})
    dstMap, dstIsMap := dv.(map[string]interface{})
    if srcIsMap && dstIsMap {
      dst[k] = MergeMap(dstMap, srcMap)
      continue
    }
    dst[k] = sv
  }
  return dst
}

func CopyFile(src, dst string) error {
  in, err := os.Open(src)
  if err != nil {
    return err
  }
  defer in.Close()

  if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
    return err
  }

  out, err := os.Create(dst)
  if err != nil {
    return err
  }
  defer out.Close()

  if _, err := out.ReadFrom(in); err != nil {
    return err
  }

  info, err := os.Stat(src)
  if err != nil {
    return err
  }
  return os.Chmod(dst, info.Mode())
}

func CopyDir(src, dst string) error {
  return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
    if err != nil {
      return err
    }

    relPath, err := filepath.Rel(src, path)
    if err != nil {
      return err
    }
    targetPath := filepath.Join(dst, relPath)

    if info.IsDir() {
      return os.MkdirAll(targetPath, info.Mode())
    }

    return CopyFile(path, targetPath)
  })
}

func ParseFlags(args []string) (map[string]string, []string) {
  flags := make(map[string]string)
  var positional []string
  for i := 0; i < len(args); i++ {
    if strings.HasPrefix(args[i], "-") && !strings.HasPrefix(args[i], "--") && args[i] != "-" {
      if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
        flags[args[i]] = args[i+1]
        i++
      } else {
        flags[args[i]] = "true"
      }
    } else {
      positional = append(positional, args[i])
    }
  }
  return flags, positional
}

func FormatSize(size int64) string {
  if size < 1024 {
    return fmt.Sprintf("%d B", size)
  }
  if size < 1024*1024 {
    return fmt.Sprintf("%.1f KB", float64(size)/1024)
  }
  return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
}

func IsTextFile(filename string) bool {
  ext := strings.ToLower(filepath.Ext(filename))
  textExts := map[string]bool{
    ".txt": true, ".md": true, ".yaml": true, ".yml": true,
    ".json": true, ".toml": true, ".cfg": true, ".conf": true,
    ".py": true, ".js": true, ".ts": true, ".go": true,
    ".sh": true, ".bash": true, ".zsh": true,
    ".env": true, ".gitignore": true, ".editorconfig": true,
    ".xml": true, ".html": true, ".css": true,
    ".sql": true, ".log": true, ".csv": true, ".tsv": true,
    ".ini": true, ".properties": true, ".rs": true, ".java": true,
    ".c": true, ".cpp": true, ".h": true, ".hpp": true,
    ".tf": true,
  }
  return textExts[ext]
}
