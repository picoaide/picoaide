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

**请求格式**（form）：
```
skill_name=code-review&username=user1&csrf_token=xxx
```

| 字段 | 类型 | 说明 |
|------|------|------|
| skill_name | string | 技能名称 |
| username | string | 目标用户名（可选，与 group_name 二选一） |
| group_name | string | 目标组名（可选，与 username 二选一） |

**响应格式**：
```json
{
  "success": true,
  "message": "部署成功",
  "deployed": ["user1"],
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

### 6. 技能仓库管理（技能来源）

技能可以从 Git 仓库安装。技能来源（sources）管理相关端点如下：

#### 来源列表

**API 端点**：`GET /api/admin/skills/sources`

**响应格式**：
```json
{
  "success": true,
  "sources": [
    {
      "name": "official-skills",
      "url": "https://github.com/picoaide/skills.git",
      "ref": "main",
      "ref_type": "branch",
      "last_pulled": "2024-01-01T00:00:00Z"
    }
  ]
}
```

#### 添加 Git 来源

**API 端点**：`POST /api/admin/skills/sources/git`

**请求格式**（form）：
```
name=official-skills&url=https://github.com/picoaide/skills.git&ref_type=branch&ref=main&csrf_token=xxx
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| name | string | 是 | 来源名称 |
| url | string | 是 | Git 仓库 URL |
| ref_type | string | 否 | `branch` 或 `tag`，默认 `branch` |
| ref | string | 否 | 分支名或标签名，默认 `main` |

#### 拉取来源更新

**API 端点**：`POST /api/admin/skills/sources/pull`

**请求格式**（form）：
```
name=official-skills&csrf_token=xxx
```

#### 刷新来源

**API 端点**：`POST /api/admin/skills/sources/refresh`

**请求格式**（form）：
```
name=official-skills&csrf_token=xxx
```

#### 删除来源

**API 端点**：`POST /api/admin/skills/sources/remove`

**请求格式**（form）：
```
name=official-skills&csrf_token=xxx
```

### 7. 技能绑定到单个用户

#### 绑定技能

**API 端点**：`POST /api/admin/skills/user/bind`

**请求格式**（form）：
```
skill_name=code-review&username=user1&csrf_token=xxx
```

#### 解绑技能

**API 端点**：`POST /api/admin/skills/user/unbind`

**请求格式**（form）：
```
skill_name=code-review&username=user1&csrf_token=xxx
```

### 8. 技能注册中心

**注册中心列表**：`GET /api/admin/skills/registry/list?source=official-skills`

**响应格式**：
```json
{
  "success": true,
  "list": [
    { "slug": "code-review", "name": "Code Review", "description": "代码审查技能", "version": "1.0.0" }
  ]
}
```

**从注册中心安装**：`POST /api/admin/skills/registry/install`

**请求格式**（form）：
```
source=official-skills&slug=code-review&csrf_token=xxx
```

### 9. 默认技能

**获取默认技能列表**：`GET /api/admin/skills/defaults`

**响应格式**：
```json
{
  "success": true,
  "skills": [
    { "name": "code-review", "default": true }
  ]
}
```

**切换默认安装状态**：`POST /api/admin/skills/defaults/toggle`

**请求格式**（form）：
```
skill_name=code-review&csrf_token=xxx
```

## 涉及的 API 端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/admin/skills` | GET | 技能列表 |
| `/api/admin/skills/deploy` | POST | 部署技能到用户/组 |
| `/api/admin/skills/remove` | POST | 删除技能 |
| `/api/admin/skills/upload` | POST | 上传技能 ZIP |
| `/api/admin/skills/download` | GET | 下载技能 ZIP |
| `/api/admin/skills/user/bind` | POST | 绑定技能到单个用户 |
| `/api/admin/skills/user/unbind` | POST | 解绑用户技能 |
| `/api/admin/skills/user/sources` | GET | 用户技能来源列表 |
| `/api/admin/skills/sources` | GET | 技能仓库来源列表 |
| `/api/admin/skills/sources/git` | POST | 添加 Git 技能仓库 |
| `/api/admin/skills/sources/remove` | POST | 移除技能仓库 |
| `/api/admin/skills/sources/pull` | POST | 拉取技能仓库更新 |
| `/api/admin/skills/sources/refresh` | POST | 刷新技能仓库 |
| `/api/admin/skills/registry/list` | GET | 技能注册中心列表 |
| `/api/admin/skills/registry/install` | POST | 从注册中心安装技能 |
| `/api/admin/skills/defaults` | GET | 默认技能列表 |
| `/api/admin/skills/defaults/toggle` | POST | 切换技能默认安装 |
