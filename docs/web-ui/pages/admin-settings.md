# 系统配置页面

## 页面路由

`/admin/settings`

## 权限要求

超管（`superadmin`）登录

## 功能概述

查看和编辑全局系统配置。使用 JSON 编辑器提供配置的在线修改能力。配置以展平键值对格式存储在 SQLite `settings` 表中。

## 功能详细说明

### 1. 读取全局配置

**API 端点**：`GET /api/config`

**响应格式**：
```json
{
  "success": true,
  "web.listen": ":80",
  "web.auth_mode": "local",
  "web.session_ttl": "24h",
  "ldap.host": "ldap.example.com:389",
  "ldap.base_dn": "dc=example,dc=com",
  "oidc.issuer_url": "https://accounts.example.com",
  "oidc.client_id": "my-client",
  "registry.mirror": "ghcr.io/picoaide/picoclaw",
  "registry.dev_mirror": "hkccr.ccs.tencentyun.com/picoaide/picoclaw",
  "picoclaw": "{...}",
  "security": "{...}",
  "skills": "{...}"
}
```

**暴露规则**：以下配置键**不暴露**给前端：
- 所有以 `internal.` 开头的配置
- `web.password`

### 2. 保存全局配置

**API 端点**：`POST /api/config`

**请求格式**（可以提交部分配置，只更新提供的键）：
```json
{
  "web.listen": ":8080",
  "web.auth_mode": "ldap",
  "ldap.host": "ldap.newserver.com:389"
}
```

**响应格式**：
```json
{
  "success": true,
  "message": "保存成功"
}
```

### 3. JSON 编辑器

页面提供一个 JSON 编辑器组件（如 Monaco Editor 或 CodeMirror）用于编辑配置：

- 读取时将配置转换为 JSON 对象展示
- 编辑后保存时，前端将 JSON 展平为点分隔键值对提交
- 编辑器支持语法高亮和格式验证

**配置展平规则**（由服务端 `flattenConfig` 处理）：

- 字符串/数字/布尔值直接存储为字符串值
- 嵌套 map 递归展平，键用 `.` 连接
- 数组整体序列化为 JSON 字符串
- `picoclaw`、`security`、`skills` 三个顶层键整体序列化为 JSON blob

**反向构建**（由服务端 `buildNested` 处理）：

- 读取展平的 KV 对，根据键的 `.` 分隔符重建嵌套结构
- 尝试解析 JSON 字符串值为数组或对象

### 4. 认证模式切换

当修改 `web.auth_mode` 时触发认证模式切换逻辑：

1. 服务端检测 `oldMode != newMode`
2. 调用 `purgeOrdinaryAuthProviderStateForConfig()`：
   - 删除所有 Docker 容器（如果 Docker 可用）
   - 清空 `containers` 表
   - 删除所有普通用户（`DELETE FROM local_users WHERE role != 'superadmin'`）
   - 清空 `groups` 和 `user_groups` 表
   - 删除 `users/` 目录下所有内容
   - 删除 `archive/` 目录下所有内容
3. 记录审计日志到 `settings_history` 表
4. 重启定时同步任务

### 5. 配置审计历史

配置变更记录在 `settings_history` 表中，包含以下信息：

| 字段 | 说明 |
|------|------|
| key | 被修改的配置键 |
| old_value | 修改前的值 |
| new_value | 修改后的值 |
| changed_by | 修改人用户名 |
| changed_at | 修改时间 |

（审计查询功能需额外开发，当前仅存储未提供查询 API）

## 涉及的 API 端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/config` | GET | 读取全局配置 |
| `/api/config` | POST | 保存全局配置 |
