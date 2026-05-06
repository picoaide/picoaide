---
title: "桌面客户端"
description: "PicoAide 桌面客户端使用指南"
weight: 5
draft: false
---

PicoAide 桌面客户端让 AI 能够控制用户的桌面环境，包括鼠标键盘操作、文件读写和屏幕截图。

## 下载与安装

从 PicoAide 官网或 GitHub Releases 下载桌面客户端安装包。

```bash
# 下载最新版本
# Linux
curl -L -o picoaide-desktop https://github.com/picoaide/picoaide/releases/latest/download/picoaide-desktop-linux

# 添加执行权限
chmod +x picoaide-desktop
```

## 登录与连接

启动桌面客户端后，输入 PicoAide 服务器地址和登录凭证：

1. 填写服务器地址（如 `http://192.168.1.100:80`）
2. 输入用户名和密码
3. 点击「连接」

客户端会自动获取 MCP Token 并通过 WebSocket 建立与 PicoAide Server 的长连接。连接成功后，AI 即可通过 MCP 协议调用桌面操作工具。

## 权限组

桌面客户端提供 6 个权限组，用户可以根据需要选择开放的控制范围：

| 权限组         | 说明                                   | 包含的工具             |
| -------------- | -------------------------------------- | ---------------------- |
| 屏幕截图       | 允许 AI 截取屏幕画面                   | `computer_screenshot`  |
| 屏幕文字识别   | 允许 AI 通过 OCR 识别屏幕文字          | `computer_screen_text` |
| 屏幕信息       | 允许 AI 获取屏幕尺寸和窗口信息         | `computer_screen_size`, `computer_active_window` |
| 鼠标控制       | 允许 AI 控制鼠标点击、移动、拖拽、滚动 | `computer_mouse_click`, `computer_mouse_move`, `computer_mouse_drag`, `computer_mouse_scroll` |
| 键盘控制       | 允许 AI 输入文字和按下组合键           | `computer_keyboard_type`, `computer_keyboard_press` |
| 文件操作       | 允许 AI 读写白名单目录内的文件         | `computer_file_read`, `computer_file_write`, `computer_file_list`, `computer_file_search` |

用户可以自由组合这些权限组，精确控制 AI 可以执行的桌面操作。

## 白名单目录

文件操作权限受白名单目录限制。AI 只能访问白名单内的目录：

```
# 默认白名单目录
/home/username/Documents
/home/username/Downloads
/home/username/Desktop
```

AI 在执行文件操作前，应先调用 `computer_whitelist` 工具了解可访问的目录范围。

## 可用工具

桌面客户端提供 15 个 MCP 工具：

### 屏幕信息工具

| 工具                     | 说明                                   |
| ------------------------ | -------------------------------------- |
| `computer_screenshot`    | 截取用户桌面屏幕截图，返回 base64 PNG  |
| `computer_screen_size`   | 获取桌面屏幕分辨率                     |
| `computer_active_window` | 获取当前活跃窗口的标题                 |
| `computer_screen_text`   | 通过 OCR 识别屏幕文字及坐标位置        |

### 鼠标控制工具

| 工具                     | 说明                                   |
| ------------------------ | -------------------------------------- |
| `computer_mouse_click`   | 在指定坐标执行鼠标点击                 |
| `computer_mouse_move`    | 移动鼠标到指定坐标                     |
| `computer_mouse_drag`    | 从起点拖拽鼠标到终点                   |
| `computer_mouse_scroll`  | 在指定位置滚动鼠标滚轮                 |

### 键盘控制工具

| 工具                       | 说明                                 |
| -------------------------- | ------------------------------------ |
| `computer_keyboard_type`   | 输入文字到当前焦点元素               |
| `computer_keyboard_press`  | 按下键盘组合键（如 Ctrl+C, Enter）  |

### 文件操作工具

| 工具                     | 说明                                     |
| ------------------------ | ---------------------------------------- |
| `computer_file_read`     | 读取桌面文件内容                         |
| `computer_file_write`    | 写入内容到桌面文件                       |
| `computer_file_list`     | 列出指定目录下的文件和子目录             |
| `computer_file_search`   | 在白名单目录内搜索文件                   |
| `computer_whitelist`     | 获取允许访问的白名单目录列表             |

## 工作原理

桌面客户端的工作流程：

```
AI 容器发起 MCP 工具调用
    │
    ▼
PicoAide Server (SSE 接收请求)
    │
    ▼
查找用户的桌面客户端 WebSocket 连接
    │
    ▼
通过 WebSocket 转发命令到桌面客户端
    │
    ▼
桌面客户端在本地执行操作
（截图、鼠标、键盘、文件操作）
    │
    ▼
客户端将结果返回 PicoAide Server
    │
    ▼
Server 通过 SSE 将结果返回给 AI
```

桌面客户端始终在用户本地运行，所有操作都在用户的桌面环境中执行。用户可以随时关闭客户端断开 AI 的桌面访问权限。
