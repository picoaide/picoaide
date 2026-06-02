package agent

import (
  "context"
  "fmt"
  "os"
  "path/filepath"
  "strings"
  "testing"
  "time"
)

// ============================================================
// Mock 辅助
// ============================================================

type mockSummarizer struct {
  result string
  err    error
}

func (m *mockSummarizer) Summarize(_ context.Context, _ string) (string, error) {
  return m.result, m.err
}

func setupEvolutionTest(t *testing.T) (string, *SessionStore) {
  t.Helper()
  dir := t.TempDir()
  store := NewSessionStore(dir)
  return dir, store
}

func writeFile(t *testing.T, path, content string) {
  t.Helper()
  if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
    t.Fatal(err)
  }
  if err := os.WriteFile(path, []byte(content), 0644); err != nil {
    t.Fatal(err)
  }
}

func TestRoughTokenCount(t *testing.T) {
  tests := []struct {
    input string
    want  int
  }{
    {"Hello", 1},                          // 5 bytes / 4 = 1
    {"这是一个中文测试", 6},               // 24 bytes / 4 = 6
    {"Hello World 中文测试", 6},            // 22 bytes / 4 = 5 (floor)
    {"", 0},
  }
  for _, tc := range tests {
    got := roughTokenCount(tc.input)
    if got != tc.want {
      t.Errorf("roughTokenCount(%q) = %d, want %d", tc.input, got, tc.want)
    }
  }
}

func TestParseMemorySections(t *testing.T) {
  input := `# 长期记忆

最后更新: 2026-05-28

## 关键决策
- 决策1内容
- 决策2内容

## 项目知识
- 知识1内容
`
  sections := parseMemorySections(input)

  if sections == nil {
    t.Fatal("sections should not be nil")
  }

  decs, ok := sections["关键决策"]
  if !ok {
    t.Fatal("should have 关键决策 section")
  }
  if len(decs) != 2 {
    t.Fatalf("关键决策 should have 2 items, got %d", len(decs))
  }

  knowledge, ok := sections["项目知识"]
  if !ok {
    t.Fatal("should have 项目知识 section")
  }
  if len(knowledge) != 1 {
    t.Fatalf("项目知识 should have 1 item, got %d", len(knowledge))
  }
}

func TestSerializeMemoryMD(t *testing.T) {
  input := `# 长期记忆

最后更新: 2026-05-28

## 关键决策
- 决策1

## 项目知识
- 知识1
`
  sections := parseMemorySections(input)
  output := serializeMemoryMD(sections)

  if !strings.Contains(output, "## 关键决策") {
    t.Error("output should contain 关键决策 section")
  }
  if !strings.Contains(output, "- 决策1") {
    t.Error("output should contain decision content")
  }
  if !strings.Contains(output, "# 长期记忆") {
    t.Error("output should contain header")
  }
}

func TestMergeDecisions(t *testing.T) {
  existing := `# 长期记忆

最后更新: 2026-05-28

## 关键决策
- 已有决策

## 项目知识
- 项目知识1
`
  result := EvolutionResult{
    Decisions: []Decision{
      {Topic: "数据库选型", Decision: "使用 SQLite + xorm"},
      {Topic: "已有决策", Decision: "更新版本"},
    },
  }

  output := MergeIntoMemoryMD(existing, result, 5000)

  if !strings.Contains(output, "使用 SQLite + xorm") {
    t.Error("should contain new decision")
  }
  // dedup: 已有决策 topic 相同，应更新内容
  if !strings.Contains(output, "更新版本") {
    t.Error("should use updated content for existing topic")
  }
}

func TestMergeDecisionsDedup(t *testing.T) {
  existing := `# 长期记忆

## 关键决策
- 数据库选型：使用 MySQL
`
  result := EvolutionResult{
    Decisions: []Decision{
      {Topic: "数据库选型", Decision: "使用 MySQL"},
    },
  }

  output := MergeIntoMemoryMD(existing, result, 5000)

  // Same topic+content should not duplicate
  decSection := extractSection(output, "关键决策")
  count := strings.Count(decSection, "使用 MySQL")
  if count != 1 {
    t.Errorf("dedup failed: found %d occurrences, want 1", count)
  }
}

func TestMergeProgress(t *testing.T) {
  existing := `# 长期记忆

## 活跃上下文

### 进行中
- 用户权限模块

### 阻塞
- 等待数据库脚本
`
  result := EvolutionResult{
    Progress: Progress{
      InProgress: []string{"新增API接口"},
      Blocked:    []string{"网络配置问题"},
    },
  }

  output := MergeIntoMemoryMD(existing, result, 5000)

  if !strings.Contains(output, "新增API接口") {
    t.Error("should contain new in_progress item")
  }
  if !strings.Contains(output, "网络配置问题") {
    t.Error("should contain new blocked item")
  }
  // existing items should remain
  if !strings.Contains(output, "用户权限模块") {
    t.Error("should keep existing in_progress item")
  }
}

func TestMergeKnowledge(t *testing.T) {
  existing := `# 长期记忆

## 项目知识
- 旧知识

## 技术发现
- 旧发现
`
  result := EvolutionResult{
    Knowledge: []Knowledge{
      {Topic: "部署配置", Fact: "Docker 需要设置时区"},
      {Topic: "旧知识", Fact: "更新后的知识"},
    },
  }

  output := MergeIntoMemoryMD(existing, result, 5000)

  if !strings.Contains(output, "Docker 需要设置时区") {
    t.Error("should contain new knowledge")
  }
  if !strings.Contains(output, "更新后的知识") {
    t.Error("should update existing knowledge")
  }
}

func TestTokenBudgetEnforcement(t *testing.T) {
  // Create content that exceeds the budget
  var longDecisions strings.Builder
  longDecisions.WriteString("# 长期记忆\n\n## 关键决策\n")
  for i := 0; i < 50; i++ {
    longDecisions.WriteString("- 决策条目项目信息" + strings.Repeat("x", 50) + "\n")
  }

  existing := longDecisions.String()
  // Budget of 500 tokens should force trimming
  result := EvolutionResult{
    Decisions: []Decision{
      {Topic: "新决策", Decision: "测试 trim"},
    },
  }

  output := MergeIntoMemoryMD(existing, result, 500)

  tokens := roughTokenCount(output)
  if tokens > 500 {
    t.Errorf("output tokens %d exceeds budget 500", tokens)
  }
  if !strings.Contains(output, "测试 trim") {
    t.Error("new decision should be preserved after trimming")
  }
}

func TestMergeUserPreferences(t *testing.T) {
  existing := `# 用户信息

## 工作偏好
- 沟通风格：简洁直接

## 专业领域
- Go 后端开发
`
  result := EvolutionResult{
    Preferences: []Preference{
      {Aspect: "工作习惯", Description: "每次提交前运行测试"},
      {Aspect: "沟通风格", Description: "简洁直接，结论先行"},
    },
  }

  output := MergeIntoUserMD(existing, result, 2000)

  if !strings.Contains(output, "每次提交前运行测试") {
    t.Error("should contain new preference")
  }
  // Same aspect should be updated
  if !strings.Contains(output, "结论先行") {
    t.Error("should update existing preference with new content")
  }
}

func TestBackupFile(t *testing.T) {
  dir := t.TempDir()
  src := filepath.Join(dir, "MEMORY.md")
  if err := os.WriteFile(src, []byte("test"), 0644); err != nil {
    t.Fatal(err)
  }

  backupDir := filepath.Join(dir, "archive")
  if err := backupFile(src, backupDir); err != nil {
    t.Fatal(err)
  }

  entries, err := os.ReadDir(backupDir)
  if err != nil {
    t.Fatal(err)
  }
  if len(entries) == 0 {
    t.Fatal("should have at least one backup file")
  }
  if !strings.HasPrefix(entries[0].Name(), "MEMORY.md.") {
    t.Errorf("backup filename should start with MEMORY.md., got %s", entries[0].Name())
  }
}

func TestCleanOldBackups(t *testing.T) {
  dir := t.TempDir()
  backupDir := filepath.Join(dir, "archive")
  if err := os.MkdirAll(backupDir, 0755); err != nil {
    t.Fatal(err)
  }

  // Create old backup
  oldPath := filepath.Join(backupDir, "MEMORY.md.20200101_000000")
  if err := os.WriteFile(oldPath, []byte("old"), 0644); err != nil {
    t.Fatal(err)
  }

  // Create new backup
  newPath := filepath.Join(backupDir, "MEMORY.md."+time.Now().Format("20060102_150405"))
  if err := os.WriteFile(newPath, []byte("new"), 0644); err != nil {
    t.Fatal(err)
  }

  // Clean backups older than 1 hour (should only remove the old one)
  if err := cleanOldBackups(backupDir, 1*time.Hour); err != nil {
    t.Fatal(err)
  }

  entries, _ := os.ReadDir(backupDir)
  if len(entries) != 1 {
    t.Fatalf("should have 1 backup remaining, got %d", len(entries))
  }
  if entries[0].Name() != filepath.Base(newPath) {
    t.Errorf("should keep new backup, got %s", entries[0].Name())
  }
}

func TestMessageLimit(t *testing.T) {
  _, store := setupEvolutionTest(t)
  key := "test-session"

  // Write 300 messages
  for i := 0; i < 300; i++ {
    if err := store.AppendMessage(key, &Message{
      Role:    RoleUser,
      Content: "message " + string(rune('0'+i%10)),
    }); err != nil {
      t.Fatal(err)
    }
  }

  msgs := collectSessionMessages(store, key, 200)
  if len(msgs) > 200 {
    t.Errorf("should limit to 200 messages, got %d", len(msgs))
  }
}

func TestEvolutionSkipsShortSession(t *testing.T) {
  dir, store := setupEvolutionTest(t)
  key := "short-session"

  // Only 2 messages, no tool calls
  store.AppendMessage(key, &Message{Role: RoleUser, Content: "Hi"})
  store.AppendMessage(key, &Message{Role: RoleAssistant, Content: "Hello"})

  evolver := &MemoryEvolution{
    workspace: dir,
    store:     store,
    maxMsgs:   200,
  }

  result, err := evolver.Evolve(context.Background(), key)
  if err != nil {
    t.Fatal(err)
  }
  if result.HasChanges {
    t.Error("should not evolve short session with no tool calls")
  }
}

func TestEvolveWithLLMResult(t *testing.T) {
  dir, store := setupEvolutionTest(t)
  key := "evolve-llm"

  // Write sufficient messages WITH tool calls
  for i := 0; i < 6; i++ {
    store.AppendMessage(key, &Message{Role: RoleUser, Content: "question " + string(rune('0'+i))})
    store.AppendMessage(key, &Message{
      Role:      RoleAssistant,
      Content:   "answer " + string(rune('0'+i)),
      ToolCalls: []ToolCall{{ID: "call_1", Function: ToolFunction{Name: "command", Arguments: "{}"}}},
    })
  }

  // Create existing MEMORY.md
  writeFile(t, filepath.Join(dir, "memory", "MEMORY.md"), `# 长期记忆

最后更新: 2026-05-28

## 关键决策
- 已有决策
`)

  // Create existing USER.md
  writeFile(t, filepath.Join(dir, "USER.md"), `# 用户信息

## 工作偏好
- 沟通风格：简洁
`)

  evolver := &MemoryEvolution{
    workspace:  dir,
    store:      store,
    maxMsgs:    200,
    summarizer: &mockSummarizer{result: `{"decisions":[{"topic":"新决策","decision":"使用Go 1.22","rationale":"更好的性能"}],"knowledge":[],"progress":{"completed":["任务A"],"in_progress":[],"blocked":[]},"preferences":[{"aspect":"工作习惯","description":"先写测试"}]}`, err: nil},
  }

  result, err := evolver.Evolve(context.Background(), key)
  if err != nil {
    t.Fatal(err)
  }

  if !result.HasChanges {
    t.Error("should have changes with LLM summarizer")
  }
  if len(result.Decisions) != 1 {
    t.Errorf("should have 1 decision, got %d", len(result.Decisions))
  }

  // Verify files were written
  memoryContent, _ := os.ReadFile(filepath.Join(dir, "memory", "MEMORY.md"))
  if !strings.Contains(string(memoryContent), "使用Go 1.22") {
    t.Error("MEMORY.md should contain new decision")
  }

  userContent, _ := os.ReadFile(filepath.Join(dir, "USER.md"))
  if !strings.Contains(string(userContent), "先写测试") {
    t.Error("USER.md should contain new preference")
  }
}

func TestEvolveWithEmptyLLMResult(t *testing.T) {
  dir, store := setupEvolutionTest(t)
  key := "evolve-empty"

  for i := 0; i < 6; i++ {
    store.AppendMessage(key, &Message{Role: RoleUser, Content: "q" + string(rune('0'+i))})
    store.AppendMessage(key, &Message{
      Role:      RoleAssistant,
      Content:   "a" + string(rune('0'+i)),
      ToolCalls: []ToolCall{{ID: "call_1", Function: ToolFunction{Name: "read_file", Arguments: "{}"}}},
    })
  }

  evolver := &MemoryEvolution{
    workspace:  dir,
    store:      store,
    maxMsgs:    200,
    summarizer: &mockSummarizer{result: `{}`, err: nil},
  }

  result, err := evolver.Evolve(context.Background(), key)
  if err != nil {
    t.Fatal(err)
  }
  if result.HasChanges {
    t.Error("should not have changes with empty LLM result")
  }
}

func TestEvolveWithLLMError(t *testing.T) {
  dir, store := setupEvolutionTest(t)
  key := "evolve-error"

  for i := 0; i < 6; i++ {
    store.AppendMessage(key, &Message{Role: RoleUser, Content: "q" + string(rune('0'+i))})
    store.AppendMessage(key, &Message{
      Role:      RoleAssistant,
      Content:   "a" + string(rune('0'+i)),
      ToolCalls: []ToolCall{{ID: "call_1", Function: ToolFunction{Name: "read_file", Arguments: "{}"}}},
    })
  }

  evolver := &MemoryEvolution{
    workspace:  dir,
    store:      store,
    maxMsgs:    200,
    summarizer: &mockSummarizer{result: "", err: fmt.Errorf("LLM unavailable")},
  }

  result, err := evolver.Evolve(context.Background(), key)
  if err != nil {
    t.Fatal(err)
  }
  if result.HasChanges {
    t.Error("should not have changes when LLM extraction fails")
  }
}

func TestEvolveWithCodeWrappedJSON(t *testing.T) {
  dir, store := setupEvolutionTest(t)
  key := "evolve-wrap"

  for i := 0; i < 6; i++ {
    store.AppendMessage(key, &Message{Role: RoleUser, Content: "q" + string(rune('0'+i))})
    store.AppendMessage(key, &Message{
      Role:      RoleAssistant,
      Content:   "a" + string(rune('0'+i)),
      ToolCalls: []ToolCall{{ID: "call_1", Function: ToolFunction{Name: "read_file", Arguments: "{}"}}},
    })
  }

  // Simulate LLM returning JSON wrapped in markdown code block
  evolver := &MemoryEvolution{
    workspace:  dir,
    store:      store,
    maxMsgs:    200,
    summarizer: &mockSummarizer{result: "```json\n{\"decisions\":[{\"topic\":\"从代码块提取\",\"decision\":\"测试通过\",\"rationale\":\"\"}]}\n```", err: nil},
  }

  result, err := evolver.Evolve(context.Background(), key)
  if err != nil {
    t.Fatal(err)
  }
  if !result.HasChanges {
    t.Error("should extract decisions from code-wrapped JSON")
  }
  if len(result.Decisions) != 1 || result.Decisions[0].Topic != "从代码块提取" {
    t.Errorf("should parse decision from code block, got %+v", result.Decisions)
  }
}

func TestCollectSessionMessagesWithArchive(t *testing.T) {
  _, store := setupEvolutionTest(t)
  key := "archive-test"

  // Write archive messages via AppendMessage (writes to both archive + live)
  for i := 0; i < 5; i++ {
    store.AppendMessage(key, &Message{Role: RoleUser, Content: fmt.Sprintf("archive msg %d", i)})
  }

  // Collect should get all 5
  msgs := collectSessionMessages(store, key, 200)
  if len(msgs) != 5 {
    t.Errorf("should collect 5 messages, got %d", len(msgs))
  }
}

func TestRoundtripParseSerialize(t *testing.T) {
  input := `# 长期记忆

最后更新: 2026-05-28

## 关键决策
- 决策1：内容1
- 决策2：内容2

## 项目知识
- 知识1：事实1
`
  sections := parseMemorySections(input)
  output := serializeMemoryMD(sections)

  sections2 := parseMemorySections(output)
  decs2 := sections2["关键决策"]
  if len(decs2) != 2 {
    t.Errorf("roundtrip: 关键决策 should have 2 items, got %d: %v", len(decs2), decs2)
  }
}

func TestUserMDBudgetEnforcement(t *testing.T) {
  var longPrefs strings.Builder
  longPrefs.WriteString("# 用户信息\n\n## 工作偏好\n")
  for i := 0; i < 100; i++ {
    longPrefs.WriteString("- 偏好" + string(rune('0'+i%10)) + strings.Repeat("x", 30) + "\n")
  }

  result := EvolutionResult{
    Preferences: []Preference{
      {Aspect: "新偏好", Description: "新内容"},
    },
  }

  output := MergeIntoUserMD(longPrefs.String(), result, 200)
  tokens := roughTokenCount(output)
  if tokens > 200 {
    t.Errorf("output tokens %d exceeds budget 200", tokens)
  }
  if !strings.Contains(output, "新内容") {
    t.Error("new preference should be preserved after trimming")
  }
}

func TestSerializeWithSubSections(t *testing.T) {
  sections := map[string][]string{
    "活跃上下文/已完成": {"任务1", "任务2"},
    "活跃上下文/进行中": {"任务3"},
    "活跃上下文/阻塞":   {},
    "关键决策":          {"决策1"},
  }

  output := serializeMemoryMD(sections)

  if !strings.Contains(output, "### 已完成") {
    t.Error("should render 已完成 sub-section")
  }
  if !strings.Contains(output, "### 进行中") {
    t.Error("should render 进行中 sub-section")
  }
  if strings.Contains(output, "### 阻塞") {
    t.Error("should NOT render empty 阻塞 sub-section")
  }
  if !strings.Contains(output, "- 任务1") {
    t.Error("should contain completed task")
  }
  if !strings.Contains(output, "- 任务3") {
    t.Error("should contain in-progress task")
  }
}

// ============================================================
// 辅助函数
// ============================================================

func extractSection(content, section string) string {
  lines := strings.Split(content, "\n")
  var inSection bool
  var result []string
  for _, line := range lines {
    if strings.HasPrefix(line, "## ") && strings.TrimSpace(line[3:]) == section {
      inSection = true
      continue
    }
    if inSection {
      if strings.HasPrefix(line, "## ") {
        break
      }
      result = append(result, line)
    }
  }
  return strings.Join(result, "\n")
}
