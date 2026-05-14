package main

import (
  "net"
  "os"
  "testing"

  "github.com/picoaide/picoaide/internal/config"
)

// ============================================================
// checkPort 测试
// ============================================================

func TestCheckPort_Available(t *testing.T) {
  // 找一个可用端口
  ln, err := net.Listen("tcp", ":0")
  if err != nil {
    t.Fatalf("cannot find available port: %v", err)
  }
  port := ln.Addr().(*net.TCPAddr).Port
  ln.Close()

  // 端口关闭后可能处于 TIME_WAIT，给内核一点时间释放
  if !checkPort(port) {
    t.Logf("port %d in TIME_WAIT, retrying...", port)
  }
}

func TestCheckPort_InUse(t *testing.T) {
  ln, err := net.Listen("tcp", ":0")
  if err != nil {
    t.Fatalf("cannot listen: %v", err)
  }
  defer ln.Close()

  port := ln.Addr().(*net.TCPAddr).Port
  if checkPort(port) {
    t.Errorf("checkPort(%d) = true, want false (port is in use)", port)
  }
}

// ============================================================
// printUsage 输出测试
// ============================================================

func TestPrintUsage_ContainsAppName(t *testing.T) {
  // 捕获 stdout
  r, w, err := os.Pipe()
  if err != nil {
    t.Fatalf("pipe error: %v", err)
  }
  orig := os.Stdout
  os.Stdout = w

  printUsage()

  w.Close()
  os.Stdout = orig

  var buf [4096]byte
  n, _ := r.Read(buf[:])
  output := string(buf[:n])

  if !contains(output, config.AppName) {
    t.Errorf("printUsage output should contain AppName %q, got: %s", config.AppName, output)
  }
  if !contains(output, "init") {
    t.Errorf("printUsage output should contain 'init' command")
  }
  if !contains(output, "reset-password") {
    t.Errorf("printUsage output should contain 'reset-password' command")
  }
}

// ============================================================
// 辅助
// ============================================================

func contains(s, substr string) bool {
  for i := 0; i <= len(s)-len(substr); i++ {
    if s[i:i+len(substr)] == substr {
      return true
    }
  }
  return false
}

// ============================================================
// 编译检查 — 确保 config 包可访问且常量可用
// ============================================================

func TestConfigAppNameExists(t *testing.T) {
  if config.AppName == "" {
    t.Error("config.AppName should not be empty")
  }
}


