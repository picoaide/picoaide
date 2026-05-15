package util

import (
  "fmt"
  "os"
  "path/filepath"
  "regexp"
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

// SafePathSegment 验证路径片段不包含路径遍历字符或危险模式。
// 用于验证将直接拼接进文件路径的用户输入（如用户名、技能名、仓库名）。
// 允许：字母、数字、短横线、下划线、点、中文及常见 Unicode 字符。
// 拒绝：/ \ .. 空字符串 以及仅由点组成的字符串。
var safeSegmentRe = regexp.MustCompile(`^[^/\\]+$`)

func SafePathSegment(name string) error {
  if name == "" {
    return fmt.Errorf("名称不能为空")
  }
  if name == "." || name == ".." {
    return fmt.Errorf("名称不能是 . 或 ..")  //nolint:staticcheck // ST1005: 中文错误无需遵循 Go 惯例
  }
  if strings.Contains(name, "..") {
    return fmt.Errorf("名称不能包含 ..") //nolint:staticcheck // ST1005: 中文错误
  }
  if !safeSegmentRe.MatchString(name) {
    return fmt.Errorf("名称包含非法字符: %s", name)
  }
  return nil
}

// SafeRelPath 验证相对路径不逃逸出基准目录。
// 返回清理后的安全绝对路径。适用于文件管理等需要子目录遍历的场景。
func SafeRelPath(baseDir, relPath string) (string, error) {
  cleaned := filepath.Clean("/" + relPath)
  absPath := filepath.Join(baseDir, cleaned)

  evalBase, err := filepath.EvalSymlinks(baseDir)
  if err != nil {
    evalBase = baseDir
  }

  evalPath, err := filepath.EvalSymlinks(absPath)
  if err != nil {
    if !os.IsNotExist(err) {
      return "", fmt.Errorf("路径验证失败")
    }
    // 路径不存在时验证父目录
    parent := filepath.Dir(absPath)
    evalParent, err2 := filepath.EvalSymlinks(parent)
    if err2 != nil {
      return "", fmt.Errorf("路径验证失败")
    }
    if !strings.HasPrefix(evalParent, evalBase+string(os.PathSeparator)) && evalParent != evalBase {
      return "", fmt.Errorf("路径越界")
    }
    return absPath, nil
  }

  if !strings.HasPrefix(evalPath, evalBase+string(os.PathSeparator)) && evalPath != evalBase {
    return "", fmt.Errorf("路径越界")
  }
  return evalPath, nil
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
