# 团队空间页面

## 页面路由

`/admin/teamspace`

## 权限要求

超管（`superadmin`）登录。普通用户在 `/manage` 通过 tab 访问只读视图。

## 功能概述

管理共享文件夹，支持按用户组配置团队共享目录。共享文件夹通过 bind mount 挂载到用户容器的 `/root/.picoclaw/workspace/share/<名称>/`，实现多 AI Agent 协同工作。

## 功能详细说明

### 1. 共享文件夹列表

**功能**：展示所有共享文件夹，含关联组、成员数和挂载状态

**API 端点**：`GET /api/admin/shared-folders`

**响应格式**：
```json
{
  "success": true,
  "folders": [
    {
      "id": 1,
      "name": "项目文档",
      "description": "项目相关共享文档",
      "is_public": false,
      "created_by": "admin",
      "created_at": "2026-05-12 10:00:00",
      "updated_at": "2026-05-12 10:00:00",
      "groups": [
        {"id": 1, "name": "研发部"},
        {"id": 2, "name": "产品部"}
      ],
      "member_count": 12,
      "mounted_count": 8,
      "orphaned": false,
      "members": [
        {"username": "user1", "mounted": true, "checked_at": "2026-05-12 10:30:00"},
        {"username": "user2", "mounted": false, "checked_at": ""}
      ]
    },
    {
      "id": 2,
      "name": "公司公告",
      "description": "公司全员可见",
      "is_public": true,
      "created_by": "admin",
      "member_count": 30,
      "mounted_count": 20,
      "orphaned": false,
      "groups": []
    }
  ]
}
```

### 2. 创建共享文件夹

**触发方式**：点击「新建共享文件夹」按钮 → 填写表单 → 提交

**API 端点**：`POST /api/admin/shared-folders/create`

**请求格式**（form）：
```
name=项目文档&description=项目相关共享文档&is_public=0&group_ids=1,2&csrf_token=xxx
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| name | string | 是 | 全局唯一，经 `SafePathSegment` 校验 |
| description | string | 否 | 描述 |
| is_public | int | 否 | 0/1，默认为 0 |
| group_ids | string | 否 | 逗号分隔的组 ID，用于初始关联 |

**响应格式（成功）**：
```json
{"success": true, "message": "共享文件夹「项目文档」创建成功", "id": 1}
```

**响应格式（失败——名称冲突）**：
```json
{"success": false, "error": "名称已被占用"}
```

### 3. 修改共享文件夹

**触发方式**：选择某个共享文件夹 → 点击「编辑」按钮 → 修改字段 → 提交

**API 端点**：`POST /api/admin/shared-folders/update`

**请求格式**（form）：
```
id=1&name=项目文档-v2&description=新的描述&is_public=1&csrf_token=xxx
```

**关键行为**：
- **改名时**：自动 `mv` 主机目录 `shared/<old>/ → shared/<new>/`，然后通过 task queue 异步重启所有已挂载用户的容器
- **is_public 切换时**（0↔1）：计算增减用户，仅对变化部分通过 task queue 重启容器

**响应格式**：
```json
{"success": true, "message": "修改成功，正在重启容器（N 个用户）", "task_id": "xxx"}
```

### 4. 删除共享文件夹

**触发方式**：选中某个共享文件夹 → 点击「删除」按钮 → 确认弹窗

**API 端点**：`POST /api/admin/shared-folders/delete`

**请求格式**（form）：
```
id=1&csrf_token=xxx
```

**关键行为**：
1. 计算所有关联用户
2. 将 `shared/<名称>/` 移动到 `archive/shared_<名称>_<时间戳>/`
3. 删除数据库记录（shared_folders, shared_folder_groups, shared_folder_mounts）
4. 通过 task queue 异步重启所有关联用户的容器

**响应格式**：
```json
{"success": true, "message": "共享文件夹已删除，文件已归档，正在重启 N 个用户容器", "task_id": "xxx"}
```

### 5. 管理关联组

**触发方式**：选中共享文件夹 → 在「关联组」区域编辑 → 保存

**API 端点**：`POST /api/admin/shared-folders/groups/set`

**请求格式**（form）：
```
folder_id=1&group_ids=1,2,3&csrf_token=xxx
```

**关键行为**：
- 全量替换 `shared_folder_groups` 关联
- 计算成员增减变化
- 仅对有变化的用户通过 task queue 重启容器
- 空 `group_ids` 表示清除所有组关联

**响应格式**：
```json
{"success": true, "message": "关联组已更新，正在重启 N 个用户容器", "task_id": "xxx"}
```

### 6. 测试挂载状态

**触发方式**：在成员列表中点击某用户行的「测试挂载」按钮

**API 端点**：`POST /api/admin/shared-folders/test`

**请求格式**（form）：
```
folder_id=1&username=user1&csrf_token=xxx
```

**测试逻辑**：
1. 检查主机目录 `shared/<名称>/` 是否存在
2. 检查容器是否在运行
3. Docker exec `test -d /root/.picoclaw/workspace/share/<名称>/` 验证容器内挂载
4. 写入 `shared_folder_mounts` 表缓存结果

**响应格式**：
```json
{"success": true, "mounted": true, "message": "用户 user1 已挂载", "checked_at": "2026-05-12 10:30:00"}
```

### 7. 一键挂载

**触发方式**：选中共享文件夹 → 点击「一键挂载」按钮

**API 端点**：`POST /api/admin/shared-folders/mount`

**请求格式**（form）：
```
folder_id=1&csrf_token=xxx
```

**关键行为**：
1. 计算所有可访问该共享文件夹的普通用户
2. 通过 task queue 异步处理
3. 对每个用户：停止容器 → 删除 → 带所有共享 mounts 重新创建 → 启动容器
4. 记录挂载状态

**响应格式**：
```json
{"success": true, "message": "已提交挂载任务，共 N 个用户", "task_id": "mount-sf-xxx"}
```

### 8. 普通用户视图

**页面路径**：`/manage` → 「团队空间」Tab

**API 端点**：`GET /api/shared-folders`

**响应格式**：
```json
{
  "success": true,
  "folders": [
    {
      "id": 1,
      "name": "项目文档",
      "description": "项目相关共享文档",
      "is_public": false,
      "member_count": 12,
      "container_path": "workspace/share/项目文档/"
    },
    {
      "id": 2,
      "name": "公司公告",
      "description": "公司全员可见",
      "is_public": true,
      "member_count": 30,
      "container_path": "workspace/share/公司公告/"
    }
  ]
}
```

**UI 展示**：只读的 flat 列表，显示共享文件夹的名称、容器内路径、类型标签（公共 / 组共享）、成员数。

## 涉及的 API 端点

| 端点 | 方法 | 说明 | 认证 |
|------|------|------|------|
| `/api/admin/shared-folders` | GET | 列表全部共享文件夹 | 超管 |
| `/api/admin/shared-folders/create` | POST | 创建共享文件夹 | 超管，CSRF |
| `/api/admin/shared-folders/update` | POST | 修改名称/描述/is_public | 超管，CSRF |
| `/api/admin/shared-folders/delete` | POST | 删除共享文件夹 | 超管，CSRF |
| `/api/admin/shared-folders/groups/set` | POST | 设置关联组 | 超管，CSRF |
| `/api/admin/shared-folders/test` | POST | 测试用户挂载状态 | 超管，CSRF |
| `/api/admin/shared-folders/mount` | POST | 一键挂载所有用户 | 超管，CSRF |
| `/api/shared-folders` | GET | 用户可见的共享文件夹列表 | 普通用户 |

## 状态标记

| 状态 | 含义 | UI 表现 |
|------|------|---------|
| 正常 | 有关联组或是公共共享 | 普通显示 |
| 公共 | `is_public=true`，所有非超管可见 | 标记「🌐 公共」 |
| 孤立 | 无关联组且非公共，无人可访问 | 红色警告「⚠ 无关联组」 |
| 已挂载 | 用户容器内已挂载该共享 | ✓ 绿色 |
| 未挂载 | 未测试或未挂载 | ✗ 灰色 |
