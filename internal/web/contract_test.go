package web

import (
  "bufio"
  "os"
  "strings"
  "testing"
)

func browserToolNames() []string {
  names := make([]string, len(browserToolDefs))
  for i, t := range browserToolDefs {
    names[i] = t.Name
  }
  return names
}

func computerToolNames() []string {
  names := make([]string, len(computerToolDefs))
  for i, t := range computerToolDefs {
    names[i] = t.Name
  }
  return names
}

func extractJSToolHandlers(t *testing.T) map[string]bool {
  f, err := os.Open("../../picoaide-extension/background.js")
  if err != nil {
    t.Skip("picoaide-extension 目录不存在，跳过 JS 契约测试")
    return nil
  }
  defer f.Close()

  handlers := map[string]bool{}
  scanner := bufio.NewScanner(f)
  for scanner.Scan() {
    line := strings.TrimSpace(scanner.Text())
    // 匹配 "browser_navigate: handleNavigate," 格式
    if strings.HasPrefix(line, "browser_") || strings.HasPrefix(line, "computer_") {
      if colonIdx := strings.Index(line, ":"); colonIdx >= 0 {
        name := strings.TrimSpace(line[:colonIdx])
        handlers[name] = true
      }
    }
  }
  return handlers
}

func extractPythonToolHandlers(t *testing.T) map[string]bool {
  f, err := os.Open("../../picoaide-desktop/core/executor.py")
  if err != nil {
    t.Skip("picoaide-desktop 目录不存在，跳过 Python 契约测试")
    return nil
  }
  defer f.Close()

  handlers := map[string]bool{}
  scanner := bufio.NewScanner(f)
  for scanner.Scan() {
    line := strings.TrimSpace(scanner.Text())
    // 匹配 "computer_screenshot": _screenshot, 格式
    if strings.HasPrefix(line, `"browser_`) || strings.HasPrefix(line, `"computer_`) {
      if colonIdx := strings.Index(line, ":"); colonIdx >= 0 {
        name := line[1 : colonIdx-1] // 去掉外层引号
        handlers[name] = true
      }
    }
  }
  return handlers
}

func TestContractToolsBrowser(t *testing.T) {
  jsHandlers := extractJSToolHandlers(t)
  if jsHandlers == nil {
    return
  }

  goTools := browserToolNames()
  for _, name := range goTools {
    if !jsHandlers[name] {
      t.Errorf("浏览器扩展缺少工具处理器: %s (在 picoaide-extension/background.js 中实现)", name)
    }
  }

  for name := range jsHandlers {
    found := false
    for _, goName := range goTools {
      if name == goName {
        found = true
        break
      }
    }
    if !found {
      t.Errorf("浏览器扩展中有未在服务端定义的工具处理器: %s", name)
    }
  }
}

func TestContractToolsComputer(t *testing.T) {
  pyHandlers := extractPythonToolHandlers(t)
  if pyHandlers == nil {
    return
  }

  goTools := computerToolNames()
  for _, name := range goTools {
    if !pyHandlers[name] {
      t.Errorf("桌面客户端缺少工具处理器: %s (在 picoaide-desktop/core/executor.py 中实现)", name)
    }
  }

  for name := range pyHandlers {
    found := false
    for _, goName := range goTools {
      if name == goName {
        found = true
        break
      }
    }
    if !found {
      t.Errorf("桌面客户端中有未在服务端定义的工具处理器: %s", name)
    }
  }
}
