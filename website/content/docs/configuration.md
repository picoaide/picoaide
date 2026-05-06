---
title: "配置参考"
description: "PicoAide 配置详细说明"
weight: 6
draft: false
---

PicoAide 的配置存储在 SQLite 数据库中，通过管理面板或 API 进行修改。旧版本的 `config.yaml` 文件会在首次运行时自动迁移到数据库。

## 配置方式

### 通过管理面板

安装浏览器扩展后，使用超管账户登录，在管理面板中可以直接修改：

- 认证配置（LDAP/本地/OIDC）
- 镜像仓库设置
- 全局模型配置
- 技能仓库管理

### 通过 API

```bash
# 读取当前配置
curl http://localhost/api/config \
  -b "picoaide-session=your-session-cookie"

# 更新配置（合并模式，只传需要修改的字段）
curl -X POST http://localhost/api/config \
  -H "Content-Type: application/json" \
  -b "picoaide-session=your-session-cookie" \
  -d '{"picoclaw": {"agents": {"defaults": {"model_name": "claude-4.7"}}}}'
```

### 从旧版配置迁移

如果存在旧版 `config.yaml` 文件，首次运行时系统会自动检测并迁移：

```
检测到 config.yaml，正在迁移到数据库...
```

## 配置结构

### 认证配置

支持三种认证模式，通过管理面板的「认证配置」页面切换：

| 模式     | 说明                                       |
| -------- | ------------------------------------------ |
| `local`  | 仅使用本地账号认证，不连接外部服务（默认） |
| `ldap`   | 使用 LDAP 服务器认证，同时支持本地超管账号 |
| `oidc`   | 使用 OpenID Connect 认证                   |

#### LDAP 配置

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

组搜索模式说明：

- **`member_of`**：通过用户的 `memberOf` 属性获取所属组
- **`group_search`**：通过搜索组的 `member` 属性反向查找用户所属组

### 镜像仓库配置

`image.registry` 控制容器镜像的拉取源：

| 值         | 拉取地址                                          | 说明         |
| ---------- | ------------------------------------------------- | ------------ |
| `github`   | `ghcr.io/picoaide/picoaide:<tag>`                 | GitHub 默认  |
| `tencent`  | `hkccr.ccs.tencentyun.com/picoaide/picoaide:<tag>` | 腾讯云镜像   |

初始化时选择镜像仓库后，后续也可以在管理面板中切换。

### 目录结构

初始化完成后，数据目录（默认 `/data/picoaide`）结构如下：

```
/data/picoaide/
  picoaide.db        # SQLite 数据库（配置、用户、容器状态）
  users/             # 用户数据目录
    zhangsan/
      .picoclaw/
        config.json    # 用户 PicoClaw 配置
        .security.yml  # 安全配置（API 密钥，权限 0600）
  archive/           # 归档目录
```

### TLS 配置

启用 HTTPS 需要在配置中设置 TLS 证书：

```json
{
  "web": {
    "tls": {
      "enabled": true,
      "cert_file": "/path/to/cert.pem",
      "key_file": "/path/to/key.pem"
    }
  }
}
```

也可以使用 Nginx 等反向代理来终止 TLS。

### PicoClaw AI 代理配置

全局 PicoClaw 配置会通过合并策略下发到各用户容器：

- 管理员通过 API 或管理面板修改全局配置
- 系统将全局配置合并到用户配置，用户已有的键值保留，缺失的键从全局补充
- 合并后的配置写入 `users/<用户名>/.picoclaw/config.json`
- 重启容器后生效

主要配置项：

```json
{
  "picoclaw": {
    "agents": {
      "defaults": {
        "model_name": "gpt-5.4",
        "max_tokens": 32768,
        "max_tool_iterations": 50
      }
    },
    "model_list": [
      {
        "model_name": "gpt-5.4",
        "model": "openai/gpt-5.4",
        "api_base": "https://api.openai.com/v1",
        "request_timeout": 6000
      }
    ],
    "gateway": {
      "host": "0.0.0.0",
      "port": 18790
    }
  }
}
```

### 安全配置

模型 API 密钥存储在安全配置中：

```json
{
  "security": {
    "model_list": {
      "gpt-5.4:0": {
        "api_keys": ["sk-openai-replace-me"]
      }
    }
  }
}
```

## 文件权限

| 文件              | 推荐权限 | 说明                       |
| ----------------- | -------- | -------------------------- |
| `picoaide.db`     | 0600     | SQLite 数据库              |
| `.security.yml`   | 0600     | 用户安全配置（API 密钥）  |
| `config.json`     | 0644     | 用户 PicoClaw 配置         |

## CLI 命令参考

```bash
# 首次运行引导（交互式）
picoaide init

# 初始化指定用户
picoaide init -user <username>

# 启动 Web 管理面板（通常由 systemd 管理）
picoaide serve -listen :80

# 重置本地用户密码
picoaide reset-password <username>
```
