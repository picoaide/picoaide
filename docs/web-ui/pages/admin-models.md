# 模型配置页面

## 页面路由

`/admin/models`

## 权限要求

超管（`superadmin`）登录

## 功能概述

管理 AI 模型配置，用于配置代理容器使用的 AI 模型参数。

## 功能详细说明

### 模型配置管理

**功能**：配置 AI 模型的连接参数和默认模型选择

**API 端点**：`GET /api/config` 读取配置，`POST /api/config` 保存配置

**配置说明**：模型配置存储在全局配置的 `picoclaw` 键下，作为 JSON blob 整体序列化存储。

**读取模型配置**：`GET /api/config`

相关配置键：
```
picoclaw → 整体序列化为 JSON blob，包含模型配置
```

典型模型配置结构：
```yaml
picoclaw:
  model: "gpt-4"
  api_base: "https://api.openai.com/v1"
  api_key: "sk-xxx"
  max_tokens: 4096
  temperature: 0.7
```

**保存模型配置**：`POST /api/config`
```json
{
  "picoclaw": {
    "model": "gpt-4",
    "api_base": "https://api.openai.com/v1",
    "max_tokens": 4096,
    "temperature": 0.7
  }
}
```

### API Key 安全

- `api_key` 等敏感字段保存时进行脱敏处理
- 读取配置时已存储的 `api_key` 返回掩码格式（如 `sk-...xyz`）
- 修改时需要完整填写新值，留空则保留原有值

### 配置生效

- 模型配置保存后写入全局 `settings` 表
- 通过配置下发（`POST /api/admin/config/apply`）推送到各用户容器
- 用户容器重启后新配置生效

## 涉及的 API 端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/config` | GET | 读取全局配置（含模型配置） |
| `/api/config` | POST | 保存全局配置 |
| `/api/admin/config/apply` | POST | 下发配置到用户 |
