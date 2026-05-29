package agent

import (
  "context"
  "encoding/json"
  "fmt"
  "os"
  "path/filepath"
  "strings"
)

// ============================================================
// update_memory — 主动更新长期记忆
// ============================================================

type UpdateMemoryTool struct {
  Workspace string
}

type updateMemoryParams struct {
  Section string            `json:"section"`
  Action  string            `json:"action"`
  Entries []updateEntry     `json:"entries"`
}

type updateEntry struct {
  Topic     string `json:"topic"`
  Content   string `json:"content"`
  Rationale string `json:"rationale,omitempty"`
}

func (t *UpdateMemoryTool) Name() string { return "update_memory" }

func (t *UpdateMemoryTool) Description() string {
  return "主动更新长期记忆。用于记录重要决策、知识点、进度状态或用户偏好。数据会被合并到 MEMORY.md 或 USER.md"
}

func (t *UpdateMemoryTool) Schema() map[string]interface{} {
  return map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
      "section": map[string]interface{}{
        "type":        "string",
        "enum":        []string{"decisions", "knowledge", "progress", "preferences"},
        "description": "要更新的章节：decisions/knowledge/progress -> MEMORY.md, preferences -> USER.md",
      },
      "action": map[string]interface{}{
        "type":        "string",
        "enum":        []string{"add", "update", "delete"},
        "description": "操作类型：add 追加新条目，update 按 topic 更新，delete 按 topic 删除",
      },
      "entries": map[string]interface{}{
        "type": "array",
        "items": map[string]interface{}{
          "type": "object",
          "properties": map[string]interface{}{
            "topic":     map[string]interface{}{"type": "string", "description": "条目主题（用于去重和匹配）"},
            "content":   map[string]interface{}{"type": "string", "description": "条目内容"},
            "rationale": map[string]interface{}{"type": "string", "description": "决策理由（仅 decisions 需要）"},
          },
          "required": []string{"topic", "content"},
        },
        "description": "条目列表",
      },
    },
    "required": []string{"section", "action", "entries"},
  }
}

func (t *UpdateMemoryTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
  var params updateMemoryParams
  if err := json.Unmarshal(args, &params); err != nil {
    return &ToolResult{Success: false, Data: "参数解析失败：需要 section、action 和 entries"}, nil
  }

  // 校验参数
  validSections := map[string]bool{"decisions": true, "knowledge": true, "progress": true, "preferences": true}
  validActions := map[string]bool{"add": true, "update": true, "delete": true}

  if !validSections[params.Section] {
    return &ToolResult{Success: false, Data: fmt.Sprintf("无效的 section: %q，可选值: decisions, knowledge, progress, preferences", params.Section)}, nil
  }
  if !validActions[params.Action] {
    return &ToolResult{Success: false, Data: fmt.Sprintf("无效的 action: %q，可选值: add, update, delete", params.Action)}, nil
  }
  if len(params.Entries) == 0 {
    return &ToolResult{Success: false, Data: "entries 不能为空"}, nil
  }

  // 过滤空条目
  var validEntries []updateEntry
  for _, e := range params.Entries {
    if strings.TrimSpace(e.Topic) != "" || strings.TrimSpace(e.Content) != "" {
      validEntries = append(validEntries, e)
    }
  }
  if len(validEntries) == 0 {
    return &ToolResult{Success: false, Data: "所有条目均为空"}, nil
  }
  params.Entries = validEntries

  // 确定目标文件
  isUserSection := params.Section == "preferences"
  filePath := filepath.Join(t.Workspace, "USER.md")
  if !isUserSection {
    filePath = filepath.Join(t.Workspace, "memory", "MEMORY.md")
  }

  // 读取当前内容
  existing := readFile(filePath)

  // 文件存在时才备份
  if existing != "" {
    backupDir := filepath.Join(t.Workspace, "memory", "archive")
    backupFile(filePath, backupDir)
  }

  // 执行操作
  var newContent string
  var affected int
  switch params.Action {
  case "add":
    newContent = executeAdd(existing, params, isUserSection)
    affected = len(params.Entries)
  case "update":
    newContent, affected = executeUpdate(existing, params, isUserSection)
  case "delete":
    newContent, affected = executeDelete(existing, params, isUserSection)
  }

  // 写回文件
  if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
    return &ToolResult{Success: false, Data: fmt.Sprintf("创建目录失败: %v", err)}, nil
  }
  if err := os.WriteFile(filePath, []byte(newContent), 0600); err != nil {
    return &ToolResult{Success: false, Data: fmt.Sprintf("写入文件失败: %v", err)}, nil
  }

  summary := fmt.Sprintf("已%s %d 条记录到 %s", actionLabel(params.Action), affected, sectionLabel(params.Section))
  if affected < len(params.Entries) && params.Action != "add" {
    summary += fmt.Sprintf("（其中 %d 条未匹配到现有条目）", len(params.Entries)-affected)
  }
  return &ToolResult{Success: true, Data: summary}, nil
}

func actionLabel(action string) string {
  switch action {
  case "add":
    return "添加"
  case "update":
    return "更新"
  case "delete":
    return "删除"
  }
  return action
}

func sectionLabel(section string) string {
  switch section {
  case "decisions":
    return "关键决策"
  case "knowledge":
    return "项目知识"
  case "progress":
    return "进度"
  case "preferences":
    return "工作偏好"
  }
  return section
}

// executeAdd 使用合并引擎追加条目
func executeAdd(existing string, params updateMemoryParams, isUser bool) string {
  result := EvolutionResult{}

  for _, e := range params.Entries {
    switch params.Section {
    case "decisions":
      result.Decisions = append(result.Decisions, Decision{
        Topic:     e.Topic,
        Decision:  e.Content,
        Rationale: e.Rationale,
      })
    case "knowledge":
      result.Knowledge = append(result.Knowledge, Knowledge{
        Topic: e.Topic,
        Fact:  e.Content,
      })
    case "progress":
      progressSub := progressSubSection(e.Topic)
      switch progressSub {
      case "completed":
        result.Progress.Completed = append(result.Progress.Completed, e.Content)
      case "blocked":
        result.Progress.Blocked = append(result.Progress.Blocked, e.Content)
      default:
        result.Progress.InProgress = append(result.Progress.InProgress, e.Content)
      }
    case "preferences":
      result.Preferences = append(result.Preferences, Preference{
        Aspect:      e.Topic,
        Description: e.Content,
      })
    }
  }

  if isUser {
    return MergeIntoUserMD(existing, result, 2000)
  }
  return MergeIntoMemoryMD(existing, result, 5000)
}

// executeUpdate 按 topic 更新已有条目，返回（新内容, 实际更新数）
func executeUpdate(existing string, params updateMemoryParams, isUser bool) (string, int) {
  if existing == "" {
    return executeAdd(existing, params, isUser), 0
  }

  if params.Section == "progress" {
    return existing, 0 // progress 条目不支持按 topic 更新
  }

  if isUser {
    sections := parseUserSections(existing)
    prefs := sectionMap(sections["工作偏好"])
    count := 0
    for _, e := range params.Entries {
      if e.Content != "" {
        prefs[e.Topic] = e.Content
        count++
      }
    }
    sections["工作偏好"] = mapToSortedSlice(prefs)
    return serializeUserMD(sections), count
  }

  // MEMORY.md — 按 section 定位
  sections := parseMemorySections(existing)
  sectionKey := memorySectionKey(params.Section)
  topicMap := sectionMap(sections[sectionKey])
  count := 0
  for _, e := range params.Entries {
    if _, exists := topicMap[e.Topic]; exists && e.Content != "" {
      entry := e.Content
      if e.Rationale != "" {
        entry = e.Content + "：" + e.Rationale
      }
      topicMap[e.Topic] = entry
      count++
    }
  }
  sections[sectionKey] = mapToSortedSlice(topicMap)
  return serializeMemoryMD(sections), count
}

// executeDelete 按 topic 删除条目，返回（新内容, 实际删除数）
func executeDelete(existing string, params updateMemoryParams, isUser bool) (string, int) {
  if existing == "" {
    if isUser {
      return "# 用户信息\n\n", 0
    }
    return "# 长期记忆\n\n", 0
  }

  if isUser {
    sections := parseUserSections(existing)
    prefs := sectionMap(sections["工作偏好"])
    count := 0
    for _, e := range params.Entries {
      if _, exists := prefs[e.Topic]; exists {
        delete(prefs, e.Topic)
        count++
      }
    }
    sections["工作偏好"] = mapToSortedSlice(prefs)
    return serializeUserMD(sections), count
  }

  sections := parseMemorySections(existing)
  count := 0

  if params.Section == "progress" {
    // 按 subsection + content 匹配删除（与 add 行为的 topic=子章节, content=条目文本一致）
    for _, e := range params.Entries {
      item := e.Content
      if item == "" {
        item = e.Topic // 向下兼容：仅传 topic 时视为匹配内容
      }
      sub := progressSubSection(e.Topic)
      switch sub {
      case "completed":
        oldLen := len(sections["活跃上下文/已完成"])
        sections["活跃上下文/已完成"] = removeItem(sections["活跃上下文/已完成"], item)
        if len(sections["活跃上下文/已完成"]) < oldLen {
          count++
        }
      case "blocked":
        oldLen := len(sections["活跃上下文/阻塞"])
        sections["活跃上下文/阻塞"] = removeItem(sections["活跃上下文/阻塞"], item)
        if len(sections["活跃上下文/阻塞"]) < oldLen {
          count++
        }
      default:
        oldLen := len(sections["活跃上下文/进行中"])
        sections["活跃上下文/进行中"] = removeItem(sections["活跃上下文/进行中"], item)
        if len(sections["活跃上下文/进行中"]) < oldLen {
          count++
        }
      }
    }
    return serializeMemoryMD(sections), count
  }

  sectionKey := memorySectionKey(params.Section)
  topicMap := sectionMap(sections[sectionKey])
  for _, e := range params.Entries {
    if _, exists := topicMap[e.Topic]; exists {
      delete(topicMap, e.Topic)
      count++
    }
  }
  sections[sectionKey] = mapToSortedSlice(topicMap)
  return serializeMemoryMD(sections), count
}

// progressSubSection 将 topic 映射到 progress 的子章节名
func progressSubSection(topic string) string {
  switch topic {
  case "completed", "已完成":
    return "completed"
  case "blocked", "阻塞":
    return "blocked"
  default:
    return "in_progress"
  }
}

// memorySectionKey 映射 section 到 MEMORY.md 的 ## 章节名
func memorySectionKey(section string) string {
  switch section {
  case "decisions":
    return "关键决策"
  case "knowledge":
    return "项目知识"
  case "progress":
    return "活跃上下文"
  }
  return "关键决策"
}
