package web

// ToolDef 描述一个 MCP 工具
type ToolDef struct {
  Name        string                 `json:"name"`
  Description string                 `json:"description"`
  InputSchema map[string]interface{} `json:"inputSchema"`
}

// toolDefs 是 MCP tools/list 返回的完整工具列表
var toolDefs = []ToolDef{
  {
    Name:        "browser_navigate",
    Description: "导航当前标签页到指定 URL",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "url": map[string]interface{}{"type": "string", "description": "目标 URL"},
      },
      "required": []string{"url"},
    },
  },
  {
    Name:        "browser_screenshot",
    Description: "截取当前标签页的屏幕截图，返回 base64 PNG",
    InputSchema: map[string]interface{}{
      "type":       "object",
      "properties": map[string]interface{}{},
    },
  },
  {
    Name:        "browser_click",
    Description: "通过 CSS 选择器点击页面元素",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "selector": map[string]interface{}{"type": "string", "description": "CSS 选择器"},
      },
      "required": []string{"selector"},
    },
  },
  {
    Name:        "browser_type",
    Description: "在指定元素中输入文字",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "selector": map[string]interface{}{"type": "string", "description": "CSS 选择器"},
        "text":     map[string]interface{}{"type": "string", "description": "要输入的文字"},
      },
      "required": []string{"selector", "text"},
    },
  },
  {
    Name:        "browser_get_content",
    Description: "获取页面文本内容，可指定选择器获取特定元素",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "selector": map[string]interface{}{"type": "string", "description": "CSS 选择器，默认为 body"},
      },
    },
  },
  {
    Name:        "browser_execute",
    Description: "在页面中执行 JavaScript 代码",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "script": map[string]interface{}{"type": "string", "description": "要执行的 JavaScript 代码"},
      },
      "required": []string{"script"},
    },
  },
  {
    Name:        "browser_tabs_list",
    Description: "列出所有打开的标签页",
    InputSchema: map[string]interface{}{
      "type":       "object",
      "properties": map[string]interface{}{},
    },
  },
  {
    Name:        "browser_tab_new",
    Description: "新建标签页，可指定初始 URL",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "url": map[string]interface{}{"type": "string", "description": "初始 URL（可选）"},
      },
    },
  },
  {
    Name:        "browser_tab_close",
    Description: "关闭指定标签页",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "tabId": map[string]interface{}{"type": "integer", "description": "标签页 ID"},
      },
      "required": []string{"tabId"},
    },
  },
  {
    Name:        "browser_go_back",
    Description: "浏览器后退",
    InputSchema: map[string]interface{}{
      "type":       "object",
      "properties": map[string]interface{}{},
    },
  },
  {
    Name:        "browser_wait",
    Description: "等待页面中指定元素出现",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "selector": map[string]interface{}{"type": "string", "description": "CSS 选择器"},
        "timeout":  map[string]interface{}{"type": "integer", "description": "超时毫秒数，默认 10000"},
      },
      "required": []string{"selector"},
    },
  },
}

// GetToolList 返回 MCP tools/list 的工具列表
func GetToolList() []ToolDef {
  return toolDefs
}
