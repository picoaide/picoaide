# LLM Wiki — 网状知识库设计

## 概述

在 PicoAide 中构建一个 LLM Wiki 系统：Agent 可在对话中检索和阅读的结构化知识库。文档按**树形文件夹**组织权限边界，通过**网状内链**（`[[wikilink]]` 和 `#tag`）连接，Agent 通过 `kb_search` 工具搜索和浏览。

## 架构

```
picoaide (host)
├── SQLite (xorm)
│   ├── knowledge_bases       — 知识库顶层容器
│   ├── kb_folders            — 树形文件夹 (权限边界)
│   ├── kb_folder_users       — 文件夹 → 用户授权
│   ├── kb_folder_groups      — 文件夹 → 组授权
│   ├── kb_documents          — 文档全文
│   ├── kb_links              — 文档间内链
│   ├── kb_tags               — 文档级标签
│   └── kb_documents_fts      — FTS5 全文索引 (trigger 自动同步)
│
│   ├── 导入管道:
│   │   parse (pdfcpu/go-readability/docx) → LLM 分类(可选) → 写表 → 全量重建链接+标签
│   │
│   └── 内部 API (供 picoagent):
│       GET /api/kb/search?q=&user=
│       GET /api/kb/read?doc_id=&user=
│       GET /api/kb/list?folder_id=&user=

picoagent (sandbox)
└── kb_search tool → HTTP → picoaide 内部 API
```

## 数据模型

### 知识库

| 字段 | 类型 | 说明 |
|------|------|------|
| id | INTEGER PK | |
| name | TEXT | 名称 |
| description | TEXT | 描述 |
| created_by | TEXT | 创建者用户名 |
| created_at | INTEGER | |
| updated_at | INTEGER | |

### 文件夹 (树形结构, 权限边界)

| 字段 | 类型 | 说明 |
|------|------|------|
| id | INTEGER PK | |
| kb_id | INTEGER FK → knowledge_bases | |
| parent_id | INTEGER FK → kb_folders | null = 根 |
| name | TEXT | |
| created_at | INTEGER | |
| updated_at | INTEGER | |

权限继承：未显式设置权限的文件夹继承父文件夹权限；根文件夹无设置时仅 superadmin 可访问。

### 文件夹权限

- `kb_folder_users` (folder_id, username)
- `kb_folder_groups` (folder_id, group_id)

### 文档

| 字段 | 类型 | 说明 |
|------|------|------|
| id | INTEGER PK | |
| kb_id | INTEGER FK | |
| folder_id | INTEGER FK | 所在文件夹 (权限由此决定) |
| title | TEXT | |
| content | TEXT | 全文 |
| url | TEXT | 来源 URL |
| source_id | TEXT | 外部文档系统原始 ID |
| source_type | TEXT | manual/upload/web/notion/confluence/… |
| file_type | TEXT | md/txt/html/pdf/docx |
| status | TEXT | pending/processing/ready/error |
| checksum | TEXT | 内容哈希, 增量同步用 |
| created_by | TEXT | |
| created_at | INTEGER | |
| updated_at | INTEGER | |

### 链接 (网状结构)

| 字段 | 类型 | 说明 |
|------|------|------|
| id | INTEGER PK | |
| source_doc | INTEGER FK → kb_documents | 来源文档 |
| target_doc | INTEGER FK → kb_documents | 目标文档 |
| keyword | TEXT | 原文中的关键词文本 |
| created_at | INTEGER | |

### 标签

| 字段 | 类型 | 说明 |
|------|------|------|
| id | INTEGER PK | |
| doc_id | INTEGER FK → kb_documents | |
| tag | TEXT | |
| UNIQUE(doc_id, tag) | | |

### FTS5

```sql
CREATE VIRTUAL TABLE kb_documents_fts USING fts5(
  title, content,
  content=kb_documents, content_rowid=id
);
```

AFTER INSERT/UPDATE/DELETE 触发器自动同步。

## API

### 管理端 (superadmin)

```
GET    /api/admin/knowledge-bases                        — 列表
POST   /api/admin/knowledge-bases                        — 新建
PUT    /api/admin/knowledge-bases/:id                    — 编辑
DELETE /api/admin/knowledge-bases/:id                    — 删除

GET    /api/admin/knowledge-bases/:id/folders            — 文件夹树
POST   /api/admin/knowledge-bases/:id/folders            — 新建文件夹
PUT    /api/admin/knowledge-bases/folders/:id            — 重命名/移动
DELETE /api/admin/knowledge-bases/folders/:id            — 删除

GET    /api/admin/knowledge-bases/folders/:id/permissions    — 查看权限
PUT    /api/admin/knowledge-bases/folders/:id/permissions    — 设置权限
```

### 用户端

```
GET    /api/user/knowledge-bases                         — 可见列表
GET    /api/user/knowledge-bases/:id                     — 知识库概览 (目录树)
GET    /api/user/knowledge-bases/:id/navigate?folder=    — 浏览文件夹内容
GET    /api/user/knowledge-bases/documents/:id           — 读取文档全文
GET    /api/user/knowledge-bases/search?q=&folder=       — FTS5 搜索

POST   /api/user/knowledge-bases/:id/import/upload       — 上传文件
POST   /api/user/knowledge-bases/:id/import/web          — 导入网页
POST   /api/user/knowledge-bases/:id/import/doc          — 文档系统
GET    /api/user/knowledge-bases/imports/:task_id        — 导入进度
```

每个用户端端点内部过滤: 只返回用户有权限的文件夹及其内容。

### 内部 API (供 picoagent)

```
GET /api/kb/search?q=&user=&kb_id=    → [{doc_id, title, snippet, tags, links}]
GET /api/kb/read?doc_id=&user=        → {title, content, tags, links, backlinks}
GET /api/kb/list?folder_id=&user=     → {folders: [...], docs: [...]}
GET /api/kb/tree?kb_id=&user=         → 整棵可访问的目录树
```

## 导入管道

```
文件/URL → parse → [LLM 分类] → 写表 → FTS5 自动同步 → 全量重建链接+标签
```

### 解析
- **PDF**: pdfcpu (纯 Go)
- **DOCX**: 标准库 zip/xml 解析
- **HTML**: go-readability 提取正文
- **MD/TXT**: 直接读

### LLM 分类 (可选, 导入时指定 auto_classify=true)

将文档内容发送给系统配置的 LLM，返回结构化建议:

```json
[
  {"title": "xxx", "content": "...", "suggested_path": "/分类A/子分类B"},
  {"title": "yyy", "content": "...", "suggested_path": "/分类C"}
]
```

系统自动创建缺失的文件夹并写入文档。

### 链接与标签重建

每次导入/更新/删除后，全量扫描该知识库下所有文档:

1. 正则匹配 `[[...]]` → 尝试解析为目标文档标题 → 写入 `kb_links`
2. 正则匹配 `#tag` → 写入 `kb_tags`
3. 未匹配到目标文档的 `[[...]]` 保留为纯文本, 不做链接

## 前端

**路由**: `/user/wiki`

**布局**:
- 左侧面板: 文件夹树 (可折叠) + 标签云 (点击过滤)
- 主区域: Markdown 渲染阅读器, `[[关键词]]` 渲染为可点击的内部链接, `#标签` 高亮
- 文档详情区: 标签列表, 相关文档 (正向链接), 被引用文档 (反向链接)
- 顶部搜索栏: FTS5 搜索, 结果按 rank 排序

## Agent 集成 (kb_search)

注册在 picoagent 的 tool 列表中, 通过内部 API 调用:

```
kb_search(query, scope, doc_id?, folder_id?)
  → scope=search:  FTS5 搜索, 返回标题+片段+链接
  → scope=read:    读取全文+链接+反向链接
  → scope=browse:  浏览文件夹内容
```

权限: 内部 API 根据用户名查询可访问的 folder_id, 所有操作限制在这些文件夹内。

## 定时同步 (后续迭代)

- `source_type` 为 `web`/`notion`/`confluence`/… 的知识库, 可配置定时同步
- 适配器接口: `Syncer { Type(), Sync(ctx, kb) → []Document, ValidateConfig(config) }`
- 每个文档存 `checksum`(内容哈希) 和 `source_id`(外部 ID)
- 同步: 拉取外部列表 → 对比 → 新增/更新/删除 → 触发全量链接重建
- 复用现有 cron 系统 (`/api/cron/create`)

## 权限检查

```
用户请求 → 获取 username
  → 查询 kb_folder_users WHERE username = ?
  → 查询 kb_folder_groups WHERE group_id IN (用户的组)
  → 向上递归补充继承了哪些文件夹
  → 得到可访问的 folder_id 集合
  → 所有搜索/读取/浏览限制在该集合内
```

## 安全

- 所有用户端和管理端端点复用现有 `requireRegularUser` / `requireSuperadmin` 中间件
- CSRF Token 校验复用现有机制
- 文件上传最大 32MB, 支持的类型: pdf/docx/md/txt/html
- 内部 API 只接受来自 picoagent 的 localhost/Unix socket 请求 (同现有模式)

## 非目标 (明确不做的)

- 不做 embedding / 向量检索 (FTS5 + LLM 自己理解内容)
- 不做文档级权限 (只有文件夹级)
- 不做实时协作文档编辑
- 不处理图片/视频/音频

## 实现阶段建议

### Phase 1: 核心 (数据库 + API + 文件导入 + Agent kb_search)
### Phase 2: 前端 Wiki 阅读器
### Phase 3: 网页抓取 + 定时同步
### Phase 4: 文档系统连接器 (逐平台添加)
