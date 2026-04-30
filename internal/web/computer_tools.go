package web

// computerToolDefs Computer Use MCP 工具列表
var computerToolDefs = []ToolDef{
  {
    Name:        "computer_screenshot",
    Description: "截取用户桌面的屏幕截图，返回 base64 PNG",
    InputSchema: map[string]interface{}{
      "type":       "object",
      "properties": map[string]interface{}{},
    },
  },
  {
    Name:        "computer_screen_size",
    Description: "获取桌面屏幕分辨率",
    InputSchema: map[string]interface{}{
      "type":       "object",
      "properties": map[string]interface{}{},
    },
  },
  {
    Name:        "computer_mouse_click",
    Description: "在桌面指定坐标执行鼠标点击",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "x": map[string]interface{}{"type": "integer", "description": "X 坐标"},
        "y": map[string]interface{}{"type": "integer", "description": "Y 坐标"},
        "button": map[string]interface{}{
          "type":        "string",
          "description": "鼠标按钮: left(默认), right, middle",
          "enum":        []string{"left", "right", "middle"},
        },
        "clicks": map[string]interface{}{
          "type":        "integer",
          "description": "点击次数，默认 1（双击设为 2）",
        },
      },
      "required": []string{"x", "y"},
    },
  },
  {
    Name:        "computer_mouse_move",
    Description: "移动鼠标到桌面指定坐标",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "x": map[string]interface{}{"type": "integer", "description": "目标 X 坐标"},
        "y": map[string]interface{}{"type": "integer", "description": "目标 Y 坐标"},
      },
      "required": []string{"x", "y"},
    },
  },
  {
    Name:        "computer_mouse_drag",
    Description: "从起点拖拽鼠标到终点",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "startX": map[string]interface{}{"type": "integer", "description": "起点 X"},
        "startY": map[string]interface{}{"type": "integer", "description": "起点 Y"},
        "endX":   map[string]interface{}{"type": "integer", "description": "终点 X"},
        "endY":   map[string]interface{}{"type": "integer", "description": "终点 Y"},
      },
      "required": []string{"startX", "startY", "endX", "endY"},
    },
  },
  {
    Name:        "computer_mouse_scroll",
    Description: "在指定位置滚动鼠标滚轮",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "x":       map[string]interface{}{"type": "integer", "description": "X 坐标"},
        "y":       map[string]interface{}{"type": "integer", "description": "Y 坐标"},
        "scrollX": map[string]interface{}{"type": "integer", "description": "水平滚动量"},
        "scrollY": map[string]interface{}{"type": "integer", "description": "垂直滚动量（正值向下）"},
      },
      "required": []string{"scrollY"},
    },
  },
  {
    Name:        "computer_keyboard_type",
    Description: "输入文字到桌面当前焦点元素",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "text": map[string]interface{}{"type": "string", "description": "要输入的文字"},
      },
      "required": []string{"text"},
    },
  },
  {
    Name:        "computer_keyboard_press",
    Description: "按下键盘组合键（如 Ctrl+C, Enter, Escape）",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "keys": map[string]interface{}{
          "type":        "array",
          "items":       map[string]interface{}{"type": "string"},
          "description": "按键组合，如 [\"ctrl\",\"c\"] 或 [\"enter\"]",
        },
      },
      "required": []string{"keys"},
    },
  },
  {
    Name:        "computer_file_read",
    Description: "读取用户桌面上的文件内容",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "path": map[string]interface{}{"type": "string", "description": "文件绝对路径"},
      },
      "required": []string{"path"},
    },
  },
  {
    Name:        "computer_file_write",
    Description: "写入内容到用户桌面上的文件",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "path":    map[string]interface{}{"type": "string", "description": "文件绝对路径"},
        "content": map[string]interface{}{"type": "string", "description": "文件内容"},
      },
      "required": []string{"path", "content"},
    },
  },
  {
    Name:        "computer_file_list",
    Description: "列出指定目录下的文件和子目录",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "path": map[string]interface{}{"type": "string", "description": "目录绝对路径"},
      },
      "required": []string{"path"},
    },
  },
}
