package agent

// ============================================================
// 结构化输出（参考 OpenCode prompt.ts 设计模式）
// ============================================================

// StructuredOutputConfig JSON Schema 结构化输出配置
type StructuredOutputConfig struct {
  Enabled     bool
  Schema      map[string]interface{}
  RetryCount  int
}

// BuildStructuredOutputTool 构建结构化输出工具定义
// 模式: 注入一个特殊的工具，强制 LLM 以 tool_choice 方式输出 JSON
func BuildStructuredOutputTool(schema map[string]interface{}) *ToolDef {
  return &ToolDef{
    Name:        "StructuredOutput",
    Description: "以指定的 JSON Schema 格式输出结构化结果。必须使用此工具来格式化你的回复。",
    InputSchema: schema,
  }
}
