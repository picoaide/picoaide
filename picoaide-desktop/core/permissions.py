"""权限定义和管理"""

# 工具分组定义：key -> 分组信息
PERMISSION_GROUPS = {
  "screenshot": {
    "label": "屏幕截图",
    "desc": "允许 AI 截取屏幕截图和获取屏幕分辨率",
    "icon": "🖥",
    "tools": ["computer_screenshot", "computer_screen_size", "computer_active_window"],
    "default": True,
  },
  "mouse": {
    "label": "鼠标控制",
    "desc": "允许 AI 控制鼠标点击、移动、拖拽和滚动",
    "icon": "🖱",
    "tools": ["computer_mouse_click", "computer_mouse_move",
              "computer_mouse_drag", "computer_mouse_scroll"],
    "default": True,
  },
  "keyboard": {
    "label": "键盘控制",
    "desc": "允许 AI 输入文字和按下组合键",
    "icon": "⌨",
    "tools": ["computer_keyboard_type", "computer_keyboard_press"],
    "default": True,
  },
  "file_read": {
    "label": "文件读取",
    "desc": "允许 AI 读取白名单目录内的文件（支持 txt/json/csv/xlsx/docx 等）",
    "icon": "📄",
    "tools": ["computer_file_read"],
    "default": True,
    "need_whitelist": True,
  },
  "file_write": {
    "label": "文件写入",
    "desc": "允许 AI 在白名单目录内创建和修改文件",
    "icon": "✏",
    "tools": ["computer_file_write"],
    "default": False,
    "need_whitelist": True,
  },
  "file_list": {
    "label": "目录浏览",
    "desc": "允许 AI 列出白名单目录内的文件和子目录",
    "icon": "📁",
    "tools": ["computer_file_list", "computer_whitelist", "computer_file_search"],
    "default": True,
    "need_whitelist": True,
  },
}

# 构建 tool -> group 反向映射
TOOL_GROUP_MAP = {}
for _group_key, _group_info in PERMISSION_GROUPS.items():
  for _tool in _group_info["tools"]:
    TOOL_GROUP_MAP[_tool] = _group_key


def get_default_permissions():
  """返回默认权限配置"""
  return {key: info["default"] for key, info in PERMISSION_GROUPS.items()}


def is_tool_allowed(tool_name, permissions):
  """检查某个工具是否被授权"""
  group_key = TOOL_GROUP_MAP.get(tool_name)
  if group_key is None:
    return False
  return permissions.get(group_key, False)


def get_allowed_tools(permissions):
  """获取所有被授权的工具名列表"""
  tools = []
  for group_key, enabled in permissions.items():
    if enabled and group_key in PERMISSION_GROUPS:
      tools.extend(PERMISSION_GROUPS[group_key]["tools"])
  return tools
