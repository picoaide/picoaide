---
title: "配置参考"
description: "PicoAide 配置文件详细说明"
weight: 6
draft: false
---

PicoAide 使用 `config.yaml` 作为主配置文件，首次运行时自动生成。本文档详细说明各配置项。

## 全局配置

### config.yaml 完整结构

```yaml
# LDAP 认证配置
ldap:
  host: "ldap://ldap.example.com:389"
  bind_dn: "cn=admin,dc=example,dc=com"
  bind_password: "your-password"
  base_dn: "ou=users,dc=example,dc=com"
  filter: "(objectClass=inetOrgPerson)"
  username_attribute: "uid"
  group_search_mode: "member_of"       # member_of | group_search
  group_base_dn: ""
  group_filter: ""
  group_member_attribute: "member"
  whitelist_enabled: false
  sync_interval: "0"                   # "0" 禁用, "1h", "24h", "30m"

# 容器镜像配置
image:
  name: "ghcr.io/picoaide/picoaide"
  timezone: "Asia/Shanghai"
  registry: "github"                   # github | tencent

# 目录配置
users_root: "./users"                  # 用户数据根目录
archive_root: "./archive"              # 归档目录

# Web 服务配置
web:
  listen: ":80"
  password: "change-me-to-a-random-secret"
  auth_mode: "ldap"                    # ldap | local
  tls:
    enabled: false
    cert_file: ""
    key_file: ""

# PicoClaw AI 代理配置
picoclaw:
  agents:
    defaults:
      model_name: "gpt-5.4"
      max_tokens: 32768
      max_tool_iterations: 50
  model_list:
    - model_name: "gpt-5.4"
      model: "openai/gpt-5.4"
      api_base: "https://api.openai.com/v1"
      request_timeout: 6000
  channels: {}
  tools:
    web:
      duckduckgo:
        enabled: true
    mcp:
      enabled: true
      max_inline_text_chars: 8192
      servers:
        browser:
          enabled: false
  gateway:
    host: "0.0.0.0"
    port: 18790

# 安全配置
security:
  model_list:
    gpt-5.4:0:
      api_keys:
        - "sk-openai-replace-me"

# 技能仓库配置
skills:
  repos: []
```

## LDAP 配置

LDAP 配置控制用户认证和组同步行为。

| 配置项                   | 类型   | 默认值                         | 说明                       |
| ------------------------ | ------ | ------------------------------ | -------------------------- |
| `host`                   | string | `ldap://ldap.example.com:389`  | LDAP 服务器地址            |
| `bind_dn`                | string | -                              | 绑定用户 DN                |
| `bind_password`          | string | -                              | 绑定用户密码               |
| `base_dn`                | string | -                              | 用户搜索基础 DN            |
| `filter`                 | string | `(objectClass=inetOrgPerson)`  | 用户搜索过滤器             |
| `username_attribute`     | string | `uid`                          | 用户名属性                 |
| `group_search_mode`      | string | `member_of`                    | 组搜索模式                 |
| `group_base_dn`          | string | 空                             | 组搜索基础 DN              |
| `group_filter`           | string | 空                             | 组搜索过滤器               |
| `group_member_attribute` | string | `member`                       | 组成员属性                 |
| `whitelist_enabled`      | bool   | `false`                        | 是否启用白名单             |
| `sync_interval`          | string | `0`                            | 自动同步间隔               |

### 组搜索模式

- **`member_of`**：通过用户的 `memberOf` 属性获取所属组
- **`group_search`**：通过搜索组的 `member` 属性反向查找用户所属组

### 认证模式

`web.auth_mode` 控制系统的认证方式：

| 模式     | 说明                                       |
| -------- | ------------------------------------------ |
| `ldap`   | 使用 LDAP 服务器认证，同时支持本地超管账号 |
| `local`  | 仅使用本地账号认证，不连接 LDAP            |

## 镜像仓库配置

`image.registry` 控制容器镜像的拉取源：

| 值         | 拉取地址                                    | 说明         |
| ---------- | ------------------------------------------- | ------------ |
| `github`   | `ghcr.io/picoaide/picoaide:<tag>`           | GitHub 默认  |
| `tencent`  | `hkccr.ccs.tencentyun.com/picoaide/picoaide:<tag>` | 腾讯云镜像   |

## TLS 配置

启用 HTTPS 需要配置 TLS 证书：

```yaml
web:
  tls:
    enabled: true
    cert_file: "/path/to/cert.pem"
    key_file: "/path/to/key.pem"
```

配置 TLS 后，PicoAide 会自动在 HTTPS 端口上提供服务，MCP SSE 端点也会使用 HTTPS。

## 配置下发

PicoAide 通过配置合并策略将全局配置下发到各用户容器：

1. 管理员修改 `config.yaml` 中的全局配置
2. 系统调用 `util.MergeMap()` 将全局配置合并到用户配置
3. 合并规则：用户已有的键值保留，缺失的键从全局默认值补充
4. 合并后的配置写入 `users/<用户名>/.picoclaw/config.json`
5. 重启容器后生效

```bash
# 通过 API 更新全局配置
curl -X POST http://localhost/api/config \
  -H "Content-Type: application/json" \
  -b "picoaide-session=your-session-cookie" \
  -d '{"picoclaw": {"agents": {"defaults": {"model_name": "claude-4.7"}}}}'
```

## 配置文件权限

| 文件              | 推荐权限 | 说明                       |
| ----------------- | -------- | -------------------------- |
| `config.yaml`     | 0600     | 包含密码和 API 密钥       |
| `whitelist.yaml`  | 0644     | 白名单配置                 |
| `picoaide.db`     | 0600     | SQLite 数据库              |
| `.security.yml`   | 0600     | 用户安全配置（API 密钥）  |
| `config.json`     | 0644     | 用户 PicoClaw 配置         |
