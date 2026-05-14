# SkillHub 注册源与企业 Git 多技能仓库设计文档

## 概述

为 PicoAide 增加对 [SkillHub](https://skillhub.cn/) 中文技能市场的兼容，同时重构 Git 技能仓库以支持多技能仓库模型。两种来源统一汇入 `skills/<源名>/` 目录，共享部署管道。

## 背景

### 现有架构问题

- Git 仓库 = 单技能：一个 Git repo 只能包含一个技能（根目录放 SKILL.md）
- 存在 `skill-repos/` 和 `skill/` 两级目录，Git 技能需要从 `skill-repos/` 复制到 `skill/`
- `skills` 表在 DB 中维护元数据，与文件系统需双向同步
- 不支持非 Git 的技能来源（如 SkillHub 的 registry 协议）

### SkillHub 协议

SkillHub 是面向中国用户的技能市场，提供标准的 registry 协议：

- **索引**：`GET skills.json` → `{"skills": [{"slug", "name", "version", ...}]}`
- **搜索**：`GET /api/v1/search?q=<query>` → `{"results": [...]}`
- **下载**：`GET /{slug}.zip` 或 `GET /download?slug={slug}` → 含 `SKILL.md` + `_meta.json` 的 ZIP 包
- **自更新**：`GET version.json` → `{"version", "zip_url", "sha256"}`
- 也支持 `file://` URI（企业私有场景）

## 设计原则

1. 文件系统唯一真相源：SKILL.md 在 `skills/` 下，不再依赖 DB 中的 skills 表维护元数据
2. 统一但无耦合：不同来源的技能统一到 `skills/<源名>/<技能名>/SKILL.md`，但每个来源的获取/更新机制独立
3. 复用部署链路：所有来源的技能安装到 `skills/` 后，部署到用户完全一致

## 目录结构

```
skills/
  skillhub.cn/                    # 注册源（内置，开箱即用）
    _index_cache.json             # 索引缓存
    weather/
      SKILL.md
      _meta.json
    github/
      SKILL.md
    ...

  <自定义源名>/                    # Git 源（用户添加时命名）
    .git/
    <技能名1>/                     # 子目录 = 一个技能
      SKILL.md
      ...
    <技能名2>/
      SKILL.md
      ...

  <另一个自定义源名>/               # Git 源
    .git/
    ...
```

### 目录规则

- `skills/<源名>/` 每个子目录是一个技能源
- 源名由管理员自定义（如 `abc`、`company-internal`）
- SkillHub 预置源名为 `skillhub.cn`，不可删除
- 技能目录名使用 `util.SafePathSegment()` 校验，防止目录遍历
- 不支持 `/`、`\`、`..`、以 `.` 开头等
- **源名允许包含 `.`**（如 `skillhub.cn`），`SafePathSegment` 需放宽或新增 `SafeSourceName()` 允许点号

## 数据模型

### 配置模型（SQLite `settings` → `skills` 键）

```go
// SkillsSourceWrapper 用于 JSON/YAML 序列化的包装
type SkillsSourceWrapper struct {
  Type string `yaml:"type" json:"type"` // "registry" | "git"
  RegistrySource *RegistrySource `yaml:",inline" json:",inline"`
  GitSource      *GitSource      `yaml:",inline" json:",inline"`
}

// SkillsConfig 技能配置
type SkillsConfig struct {
  Sources []SkillsSourceWrapper `yaml:"sources" json:"sources"`
}
```

序列化时根据 `type` 字段写入对应结构的字段；反序列化时根据 `type` 字段实例化正确的结构体。

### DB 表变化

**删除** `skills` 表（不再由 ORM 管理技能元数据）  
**保留** `user_skills` 表（用户-技能绑定）  
**保留** `group_skills` 表（组-技能绑定）

`user_skills` 表扩展字段：

```go
type UserSkill struct {
  ID        int64  `xorm:"pk autoincr"`
  Username  string `xorm:"unique(username,skill_name) notnull"`
  SkillName string `xorm:"unique(username,skill_name) notnull"`
  Source    string `xorm:"notnull default ''"`  // 来源名（如 "skillhub.cn", "abc"）
  UpdatedAt string `xorm:"updated"`
}
```

`group_skills` 同理。

## API 设计

### 技能源管理

| 方法 | 端点 | 用途 |
|------|------|------|
| GET | `/api/admin/skills/sources` | 列出所有源及技能数 |
| POST | `/api/admin/skills/sources/git` | 添加 Git 源 |
| POST | `/api/admin/skills/sources/remove` | 删除源 |
| POST | `/api/admin/skills/sources/pull` | Git 源全量拉取更新 |
| POST | `/api/admin/skills/sources/refresh` | 注册源刷新索引 |

### 技能操作

| 方法 | 端点 | 用途 |
|------|------|------|
| GET | `/api/admin/skills` | 列出所有已安装技能（扫描文件） |
| GET | `/api/admin/skills/list?source=X` | 过滤某源的技能 |
| POST | `/api/admin/skills/install` | 从注册源安装单个技能 |
| POST | `/api/admin/skills/remove` | 删除技能 |
| POST | `/api/admin/skills/deploy` | 部署到用户/组（不变） |

### 技能源添加（POST /api/admin/skills/sources/git）

```json
{
  "name": "abc",
  "url": "https://github.com/aaa/bb.git",
  "ref": "main",
  "ref_type": "branch",
  "credentials": [...]
}
```

**后端流程：**

1. 校验名称（`util.SafePathSegment` + 不自带文件扩展名）
2. 校验 Git 安装
3. `git clone --depth 1` → `skills/abc/`
4. 扫描 `skills/abc/` 下所有含 `SKILL.md` 的子目录
5. 注册到 DB（记录源名、技能名、来源）
6. 返回发现的技能列表
7. 自动部署到已有绑定用户（任务队列）

### 注册源安装（POST /api/admin/skills/install）

```json
{
  "source": "skillhub.cn",
  "slug": "weather",
  "force": false
}
```

**后端流程（复用 fallback 逻辑）：**

1. 查 RegistrySource 配置
2. 尝试 PrimaryDownloadURL → 失败则 FallbackDownloadURLTemplate
3. 下载 ZIP → SHA256 校验（如有 `sha256`）
4. 解压到 `skills/skillhub.cn/weather/`
5. 校验 `SKILL.md` 是否存在
6. 注册到 DB
7. 若已有绑定用户，自动重部署

## 更新机制

### Git 源（全量更新）

```
POST /api/admin/skills/sources/pull { "name": "abc" }
```

1. `git fetch` + `git reset`（同现有 pull 逻辑）
2. 重新扫描所有子技能
3. 对比变更（新增/更新/删除）：
   - 新增技能 → 注册到 DB
   - 更新技能 → 若有绑定用户，加入重部署队列
   - 删除技能 → 解绑所有用户 + 清理用户目录
4. 返回 `{ added: [...], updated: [...], removed: [...] }`

### 注册源（单技能更新）

```
POST /api/admin/skills/install { "source": "skillhub.cn", "slug": "weather" }
```

安装天然幂等，已安装时覆盖即为更新。若该技能有已绑用户，自动重部署。

### 注册源刷新（仅索引）

```
POST /api/admin/skills/sources/refresh { "name": "skillhub.cn" }
```

重新拉取 `skills.json` 覆盖 `_index_cache.json`，不下任何技能包。用于管理界面展示最新列表。

## 核心实现

### 新增文件

```
internal/skill/
  parser.go       ← 已有，不动
  sync.go         ← 重写：改为扫描 skills/*/*/SKILL.md
  registry.go     ← 新增：注册源客户端
  git_source.go   ← 新增：Git 源操作
  source.go       ← 新增：源管理器 + 统一扫描
```

### `source.go` — 源管理器

```go
// ListAllSkills 扫描 skills/*/*/SKILL.md 返回所有技能
func ListAllSkills() ([]SkillInfo, error)

// ListSourceSkills(name) 扫描指定源下的技能目录
func ListSourceSkills(name string) ([]SkillInfo, error)

// DeleteSkill(source, skillName) 删除技能目录 + 解绑关联用户
func DeleteSkill(source, skillName string) error

// GetSkillPath(source, skillName) 返回 skills/<source>/<skillName>/
func GetSkillPath(source, skillName string) string

// SkillInfo 技能信息
type SkillInfo struct {
  Name        string `json:"name"`
  Description string `json:"description"`
  Source      string `json:"source"`
  Version     string `json:"version,omitempty"`
  FileCount   int    `json:"file_count"`
  Size        int64  `json:"size"`
  SizeStr     string `json:"size_str"`
  ModTime     string `json:"mod_time"`
}
```

### `registry.go` — 注册源客户端

```go
type RegistryClient struct {
  config config.RegistrySource
}

func NewRegistryClient(cfg config.RegistrySource) *RegistryClient

// FetchIndex 从 IndexURL 拉取并解析 skills.json
func (c *RegistryClient) FetchIndex() ([]RegistrySkill, error)

// Search 调用远程搜索 API
func (c *RegistryClient) Search(query string, limit int) ([]RegistrySkill, error)

// Install 从注册源下载并安装技能
func (c *RegistryClient) Install(slug string, force bool) error

// RegistrySkill 技能条目
type RegistrySkill struct {
  Slug        string   `json:"slug"`
  Name        string   `json:"name"`
  Description string   `json:"description"`
  Version     string   `json:"version"`
  Categories  []string `json:"categories,omitempty"`
  Downloads   int      `json:"downloads,omitempty"`
  Stars       int      `json:"stars,omitempty"`
  SHA256      string   `json:"sha256,omitempty"`
}
```

### `git_source.go` — Git 源操作

```go
// CloneGitSource clone repo 到 skills/<name>/
func CloneGitSource(name string, repo config.GitSource) error

// PullGitSource 拉取 Git 源更新 + 重新扫描
func PullGitSource(name string, repo config.GitSource) (*SyncResult, error)

// SyncResult Git 源更新结果
type SyncResult struct {
  Added   []string `json:"added"`
  Updated []string `json:"updated"`
  Removed []string `json:"removed"`
}

// RescanSkills 扫描 Git 源下所有含 SKILL.md 的子目录
func RescanSkills(name string) ([]string, error)
```

### 现有代码的修改

| 文件 | 修改内容 |
|------|----------|
| `internal/config/config.go` | 新增 `RegistrySource`、`GitSource`、修改 `SkillsConfig` |
| `internal/web/admin_skills.go` | 重写为基于文件系统扫描的列表 |
| `internal/web/admin_skills_repos.go` | 替换为 sources 管理 handlers |
| `internal/web/admin_skills_repo_util.go` | Git 操作函数移入 `internal/skill/git_source.go` |
| `internal/web/admin_skills_util.go` | 删除 syncGitRepoToSkill，替换为直接路径操作 |
| `internal/web/server.go` | 更新路由注册 |
| `internal/web/ui/admin/modules/skills.js` | 重写前端模块 |
| `internal/web/ui/admin/templates/skills.html` | 重写模板 |
| `internal/auth/skills.go` | 简化：移除 GetAllSkills/UpsertSkill/DeleteSkill 等 |
| `internal/auth/models.go` | UserSkill/GroupSkill 添加 Source 字段 |
| `internal/skill/sync.go` | 移除（不再需要 DB 同步），或改为仅做扫描工具函数 |
| `internal/web/server.go` | 移除启动时的 `SyncSkillsFromDirectory()` 调用 |
| `internal/web/server.go:557/563` | 删除相关调用 |

### 不变的部分

- `internal/web/admin_skills_util.go` 中的 ZIP 工具函数
- `internal/web/admin_skills.go` 中的 deploy/remove/bind/unbind handlers
- `internal/web/taskqueue.go` 异步部署任务
- `internal/user/` 用户技能目录管理

## 前端 UI 变化

### 技能管理页面重构

```
┌──────────────────────────────────────────────┐
│  技能库                                       │
│                                              │
│  ┌─ 源管理 ──────────────────────────────┐   │
│  │  [skillhub.cn]  🔵 内置  45 个技能     │   │
│  │                     [搜索] [刷新索引]   │   │
│  │                                          │   │
│  │  [abc] ⚡ Git  12 个技能                 │   │
│  │  https://github.com/aaa/bb.git          │   │
│  │  [拉取更新] [删除源]                      │   │
│  │                                          │   │
│  │  [+ 添加 Git 源]                        │   │
│  └─────────────────────────────────────────┘   │
│                                              │
│  ┌─ 已安装技能 ──────────────────────────┐   │
│  │  搜索: [____________] [搜索]            │   │
│  │                                          │   │
│  │  技能名      来源        版本    操作    │   │
│  │  weather    skillhub.cn  1.0.0  部署 ✓  │   │
│  │  github     skillhub.cn  1.0.0  部署 ✓  │   │
│  │  browser-a  abc          -      部署 ✓  │   │
│  │  ...                                     │   │
│  └─────────────────────────────────────────┘   │
│                                              │
│  ┌─ 部署区（不变）─────────────────────┐   │
│  ...                                       │
│  └─────────────────────────────────────────┘   │
└──────────────────────────────────────────────┘
```

### 添加 Git 源弹窗

- 仓库 URL（必填）
- 源名称（自动从 URL 推断，可自定义）
- Ref（可选，默认 main）
- 凭证管理（复用现有凭证 UI）
- [确认添加] 按钮 → SSE 流式反馈 clone + 技能发现进度

### SkillHub 技能浏览弹窗

- 从索引或远程搜索加载技能列表
- 每项显示：名称、描述、下载量、分类标签
- 搜索框 → 远程搜索
- [安装] 按钮 → 下载 ZIP + 解压

## 测试策略

### 单元测试（`internal/skill/`）

| 测试 | 说明 |
|------|------|
| `TestRegistryFetchIndex` | Mock HTTP server 返回 skills.json，验证解析 |
| `TestRegistrySearch` | Mock 搜索 API，验证 query param 和结果解析 |
| `TestRegistryInstall` | Mock ZIP 下载，验证解压到正确目录 |
| `TestRegistryInstallFallback` | 主通道失败时 fallback 到备选通道 |
| `TestRegistryInstallSHA256` | SHA256 校验正确/错误场景 |
| `TestGitSourceRescan` | 创建含多个子 SKILL.md 的目录，验证扫描结果 |
| `TestGitSourcePullResult` | 模拟新增/更新/删除技能，验证 SyncResult |
| `TestListAllSkills` | 创建多个源+技能目录，验证统一扫描 |
| `TestDeleteSkill` | 删除技能目录并验证 DB 解绑 |

### Web Handler 测试（`internal/web/`）

| 测试 | 说明 |
|------|------|
| `TestHandleSkillsSources` | GET sources 列表 |
| `TestHandleSkillsSourcesGitAdd` | 添加 Git 源（使用 mock） |
| `TestHandleSkillsInstall` | 从注册源安装技能 |
| `TestHandleSkillsSourcesPull` | Git 源拉取更新 |
| `TestHandleSkillsSourcesRemove` | 删除源 |

### 集成测试

| 场景 | 步骤 |
|------|------|
| SkillHub 完整流程 | 添加 skillhub 源 → 搜索技能 → 安装 → 部署到用户 |
| Git 源完整流程 | 添加 Git 源 → 自动发现技能 → 部署 → Pull 更新 |
| 源删除 | 删除 Git 源 → 所有技能目录被清理 → 用户解绑 |
| 多源共存 | 同时有 Git 源 + 注册源 → 技能列表合并 → 无冲突 |

## 安全考虑

- 所有源名/技能名通过 `util.SafePathSegment()` 校验
- ZIP 解压使用现有 `safe_extract_zip` 逻辑（拒绝绝对路径和 `..`）
- Git 命令沿用现有 wrapper 脚本模式，不做命令拼接
- 企业 Registry 的 `AuthHeader` 不在 JSON 响应中暴露
- 源配置的 `Secret` 字段不在 API 响应中返回
- 删除源时确认二次确认（弹窗）

## 迁移策略

### 从旧版本升级

#### 目录迁移（一次性）

旧版本使用 `skill/` 和 `skill-repos/` 两个目录。新版本改用 `skills/`。

**启动时自动检测**：若 `skills/` 目录不存在但 `skill/` 或 `skill-repos/` 存在，执行一次性迁移：

```
启动检测：
  if !skills/ 存在 && (skill/ 存在 || skill-repos/ 存在):
    创建 skills/_migrated_from_v1/.flag   ← 标记

    // 迁移 skill/ 中的已安装技能（来自旧版本 Git 仓库）
    if skill/ 存在:
      遍历 skill/<技能名>/ 下含 SKILL.md 的子目录
      → 复制到 skills/unknown-source/<技能名>/

    // 迁移 skill-repos/（转为新的 Git 源配置）
    if skill-repos/ 存在:
      遍历 skill-repos/<repo>/ 下含 SKILL.md 的子目录
      → 复制到 skills/<repo>/<技能名>/
      → 自动添加对应 GitSource 配置（URL 从旧 SkillsConfig.Repos 中查找）

    写入日志：迁移完成
```

迁移后旧目录保留不动，新版本只读 `skills/`。若迁移过程出错，用户可自行 `rm -rf skills/` 后重启重试。

#### DB 变更

- `skills` 表：启动时自动忽略（不清理，保留只读）
- `user_skills`/`group_skills`：添加 `source` 列（ALTER TABLE），默认空字符串

#### 启动行为变化

`SyncSkillsFromDirectory()`（当前在 `server.go` 启动时调用）**移除**。不再需要 DB ↔ 文件系统同步——文件系统实时扫描就是真相源。

## 附录：SkillHub 协议参考

```
GET https://skillhub-1388575217.cos.ap-guangzhou.myqcloud.com/skills.json
→ {"total": 50, "skills": [{"slug","name","description","version","categories","downloads","stars"}]}

GET https://lightmake.site/api/v1/search?q=<query>&limit=<n>
→ {"results": [{"slug","name","displayName","summary","description","version"}]}

Primary: GET https://lightmake.site/api/v1/download?slug={slug}
Fallback: GET https://skillhub-1388575217.cos.ap-guangzhou.myqcloud.com/skills/{slug}.zip
→ ZIP { SKILL.md, _meta.json }
```
