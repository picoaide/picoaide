# 技能库页面

## 页面路由

`/admin/skills`

## 权限要求

超管（`superadmin`）登录

## 功能概述

管理 AI 代理的技能库，包括技能列表、部署技能到用户/组、上传/下载技能 ZIP、管理技能仓库（Git 仓库的添加/拉取/删除）。

## 功能详细说明

### 1. 技能列表

**功能**：展示所有已安装的技能

**API 端点**：`GET /api/admin/skills`

**响应格式**：
```json
{
  "success": true,
  "skills": [
    {
      "name": "code-review",
      "version": "1.0.0",
      "description": "代码审查技能",
      "type": "builtin",
      "installed_at": "2024-01-01T00:00:00Z",
      "deployed_users": ["user1", "user2"],
      "deployed_groups": [{"id": 1, "name": "研发部"}]
    }
  ]
}
```

### 2. 部署技能到用户/组

**触发方式**：在技能详情中选择"部署到用户"或"部署到组"

**API 端点**：`POST /api/admin/skills/deploy`

**请求格式**：
```json
{
  "skill_name": "code-review",
  "target_type": "user",
  "targets": ["user1", "user2"]
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| skill_name | string | 技能名称 |
| target_type | string | 部署目标类型：`user` 或 `group` |
| targets | array | 目标用户名或组名的列表 |

**响应格式**：
```json
{
  "success": true,
  "message": "部署成功",
  "deployed": ["user1", "user2"],
  "failed": []
}
```

### 3. 上传技能 ZIP

**触发方式**：点击"上传技能"按钮 → 选择 ZIP 文件 → 上传

**API 端点**：`POST /api/admin/skills/upload`

**请求格式**（multipart/form-data）：

| 字段 | 类型 | 说明 |
|------|------|------|
| file | file | ZIP 格式的技能包 |

**响应格式**：
```json
{
  "success": true,
  "message": "技能上传成功",
  "name": "custom-skill",
  "version": "1.0.0"
}
```

### 4. 下载技能 ZIP

**触发方式**：点击技能行的"下载"按钮

**API 端点**：`GET /api/admin/skills/download?name=code-review`

**响应**：ZIP 文件二进制流。

### 5. 删除技能

**触发方式**：点击技能行的"删除"按钮 → 确认删除

**API 端点**：`POST /api/admin/skills/remove`

**请求格式**：
```json
{
  "name": "custom-skill"
}
```

**响应格式**：
```json
{
  "success": true,
  "message": "技能已删除"
}
```

### 6. 技能仓库管理

技能可以从 Git 仓库安装，支持 SSH 和 HTTPS 两种认证方式。

#### 仓库列表

**API 端点**：`GET /api/admin/skills/repos/list`

**响应格式**：
```json
{
  "success": true,
  "repos": [
    {
      "name": "official-skills",
      "url": "https://github.com/picoaide/skills.git",
      "branch": "main",
      "last_pulled": "2024-01-01T00:00:00Z"
    }
  ]
}
```

#### 添加仓库

**API 端点**：`POST /api/admin/skills/repos/add`

**请求格式**：
```json
{
  "name": "my-skills",
  "url": "git@github.com:user/skills.git",
  "branch": "main",
  "auth_type": "ssh",
  "ssh_key": "-----BEGIN OPENSSH PRIVATE KEY-----\n...",
  "ssh_key_passphrase": ""
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| name | string | 是 | 仓库名称 |
| url | string | 是 | Git 仓库 URL |
| branch | string | 否 | 分支名，默认 `main` |
| auth_type | string | 否 | `ssh` 或 `https` |
| ssh_key | string | 否 | SSH 私钥内容 |
| ssh_key_passphrase | string | 否 | SSH 私钥密码 |
| https_username | string | 否 | HTTPS 用户名 |
| https_token | string | 否 | HTTPS 访问令牌 |

#### 拉取仓库更新

**API 端点**：`POST /api/admin/skills/repos/pull`

**请求格式**：
```json
{
  "name": "my-skills"
}
```

#### 删除仓库

**API 端点**：`POST /api/admin/skills/repos/remove`

**请求格式**：
```json
{
  "name": "my-skills"
}
```

## 涉及的 API 端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/admin/skills` | GET | 技能列表 |
| `/api/admin/skills/deploy` | POST | 部署技能到用户/组 |
| `/api/admin/skills/upload` | POST | 上传技能 ZIP |
| `/api/admin/skills/download` | GET | 下载技能 ZIP |
| `/api/admin/skills/remove` | POST | 删除技能 |
| `/api/admin/skills/repos/list` | GET | 技能仓库列表 |
| `/api/admin/skills/repos/add` | POST | 添加技能仓库 |
| `/api/admin/skills/repos/pull` | POST | 拉取仓库更新 |
| `/api/admin/skills/repos/remove` | POST | 删除技能仓库 |
