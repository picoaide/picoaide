package user

import (
  "strings"
  "testing"
)

func TestValidateUsername(t *testing.T) {
  validNames := []string{
    "admin",
    "user123",
    "test-user",
    "test_user",
    "user.name",
    "a",
    "A",
    "a0",
    "my-cool_user.v2",
    "123",
  }
  for _, name := range validNames {
    if err := ValidateUsername(name); err != nil {
      t.Errorf("ValidateUsername(%q) = %v, want nil", name, err)
    }
  }

  invalidCases := []struct {
    name string
    msg  string
  }{
    {"", "空用户名"},
    {"a/b", "含斜杠"},
    {"a b", "含空格"},
    {"-test", "短横线开头"},
    {"test-", "短横线结尾"},
    {".test", "点开头"},
    {"test.", "点结尾"},
    {"_test", "下划线开头"},
    {"test_", "下划线结尾"},
  }

  for _, tt := range invalidCases {
    err := ValidateUsername(tt.name)
    if err == nil {
      t.Errorf("ValidateUsername(%q) = nil, want error (%s)", tt.name, tt.msg)
    }
  }

  // 超长用户名
  longName := strings.Repeat("a", 100)
  if err := ValidateUsername(longName); err == nil {
    t.Error("ValidateUsername(100 chars) should fail")
  }

  // 恰好 64 字符应通过
  maxName := strings.Repeat("a", 64)
  if err := ValidateUsername(maxName); err != nil {
    t.Errorf("ValidateUsername(64 chars) should pass, got %v", err)
  }

  // 65 字符应失败
  overName := strings.Repeat("a", 65)
  if err := ValidateUsername(overName); err == nil {
    t.Error("ValidateUsername(65 chars) should fail")
  }
}

func TestIsWhitelisted(t *testing.T) {
  // nil 白名单 = 全部允许
  if !IsWhitelisted(nil, "anyone") {
    t.Error("IsWhitelisted(nil, ...) should return true")
  }

  // 空白名单 = 无匹配
  whitelist := map[string]bool{}
  if IsWhitelisted(whitelist, "user") {
    t.Error("IsWhitelisted(empty, user) should return false")
  }

  // 有匹配
  whitelist = map[string]bool{
    "alice": true,
    "bob":   true,
  }
  if !IsWhitelisted(whitelist, "alice") {
    t.Error("IsWhitelisted should find alice")
  }
  if IsWhitelisted(whitelist, "charlie") {
    t.Error("IsWhitelisted should not find charlie")
  }
}
