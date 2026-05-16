# CLAUDE.md

本文件为 Claude Code (claude.ai/code) 在本仓库中工作时提供指导。

## 项目概述

PicoAide（`picoaide`）是一个 Go 语言编写的 CLI 工具，用于批量管理多个用户的 PicoClaw AI 代理容器。它使用 Docker Engine Go SDK 管理容器生命周期，SQLite（xorm + modernc）记录容器状态，自管 `picoaide-net` 私有网络（100.64.0.0/16, ICC=false 隔离容器间通信），并通过 Gin 框架提供 JSON API 和 Web 管理面板。每个用户拥有一个独立容器，模型和安全配置由中心统一管理，配置通过 SQLite `settings` 表以展平的键值对存储并合并到各用户配置中。

## 构建与运行

### 构建命令

```bash
# 构建
go build -o picoaide ./cmd/picoaide/

# 运行 CLI（需要主机上安装 Docker，且必须以 root 运行）
./picoaide <命令> [选项]

# 运行 API 服务
./picoaide serve

# 生产环境构建（Linux amd64，注入版本信息）

# 部署到测试服务器（10.88.7.22）
ssh root@10.88.7.22 "systemctl stop picoaide" && scp ./picoaide root@10.88.7.22:/usr/sbin/picoaide && ssh root@10.88.7.22 "systemctl start picoaide && sleep 1 && /usr/sbin/picoaide --help" 2>&1
GOOS=linux GOARCH=amd64 go build -ldflags "-X github.com/picoaide/picoaide/internal/config.Version=1.0.0" -o picoaide ./cmd/picoaide/

# 全平台发布构建
make release PICOCLAW_VERSION=v1.0.0
```

### 测试命令

本项目使用 Go `testing` + Python `pytest` + Node.js `node:test` 三套测试框架。

```bash
# 运行全部测试
make test

# 单独运行
make test-go        # Go 单元测试 + 集成测试（internal/... 所有包）
make test-python    # Python 桌面客户端测试（picoaide-desktop/）
make test-js        # JS 浏览器扩展测试（picoaide-extension/）

# 代码检查
make lint           # golangci-lint（仅启用 govet + ineffassign）
make format         # 替换全部制表符为两个空格
make check          # format + lint + test
```

### 关键依赖

| 依赖 | 用途 |
|------|------|
| `github.com/docker/docker` | Docker Engine SDK（容器生命周期管理） |
| `github.com/go-ldap/ldap/v3` | LDAP 客户端（用户查询、认证、组查询） |
| `github.com/coreos/go-oidc/v3` | OIDC 认证（浏览器跳转登录） |
| `modernc.org/sqlite` + `xorm.io/xorm` | SQLite 数据库（纯 Go 实现，无 CGO） |
| `github.com/gin-gonic/gin` | HTTP 框架（Web 服务和 JSON API） |
| `github.com/gorilla/websocket` | WebSocket（浏览器/桌面代理实时通信） |
| `golang.org/x/crypto` | 密码哈希（argon2id + bcrypt） |
| `gopkg.in/natefinch/lumberjack.v2` | 日志轮转 |
| `gopkg.in/yaml.v3` | YAML 解析（配置文件和 .security.yml） |

## 架构

### 入口点

- `cmd/picoaide/main.go`：CLI 入口，解析命令（`init`、`serve`、`reset-password`），初始化数据库和全局配置，启动 Web 服务。`init` 子命令包含完整的首次运行引导流程（设置超管、选择镜像仓库、拉取镜像、安装 systemd 服务）。

### 包结构与职责

```
cmd/picoaide/              CLI 入口：命令路由、初始化引导、超管设置、reset-password
internal/config/           GlobalConfig 结构体及方法、YAML/JSON 加载保存、展平 KV 存储、默认值、HomeConfig 管理
internal/auth/             SQLite ORM（xorm）、9 张表、密码哈希（argon2id/bcrypt）、会话密钥持久化、IP 分配（拆分为 models.go、users.go、groups.go、containers.go、auth.go）
internal/authsource/       统一认证源抽象（3 个接口）、Provider 注册表（init() 自动注册）、LDAP/OIDC 实现、sync 编排、claims 解析
internal/ldap/             LDAP 底层客户端：用户查询、认证（双步验证）、组查询（member_of / group_search 两种模式）、连接测试
internal/docker/           Docker Engine SDK 封装：容器 CRUD、镜像管理、网络管理、私有仓库标签查询（ghcr.io + 腾讯云）
internal/user/             用户生命周期：目录创建、IP 分配、白名单、配置合并下发、钉钉配置、Picoclaw Adapter 包管理、迁移引擎（fixup 函数拆分到 picoclaw_fixups.go）
internal/util/             通用工具：深拷贝、Map 合并、文件/目录复制、ParseFlags、SafePathSegment、SafeRelPath、IsTextFile
internal/web/              Gin HTTP 服务：会话/CSRF、认证处理器、文件管理器、MCP SSE 服务、WebSocket 代理、任务队列、嵌入 UI
internal/logger/           结构化日志（slog + JSON 格式 + lumberjack 轮转）、HTTP 访问日志中间件、审计日志
```

### 核心设计模式

#### 1. 配置展平存储

全局配置在 SQLite `settings` 表中以点分隔的键值对存储。`flattenConfig()` 将嵌套 map 展平——字符串/数字/布尔值直接存为字符串，map 递归展平，数组序列化为 JSON。`picoclaw`、`security`、`skills` 三个顶层键整体序列化为 JSON blob 存储。`buildNested()` 反向重建嵌套结构。

#### 2. 认证源注册表模式

`internal/authsource/provider.go` 定义三个能力接口：
- `PasswordProvider`：用户名密码认证（LDAP 实现）
- `BrowserProvider`：浏览器跳转/回调认证（OIDC 实现）
- `DirectoryProvider`：可枚举用户、组和成员关系的目录源（LDAP 实现）

每种认证源通过 `init()` 函数自动注册到全局 `providers` map，无需手动引入。新增认证源只需：新建文件实现对应接口 → `init()` 中 `Register("name", ProviderType{})` → 补充测试。

#### 3. 服务端 MCP 代理架构

PicoAide Go 服务直接提供 MCP SSE 端点（`/api/mcp/sse/{service}`），取代 Node.js 中继。架构分为三层：

- **MCP Service 层**：注册 MCP 服务（browser、computer），处理 `initialize`、`tools/list`、`tools/call` 等 JSON-RPC 2.0 消息，支持 Streamable HTTP 协议（`Mcp-Protocol-Version` header）
- **ServiceHub 层**：管理 WebSocket 代理连接（`/api/browser/ws`、`/api/computer/ws`），支持连接注册/注销、命令发送、超时处理、keep-alive
- **代理层**：浏览器扩展或桌面代理通过 WebSocket 连接，接收命令并在用户设备上执行

认证通过 MCP token（`用户名:随机hex`）进行，从 query string 或 `Authorization: Bearer` header 提取。

### 数据库表结构（SQLite，xorm ORM）

| 表名 | 用途 | 关键字段 |
|------|------|---------|
| `local_users` | 本地用户（含超管和认证源快照） | username(unique), password_hash, role, source |
| `containers` | 容器状态记录 | username(unique), container_id, image, status, ip, cpu_limit, memory_limit, mcp_token |
| `settings` | 全局配置（展平 KV） | key(pk), value |
| `settings_history` | 配置变更审计 | key, old_value, new_value, changed_by |
| `whitelist` | 用户白名单 | username(unique), added_by |
| `groups` | 用户组（支持树形结构） | name(unique), parent_id, source |
| `user_groups` | 用户-组关联 | username, group_id |
| `group_skills` | 组-技能绑定 | group_id, skill_name |
| `user_channels` | 用户渠道状态 | username, channel, allowed, enabled, configured, config_version |

密码哈希支持两种方案：新用户使用 argon2id（`$argon2id$` 前缀），兼容旧 bcrypt 密码。会话密钥持久化在 `settings` 表的 `internal.session_secret` 键中。

### Docker 容器管理

- **网络**：单桥接网络 `picoaide-net`（100.64.0.0/16，`com.docker.network.bridge.enable_icc=false` 禁止容器间通信），IP 从 100.64.0.2 起递增分配
- **挂载**：用户目录 `users/<用户名>` bind mount 到容器 `/root`
- **资源限制**：支持 CPU（NanoCPUs）和内存（Memory）限制
- **镜像仓库**：支持 ghcr.io 和腾讯云（hkccr.ccs.tencentyun.com）两个仓库，`PICOCLAW_DEV=1` 环境变量切换到开发镜像
- **容器命名**：`picoaide-<用户名>` 格式
- **重启策略**：`unless-stopped`

### Web 认证与会话

- **会话**：HMAC-SHA256 签名的 Cookie（非 JWT），格式为 `username:timestamp:signature`
- **CSRF**：按小时滚动的时间窗口令牌，从 session secret 派生
- **本地认证**：超管始终走本地认证；普通用户在 `local` 模式下走本地，统一认证模式（LDAP/OIDC）下走对应 provider
- **OIDC 流程**：`/api/login/oidc/auth` 获取授权 URL → 浏览器跳转 → `/api/login/oidc/callback` 完成登录 → 自动同步用户快照
- **认证源切换**：切换认证模式时自动清理旧 provider 的用户快照、组成员、容器记录、用户目录和归档目录

### 用户配置下发体系

PicoAide 管理各用户的 Picoclaw 配置（`config.json` 和 `.security.yml`），通过 Picoclaw Adapter 包实现版本适配：

- **Adapter 包**：`rules/picoclaw/` 目录，包含 `index.json`、`hash` 文件、各版本的 config schema 和 UI schema、迁移规则
- **配置版本**：当前支持 v1→v2→v3，`PicoAideSupportedPicoClawConfigVersion = 3`
- **迁移引擎**：基于操作的规则系统（`op`: move/rename/delete/set），`ApplyConfigToJSONForTag()` 根据目标 Picoclaw 版本自动应用迁移链
- **配置下发**：`ApplyConfigToPicoclawDir()` 合并全局 Picoclaw 配置 + 渠道安全配置 → 写入用户目录
- **Adapter 刷新**：支持远程 URL 拉取（SHA256 校验）和 ZIP 上传两种方式

### 输入验证与安全

- **路径安全**：`util.SafePathSegment()` 验证无目录遍历字符（拒绝 `/`、`\`、`..`），`util.SafeRelPath()` 基于 `os.Root` 的沙盒文件访问
- **用户名验证**：`user.ValidateUsername()` 使用正则 `^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?$`，最长 64 字符
- **密码哈希**：argon2id（memory=4KB, time=1, threads=1, keyLen=32, saltLen=16）或 bcrypt 兼容
- **速率限制**：登录接口 10 次/5 分钟，内存存储 + 后台清理 goroutine

## 开发流程

所有新需求开发必须严格遵循以下完整流程。

### 1. 产品经理：需求分析（10 问）

向用户提出至少 **10 个需求细节问题**，覆盖功能目标、输入输出、边界条件、异常场景、已有功能影响、性能安全考量、兼容性等。将问答结果整理为产品需求文档。

### 2. 文档先行

在 `/docs/` 目录下按功能模块划分子目录（如 `docs/auth/`、`docs/docker/`、`docs/mcp/` 等），根据产品需求文档编写设计文档，必须包含：
- 接口定义（API 签名、数据结构）
- 业务流程（正常路径 + 异常路径）
- 数据库变更（如有）
- 测试策略

### 3. 双架构师独立设计

两位架构师（架构师1、架构师2）**各自独立**阅读设计文档，并充分理解需要修改的上下文代码组成，分别产出：
- 技术方案设计（接口定义、数据结构、业务流程、数据库变更）
- 设计理念阐述和方案优势说明

两人之间禁止互相参考，确保方案多样性。

### 4. 架构审计师评审

独立架构审计师对两份设计方案进行评审：
- 如两份设计均不合理或有重大缺陷 → 退回给两位架构师重新设计
- 重新设计后再次评审，循环直至至少一份方案通过
- 评审通过后，融合双方设计的最佳点
- 两位架构师各自阐述设计理念和优势，形成最终融合方案

### 5. UI/UX 双链设计

UI设计师、UX设计师作为**独立 agent**，根据最终架构方案分别产出：
- UX设计师：交互流程、信息架构、用户旅程
- UI设计师：视觉设计、组件规范、界面布局

### 6. 全栈工程师总结

全栈工程师汇总以上全部产出，形成完整的实现方案文档。

### 7. 严格 TDD

必须遵循红-绿-重构循环：
1. **红**：先编写单元测试，运行确认失败
2. **绿**：写最少代码让测试通过
3. **重构**：优化代码质量，保持测试绿色
4. 循环直至功能完整

测试要求：`testing.T` + 表驱动测试，`t.TempDir()` 隔离，`auth.ResetDB()` 重置数据库状态。

### 8. 代码审计

功能开发完成后，启动独立 agent 进行代码审计，覆盖四个方面：
- **功能正确性**：逻辑完整性、边界覆盖
- **健壮性**：错误处理、资源泄漏、并发安全、panic 恢复
- **安全性**：注入攻击、路径遍历、认证绕过、敏感信息泄露
- **代码质量**：命名规范、重复代码、复杂度

审计报告列出所有问题 → 逐条修复 → 确认审计结果完全闭合。

### 9. 部署与 E2E 测试

```bash
# 构建并部署到测试服务器
go build -o picoaide ./cmd/picoaide/
scp picoaide root@10.88.7.22:/usr/sbin/picoaide
ssh root@10.88.7.22 systemctl restart picoaide
```

通过 MCP browser 代理连接测试服务器，模拟用户操作本次新功能场景：
- 操作路径覆盖完整用户旅程
- 检查所有 UI 元素渲染正确
- 确认 API 返回符合预期
- 审查交互流畅性和错误提示

### 10. 验证与关闭

- 运行 `make check`（format + lint + test）确保全部通过
- 确认审计问题全部修复
- 确认 E2E 测试通过
- 提交 PR

### 分支策略与工作流

`main` 为稳定分支，`dev` 为开发分支。**以本地 `dev` 和远程 `main` 为准，`dev` 始终与 `main` 对齐**，禁止分叉。

#### 开发流程

```
本地 dev → 提交 → 推送 origin/dev → 创建 PR 到 main → CI 通过 → 合并到 main
```

- **本地开发始终在 `dev` 分支上**，不要创建功能分支
- 每次创建新功能前，先同步 `main`：
  ```bash
  git fetch origin main
  git rebase origin/main
  ```
- 提交到本地 `dev`，然后推送 `origin/dev`
- 从 `dev` 创建 PR 到 `main`

#### 禁止操作

### 分支与合并策略

#### Git 提交规范

- **不允许添加协作者信息**：提交时禁止使用 `Co-Authored-By` 行，不要在提交信息中添加 Claude 或任何 AI 协作者信息

#### 编译与测试

- **每次编译完成后必须进行测试**：编译成功后必须验证 API 工作正常、符合预期
- **每次代码修改完成后必须部署到测试服务器**：本地测试通过后，将最新二进制部署到测试服务器并重启 `picoaide` 服务，确认服务正常启动
- 测试步骤：登录 API → 获取用户列表 → 创建/删除用户 → 容器操作 → 镜像管理，确保所有关键 API 端点返回正确响应
- 测试服务器：10.88.7.22，二进制路径 `/usr/sbin/picoaide`，服务名 `picoaide`

#### GitHub Actions

- **上传代码后必须监控构建状态**：推送代码后持续跟踪 GitHub Actions 工作流运行状态，直至构建成功或报告失败原因
- 仓库地址：`github.com:picoaide/picoaide.git`
- 镜像会自动构建并推送到 `ghcr.io/picoaide/picoaide`

#### 分支保护与合并规则

- **所有代码必须通过 PR 合并到 main**：禁止任何人直接推送 main 分支（包括仓库管理员）
- **测试必须通过才能合并**：PR 必须通过 `test` 状态检查（Go/Python/JS 全部测试通过），否则不允许合并。所有 build job 依赖 test job，测试不过不编译
- **新增 API 必须包含对应的单元测试或集成测试**：没有测试覆盖的新功能代码不允许合并
- **PR 审批规则**：
  - 需要 1 人审批才能合并
  - 别人不能自己审批自己的 PR（`require_last_push_approval`）
  - 仓库管理员（`lostmaniac`）的 PR 由 `auto-approve` 工作流自动审批，无需等待他人
- **开发流程**：在 `dev` 或功能分支上开发 → 推送 → 创建 PR 到 main → CI 测试通过 + 审批通过 → 合并

### 认证源扩展规范

统一认证源必须通过 `internal/authsource` 扩展，禁止把新认证源逻辑堆到 `internal/web`、`internal/auth` 或单个大文件里。新增认证源前先阅读 `docs/auth-source-guide.md`。

- `internal/authsource/provider.go` 只放认证源接口和注册表
- 每一种认证源独立一个文件（如 `ldap.go`、`oidc.go`、后续新增 `saml.go`、`feishu.go` 等）
- 公共 claim/字段解析放在 `claims.go`；用户和组同步编排放在 `sync.go`；不要复制同步逻辑到每个 provider
- 新认证源只实现自己支持的能力接口。比如只有浏览器登录能力的源只实现 `BrowserProvider`，不要写空的 `FetchUsers`
- 新认证源需要在文件内 `init()` 注册：`Register("provider_name", ProviderType{})`
- 认证源返回的普通用户只作为本地快照写入 `local_users.source = provider_name`；本地普通用户不允许手工创建、删除或修改组成员
- 切换 `web.auth_mode` 时必须走统一清理逻辑（`purgeOrdinaryAuthProviderStateForConfig`），清空旧普通用户、组成员、容器记录、用户目录和归档目录
- 如果 provider 能提供组成员，使用 `auth.ReplaceGroupMembersBySource(source, groupMembers)` 替换对应来源的成员关系；不要写 LDAP 专属 SQL
- 新增认证源必须至少补充：provider 注册/能力断言单元测试；claims 或用户映射测试；登录/同步相关集成测试

## 编码规范

### 语言与约定

- 界面文字全部使用中文（API 消息、面向用户的错误提示）
- 代码注释使用中文
- 提交信息使用中文
- **缩进必须使用两个空格**，禁止使用制表符（Tab）。运行 `./format.sh` 格式化，`./format.sh --check` 检查。脚本扫描 `.go`、`.js`、`.html`、`.yaml`、`.yml`、`.css`、`.json`、`.sh`、`.md` 文件
- 参数解析为手工实现（`util.ParseFlags`），不使用 `flag` 或 `cobra`
- 配置结构体使用 `yaml` 标签；用户级配置为 JSON（`config.json`）或 YAML（`.security.yml`）

### 命名约定

- **PicoAide**（`picoaide`）是管理工具——即本仓库
- **PicoClaw**（`picoclaw`）是来自 [sipeed/picoclaw](https://github.com/sipeed/picoclaw) 的 AI 代理二进制文件——一个独立项目。容器镜像名、容器内的 `picoclaw` 二进制文件、`.picoclaw/` 用户目录都引用的是上游项目，不应被重命名
- Go 模块路径：`github.com/picoaide/picoaide`

### Go 编码规范

- 包级变量和常量放在文件顶部，用分隔注释 `// ============================================================` 组织区块
- 函数和方法的文档字符串为中文
- 错误处理：返回 `fmt.Errorf("描述: %w", err)` 包装底层错误
- 并发安全：`sync.Mutex`/`sync.RWMutex` 保护共享状态，`sync.Once` 用于一次性初始化，`atomic` 包用于简单计数器
- 测试：使用 `testing.T` + 表驱动测试，每个测试创建独立临时目录 `t.TempDir()`，通过 `auth.ResetDB()` 隔离数据库状态

### 文件组织

#### 文件大小限制

- **单个 Go 源文件不超过 1000 行**（含注释和空行）。这是硬性约束
- 测试文件不受 1000 行限制，但超过 1000 行时应考虑按测试主题拆分
- 所有 Go 源文件均已拆分至 1000 行以内（最大文件约 700 行），新增代码时注意不要使文件再次膨胀

#### 业务逻辑与工具函数分离

- **工具/纯函数**：无副作用、不依赖外部状态（数据库、网络、文件系统等）的函数放入 `internal/util/` 或当前包的 `*_util.go` 文件
  - 示例：`util.DeepCopyMap`、`util.ParseFlags`、`util.SafePathSegment`
  - 示例（包内工具）：`config.flattenConfig`、`web.compareTagsForDisplay`、`auth.hashPassword`
- **业务逻辑**：依赖数据库、Docker、LDAP 等外部资源的函数，按资源域组织在各自的 handler/service 文件中
  - Handler 只负责：参数解析、权限检查、调用 service、构造响应
  - Service/业务函数负责：事务编排、资源操作、数据转换

#### 函数复用原则

- 跨包可复用的逻辑提取到 `internal/util/`
- 同包内多处使用的逻辑提取为包级私有函数，避免在 handler 中内联复杂逻辑
- 相似的 CRUD 操作应抽取通用方法而非复制粘贴，例如 `admin_handlers.go` 中的容器操作 handler 可抽取通用模式
- Git 相关操作（clone/pull/fetch/checkout）应集中在 1-2 个函数中通过参数差异化，而非 10+ 个函数各写一遍

#### Handler 拆分模式

Web handler 按以下规则拆分文件，避免单文件膨胀：

```
internal/web/
  server.go           # Server 结构体、路由注册、启动/关闭、会话/CSRF（708 行）
  handlers.go         # 基础 handler：health、login、logout、config（704 行）
  admin_handlers.go   # 核心 helper：requireSuperadmin、initExternalUser、syncLDAPUsers（237 行）
  admin_images.go     # 镜像列表/拉取/删除、仓库标签、显示工具函数（447 行）
  admin_images_migrate.go  # 镜像迁移、升级候选、版本升级（405 行）
  admin_skills.go     # 技能操作：list/deploy/download/remove/upload（372 行）
  admin_skills_repos.go    # 技能仓库管理：add/pull/remove/save/install/list（383 行）
  admin_skills_repo_util.go # Git 凭证/命令工具函数（476 行）
  admin_skills_util.go     # 文件/ZIP 工具函数（226 行）
  admin_groups.go     # 组 CRUD、成员管理、LDAP 组同步（400 行）
  admin_auth.go       # LDAP 测试/同步、白名单管理（291 行）
  admin_superadmins.go # 超管账户 CRUD（166 行）
  admin_config.go     # 配置下发、迁移规则、容器日志、任务状态（246 行）
  admin_users.go      # 用户 CRUD（405 行）
  admin_containers.go # 容器操作（335 行）
  admin_groups.go     # 组管理：CRUD + LDAP 同步（从 admin_handlers.go 拆出）
  admin_auth.go       # 认证管理：LDAP 测试、用户同步、白名单、超管（从 admin_handlers.go 拆出）
  admin_config.go     # 配置管理：Picoclaw 配置下发、迁移规则、Adapter 刷新（从 admin_handlers.go 拆出）
  files.go            # 文件管理（416 行，合适）
  mcp_handlers.go     # MCP token、SSE 服务、WebSocket 代理（合适）
  mcp_service.go      # MCP 服务注册、JSON-RPC 处理、tools/call（284 行，合适）
  browser_tools.go    # Browser MCP 工具定义（218 行，合适）
  computer_tools.go   # Computer MCP 工具定义（184 行，合适）
  service_hub.go      # ServiceHub 连接管理（226 行，合适）
  taskqueue.go        # 异步任务队列（176 行，合适）
  pagination.go       # 分页工具（合适）
  ratelimit.go        # 速率限制（合适）
  ui.go               # 嵌入 UI 路由（合适）
```

### UI 设计规范

- **产品经理视角**：UI 设计必须从产品经理视角出发，以用户体验为核心，关注用户任务流程的完整性和流畅性
- **禁止浏览器原生弹窗**：所有提示、确认、警告等信息展示必须使用自定义模态框，禁止使用 `alert()`、`confirm()`、`prompt()` 等浏览器原生弹窗
- **禁止浏览器原生复选框**：所有复选框必须使用自定义样式实现，禁止使用浏览器默认 `<input type="checkbox">` 样式
- **考虑用户天花板**：所有设计都要考虑用户的认知负荷和操作容错，界面信息不应过载，操作路径应尽量简短，关键操作需提供二次确认

## API 端点

所有路由均注册在 `/api` 和 `/api/v1` 双前缀下（如 `/api/health` 和 `/api/v1/health` 均可访问）。
`/api/version` 为单一路径。

### 基础端点（无需认证 / 公开）

```
GET  /api/version              获取服务端版本号
GET  /api/health               健康检查
POST /api/login                用户名密码登录 → 设置 session Cookie（需 CSRF token）
GET  /api/login/auth           浏览器认证跳转（OIDC 等，获取授权 URL 并重定向）
GET  /api/login/callback       浏览器认证回调（统一入口，处理 OIDC callback 等）
GET  /api/login/mode           返回当前认证模式（local/ldap/oidc）
POST /api/logout               清除会话
GET  /api/csrf                 获取 CSRF token（用于后续 POST/PUT/PATCH 请求）
```

### 普通用户端点（需 session Cookie）

```
GET  /api/user/info            当前用户信息（用户名、角色、来源）
GET  /api/user/init-status     用户目录初始化状态
POST /api/user/password        修改密码（仅本地认证模式）
GET  /api/picoclaw/channels    当前用户的渠道列表及启用状态
GET  /api/picoclaw/config-fields  获取渠道配置字段定义
POST /api/picoclaw/config-fields  保存渠道配置字段
GET  /api/dingtalk             读取钉钉配置（client_id/client_secret）
POST /api/dingtalk             保存钉钉配置 + 重启容器
GET  /api/config               读取全局 Picoclaw 配置
POST /api/config               保存全局 Picoclaw 配置
GET  /api/mcp/token            获取当前用户的 MCP token
GET  /api/mcp/sse/:service     建立 MCP SSE 连接（browser/computer）
POST /api/mcp/sse/:service     发送 MCP JSON-RPC 消息
GET  /api/mcp/cookies          MCP token 认证的 Cookie API（容器内技能读写 cookie）
POST /api/mcp/cookies          写入 cookie（MCP token 认证）
GET  /api/browser/ws           浏览器代理 WebSocket（?token=xxx）
GET  /api/computer/ws          桌面代理 WebSocket（?token=xxx）
POST /api/cookies              写入用户 .security.yml（Cookie 同步）
GET  /api/user/cookies         查看已授权的 Cookie 域名列表
POST /api/user/cookies/delete  取消 Cookie 域名授权
GET  /api/shared-folders       查看可见的团队空间文件夹列表
GET  /api/user/skills          用户已安装的技能列表
POST /api/user/skills/install  安装技能
POST /api/user/skills/uninstall  卸载技能
```

### 文件管理端点（用户在 `.picoclaw/workspace/` 中的沙盒目录）

```
GET  /api/files                列出文件（JSON，含面包屑导航）
POST /api/files/upload         上传文件（最大 32MB）
GET  /api/files/download       下载文件
POST /api/files/delete         删除文件/目录
POST /api/files/mkdir          创建目录
GET  /api/files/edit           读取文本文件内容（JSON）
POST /api/files/edit           保存文本文件内容
```

### 超管端点（需 superadmin 角色 + session Cookie）

**用户管理**
```
GET  /api/admin/users                  用户列表（分页+搜索）
POST /api/admin/users/create           创建用户
POST /api/admin/users/batch-create     批量导入用户（异步任务队列）
POST /api/admin/users/delete           删除用户（含目录归档）
GET  /api/admin/superadmins            超管列表
POST /api/admin/superadmins/create     创建超管（返回随机密码）
POST /api/admin/superadmins/delete     删除超管（至少保留一个）
POST /api/admin/superadmins/reset      重置超管密码（返回新密码）
```

**容器管理**
```
POST /api/admin/container/start        启动容器（单用户或批量）
POST /api/admin/container/stop         停止容器
POST /api/admin/container/restart      重启容器
POST /api/admin/container/debug        进入容器调试模式
GET  /api/admin/container/logs         获取容器日志
```

**认证与白名单**
```
GET  /api/admin/whitelist              白名单列表
POST /api/admin/whitelist              更新白名单
POST /api/admin/auth/test-ldap         测试 LDAP 连接
GET  /api/admin/auth/ldap-users        获取 LDAP 用户列表
POST /api/admin/auth/sync-users        手动同步 LDAP/OIDC 用户
POST /api/admin/auth/sync-groups       手动同步 LDAP 组
GET  /api/admin/auth/providers         已注册的认证源列表
```

**用户组管理**
```
GET  /api/admin/groups                 用户组列表（支持树形结构）
POST /api/admin/groups/create          创建组
POST /api/admin/groups/delete          删除组
GET  /api/admin/groups/members         查看组成员
POST /api/admin/groups/members/add     添加组成员
POST /api/admin/groups/members/remove  移除组成员
POST /api/admin/groups/skills/bind     绑定技能到组
POST /api/admin/groups/skills/unbind   解绑组技能
```

**技能管理**
```
GET  /api/admin/skills                 已安装的技能列表
POST /api/admin/skills/deploy          部署技能到用户/组
POST /api/admin/skills/remove          移除技能
POST /api/admin/skills/user/bind       绑定技能到单个用户
POST /api/admin/skills/user/unbind     解绑用户技能
GET  /api/admin/skills/user/sources    用户技能来源列表
GET  /api/admin/skills/sources         技能仓库来源列表
POST /api/admin/skills/sources/git     添加 Git 技能仓库
POST /api/admin/skills/sources/remove  移除技能仓库
POST /api/admin/skills/sources/pull    拉取技能仓库更新
POST /api/admin/skills/sources/refresh  刷新技能仓库
GET  /api/admin/skills/registry/list   技能注册中心列表
POST /api/admin/skills/registry/install  从注册中心安装技能
GET  /api/admin/skills/defaults        默认技能列表
POST /api/admin/skills/defaults/toggle  切换技能默认安装
```

**镜像管理**
```
GET  /api/admin/images                 本地镜像列表
POST /api/admin/images/pull            拉取镜像（SSE 流式进度）
POST /api/admin/images/delete          删除本地镜像
POST /api/admin/images/migrate         镜像迁移
POST /api/admin/images/upgrade         容器镜像升级
GET  /api/admin/images/registry        远程仓库标签（按版本号降序）
GET  /api/admin/images/local-tags      本地镜像标签列表
GET  /api/admin/images/upgrade-candidates  可升级的版本列表
GET  /api/admin/images/users           使用指定镜像的用户列表
GET  /api/admin/images/pull-status     镜像拉取状态查询
```

**团队空间（共享文件夹）**
```
GET  /api/admin/shared-folders          共享文件夹列表
POST /api/admin/shared-folders/create   创建共享文件夹
POST /api/admin/shared-folders/update   更新共享文件夹
POST /api/admin/shared-folders/delete   删除共享文件夹
POST /api/admin/shared-folders/groups/set  设置文件夹的组可见范围
POST /api/admin/shared-folders/test     测试文件夹挂载
POST /api/admin/shared-folders/mount    手动挂载文件夹
```

**Picoclaw 配置管理**
```
POST /api/admin/config/apply             下发 Picoclaw 配置给用户
GET  /api/admin/migration-rules          查看迁移规则信息
POST /api/admin/migration-rules/refresh  刷新迁移规则（远程拉取）
POST /api/admin/migration-rules/upload   上传迁移规则 ZIP
GET  /api/admin/picoclaw/channels        渠道管理列表
```

**TLS 证书管理**
```
GET  /api/admin/tls/status               TLS 证书状态
POST /api/admin/tls/upload               上传 TLS 证书
```

**其他**
```
GET  /api/admin/task/status              异步任务状态查询
GET  /api/admin/skill-install-policy     技能安装策略
POST /api/admin/skill-install-policy     设置技能安装策略
```

### MCP 工具列表

**Browser MCP**（19 个工具）：navigate, screenshot, click, type, get_content, execute, tabs_list, tab_new, tab_close, go_back, go_forward, reload, current_tab, tab_select, scroll, key_press, get_attribute, get_links, wait

**Computer MCP**（15 个工具）：screenshot, screen_size, active_window, mouse_click, mouse_move, mouse_drag, mouse_scroll, keyboard_type, keyboard_press, screen_text(OCR), file_read, file_write, file_list, whitelist, file_search

## 本地状态文件

- `picoaide.db`：SQLite 数据库，存储全局配置、白名单、用户账户和容器记录（工作目录下）
- `logs/picoaide.log`：JSON 格式结构化日志，支持按时间/大小轮转
- `rules/picoclaw/`：Picoclaw Adapter 缓存目录

## 数据库迁移规范

### 迁移系统说明

使用基于时间戳的迁移系统（`internal/auth/migrations/`），替代直接修改 `syncSchema()` 中的 `ALTER TABLE`。

### 新增迁移步骤

1. 在 `internal/auth/migrations/` 下创建 `YYYYMMDD_HHMMSS_description.go`
2. 实现 `init()` + `Register()`，迁移函数必须**幂等**（使用 `ColumnExists` 检查）
3. 运行 `migrations.RunAll(engine)` 在 `syncSchema()` 末尾自动执行，不需要手动调用

### 模板

```go
package migrations

func init() {
  Register(Migration{
    Timestamp: "20250601093000",
    Desc:      "添加 xxx 表的 yyy 列",
    Up: func(engine *xorm.Engine) error {
      exists, err := ColumnExists(engine, "table_name", "column_name")
      if err != nil { return err }
      if !exists {
        _, err = engine.Exec("ALTER TABLE table_name ADD COLUMN column_name TEXT DEFAULT ''")
      }
      return err
    },
  })
}
```

### 注意事项

- 时间戳用 `date +%Y%m%d%H%M%S` 生成，同一分支内递增，不同分支天然不冲突
- 合并冲突时重命名其中一人文件的时间戳（加 1 秒即可）
- 禁止直接在 `syncSchema()` 中加 `ALTER TABLE`，必须通过 migration 文件
- `RunAll` 在 `syncSchema()` 末尾执行，对上层完全透明
