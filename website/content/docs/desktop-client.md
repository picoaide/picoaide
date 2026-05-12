---
title: "桌面客户端"
description: "PicoAide 桌面客户端的安装、连接、权限配置和 Computer MCP 工具详解"
weight: 10
draft: false
---

桌面客户端是 computer MCP 的执行端。它运行在用户的本地计算机上，让 AI 可以操作桌面环境——包括截屏、鼠标控制、键盘输入、OCR 识别和白名单范围内的文件操作。

桌面客户端支持 Windows、macOS 和 Linux。

## 安装与运行

### 环境要求

- Python 3.10 或更新版本
- pip（Python 包管理器）

### 安装步骤

```bash
# 克隆仓库（或从管理员处获取 picoaide-desktop 目录）
git clone https://github.com/picoaide/picoaide.git
cd picoaide/picoaide-desktop

# 安装依赖
pip install -r requirements.txt

# 运行客户端
python main.py
```

### 主要模块

| 文件 | 说明 |
|------|------|
| `core/connection.py` | 连接 PicoAide Server |
| `core/executor.py` | 执行桌面工具 |
| `core/permissions.py` | 权限控制 |
| `core/config.py` | 本地配置 |
| `ui/main_window.py` | 主窗口 |
| `ui/login_window.py` | 登录窗口 |

## 连接流程

```text
1. 启动桌面客户端
2. 客户端显示登录窗口
3. 输入 PicoAide 服务器地址和普通用户账号
4. 登录成功后自动获取 MCP Token
5. 客户端通过 WebSocket 连接到 PicoAide Server
6. 服务端登记该用户的 computer 执行端
7. AI Agent 可以通过 MCP 协议调用桌面工具
```

如果客户端关闭或网络断开，AI 调用 computer 工具会得到代理未连接错误。

## 权限组

桌面客户端提供按组开关的权限控制。你可以只开放当前任务需要的能力，不需要的功能保持关闭。

| 权限组 | 包含的工具 | 说明 |
|--------|-----------|------|
| 屏幕截图 | `computer_screenshot` | 截取屏幕，返回 base64 PNG 图片 |
| 屏幕信息 | `computer_screen_size`, `computer_active_window` | 获取屏幕尺寸和当前活跃窗口标题 |
| OCR | `computer_screen_text` | 识别屏幕上的文字和坐标 |
| 鼠标控制 | `computer_mouse_click`, `computer_mouse_move`, `computer_mouse_drag`, `computer_mouse_scroll` | 模拟鼠标操作 |
| 键盘控制 | `computer_keyboard_type`, `computer_keyboard_press` | 模拟键盘输入 |
| 文件操作 | `computer_file_read`, `computer_file_write`, `computer_file_list`, `computer_file_search`, `computer_whitelist` | 在白名单目录内的文件操作 |

### 权限推荐策略

- **日常办公**：截图 + 屏幕信息 + 键盘控制
- **开发工作**：截图 + 键盘控制 + 文件操作（限定项目目录）
- **数据录入**：截图 + OCR + 鼠标控制 + 键盘控制
- **全功能**：所有权限组开启（仅在你完全信任 AI 时使用）

## 文件白名单

出于安全考虑，桌面文件操作不能读取整个磁盘。AI 必须先调用 `computer_whitelist` 获取可访问目录列表。

### 配置白名单

在桌面客户端的设置中可以配置文件白名单目录：

```text
允许 AI 访问的目录：
- /Users/yourname/Documents/work/
- /Users/yourname/Downloads/reports/
- /Users/yourname/Desktop/projects/
```

### AI 访问文件的流程

```text
1. AI 调用 computer_whitelist 获取可访问的目录列表
2. AI 调用 computer_file_list 浏览白名单内的目录
3. AI 调用 computer_file_read 读取需要的文件
4. AI 调用 computer_file_write 写入处理结果
5. 所有操作都被限制在白名单目录范围内
```

### 安全建议

- 只开放当前任务需要的最小目录范围
- 不要将整个用户目录加入白名单
- 敏感数据目录（如 `~/.ssh/`、`~/.aws/`）不应该加入白名单

## Computer MCP 工具列表

| 工具 | 说明 | 典型使用场景 |
|------|------|-------------|
| `computer_screenshot` | 截取屏幕，返回 base64 PNG | AI 分析屏幕内容 |
| `computer_screen_size` | 获取屏幕尺寸 | AI 了解显示区域大小 |
| `computer_active_window` | 获取当前活跃窗口标题 | AI 了解用户正在使用的应用 |
| `computer_mouse_click` | 点击指定坐标 | AI 模拟鼠标点击按钮 |
| `computer_mouse_move` | 移动鼠标 | AI 移动鼠标到目标位置 |
| `computer_mouse_drag` | 鼠标拖拽 | AI 拖拽文件或选择区域 |
| `computer_mouse_scroll` | 滚动鼠标滚轮 | AI 滚动页面或列表 |
| `computer_keyboard_type` | 输入文本 | AI 在文本框中输入内容 |
| `computer_keyboard_press` | 按下按键组合 | AI 发送快捷键（如 Ctrl+S） |
| `computer_screen_text` | OCR 识别屏幕文字和坐标 | AI 读取屏幕上的文本 |
| `computer_file_read` | 读取白名单内文件 | AI 读取配置文件 |
| `computer_file_write` | 写入白名单内文件 | AI 保存处理结果 |
| `computer_file_list` | 列出目录内容 | AI 浏览文件夹中的文件 |
| `computer_whitelist` | 获取白名单目录 | AI 确认可访问路径 |
| `computer_file_search` | 按文件名搜索 | AI 在目录中查找文件 |

## 典型使用场景

### AI 填写桌面应用表单

```text
1. AI 调用 computer_screenshot 看屏幕
2. AI 调用 computer_screen_text 识别表单字段位置
3. AI 通过坐标调用 computer_mouse_click 聚焦输入框
4. AI 调用 computer_keyboard_type 输入内容
5. AI 调用 computer_mouse_click 点击提交按钮
```

### AI 整理文件

```text
1. AI 调用 computer_whitelist 获取可访问目录
2. AI 调用 computer_file_list 浏览目录内容
3. AI 调用 computer_file_read 读取文件内容
4. AI 分析后调用 computer_file_write 写入整理结果
5. AI 调用 computer_file_search 确认文件已归类
```

## 安全建议

- **最小权限原则**：只开启当前任务需要的权限组
- **文件先查白名单**：AI 操作文件前调用 `computer_whitelist` 确认路径
- **随时可断开**：鼠标和键盘工具会操作用户真实桌面，你应能随时关闭客户端
- **OCR 辅助决策**：OCR 结果给 AI 提供坐标辅助，不代表 AI 可以绕过权限判断
- **工作结束后断开**：完成 AI 任务后关闭桌面客户端，避免不必要的访问
