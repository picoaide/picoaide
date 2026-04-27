# PicoAide 浏览器助手扩展方案

## 1. 目标

用 Chrome 扩展替代现有的代理工具 + SSH 隧道 + 远程调试方案，将用户连接浏览器的操作从 7 步简化为 1 步（装扩展 + 点连接）。

## 2. 架构总览

```
用户电脑                              服务器
┌───────────────────┐              ┌──────────────────────────┐
│ Chrome 浏览器      │              │ 宿主机                    │
│  └─ PicoAide 扩展  │──WebSocket──│  └─ PicoAide API (:80)   │
│                    │              │        │                  │
│  用户正常使用浏览器  │              │        │ HTTP（Bearer 鉴权） │
│  AI 直接操作它     │              │        ↓                  │
│                    │              │  └─ 容器 (zhangsan)       │
│                    │              │      └─ PicoClaw AI      │
└───────────────────┘              └──────────────────────────┘
```

### 数据流

```
AI 发指令 → HTTP(Bearer token) → Web服务器 → WebSocket(session) → 扩展 → chrome.debugger API → 用户浏览器
                                                                                     ↓
AI 收到结果 ← HTTP响应           ← Web服务器 ← WebSocket         ← 扩展 ← 执行结果
```

## 3. 三层鉴权

### 3.1 扩展 → Web 服务器（WebSocket 连接）

**方式：session cookie**

- 扩展连接 WebSocket 时，携带 PicoAide API 的 session cookie
- Web 服务器用现有的 `getSessionUser()` 识别用户身份
- 每个 WebSocket 连接绑定用户名，存入连接表

```
扩展连接: wss://picoaide.example.com/ws/connect
Cookie: session=zhangsan:1713836400:a1b2c3...
服务器验证: parseSessionToken() → zhangsan
```

### 3.2 容器 → Web 服务器（HTTP 请求）

**方式：per-container Bearer token**

- `initUser` 时为每个用户生成 32 字节随机 token
- Token 写入两个地方：
  1. 宿主机 `users/.container_tokens.json`（Web 服务器验证用）
  2. 容器的 `docker-compose.yaml` 环境变量 `PICOAIDE_TOKEN`

```
容器请求: POST http://172.17.0.1:80/api/cdp
Authorization: Bearer a3f8b2c1d4e5f6...
Body: {"method": "Runtime.evaluate", "params": {...}}

Web 服务器验证:
  1. 查 token → 属于 zhangsan
  2. 只转发给 zhangsan 的 WebSocket
  3. token 不匹配 → 403 拒绝
```

### 3.3 消息路由

Web 服务器维护内存中的连接映射表：

```go
// 连接表：用户名 → WebSocket 连接
var connections sync.Map  // map[string]*websocket.Conn
// "zhangsan" → WebSocket A
// "lisi"     → WebSocket B
```

路由规则：
- 容器请求带的 token → 解析出用户名 → 查表找到对应的 WebSocket → 只转发给该连接
- 扩展连上时绑定用户名，断开时从表中移除
- 用户不在线（无 WebSocket）→ 返回错误给容器，AI fallback 到容器内的无头 Chrome

## 4. 容器 Token 管理

### 4.1 存储文件

`users/.container_tokens.json`（与 `.ssh_ports.json` 同级）

```json
{
  "zhangsan": "a3f8b2c1d4e5f6a7b8c9d0e1f2a3b4c5",
  "lisi": "9k7m2n5p8q1r4t6u9w2x5z8b1d4f7g0j"
}
```

### 4.2 docker-compose.yaml 注入

```yaml
services:
  picoaide-zhangsan:
    image: ghcr.io/picoaide/picoaide:v0.2.6
    container_name: picoaide-zhangsan
    environment:
      - TZ=Asia/Shanghai
      - PICOAIDE_API=http://172.17.0.1:80
      - PICOAIDE_TOKEN=a3f8b2c1d4e5f6a7b8c9d0e1f2a3b4c5
    volumes:
      - ./root:/root
    ports:
      - "22001:22"
    restart: unless-stopped
    network_mode: "bridge"
```

### 4.3 生成与分配

在 `user.AllocateContainerToken()` 中实现，逻辑与 `AllocateSSHPort` 相同：
- 已有 token → 返回已有
- 未分配 → `crypto/rand` 生成 32 字节 hex 字符串 → 存入文件 → 返回

### 4.4 部署流程

`sync` 或 `initUser` 时：
1. 分配/读取 token
2. 将 `PICOAIDE_API` 和 `PICOAIDE_TOKEN` 写入 `docker-compose.yaml` 环境变量
3. 容器重启后 PicoClaw 自动读取环境变量，知道向哪里发 CDP 命令

## 5. Web 服务器新增接口

### 5.1 WebSocket 端点（扩展连接）

```
GET /ws/connect
Cookie: session=zhangsan:1713836400:...
Upgrade: websocket
```

- 验证 session → 获取用户名
- 升级为 WebSocket
- 存入连接表 `connections[username] = conn`
- 开始双向转发：
  - 收到服务器消息 → 发给扩展（CDP 命令）
  - 收到扩展消息 → 匹配等待中的 HTTP 请求，返回响应

### 5.2 CDP 代理端点（容器调用）

```
POST /api/cdp
Authorization: Bearer <token>
Content-Type: application/json

{
  "method": "Runtime.evaluate",
  "params": {"expression": "document.title"}
}
```

处理流程：
1. 验证 Bearer token → 解析用户名
2. 查连接表，用户是否在线（有 WebSocket 连接）
3. 不在线 → 返回 `{"error": "browser offline", "fallback": true}`
4. 在线 → 通过 WebSocket 转发给扩展
5. 阻塞等待扩展返回结果（带超时，如 30 秒）
6. 返回结果给容器

### 5.3 连接状态查询

```
GET /api/cdp/status
Authorization: Bearer <token>

响应: {"online": true}
```

PicoClaw 可用此接口判断用户浏览器是否在线，决定走扩展还是走容器内 Chrome。

## 6. PicoAide 浏览器扩展

### 6.1 目录结构

```
picoaide-helper/
├── manifest.json
├── background.js      # Service Worker：WebSocket 连接、消息转发
├── popup.html          # 弹出页面：连接状态、一键连接
├── popup.js            # 弹出页面逻辑
├── icons/
│   ├── icon16.png
│   ├── icon48.png
│   └── icon128.png
└── styles/
    └── popup.css
```

### 6.2 manifest.json

```json
{
  "manifest_version": 3,
  "name": "PicoAide Helper",
  "version": "1.0.0",
  "description": "一键连接你的 AI 助手，让 AI 帮你操作浏览器",
  "permissions": [
    "debugger",
    "cookies",
    "activeTab"
  ],
  "host_permissions": [
    "http://*/",
    "https://*/"
  ],
  "background": {
    "service_worker": "background.js"
  },
  "action": {
    "default_popup": "popup.html",
    "default_icon": {
      "16": "icons/icon16.png",
      "48": "icons/icon48.png",
      "128": "icons/icon128.png"
    }
  },
  "icons": {
    "16": "icons/icon16.png",
    "48": "icons/icon48.png",
    "128": "icons/icon128.png"
  }
}
```

### 6.3 扩展核心逻辑（background.js）

```javascript
let ws = null;
let tabId = null;
let serverUrl = '';

// 连接 WebSocket
function connect() {
  ws = new WebSocket(`${serverUrl}/ws/connect`);

  ws.onopen = () => {
    chrome.debugger.attach({ tabId }, '1.3', () => {
      updateStatus('connected');
    });
  };

  ws.onmessage = (event) => {
    const cdpCommand = JSON.parse(event.data);
    chrome.debugger.sendCommand(
      { tabId },
      cdpCommand.method,
      cdpCommand.params,
      (result) => {
        ws.send(JSON.stringify({ id: cdpCommand.id, result }));
      }
    );
  };

  ws.onclose = () => {
    chrome.debugger.detach({ tabId });
    updateStatus('disconnected');
    setTimeout(connect, 5000);
  };
}

// Cookie 导出功能
function exportCookies(domain) {
  chrome.cookies.getAll({ domain }, (cookies) => {
    fetch(`${serverUrl}/api/cookies`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ domain, cookies })
    });
  });
}
```

### 6.4 弹出页面（popup.html）

```
┌─────────────────────────────┐
│  PicoAide Helper             │
│                              │
│  服务器: [picoaide.example.com] │
│                              │
│  [连接]  或  [已连接 ✓]       │
│                              │
│  ── Cookie 管理 ──           │
│  当前网站: example.com       │
│  [保存登录状态]              │
│                              │
│  ── 状态 ──                 │
│  ● 已连接                   │
│  标签: 工作台                │
└─────────────────────────────┘
```

功能：
- **连接**：建立 WebSocket + attach debugger
- **保存登录状态**：一键导出当前网站的 cookies
- **状态显示**：连接状态、当前操作的标签页
- **断开连接**：detach debugger + 关闭 WebSocket

## 7. 容器侧适配

### 7.1 PicoClaw 配置变更

PicoClaw 启动时读取环境变量：

```bash
PICOAIDE_API=http://172.17.0.1:80    # Web 服务器地址
PICOAIDE_TOKEN=a3f8b2c1d4e5...       # 容器身份凭证
```

### 7.2 CDP 命令发送方式

PicoClaw 现有的 CDP 客户端（连接 localhost:9222）需要改为：

```python
# 伪代码：优先走用户浏览器，fallback 到容器内 Chrome

def send_cdp(method, params):
    status = http_get(f"{PICOAIDE_API}/api/cdp/status")

    if status.online:
        result = http_post(f"{PICOAIDE_API}/api/cdp", {
            "method": method,
            "params": params
        })
        if not result.error:
            return result

    # fallback: 走容器内的无头 Chrome
    return cdp_local(method, params)
```

### 7.3 兼容性

- 用户浏览器在线 → 走扩展（交互式，可看到操作过程）
- 用户浏览器离线 → 走容器内 Chrome（无头模式，自动执行定时任务等）
- 两种模式无缝切换，PicoClaw 代码无需感知区别

## 8. 安全性

| 风险点 | 防护措施 |
|--------|---------|
| 容器冒充其他用户 | per-container token，Web 服务器严格校验 |
| WebSocket 被劫持 | session cookie + HMAC 签名，现有鉴权机制 |
| CDP 命令注入 | Web 服务器只转发 CDP 命令格式，不执行任意代码 |
| Cookie 泄露 | HTTPS 传输，sessions.json 权限 0600 |
| 扩展权限滥用 | 最小权限原则，只申请 debugger + cookies + activeTab |

## 9. 实施计划

### 阶段 1：基础设施（服务端）

1. 添加 `user.AllocateContainerToken()` — token 生成与存储
2. 修改 `user.GenerateDockerCompose()` — 注入 `PICOAIDE_API` + `PICOAIDE_TOKEN` 环境变量
3. Web 服务器添加 WebSocket 升级 + 连接表管理
4. Web 服务器添加 `/api/cdp` 代理端点（Bearer token 鉴权）
5. Web 服务器添加 `/api/cookies` 端点（接收扩展导出的 cookies）

### 阶段 2：Chrome 扩展

1. 扩展基础框架（manifest.json + popup + background）
2. WebSocket 连接管理 + 自动重连
3. CDP 命令转发（chrome.debugger API）
4. Cookie 导出功能
5. 连接状态 UI

### 阶段 3：容器适配

1. PicoClaw 读取 `PICOAIDE_API` / `PICOAIDE_TOKEN` 环境变量
2. CDP 客户端支持双模式（用户浏览器 / 容器内 Chrome）
3. 在线状态检测 + 自动 fallback

### 阶段 4：部署

1. 现有用户执行 `sync`，自动分配 token + 更新 docker-compose
2. 发布扩展到 Edge 商店（免费）或 Chrome 商店（$5）
3. 用户安装扩展，点连接即可

## 10. 验证

```bash
# 阶段 1
go build -o picoaide ./cmd/picoaide/
./picoaide sync  # 验证 token 分配和 docker-compose 更新

# 阶段 2
chrome://extensions → 加载扩展 → 点击连接 → 验证 WebSocket 连接建立

# 阶段 3
在容器内 curl http://172.17.0.1:80/api/cdp/status → 验证在线状态
curl -X POST http://172.17.0.1:80/api/cdp -H "Authorization: Bearer <token>" → 验证 CDP 命令转发
```
