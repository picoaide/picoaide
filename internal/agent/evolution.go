package agent

import (
  "context"
  "encoding/json"
  "fmt"
  "log/slog"
  "os"
  "path/filepath"
  "sort"
  "strings"
  "time"
)

// ============================================================
// 类型定义
// ============================================================

// EvolutionResult LLM 提取的结构化记忆数据
type EvolutionResult struct {
  Decisions   []Decision   `json:"decisions"`
  Knowledge   []Knowledge  `json:"knowledge"`
  Progress    Progress     `json:"progress"`
  Preferences []Preference `json:"preferences"`
  HasChanges  bool         // 是否产生有效变更
}

type Decision struct {
  Topic     string `json:"topic"`
  Decision  string `json:"decision"`
  Rationale string `json:"rationale"`
}

type Knowledge struct {
  Topic string `json:"topic"`
  Fact  string `json:"fact"`
}

type Progress struct {
  Completed  []string `json:"completed"`
  InProgress []string `json:"in_progress"`
  Blocked    []string `json:"blocked"`
}

type Preference struct {
  Aspect      string `json:"aspect"`
  Description string `json:"description"`
}

// MemoryEvolution 记忆进化引擎
// 在会话结束后运行，提取关键信息合并到 MEMORY.md 和 USER.md
type MemoryEvolution struct {
  workspace  string
  store      *SessionStore
  summarizer Summarizer // LLM 提取器，为空时仅做格式维护
  maxMsgs    int        // 送入 LLM 的消息上限
}

func NewMemoryEvolution(workspace string, store *SessionStore) *MemoryEvolution {
  return &MemoryEvolution{
    workspace: workspace,
    store:     store,
    maxMsgs:   200,
  }
}

func (e *MemoryEvolution) SetSummarizer(s Summarizer) {
  e.summarizer = s
}

func (e *MemoryEvolution) SetMaxMsgs(n int) {
  if n > 0 {
    e.maxMsgs = n
  }
}

// Evolve 执行一次完整的记忆进化
// 返回变更摘要供审计日志使用
func (e *MemoryEvolution) Evolve(ctx context.Context, sessionKey string) (res *EvolutionResult, err error) {
  defer func() {
    if r := recover(); r != nil {
      slog.Error("evolution.panic_recovered", "session_key", sessionKey, "panic", r)
      err = fmt.Errorf("evolution panic: %v", r)
      res = &EvolutionResult{}
    }
  }()
  slog.Debug("evolution.start", "session_key", sessionKey)

  // 1. 收集会话消息（archive + live）
  msgs := collectSessionMessages(e.store, sessionKey, e.maxMsgs)
  if len(msgs) < 5 {
    slog.Debug("evolution.skip", "reason", "too_few_messages", "count", len(msgs))
    return &EvolutionResult{}, nil
  }

  // 2. 检查是否有工具调用（排除纯对话 session）
  hasToolCalls := false
  for _, m := range msgs {
    if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
      hasToolCalls = true
      break
    }
  }
  if !hasToolCalls {
    slog.Debug("evolution.skip", "reason", "no_tool_calls")
    return &EvolutionResult{}, nil
  }

  // 3. 读取现有记忆文件
  memoryPath := filepath.Join(e.workspace, "memory", "MEMORY.md")
  userPath := filepath.Join(e.workspace, "USER.md")

  existingMemory := readFile(memoryPath)
  existingUser := readFile(userPath)

  // 4. LLM 提取（如有 summarizer）
  result := EvolutionResult{}
  if e.summarizer != nil {
    extracted, err := e.extract(ctx, msgs, existingMemory)
    if err != nil {
      slog.Warn("evolution.extract_failed", "error", err.Error())
      // 降级：跳过 LLM 提取，仅做格式维护
    } else {
      result = *extracted
    }
  }

  // 如果没有提取到新信息，跳过写入
  if !result.HasChanges && len(result.Decisions) == 0 &&
    len(result.Knowledge) == 0 && len(result.Preferences) == 0 &&
    len(result.Progress.Completed) == 0 &&
    len(result.Progress.InProgress) == 0 &&
    len(result.Progress.Blocked) == 0 {
    slog.Debug("evolution.skip", "reason", "no_new_information")
    return &EvolutionResult{}, nil
  }

  // 5. 合并 USER.md（先写，即使失败也不影响 MEMORY.md 完整性）
  newUser := MergeIntoUserMD(existingUser, result, 2000)
  if err := e.writeWithBackup(userPath, newUser); err != nil {
    return &result, fmt.Errorf("写入 USER.md 失败: %w", err)
  }

  // 6. 合并 MEMORY.md（后写——更关键的数据最后落盘）
  newMemory := MergeIntoMemoryMD(existingMemory, result, 5000)
  if err := e.writeWithBackup(memoryPath, newMemory); err != nil {
    return &result, fmt.Errorf("写入 MEMORY.md 失败: %w", err)
  }

  // 7. 清理 90 天前的备份
  backupDir := filepath.Join(e.workspace, "memory", "archive")
  if err := cleanOldBackups(backupDir, 90*24*time.Hour); err != nil {
    slog.Warn("evolution.clean_backup_error", "error", err.Error())
  }

  result.HasChanges = true

  slog.Debug("evolution.complete",
    "decisions", len(result.Decisions),
    "knowledge", len(result.Knowledge),
    "preferences", len(result.Preferences),
  )

  return &result, nil
}

// extract 调用 LLM 从会话中提取记忆信息
func (e *MemoryEvolution) extract(ctx context.Context, msgs []*Message, existingMemory string) (*EvolutionResult, error) {
  var dialog strings.Builder
  for _, m := range msgs {
    prefix := ""
    switch m.Role {
    case RoleUser:
      prefix = "用户"
    case RoleAssistant:
      if len(m.ToolCalls) > 0 {
        prefix = "助手(调用工具)"
      } else {
        prefix = "助手"
      }
    case RoleTool:
      // 工具结果太长时截断，保留内容和摘要即可
      content := m.Content
      if len(content) > 200 {
        content = content[:200] + "..."
      }
      fmt.Fprintf(&dialog, "工具结果: %s\n\n", content)
      continue
    }
    if prefix != "" {
      fmt.Fprintf(&dialog, "%s: %s\n\n", prefix, m.Content)
    }
  }

  prompt := fmt.Sprintf(`你是一个企业 AI 助手的记忆提取器。分析以下会话记录，与现有记忆对比，提取新增的有长期价值的企业工作信息。

现有的 MEMORY.md：
%s

会话记录：
%s

输出格式（只输出 JSON，不要其他内容）：
{"decisions":[{"topic":"...","decision":"...","rationale":"..."}],"knowledge":[{"topic":"...","fact":"..."}],"progress":{"completed":["..."],"in_progress":["..."],"blocked":["..."]},"preferences":[{"aspect":"沟通风格|技术偏好|工作习惯|关注重点","description":"..."}]}

约束：
- 只提取与现有记忆不同的新增信息
- 每条不超过 40 字
- 只输出企业工作相关信息，忽略问候语、闲聊、技术调试过程
- 没有新信息时返回空对象 {}
- 必须返回合法的 JSON`, existingMemory, dialog.String())

  raw, err := e.summarizer.Summarize(ctx, prompt)
  if err != nil {
    return nil, fmt.Errorf("LLM 提取失败: %w", err)
  }

  // 清理可能的 markdown 代码块包裹
  raw = cleanJSONResponse(raw)

  var result EvolutionResult
  if raw == "" || raw == "{}" {
    return &result, nil
  }

  if err := json.Unmarshal([]byte(raw), &result); err != nil {
    return nil, fmt.Errorf("解析提取结果失败: %w", err)
  }

  // 校验必要字段
  if result.Decisions == nil {
    result.Decisions = []Decision{}
  }
  if result.Knowledge == nil {
    result.Knowledge = []Knowledge{}
  }
  if result.Preferences == nil {
    result.Preferences = []Preference{}
  }

  return &result, nil
}

// writeWithBackup 写文件并备份旧版本
func (e *MemoryEvolution) writeWithBackup(path, content string) error {
  // 备份旧版本
  if _, err := os.Stat(path); err == nil {
    backupDir := filepath.Join(e.workspace, "memory", "archive")
    if err := backupFile(path, backupDir); err != nil {
      slog.Warn("evolution.backup_failed", "path", path, "error", err.Error())
    }
  }

  // 确保目录存在
  if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
    return err
  }

  return os.WriteFile(path, []byte(content), 0600)
}

// ============================================================
// 合并引擎
// ============================================================

// MergeIntoMemoryMD 将提取结果合并到 MEMORY.md
func MergeIntoMemoryMD(existing string, result EvolutionResult, maxTokens int) string {
  sections := parseMemorySections(existing)

  // 合并决策
  if len(result.Decisions) > 0 {
    existingDecs := sectionMap(sections["关键决策"])
    for _, d := range result.Decisions {
      entry := fmt.Sprintf("%s：%s", d.Decision, d.Rationale)
      if d.Rationale == "" {
        entry = d.Decision
      }
      existingDecs[d.Topic] = entry
    }
    sections["关键决策"] = mapToSortedSlice(existingDecs)
  }

  // 合并知识
  if len(result.Knowledge) > 0 {
    existingKnowledge := sectionMap(sections["项目知识"])
    existingTech := sectionMap(sections["技术发现"])
    for _, k := range result.Knowledge {
      // 优先放入"项目知识"，已存在的 topic 更新内容
      existingKnowledge[k.Topic] = k.Fact
    }
    sections["项目知识"] = mapToSortedSlice(existingKnowledge)
    // 清理"技术发现"中已被移入"项目知识"的条目
    foundInKnowledge := make(map[string]bool)
    for k := range existingKnowledge {
      foundInKnowledge[k] = true
    }
    for k := range existingTech {
      if foundInKnowledge[k] {
        delete(existingTech, k)
      }
    }
    sections["技术发现"] = mapToSortedSlice(existingTech)
  }

  // 合并进度
  hasProgress := len(result.Progress.Completed) > 0 ||
    len(result.Progress.InProgress) > 0 ||
    len(result.Progress.Blocked) > 0
  if hasProgress {
    completed := sectionSlice(sections["活跃上下文/已完成"])
    inProgress := sectionSlice(sections["活跃上下文/进行中"])
    blocked := sectionSlice(sections["活跃上下文/阻塞"])

    // 完成项从进行中移除
    for _, c := range result.Progress.Completed {
      inProgress = removeItem(inProgress, c)
      blocked = removeItem(blocked, c)
      if !contains(completed, c) {
        completed = append(completed, c)
      }
    }
    // 进行中
    for _, p := range result.Progress.InProgress {
      if !contains(inProgress, p) {
        inProgress = append(inProgress, p)
      }
    }
    // 阻塞
    for _, b := range result.Progress.Blocked {
      if !contains(blocked, b) {
        blocked = append(blocked, b)
      }
    }

    // 写回子章节
    if len(completed) > 0 {
      sections["活跃上下文/已完成"] = completed
    }
    if len(inProgress) > 0 {
      sections["活跃上下文/进行中"] = inProgress
    }
    if len(blocked) > 0 {
      sections["活跃上下文/阻塞"] = blocked
    }
  }

  // 预算控制
  output := serializeMemoryMD(sections)
  if roughTokenCount(output) > maxTokens {
    output = enforceMemoryBudget(output, maxTokens)
  }

  return output
}

// MergeIntoUserMD 将提取结果合并到 USER.md
func MergeIntoUserMD(existing string, result EvolutionResult, maxTokens int) string {
  sections := parseUserSections(existing)

  if len(result.Preferences) > 0 {
    existingPrefs := sectionMap(sections["工作偏好"])
    for _, p := range result.Preferences {
      existingPrefs[p.Aspect] = p.Description
    }
    sections["工作偏好"] = mapToSortedSlice(existingPrefs)
  }

  output := serializeUserMD(sections)
  if roughTokenCount(output) > maxTokens {
    output = enforceUserBudget(output, maxTokens)
  }

  return output
}

// ============================================================
// 文件解析和序列化
// ============================================================

func parseMemorySections(content string) map[string][]string {
  sections := make(map[string][]string)
  if content == "" {
    content = "# 长期记忆\n\n"
  }

  currentSection := ""
  currentItems := []string{}
  inSubSection := ""
  subItems := []string{}

  lines := strings.Split(content, "\n")
  for _, line := range lines {
    trimmed := strings.TrimSpace(line)
    if strings.HasPrefix(trimmed, "## ") {
      // 保存上一节
      if inSubSection != "" {
        saveSubSection(sections, currentSection, inSubSection, subItems)
        inSubSection = ""
        subItems = nil
      }
      if currentSection != "" {
        sections[currentSection] = currentItems
      }
      currentSection = strings.TrimSpace(trimmed[3:])
      currentItems = []string{}
    } else if strings.HasPrefix(trimmed, "### ") && currentSection == "活跃上下文" {
      if inSubSection != "" {
        saveSubSection(sections, currentSection, inSubSection, subItems)
      }
      inSubSection = strings.TrimSpace(trimmed[4:])
      subItems = []string{}
    } else if strings.HasPrefix(trimmed, "- ") {
      item := strings.TrimSpace(trimmed[2:])
      if item != "" {
        if inSubSection != "" {
          subItems = append(subItems, item)
        } else {
          currentItems = append(currentItems, item)
        }
      }
    }
  }

  // 保存最后一段
  if inSubSection != "" {
    saveSubSection(sections, currentSection, inSubSection, subItems)
  }
  if currentSection != "" {
    sections[currentSection] = currentItems
  }

  return sections
}

func saveSubSection(sections map[string][]string, section, sub string, items []string) {
  key := section + "/" + sub
  sections[key] = items
}

func serializeMemoryMD(sections map[string][]string) string {
  if len(sections) == 0 {
    return "# 长期记忆\n\n最后更新: " + time.Now().Format("2006-01-02 15:04") + "\n"
  }

  var b strings.Builder
  b.WriteString("# 长期记忆\n\n")
  b.WriteString("最后更新: " + time.Now().Format("2006-01-02 15:04") + "\n\n")

  // 按照固定顺序输出章节
  sectionOrder := []string{"活跃上下文", "关键决策", "项目知识", "技术发现"}
  for _, name := range sectionOrder {
    items := sections[name]
    if len(items) == 0 {
      // 检查是否有子章节（如活跃上下文/进行中）
      hasSub := false
      for k := range sections {
        if strings.HasPrefix(k, name+"/") {
          hasSub = true
          break
        }
      }
      if !hasSub {
        continue
      }
    }

    b.WriteString("## " + name + "\n")
    if name == "活跃上下文" {
      // 渲染子章节
      subSectionOrder := []string{"已完成", "进行中", "阻塞"}
      for _, sub := range subSectionOrder {
        subItems := sections[name+"/"+sub]
        if len(subItems) > 0 {
          b.WriteString("### " + sub + "\n")
          for _, item := range subItems {
            b.WriteString("- " + item + "\n")
          }
          b.WriteString("\n")
        }
      }
    } else {
      for _, item := range items {
        b.WriteString("- " + item + "\n")
      }
      b.WriteString("\n")
    }
  }

  return b.String()
}

func parseUserSections(content string) map[string][]string {
  sections := make(map[string][]string)
  if content == "" {
    content = "# 用户信息\n\n"
  }

  currentSection := ""
  currentItems := []string{}

  lines := strings.Split(content, "\n")
  for _, line := range lines {
    trimmed := strings.TrimSpace(line)
    if strings.HasPrefix(trimmed, "## ") {
      if currentSection != "" {
        sections[currentSection] = currentItems
      }
      currentSection = strings.TrimSpace(trimmed[3:])
      currentItems = []string{}
    } else if strings.HasPrefix(trimmed, "- ") {
      item := strings.TrimSpace(trimmed[2:])
      if item != "" {
        currentItems = append(currentItems, item)
      }
    }
  }
  if currentSection != "" {
    sections[currentSection] = currentItems
  }

  return sections
}

func serializeUserMD(sections map[string][]string) string {
  if len(sections) == 0 {
    return "# 用户信息\n\n最后更新: " + time.Now().Format("2006-01-02 15:04") + "\n"
  }

  var b strings.Builder
  b.WriteString("# 用户信息\n\n")
  b.WriteString("最后更新: " + time.Now().Format("2006-01-02 15:04") + "\n\n")

  sectionOrder := []string{"工作偏好", "专业领域", "学习记录"}
  for _, name := range sectionOrder {
    items := sections[name]
    if len(items) == 0 {
      continue
    }
    b.WriteString("## " + name + "\n")
    for _, item := range items {
      b.WriteString("- " + item + "\n")
    }
    b.WriteString("\n")
  }

  return b.String()
}

// ============================================================
// 预算控制
// ============================================================

func roughTokenCount(s string) int {
  return len(s) / 4
}

func enforceMemoryBudget(content string, maxTokens int) string {
  for roughTokenCount(content) > maxTokens {
    sections := parseMemorySections(content)

    // 优先级：关键决策 > 项目知识 > 技术发现 > 活跃上下文/已完成 > 活跃上下文/进行中
    // 从低优先级章节开始移除一条（保留空章节，后续 evolution 可重新填充）
    removed := false
    for _, key := range []string{"活跃上下文/已完成", "活跃上下文/进行中", "活跃上下文/阻塞", "技术发现", "项目知识", "关键决策"} {
      items := sections[key]
      if len(items) == 0 {
        continue // 章节已空，尝试下一优先级
      }
      if len(items) == 1 {
        sections[key] = []string{} // 保留 key 但置空，不删除（章节可重新生长）
        removed = true
        break
      }
      sections[key] = items[1:] // 移除第一条（最旧）
      removed = true
      break
    }

    if !removed {
      break
    }

    content = serializeMemoryMD(sections)
  }
  return content
}

func enforceUserBudget(content string, maxTokens int) string {
  for roughTokenCount(content) > maxTokens {
    sections := parseUserSections(content)

    removed := false
    for _, key := range []string{"学习记录", "专业领域", "工作偏好"} {
      items := sections[key]
      if len(items) > 1 {
        sections[key] = items[1:]
        removed = true
        break
      }
      if len(items) == 1 {
        delete(sections, key)
        removed = true
        break
      }
    }

    if !removed {
      break
    }

    content = serializeUserMD(sections)
  }
  return content
}

// ============================================================
// 辅助函数
// ============================================================

func collectSessionMessages(store *SessionStore, key string, max int) []*Message {
  live, _ := store.LoadLive(key)
  archive, _ := store.LoadArchive(key)

  // 合并 live + archive，使用 role + content + toolCallID 去重（最多 200 条）
  // 先加载 live（最新会话），再加载 archive（历史存档），
  // 确保最相关的近期消息优先保留
  seen := make(map[string]bool)
  var all []*Message

  for _, m := range live {
    if len(all) >= max {
      break
    }
    k := messageDedupKey(m)
    if !seen[k] {
      seen[k] = true
      all = append(all, m)
    }
  }
  for _, m := range archive {
    if len(all) >= max {
      break
    }
    k := messageDedupKey(m)
    if !seen[k] {
      seen[k] = true
      all = append(all, m)
    }
  }

  return all
}

func messageDedupKey(m *Message) string {
  if len(m.ToolCalls) > 0 {
    return string(m.Role) + ":" + m.Content + ":" + m.ToolCalls[0].ID
  }
  return string(m.Role) + ":" + m.Content
}

func readFile(path string) string {
  data, err := os.ReadFile(path)
  if err != nil {
    if !os.IsNotExist(err) {
      slog.Warn("evolution.read_file_error", "path", path, "error", err.Error())
    }
    return ""
  }
  return strings.TrimSpace(string(data))
}

func cleanJSONResponse(raw string) string {
  raw = strings.TrimSpace(raw)
  // 移除各种代码 fence 包裹：```json, ```JSON, ```javascript, ``` 等
  for _, prefix := range []string{"```json", "```JSON", "```javascript", "```js", "```"} {
    raw = strings.TrimPrefix(raw, prefix)
  }
  raw = strings.TrimSuffix(raw, "```")
  raw = strings.TrimSpace(raw)
  return raw
}

func sectionMap(items []string) map[string]string {
  result := make(map[string]string)
  for _, item := range items {
    parts := splitSection(item)
    if len(parts) == 2 {
      result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
    } else {
      result[item] = item
    }
  }
  return result
}

// splitSection 按中文或英文冒号分割
func splitSection(s string) []string {
  if parts := strings.SplitN(s, "：", 2); len(parts) == 2 {
    return parts
  }
  return strings.SplitN(s, ":", 2)
}

func mapToSortedSlice(m map[string]string) []string {
  keys := make([]string, 0, len(m))
  for k := range m {
    keys = append(keys, k)
  }
  sort.Strings(keys)
  result := make([]string, 0, len(keys))
  for _, k := range keys {
    v := m[k]
    if v == k {
      result = append(result, k)
    } else {
      result = append(result, k+"："+v)
    }
  }
  return result
}

func sectionSlice(items []string) []string {
  if items == nil {
    return []string{}
  }
  return items
}

func contains(slice []string, item string) bool {
  for _, s := range slice {
    if s == item {
      return true
    }
  }
  return false
}

func removeItem(slice []string, item string) []string {
  var result []string
  for _, s := range slice {
    if s != item {
      result = append(result, s)
    }
  }
  return result
}

func truncateStr(s string, n int) string {
  runes := []rune(s)
  if len(runes) <= n {
    return s
  }
  return string(runes[:n])
}

// backupFile 备份文件到指定目录，文件名添加时间戳后缀
func backupFile(srcPath, backupDir string) error {
  if err := os.MkdirAll(backupDir, 0755); err != nil {
    return err
  }

  base := filepath.Base(srcPath)
  timestamp := time.Now().Format("20060102_150405")
  dst := filepath.Join(backupDir, base+"."+timestamp)

  data, err := os.ReadFile(srcPath)
  if err != nil {
    return err
  }
  return os.WriteFile(dst, data, 0600)
}

// cleanOldBackups 清理指定目录中超过 maxAge 的备份文件
// 备份文件名格式: {basename}.YYYYMMDD_HHMMSS
// 按文件名中的时间戳判断，而非文件 modtime（解决测试中无法设置 modtime 的问题）
func cleanOldBackups(backupDir string, maxAge time.Duration) error {
  entries, err := os.ReadDir(backupDir)
  if err != nil {
    if os.IsNotExist(err) {
      return nil
    }
    return err
  }

  cutoff := time.Now().Add(-maxAge)
  for _, entry := range entries {
    if entry.IsDir() {
      continue
    }
    name := entry.Name()
    // 提取时间戳部分（最后一个 '.' 之后的内容）
    dotIdx := strings.LastIndex(name, ".")
    if dotIdx < 0 || dotIdx+1 >= len(name) {
      continue
    }
    tsStr := name[dotIdx+1:]
    t, err := time.ParseInLocation("20060102_150405", tsStr, time.Local)
    if err != nil {
      continue
    }
    if t.Before(cutoff) {
      if err := os.Remove(filepath.Join(backupDir, name)); err != nil {
        slog.Warn("evolution.remove_backup_error", "file", name, "error", err.Error())
      }
    }
  }
  return nil
}
