package util

import (
  "fmt"
  "time"
)

// ============================================================
// 时间工具函数 — 统一时间格式
// ============================================================

var parseFormats = []string{
  time.RFC3339,
  "2006-01-02 15:04:05 -0700 MST",
  "2006-01-02 15:04:05",
}

// ParseTime 解析时间字符串为 time.Time，支持多种格式
func ParseTime(s string) (time.Time, error) {
  for _, f := range parseFormats {
    if t, err := time.Parse(f, s); err == nil {
      return t, nil
    }
  }
  return time.Time{}, fmt.Errorf("无法解析时间: %s", s)
}

// FormatTime 将 time.Time 格式化为 RFC3339 字符串
func FormatTime(t time.Time) string {
  return t.Format(time.RFC3339)
}
