---
title: "技能系统"
description: "PicoAide 技能开发与管理"
weight: 3
draft: false
---

技能（Skills）是 PicoAide 扩展 AI 能力的核心机制。通过技能系统，管理员可以为不同用户组部署定制化的工具和能力。

## 什么是技能

技能是对内部网站和工具的 CLI 包装，让 PicoClaw AI 能够调用企业内部的 Web 服务。每个技能本质上是一个目录，包含可被 AI 代理调用的工具文件。

技能可以包括：

- **内部系统接口**：如 OA 系统、CRM、ERP 的 API 封装
- **数据处理工具**：如报表生成、数据查询
- **自动化脚本**：如批量操作、定时任务触发
- **文档检索**：如知识库查询、内部 Wiki 搜索

## 技能目录结构

每个技能是一个独立目录，存放在 `skills/` 下：

```
skills/
├── skill-a/
│   ├── manifest.json        # 技能元数据
│   ├── tools/
│   │   ├── query.sh         # 工具脚本
│   │   └── report.py        # 工具脚本
│   └── config/
│       └── defaults.json    # 默认配置
├── skill-b/
│   ├── manifest.json
│   └── ...
```

## 技能开发流程

### 1. 创建技能仓库

管理员在 Git 仓库中管理技能代码：

```bash
# 技能仓库结构示例
my-skills-repo/
├── internal-tools/
│   ├── manifest.json
│   └── tools/
│       └── search.sh
├── report-generator/
│   ├── manifest.json
│   └── tools/
│       └── generate.py
└── README.md
```

### 2. 添加技能仓库

通过 API 或管理后台添加 Git 仓库：

```bash
# 添加技能仓库
curl -X POST http://localhost/api/admin/skills/repos/add \
  -H "Content-Type: application/json" \
  -b "picoaide-session=your-session-cookie" \
  -d '{"name": "company-skills", "url": "https://git.example.com/skills/company-skills.git"}'
```

### 3. 拉取技能

从 Git 仓库拉取最新技能到本地：

```bash
# 拉取指定仓库的技能
curl -X POST http://localhost/api/admin/skills/repos/pull \
  -H "Content-Type: application/json" \
  -b "picoaide-session=your-session-cookie" \
  -d '{"name": "company-skills"}'
```

### 4. 测试环境验证

在部署到生产环境之前，先在测试用户容器中验证技能：

```bash
# 部署技能到单个测试用户
curl -X POST http://localhost/api/admin/skills/deploy \
  -H "Content-Type: application/json" \
  -b "picoaide-session=your-session-cookie" \
  -d '{"skill_name": "internal-tools", "username": "test-user"}'
```

### 5. 部署到生产环境

验证通过后，通过组部署将技能批量部署给目标用户：

```bash
# 部署技能到整个组
curl -X POST http://localhost/api/admin/skills/deploy \
  -H "Content-Type: application/json" \
  -b "picoaide-session=your-session-cookie" \
  -d '{"skill_name": "internal-tools", "group_name": "engineering"}'
```

## Git 仓库管理

### 查看仓库列表

```bash
curl http://localhost/api/admin/skills/repos/list \
  -b "picoaide-session=your-session-cookie"
```

### 删除仓库

```bash
curl -X POST http://localhost/api/admin/skills/repos/remove \
  -H "Content-Type: application/json" \
  -b "picoaide-session=your-session-cookie" \
  -d '{"name": "old-skills"}'
```

## 基于组的部署

PicoAide 支持基于用户组的技能绑定，实现差异化能力分配：

### 绑定技能到组

```bash
curl -X POST http://localhost/api/admin/groups/skills/bind \
  -H "Content-Type: application/json" \
  -b "picoaide-session=your-session-cookie" \
  -d '{"group_name": "finance", "skill_name": "financial-report"}'
```

### 解绑技能

```bash
curl -X POST http://localhost/api/admin/groups/skills/unbind \
  -H "Content-Type: application/json" \
  -b "picoaide-session=your-session-cookie" \
  -d '{"group_name": "finance", "skill_name": "financial-report"}'
```

### 组层级

组支持父子层级关系，部署到父组时递归包含所有子组的成员。这使得技能管理可以按照组织架构进行：

```
公司
├── 技术部
│   ├── 前端组
│   └── 后端组
└── 财务部
    ├── 会计组
    └── 审计组
```

部署技能到「技术部」组时，前端组和后端组的成员都会获得该技能。

## 技能生命周期

```
开发 → Git 提交 → 添加仓库 → 拉取技能 → 测试验证 → 绑定组 → 部署 → 更新迭代
```

每次技能更新后，重新拉取并部署即可将新版本推送到用户容器。
