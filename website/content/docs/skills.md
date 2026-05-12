---
title: "技能系统"
description: "PicoAide 技能仓库、上传安装、按用户和按组部署说明"
weight: 3
draft: true
---

技能是部署到用户 PicoClaw 工作区的目录。PicoAide 不限定技能必须使用某种语言；实际部署动作是把技能目录复制到用户的：

```text
users/<username>/.picoclaw/workspace/skills/<skill-name>
```

## 本地目录

服务端工作目录下有两个技能相关目录：

| 目录 | 说明 |
| --- | --- |
| `skill/` | 已安装技能目录 |
| `skill-repos/` | Git 仓库克隆目录 |

`config.SkillsDirPath()` 当前返回工作目录下的 `skill`。

## 技能来源

PicoAide 支持三种来源：

1. 上传 zip 到 `/api/admin/skills/upload`
2. 添加 Git 仓库到 `/api/admin/skills/repos/add`
3. 从已拉取仓库安装指定技能到 `/api/admin/skills/install`

上传 zip 时需要传：

| 字段 | 说明 |
| --- | --- |
| `name` | 技能名 |
| `file` | zip 包 |
| `csrf_token` | CSRF token |

zip 包会被校验路径，避免写出目标目录。

## Git 仓库

仓库配置字段：

| 字段 | 说明 |
| --- | --- |
| `name` | 仓库名称 |
| `url` | Git 地址 |
| `ref` | 分支或 tag |
| `ref_type` | `branch` 或 `tag` |
| `public` | 是否公开仓库 |
| `credentials` | 私有仓库凭据列表 |

支持地址：

- `https://`
- `http://`
- `git@`
- `ssh://`

私有仓库必须提供凭据。Git 操作受全局互斥锁保护，避免并发 clone 或 pull。

## 部署方式

### 部署到单个用户

```text
POST /api/admin/skills/deploy
skill_name=<skill>
username=<user>
```

如果 `skill_name` 为空，服务端会部署 `skill/` 下全部技能。

### 部署到用户组

```text
POST /api/admin/skills/deploy
skill_name=<skill>
group_name=<group>
```

按组部署会走任务队列，目标用户来自组成员展开结果。

### 绑定到用户组

```text
POST /api/admin/groups/skills/bind
group_name=<group>
skill_name=<skill>
```

绑定成功后代码会立即把技能复制到该组所有可部署成员的工作区。

解绑只删除绑定关系，不会自动删除已经复制到用户工作区的技能目录。

## 技能管理接口

| 接口 | 说明 |
| --- | --- |
| `GET /api/admin/skills` | 列出已安装技能和仓库 |
| `POST /api/admin/skills/deploy` | 部署技能 |
| `GET /api/admin/skills/download` | 下载技能 zip |
| `POST /api/admin/skills/remove` | 删除已安装技能 |
| `POST /api/admin/skills/upload` | 上传技能 zip |
| `POST /api/admin/skills/install` | 从仓库安装技能 |
| `GET /api/admin/skills/repos/list` | 列仓库和仓库内技能 |
| `POST /api/admin/skills/repos/add` | 添加并 clone 仓库 |
| `POST /api/admin/skills/repos/save` | 保存仓库配置 |
| `POST /api/admin/skills/repos/pull` | 拉取更新并同步技能 |
| `POST /api/admin/skills/repos/remove` | 删除仓库配置和克隆目录 |

## 推荐流程

1. 在 Git 仓库中维护技能源码。
2. 在管理后台添加仓库并拉取。
3. 安装技能到 `skill/`。
4. 用测试用户部署验证。
5. 绑定到目标用户组。
6. 更新仓库后重新 pull 并部署。
