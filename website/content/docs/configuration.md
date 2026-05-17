---
title: "系统配置参考"
description: "PicoAide 全局配置结构、字段说明和文件布局"
weight: 6
draft: false
---

PicoAide 的配置主要存储在 SQLite 数据库中，通过展平键值对存储。超管可以通过管理后台或 API 修改配置。本文档是配置字段的完整参考。

## 配置文件结构

### 数据目录

初始化时默认数据目录是 `/data/picoaide`。典型结构：

```text
/data/picoaide/
├── picoaide.db              # SQLite 数据库
├── users/                   # 用户工作目录
│   └── <username>/
│       └── .picoclaw/
│           ├── config.json      # PicoClaw 配置
│           ├── .security.yml    # 密钥配置
│           └── workspace/
│               ├── skills/      # 已部署的技能
│               └── ...          # 用户工作文件
├── archive/                 # 删除用户时的归档
├── skill/                   # 已安装的技能
├── skill-repos/             # Git 仓库克隆目录
├── rules/                   # Picoclaw Adapter 缓存
│   └── picoclaw/
│       ├── index.json
│       ├── hash
│       ├── schemas/
│       ├── ui/
│       └── migrations/
└── logs/
    └── picoaide.log          # 结构化日志
```

## 全局配置字段

全局配置存储在 SQLite `settings` 表中，以点分隔的键值对存储。以下是所有可用字段。

### Web 配置

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `web.listen` | string | `:80` | 服务监听地址 |
| `web.auth_mode` | string | `local` | 认证模式：`local`、`ldap`、`oidc` |
| `web.ldap_enabled` | bool | `false` | 兼容旧版本：未设置 `web.auth_mode` 时根据此字段推断认证模式（已弃用，建议直接设置 `web.auth_mode`） |
| `web.container_base_url` | string | `http://100.64.0.1:80` | 容器访问服务端的基础地址 |
| `web.log_level` | string | `info` | 日志级别：`debug`、`info`、`warn`、`error` |
| `web.log_retention` | string | `6m` | 日志保留周期（可选值：`1m`、`3m`、`6m`、`1y`、`3y`、`5y`、`forever`） |
| `web.tls.enabled` | bool | `false` | 是否启用 TLS |
| `web.tls.cert_file` | string | | TLS 证书路径 |
| `web.tls.key_file` | string | | TLS 私钥路径 |

当 TLS 启用且监听地址是 `:443` 时，服务端会额外启动 `:80` 入口处理 HTTP 到 HTTPS 的重定向。

### LDAP 配置

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `ldap.host` | string | | LDAP 服务器地址（含端口） |
| `ldap.bind_dn` | string | | Bind DN |
| `ldap.bind_password` | string | | Bind 密码 |
| `ldap.base_dn` | string | | 用户搜索根 DN |
| `ldap.filter` | string | `(uid={{username}})` | 用户过滤器 |
| `ldap.username_attribute` | string | `uid` | 用户名属性 |
| `ldap.group_search_mode` | string | `member_of` | `member_of` 或 `group_search` |
| `ldap.group_base_dn` | string | | 组搜索根 DN |
| `ldap.group_filter` | string | | 组过滤器 |
| `ldap.group_member_attribute` | string | `member` | 组成员属性 |
| `ldap.whitelist_enabled` | bool | `false` | 是否使用白名单 |
| `ldap.sync_interval` | string | `0` | 定时同步间隔，`0` 关闭 |

### OIDC 配置

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `oidc.issuer_url` | string | | Issuer URL |
| `oidc.client_id` | string | | 客户端 ID |
| `oidc.client_secret` | string | | 客户端密钥 |
| `oidc.redirect_url` | string | | 回调地址 |
| `oidc.username_claim` | string | `preferred_username` | 用户名声明字段 |

### 镜像配置

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `image.name` | string | `ghcr.io/picoaide/picoaide` | 镜像名 |
| `image.tag` | string | `latest` | 默认标签 |
| `image.timezone` | string | `Asia/Shanghai` | 容器时区 |
| `image.registry` | string | `github` | 仓库源：`github` 或 `tencent` |
| `image.cpu_limit` | int64 | | CPU 限制（NanoCPUs） |
| `image.memory_limit` | int64 | | 内存限制（字节） |

拉取地址由代码生成：

| registry | 拉取地址 |
|----------|---------|
| `github` | `ghcr.io/picoaide/picoaide:<tag>` |
| `tencent` | `hkccr.ccs.tencentyun.com/picoaide/picoaide:<tag>` |

腾讯云模式拉取后会 retag 成统一的 `ghcr.io/...` 引用，便于后续容器记录统一。

### 技能仓库配置

技能仓库写入 `skills.repos` 数组：

```yaml
skills:
  repos:
    - name: company-skills
      url: https://git.example.com/ai/company-skills.git
      ref: main
      ref_type: branch
      public: false
      credentials:
        - name: gitlab-token
          provider: gitlab
          mode: https
          username: oauth2
          secret: <token>
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `name` | string | 仓库名称 |
| `url` | string | Git 地址 |
| `ref` | string | 分支或 tag |
| `ref_type` | string | `branch` 或 `tag` |
| `public` | bool | 是否公开仓库 |
| `credentials` | array | 私有仓库凭据列表 |

支持的 Git 地址协议：`https://`、`http://`、`git@`、`ssh://`。

## Picoclaw Adapter 包

Picoclaw Adapter 位于 `rules/picoclaw`，负责配置的版本管理和迁移。

### 目录结构

```text
rules/picoclaw/
├── index.json               # 包元信息、支持的版本范围
├── hash                     # SHA256 校验和
├── schemas/
│   ├── config-v1.json       # v1 配置 schema 定义
│   ├── config-v2.json       # v2 配置 schema 定义
│   └── config-v3.json       # v3 配置 schema 定义
├── ui/
│   ├── ui-v1.json           # v1 UI 表单定义
│   ├── ui-v2.json
│   └── ui-v3.json
└── migrations/
    ├── v1-to-v2.json        # v1→v2 迁移规则
    └── v2-to-v3.json        # v2→v3 迁移规则
```

### 配置版本

`PicoAideSupportedPicoClawConfigVersion = 3` 指定当前支持的 Picoclaw 配置版本。

迁移引擎支持的操作规则：

| 操作 | 说明 |
|------|------|
| `move` | 移动字段到新位置 |
| `rename` | 重命名字段 |
| `delete` | 删除字段 |
| `set` | 设置默认值 |

### 刷新方式

支持两种方式更新 Adapter 包：

1. **远程 URL 拉取**：从 `picoclaw_adapter_remote_base_url` 拉取，自动校验 SHA256
2. **ZIP 上传**：通过管理后台上传 zip 包。上传时 zip 内应直接包含 `index.json`、`hash`、`schemas/`、`ui/`、`migrations/` 等文件，不要包一层 `picoclaw/` 目录

## 文件权限

| 文件 | 推荐权限 | 说明 |
|------|---------|------|
| `picoaide.db` | `0600` | SQLite 数据库 |
| `.security.yml` | `0600` | 用户密钥配置 |
| `config.json` | `0644` | 用户 PicoClaw 配置 |

## CLI 命令参考

| 命令 | 说明 |
|------|------|
| `picoaide init` | 全自动初始化 |
| `picoaide serve` | 启动服务（监听地址由后台配置，默认 :80） |
| `picoaide reset-password <username>` | 重置本地用户密码 |

## 环境变量

| 变量 | 说明 |
|------|------|
| `PICOAIDE_DEV` | 设置为 `1` 使用开发镜像 |
| `PICOAIDE_ALLOWED_EXTENSION_ORIGINS` | 限制浏览器扩展 CORS 来源 |
| `PICOAIDE_RULE_CACHE_DIR` | 规则缓存目录 |
| `PICOAIDE_PICOCLAW_ADAPTER_URL` | Adapter 远程 URL |
