package docker

import (
  "testing"
)

// ============================================================
// stripANSI 测试
// ============================================================

func TestStripANSI_NoANSI(t *testing.T) {
  input := "hello world"
  got := stripANSI(input)
  if got != input {
    t.Errorf("stripANSI(%q) = %q, want %q", input, got, input)
  }
}

func TestStripANSI_SimpleColor(t *testing.T) {
  input := "\x1b[31mred\x1b[0m"
  want := "red"
  got := stripANSI(input)
  if got != want {
    t.Errorf("stripANSI(%q) = %q, want %q", input, got, want)
  }
}

func TestStripANSI_MultipleCodes(t *testing.T) {
  input := "\x1b[32mgreen\x1b[0m \x1b[1mbold\x1b[0m"
  want := "green bold"
  got := stripANSI(input)
  if got != want {
    t.Errorf("stripANSI(%q) = %q, want %q", input, got, want)
  }
}

func TestStripANSI_WithNumbersAndSemicolons(t *testing.T) {
  input := "\x1b[38;5;82mcolorful\x1b[0m"
  want := "colorful"
  got := stripANSI(input)
  if got != want {
    t.Errorf("stripANSI(%q) = %q, want %q", input, got, want)
  }
}

func TestStripANSI_EmptyString(t *testing.T) {
  got := stripANSI("")
  if got != "" {
    t.Errorf("stripANSI(\"\") = %q, want \"\"", got)
  }
}

func TestStripANSI_IncompleteSequence(t *testing.T) {
  // \x1b[ without closing letter — function keeps the incomplete sequence
  input := "text\x1b["
  want := "text\x1b[" // incomplete sequences are preserved
  got := stripANSI(input)
  if got != want {
    t.Errorf("stripANSI(%q) = %q, want %q", input, got, want)
  }
}

func TestStripANSI_CursorMovement(t *testing.T) {
  input := "line1\x1b[2K\x1b[1Aline2"
  want := "line1line2"
  got := stripANSI(input)
  if got != want {
    t.Errorf("stripANSI(%q) = %q, want %q", input, got, want)
  }
}

// ============================================================
// parseWWWAuthenticate 测试
// ============================================================

func TestParseWWWAuthenticate_Full(t *testing.T) {
  // 注意：函数按逗号分割后检查 realm="/service="/scope=" 前缀
  // 如果 header 以 "Bearer " 开头，第一部分不会被匹配
  header := `realm="https://ghcr.io/token",service="ghcr.io",scope="repository:repo:pull"`
  want := "https://ghcr.io/token?service=ghcr.io&scope=repository:repo:pull"
  got := parseWWWAuthenticate(header)
  if got != want {
    t.Errorf("parseWWWAuthenticate = %q, want %q", got, want)
  }
}

func TestParseWWWAuthenticate_EmptyHeader(t *testing.T) {
  got := parseWWWAuthenticate("")
  if got != "" {
    t.Errorf("parseWWWAuthenticate(\"\") = %q, want \"\"", got)
  }
}

func TestParseWWWAuthenticate_NoRealm(t *testing.T) {
  header := `Bearer service="ghcr.io",scope="repository:repo:pull"`
  got := parseWWWAuthenticate(header)
  if got != "" {
    t.Errorf("parseWWWAuthenticate = %q, want \"\"", got)
  }
}

func TestParseWWWAuthenticate_RealmOnly(t *testing.T) {
  header := `realm="https://registry.example.com/token"`
  want := "https://registry.example.com/token?service=&scope="
  got := parseWWWAuthenticate(header)
  if got != want {
    t.Errorf("parseWWWAuthenticate = %q, want %q", got, want)
  }
}

func TestParseWWWAuthenticate_OrderVariation(t *testing.T) {
  header := `realm="https://token.example.com",service="example_service",scope="repository:myimage:pull"`
  want := "https://token.example.com?service=example_service&scope=repository:myimage:pull"
  got := parseWWWAuthenticate(header)
  if got != want {
    t.Errorf("parseWWWAuthenticate = %q, want %q", got, want)
  }
}
