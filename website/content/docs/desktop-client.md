---
title: "桌面客户端"
description: "PicoAide 桌面客户端的连接方式、权限组、白名单和 computer MCP 工具"
weight: 5
draft: false
---

桌面客户端是 computer MCP 的执行端。它运行在用户本机，负责截图、鼠标、键盘、OCR 和白名单范围内的文件操作。

## 安装与运行

桌面客户端代码位于 `picoaide-desktop/`，依赖 Python：

```bash
cd picoaide-desktop
pip install -r requirements.txt
python main.py
```

主要模块：

| 文件 | 说明 |
| --- | --- |
| `core/connection.py` | 连接 PicoAide Server |
| `core/executor.py` | 执行桌面工具 |
| `core/permissions.py` | 权限控制 |
| `core/config.py` | 本地配置 |
| `ui/main_window.py` | 主窗口 |
| `ui/login_window.py` | 登录窗口 |

## 连接流程

```text
用户登录桌面客户端
  -> 获取 MCP token
  -> 连接 /api/computer/ws?token=...
  -> PicoAide Server 登记该用户的 computer 执行端
  -> AI 容器调用 /api/mcp/sse/computer
  -> 命令经 WebSocket 转发到桌面客户端
```

如果客户端关闭或网络断开，AI 调用 computer 工具会得到代理未连接错误。

## 权限组

桌面客户端提供按组开关的权限。用户可以只开放必要能力。

| 权限组 | 工具 |
| --- | --- |
| 屏幕截图 | `computer_screenshot` |
| 屏幕信息 | `computer_screen_size`, `computer_active_window` |
| OCR | `computer_screen_text` |
| 鼠标控制 | `computer_mouse_click`, `computer_mouse_move`, `computer_mouse_drag`, `computer_mouse_scroll` |
| 键盘控制 | `computer_keyboard_type`, `computer_keyboard_press` |
| 文件操作 | `computer_file_read`, `computer_file_write`, `computer_file_list`, `computer_file_search`, `computer_whitelist` |

## 文件白名单

桌面文件操作不应开放整个磁盘。AI 应先调用：

```text
computer_whitelist
```

获取可访问目录，然后再读写或搜索文件。客户端侧会限制文件工具只能访问白名单目录。

## Computer MCP 工具

| 工具 | 说明 |
| --- | --- |
| `computer_screenshot` | 截取屏幕，返回 base64 PNG |
| `computer_screen_size` | 获取屏幕尺寸 |
| `computer_active_window` | 获取当前活跃窗口标题 |
| `computer_mouse_click` | 点击指定坐标 |
| `computer_mouse_move` | 移动鼠标 |
| `computer_mouse_drag` | 鼠标拖拽 |
| `computer_mouse_scroll` | 滚动鼠标滚轮 |
| `computer_keyboard_type` | 输入文本 |
| `computer_keyboard_press` | 按下按键组合 |
| `computer_screen_text` | OCR 识别屏幕文字和坐标 |
| `computer_file_read` | 读取白名单内文件 |
| `computer_file_write` | 写入白名单内文件 |
| `computer_file_list` | 列出目录 |
| `computer_whitelist` | 获取白名单目录 |
| `computer_file_search` | 按文件名搜索 |

## 使用建议

- 只开启当前任务需要的权限组。
- 文件工具先调用 `computer_whitelist`。
- 鼠标和键盘工具会操作用户真实桌面，用户应能随时断开客户端。
- OCR 结果给 AI 提供坐标辅助，不替代真实权限判断。
