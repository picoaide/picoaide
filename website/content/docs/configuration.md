---
title: "配置参考"
description: "PicoAide 配置结构、数据目录、认证、镜像和 Picoclaw 适配包"
weight: 6
draft: false
---

PicoAide 的配置主要存储在 SQLite 数据库中，用户运行时配置落在用户目录。超管可以通过管理后台或 `/api/config` 修改全局配置。

## 数据目录

初始化时默认数据目录是 `/data/picoaide`。典型结构：

```text
/data/picoaide/
  picoaide.db
  users/
    <username>/
      .picoclaw/
        config.json
        .security.yml
        workspace/
          skills/
  archive/
```

`~/.picoaide-config.yaml` 用于保存本机级配置：

```yaml
work_dir: /data/picoaide
rule_cache_dir: /data/picoaide/rules
picoclaw_adapter_remote_base_url: https://raw.githubusercontent.com/picoaide/picoaide/main/rules/picoclaw
```

## 全局配置结构

主要结构来自 `internal/config/config.go`：

| 顶层字段 | 说明 |
| --- | --- |
| `ldap` | LDAP 服务器、用户搜索和组同步配置 |
| `image` | PicoClaw 镜像名、标签、时区、仓库源 |
| `users_root` | 用户目录根路径 |
| `archive_root` | 归档目录根路径 |
| `picoclaw_adapter_remote_base_url` | 适配包远端地址 |
| `web` | 监听、认证模式、日志和 TLS |
| `picoclaw` | 下发给用户的 PicoClaw 配置 |
| `security` | 下发给用户的密钥类配置 |
| `skills` | 技能仓库配置 |

## Web 配置

| 字段 | 说明 |
| --- | --- |
| `web.listen` | 服务监听地址，默认 `:80` |
| `web.container_base_url` | 容器访问服务端时使用的基础地址 |
| `web.password` | 兼容旧版本的会话密钥来源 |
| `web.ldap_enabled` | 兼容旧版本的 LDAP 开关 |
| `web.auth_mode` | `local` 或 `ldap` |
| `web.log_retention` | 访问日志保留周期 |
| `web.log_level` | `debug`、`info`、`warn`、`error` |
| `web.tls.enabled` | 是否启用 TLS |
| `web.tls.cert_file` | 证书路径 |
| `web.tls.key_file` | 私钥路径 |

当 TLS 启用且监听地址是 `:443` 时，服务端会额外启动 `:80` 入口。来自 Docker 网络的内部请求仍由应用处理，外部 HTTP 请求重定向到 HTTPS。

## 认证配置

### 本地模式

`web.auth_mode = "local"` 时，普通用户和超管都由本地数据库管理。普通用户可以在 `/manage` 修改自己的密码。

### LDAP 模式

`web.auth_mode = "ldap"` 时，普通用户通过 LDAP 验证，本地超管仍可登录管理后台。

| 字段 | 说明 |
| --- | --- |
| `ldap.host` | LDAP 服务器地址 |
| `ldap.bind_dn` | Bind DN |
| `ldap.bind_password` | Bind 密码 |
| `ldap.base_dn` | 用户搜索根 DN |
| `ldap.filter` | 用户过滤器 |
| `ldap.username_attribute` | 用户名属性 |
| `ldap.group_search_mode` | `member_of` 或 `group_search` |
| `ldap.group_base_dn` | 组搜索根 DN |
| `ldap.group_filter` | 组过滤器 |
| `ldap.group_member_attribute` | 组成员属性 |
| `ldap.whitelist_enabled` | 是否使用白名单 |
| `ldap.sync_interval` | 定时同步间隔，`0` 表示关闭 |

`sync_interval` 支持 Go duration，比如 `30m`、`1h`、`24h`。纯数字会按小时处理。

## 镜像配置

| 字段 | 说明 |
| --- | --- |
| `image.name` | 镜像名 |
| `image.tag` | 默认标签 |
| `image.timezone` | 容器时区 |
| `image.registry` | `github` 或 `tencent` |

拉取地址由代码生成：

| registry | 拉取地址 |
| --- | --- |
| `github` | `ghcr.io/picoaide/picoaide:<tag>` |
| `tencent` | `hkccr.ccs.tencentyun.com/picoaide/picoaide:<tag>` |

腾讯云模式拉取后会 retag 成统一的 `ghcr.io/...` 引用，便于后续容器记录统一。

## 技能仓库配置

技能仓库写入 `skills.repos`：

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

支持的 Git 地址前缀：

- `https://`
- `http://`
- `git@`
- `ssh://`

私有仓库必须配置凭据。仓库会克隆到工作目录下的 `skill-repos/`，同步安装到 `skill/`。

## Picoclaw 适配包

Picoclaw adapter 位于 `rules/picoclaw`，包含：

- `index.json`
- `hash`
- `schemas/config-v*.json`
- `ui/ui-v*.json`
- `migrations/*.json`

管理后台可以刷新远端适配包，也可以上传 zip。上传 zip 时不能包含 `picoclaw` 顶层目录，应直接压缩 `rules/picoclaw/` 下的文件。

## 文件权限

| 文件 | 推荐权限 | 说明 |
| --- | --- | --- |
| `picoaide.db` | `0600` | SQLite 数据库 |
| `.security.yml` | `0600` | 用户密钥配置 |
| `config.json` | `0644` | 用户 PicoClaw 配置 |

## 常用命令

```bash
picoaide init
picoaide init -user <username>
picoaide serve -listen :80
picoaide reset-password <username>
```
