# CLAUDE.md

本文件为 Claude Code (claude.ai/code) 在本仓库中工作时提供指导。

## 项目概述

PicoAide（`picoaide`）是一个 Go 语言编写的 CLI 工具，用于批量管理多个用户的 PicoClaw AI 代理容器。它使用 Docker Engine Go SDK 管理容器生命周期，SQLite 记录容器状态，自管 `picoaide-net` 私有网络（100.64.0.0/16, ICC=false），并提供 JSON API 供浏览器扩展集成。每个用户拥有一个独立容器，模型和安全配置由中心统一管理。

## 构建与运行

```bash
# 构建
go build -o picoaide ./cmd/picoaide/

# 运行 CLI（需要主机上安装 Docker）
./picoaide <命令> [选项]

# 运行 API 服务
./picoaide serve -listen :80

# 生产环境构建（Linux amd64）
GOOS=linux GOARCH=amd64 go build -o picoaide ./cmd/picoaide/
```

本项目使用 Go `testing` + Python `pytest` + Node.js `node:test` 三套测试框架，详见 Makefile。

```bash
# 运行全部测试
make test

# 单独运行
make test-go        # Go 单元测试 + 集成测试
make test-python    # Python 桌面客户端测试
make test-js        # JS 浏览器扩展测试
```

## 架构

标准 Go 项目布局，`cmd/` 为入口点，`internal/` 为内部包。

### 包结构

```
cmd/picoaide/          CLI 入口点 + 命令实现
internal/config/       GlobalConfig 类型、YAML/JSON 加载、常量定义、预检
internal/docker/       Docker Engine SDK 容器管理（Create/Start/Stop/Restart/Remove、镜像管理）
internal/auth/         SQLite 数据库：用户认证 + 容器状态记录 + IP 分配
internal/ldap/         LDAP 客户端：获取用户、认证
internal/user/         用户生命周期、白名单、配置合并、钉钉配置
internal/util/         深拷贝、Map 合并、文件复制、参数解析、格式化工具
internal/web/          HTTP API 服务、会话/CSRF、处理器、文件管理器
```

### 核心概念

- **容器管理**：使用 Docker Engine Go SDK（`github.com/docker/docker`），不依赖 docker-compose。容器状态存储在 SQLite `containers` 表中。
- **网络方案**：单一 `picoaide-net` 网络（100.64.0.0/16, ICC=false 禁止容器间通信），静态 IP 从 100.64.0.2 起递增分配。
- **用户目录布局**：`users/<用户名>/.picoclaw/`，其中存放 `config.json`（JSON）和 `.security.yml`（YAML，权限 0600）。用户目录直接挂载为容器 `/root`。
- **配置合并策略**：`config.yaml` 中的全局配置通过 `util.MergeMap()` 合并到各用户配置中——用户已有的键值保留，缺失的键从全局默认值补充。
- **Web 认证**：支持本地认证和 LDAP 认证。会话使用 HMAC 签名的 Cookie（非 JWT）。CSRF 令牌使用按小时滚动的时间窗口。
- **JSON API**：所有 Web 端点返回 JSON。API 专为浏览器扩展消费而设计。

### API 端点

```
POST /api/login           认证 → 会话 Cookie
POST /api/logout          清除会话
GET  /api/user/info       当前用户信息
POST /api/user/password   修改密码（仅本地模式）
GET  /api/dingtalk        读取钉钉配置
POST /api/dingtalk        保存钉钉配置 + 重启容器
GET  /api/config          读取全局配置
POST /api/config          保存全局配置
GET  /api/files           列出文件（JSON）
POST /api/files/upload    上传文件
GET  /api/files/download  下载文件
POST /api/files/delete    删除文件/目录
POST /api/files/mkdir     创建目录
GET  /api/files/edit      读取文件内容（JSON）
POST /api/files/edit      保存文件内容
GET  /api/csrf            获取 CSRF token

# 超管接口
GET  /api/admin/users                  用户列表
POST /api/admin/users/create           创建用户
POST /api/admin/users/delete           删除用户
POST /api/admin/container/start        启动容器
POST /api/admin/container/stop         停止容器
POST /api/admin/container/restart      重启容器
GET  /api/admin/whitelist              白名单列表
POST /api/admin/whitelist              更新白名单
GET  /api/admin/images                 本地镜像列表
POST /api/admin/images/pull            拉取镜像（SSE 流式）
GET  /api/admin/images/registry        远程仓库标签（固定查询 picoaide/picoaide）
GET  /api/admin/images/local-tags      本地镜像标签列表
GET  /api/admin/skills                 技能列表
POST /api/admin/skills/deploy          部署技能
POST /api/admin/skills/install         安装技能
```

## 配置文件

- `config.yaml`（已加入 gitignore，包含 LDAP 凭据和 API 密钥）：首次运行时若不存在则自动生成。
- `whitelist.yaml`：允许同步的用户名列表（控制哪些 LDAP 用户获得容器）。
- `picoaide.db`：SQLite 数据库，存储用户账户和容器记录。
- `~/.picoaide-config.yaml`：存储工作目录，使二进制文件可从任意位置运行。

## 语言与约定

- 界面文字全部使用中文（API 消息、面向用户的文本）。
- 代码注释使用中文。
- 提交信息使用中文。
- **缩进必须使用两个空格**，禁止使用制表符（Tab）。使用 `./format.sh` 格式化，`./format.sh --check` 检查。
- 参数解析为手工实现（`util.ParseFlags`），不使用 `flag` 或 `cobra`。
- 配置结构体使用 `yaml` 标签；用户级配置为 JSON（`config.json`）或 YAML（`.security.yml`）。

## 命名约定

- **PicoAide**（`picoaide`）是管理工具——即本仓库。
- **PicoClaw**（`picoclaw`）是来自 [sipeed/picoclaw](https://github.com/sipeed/picoclaw) 的 AI 代理二进制文件——一个独立项目。容器镜像名、容器内的 `picoclaw` 二进制文件、`.picoclaw/` 用户目录都引用的是上游项目，不应被重命名。

## 开发流程规范

### Git 提交规范

- **不允许添加协作者信息**：提交时禁止使用 `Co-Authored-By` 行，不要在提交信息中添加 Claude 或任何 AI 协作者信息。

### 编译与测试

- **每次编译完成后必须进行测试**：编译成功后必须验证 API 工作正常、符合预期。
- **每次代码修改完成后必须部署到测试服务器**：本地测试通过后，将最新二进制部署到测试服务器并重启 `picoaide` 服务，确认服务正常启动。
- 测试步骤：登录 API → 获取用户列表 → 创建/删除用户 → 容器操作 → 镜像管理，确保所有关键 API 端点返回正确响应。
- 测试服务器：10.88.7.22，二进制路径 `/usr/sbin/picoaide`，服务名 `picoaide`。

### GitHub Actions

- **上传代码后必须监控构建状态**：推送代码后持续跟踪 GitHub Actions 工作流运行状态，直至构建成功或报告失败原因。
- 仓库地址：`github.com:picoaide/picoaide.git`
- 镜像会自动构建并推送到 `ghcr.io/picoaide/picoaide`

### 分支保护与合并规则

- **所有代码必须通过 PR 合并到 main**：禁止任何人直接推送 main 分支（包括仓库管理员）。
- **测试必须通过才能合并**：PR 必须通过 `test` 状态检查（Go/Python/JS 全部测试通过），否则不允许合并。所有 build job 依赖 test job，测试不过不编译。
- **新增 API 必须包含对应的单元测试或集成测试**：没有测试覆盖的新功能代码不允许合并。
- **PR 审批规则**：
  - 需要 1 人审批才能合并。
  - 别人不能自己审批自己的 PR（`require_last_push_approval`）。
  - 仓库管理员（`lostmaniac`）的 PR 由 `auto-approve` 工作流自动审批，无需等待他人。
- **开发流程**：在 `dev` 或功能分支上开发 → 推送 → 创建 PR 到 main → CI 测试通过 + 审批通过 → 合并。
