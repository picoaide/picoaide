---
title: "系统配置参考"
description: "PicoAide 全局配置结构、字段说明、目录布局和 CLI 命令参考"
weight: 6
draft: false
---

PicoAide 的配置主要存储在 SQLite 数据库中，通过展平键值对存储。超管可以通过管理后台或 API 修改配置。本文档是配置字段的完整参考。

## 配置存储机制

系统使用 `flattenConfig()` / `buildNested()` 机制实现 Go 结构体到点分隔键值对的双向转换：

- 字符串、数字、布尔值直接存储为字符串
- Map 递归展平为多级键（如 `ldap.host`、`ldap.bind_dn`）
- 数组和复杂结构序列化为 JSON blob（`security`、`skills` 整体存储）
- 配置变更记录在 `settings_history` 表中，包含旧值、新值和操作人

## 数据目录结构

初始化时默认数据目录是 `/data/picoaide`。典型结构：

```text
/data/picoaide/
├── picoaide.db              # SQLite 数据库（用户/组/配置/技能）
├── picoaide.sock            # Unix socket（权限 0700，供本地通信）
├── users/                   # 用户工作目录
│   └── <username>/
│       ├── AGENT.md         # AI Agent 行为配置
│       ├── SOUL.md          # 个性设定
│       ├── USER.md          # 用户偏好
│       ├── memory/          # 长期记忆存储
│       │   └── MEMORY.md
│       ├── skills/          # 已部署的技能（只读挂载）
│       └── sessions/        # 对话历史
├── archive/                 # 删除用户时的归档（带时间戳）
├── skill/                   # 已安装的技能目录
├── skill-repos/             # Git 仓库克隆目录
├── rules/                   # 规则缓存
├── shared/                  # 团队空间共享文件夹
├── logs/
│   └── picoaide.log         # 结构化 JSON 日志
└── server.key / server.crt  # TLS 证书（上传后生成）
```

## 全局配置字段

全局配置存储在 SQLite `settings` 表中，以点分隔的键值对存储。

### Web 配置

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `web.auth_mode` | string | `local` | 认证模式：`local` / `ldap` / `oidc` |
| `web.log_level` | string | `info` | 日志级别：`debug` / `info` / `warn` / `error` |
| `web.log_retention` | string | `6m` | 日志保留周期 |

### LDAP 配置

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `ldap.host` | string | | LDAP 服务器地址（含端口） |
| `ldap.bind_dn` | string | | Bind DN |
| `ldap.bind_password` | string | | Bind 密码 |
| `ldap.base_dn` | string | | 用户搜索根 DN |
| `ldap.filter` | string | `(uid={{username}})` | 用户过滤器 |
| `ldap.username_attribute` | string | `uid` | 用户名属性 |
| `ldap.group_search_mode` | string | `member_of` | 组搜索模式：`member_of` 或 `group_search` |
| `ldap.group_base_dn` | string | | 组搜索根 DN |
| `ldap.group_filter` | string | | 组过滤器 |
| `ldap.group_member_attribute` | string | `member` | 组成员属性 |
| `ldap.whitelist_enabled` | bool | `false` | 是否启用白名单 |
| `ldap.sync_interval` | string | `0` | 定时同步间隔（如 `30m`、`1h`），`0` 关闭 |

### OIDC 配置

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `oidc.issuer_url` | string | | Issuer URL |
| `oidc.client_id` | string | | 客户端 ID |
| `oidc.client_secret` | string | | 客户端密钥 |
| `oidc.redirect_url` | string | | 回调地址 |
| `oidc.username_claim` | string | `preferred_username` | 用户名声明字段 |
| `oidc.whitelist_enabled` | bool | `false` | 是否启用白名单 |

### 技能仓库配置

技能仓库写入 `skills` 配置项（整体 JSON blob）：

```json
{
  "repos": [
    {
      "name": "company-skills",
      "url": "https://git.example.com/ai/company-skills.git",
      "ref": "main",
      "ref_type": "branch",
      "public": false,
      "credentials": [
        {
          "name": "gitlab-token",
          "provider": "gitlab",
          "mode": "https",
          "username": "oauth2",
          "secret": "<gitlab-personal-access-token>"
        }
      ]
    }
  ]
}
```

### 安全配置

写入 `security` 配置项（整体 JSON blob）。包含 API Key 等敏感信息，不通过 `config` API 暴露。

### 工具/渠道配置

渠道相关配置项：

| 字段 | 说明 |
|------|------|
| `tools.install_skill.enabled` | 是否允许用户自主安装技能 |

## 环境变量

| 变量 | 说明 |
|------|------|
| `PICOAIDE_DEV` | 设置为 `1` 使用开发模式 |
| `PICOAIDE_ALLOWED_EXTENSION_ORIGINS` | 限制浏览器扩展 CORS 来源 |

## CLI 命令参考

| 命令 | 说明 |
|------|------|
| `picoaide init` | 全自动初始化（创建目录、数据库、超管、systemd 服务） |
| `picoaide serve` | 启动服务（固定 :80，TLS 启用时同时监听 :443） |
| `picoaide reset-password <username>` | 重置本地用户密码 |

## 文件与密钥管理

| 文件 | 推荐权限 | 说明 |
|------|---------|------|
| `picoaide.db` | `0600` | SQLite 数据库 |
| `picoaide.sock` | `0700` | Unix socket（仅本机 root 可访问） |
| `secret` | `0600` | 初始化超管密码（首次登录后自动删除） |
| `server.key` | `0600` | TLS 私钥 |
| `server.crt` | `0644` | TLS 证书 |
