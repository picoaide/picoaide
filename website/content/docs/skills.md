---
title: "技能系统"
description: "PicoAide 技能系统 — 技能来源、仓库管理、安装部署和生命周期管理"
weight: 7
draft: false
---

技能是部署到用户沙箱工作区的目录。PicoAide 不限定技能必须使用某种语言——任何可以被 AI Agent 理解的指令、配置或脚本都可以作为技能。实际部署动作是将技能目录复制或只读挂载到用户的 `skills/<skill-name>` 目录。

## 技能目录结构

```text
/data/picoaide/
├── skill/                    # 已安装技能目录
│   ├── <skill-name>/
│   │   ├── SKILL.md          # 技能元数据（名称、描述、版本）
│   │   └── ...               # 技能内容文件
│   └── ...
├── skill-repos/              # Git 仓库克隆目录
│   ├── <repo-name>/
│   │   ├── <skill-name>/
│   │   └── ...
│   └── ...
```

## 技能来源

PicoAide 支持三种技能来源：

| 来源 | 管理入口 | 说明 |
|------|---------|------|
| Git 仓库 | 技能管理 → 仓库管理 | 团队 Git 仓库，支持版本管理 |
| 注册中心 | 技能管理 → 注册中心 | 在线技能市场（如 SkillHub） |
| ZIP 上传 | API `POST /api/admin/skills/upload` | 单次部署 |

## Git 仓库管理

### 添加 Git 仓库

在管理后台通过三步向导添加：

1. **基本信息**：填写仓库名称（唯一标识）、Git 地址、引用（branch/tag）
2. **凭据**：私有仓库需填写凭据（支持 GitLab token、GitHub token 等）
3. **完成**：系统自动 clone 仓库，验证 SKILL.md 元数据

支持的 Git 地址协议：

- `https://` — HTTPS 协议
- `http://` — HTTP 协议
- `git@` — SSH 协议
- `ssh://` — SSH 协议（URL 格式）

### 仓库配置字段

```json
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
```

### 仓库维护

| 操作 | 说明 |
|------|------|
| 拉取更新 | `POST /api/admin/skills/sources/pull` — 从远程拉取最新代码 |
| 刷新 | `POST /api/admin/skills/sources/refresh` — 重新扫描目录 |
| 删除 | `POST /api/admin/skills/sources/remove` — 删除仓库和关联技能 |

Git 操作受互斥锁保护，避免并发 clone 或 pull。

## 安装技能

技能安装是将技能从来源复制到 `skill/` 目录的过程。

### 从注册中心安装

1. 在管理后台的技能管理页面打开「注册中心」
2. 搜索技能关键词
3. 查看技能详情（描述、版本）
4. 点击「安装」按钮

安装时系统会自动验证 `SKILL.md` 格式。

### 从 Git 仓库安装

Git 仓库中的技能自动被扫描识别，在仓库详情中可以查看所有可用技能并执行安装。

## 部署技能

技能安装到 `skill/` 目录后，还需要部署到用户才能生效。

### 部署方式

| 方式 | API | 说明 |
|------|-----|------|
| 部署到单个用户 | `POST /api/admin/skills/deploy` | 指定技能和用户名 |
| 部署到用户组 | `POST /api/admin/skills/deploy` | 指定技能和组名，走任务队列 |
| 绑定到组 | `POST /api/admin/groups/skills/bind` | 自动部署到所有现有成员 |
| 绑定到单个用户 | `POST /api/admin/skills/user/bind` | 指定用户名和技能 |

### 部署流程

1. 系统将技能目录从 `skill/<name>/` 复制到 `users/<username>/skills/<name>/`
2. 在沙箱中以只读方式挂载（防止 AI 误修改技能文件）
3. 如果目标用户沙箱正在运行，下次启动时自动生效

### 推荐流程

```text
1. 在 Git 仓库中维护技能源码（团队协作、版本管理）
2. 在管理后台添加 Git 仓库
3. 拉取仓库更新
4. 从仓库安装技能到 skill/ 目录
5. 用测试用户部署验证
6. 设置技能为默认安装（新用户自动获得）
7. 或绑定到目标用户组（现有成员自动获得）
```

## 技能生命周期

```text
添加源 → 拉取/刷新 → 安装到 skill/ → 部署到用户 → 用户沙箱使用
                                    ↑
                              绑定到组（自动批量部署）
```

| 阶段 | 操作 | 效果 |
|------|------|------|
| 添加源 | 添加 Git 仓库或浏览注册中心 | 技能可被发现 |
| 安装 | 复制到 `skill/` 目录 | 技能可被部署 |
| 部署 | 复制到用户工作区 | AI Agent 可调用技能 |
| 解绑 | 删除绑定关系 | 不影响已部署的技能文件 |
| 卸载 | 删除技能文件 | 从 `skill/` 中完全移除 |
| 移除源 | 删除仓库配置 | 源下的技能不可再安装 |

## 用户技能管理

普通用户可以在「技能中心」页面查看和管理自己的技能：

| 状态 | 说明 |
|------|------|
| 已部署 | 管理员或组部署的技能（不可卸载） |
| 已安装 | 用户自己安装的技能（可卸载） |
| 可用 | 未安装，可手动安装 |

用户能否自主安装/卸载技能受管理员的「技能安装策略」控制。

## 默认技能

管理员可以设置技能的默认安装状态。开启后：

- 新创建的用户自动获得该技能
- 不影响已有用户
- 可以在「技能管理」页面切换

## 相关 API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/admin/skills` | 列出已安装技能 |
| `POST` | `/api/admin/skills/deploy` | 部署技能到用户或组 |
| `POST` | `/api/admin/skills/remove` | 删除已安装技能 |
| `POST` | `/api/admin/skills/user/bind` | 绑定技能到单个用户 |
| `POST` | `/api/admin/skills/user/unbind` | 解绑用户技能 |
| `GET` | `/api/admin/skills/sources` | 技能仓库来源列表 |
| `POST` | `/api/admin/skills/sources/git` | 添加 Git 技能仓库 |
| `POST` | `/api/admin/skills/sources/remove` | 移除技能仓库 |
| `POST` | `/api/admin/skills/sources/pull` | 拉取技能仓库更新 |
| `GET` | `/api/admin/skills/registry/list` | 技能注册中心列表 |
| `POST` | `/api/admin/skills/registry/install` | 从注册中心安装技能 |
| `GET` | `/api/admin/skills/defaults` | 默认技能列表 |
| `POST` | `/api/admin/skills/defaults/toggle` | 切换技能默认安装 |
| `GET` | `/api/user/skills` | 用户查看已安装技能 |
| `POST` | `/api/user/skills/install` | 用户安装技能 |
| `POST` | `/api/user/skills/uninstall` | 用户卸载技能 |
