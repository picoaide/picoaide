---
title: "文档"
description: "PicoAide 文档中心 — 架构设计、管理员手册、用户指南和 API 参考"
weight: 1
draft: false
---

PicoAide 是一个面向企业内网的 AI 工作平台，为每位员工分配独立的 AI 操作助手，同时确保企业数据的边界得到守护。它将用户认证、容器沙箱隔离、浏览器控制、桌面操作、文件管理、技能分发、定时任务和 MCP 中继整合到统一控制平面中。

本文档按角色和场景分层组织。如果你是**系统管理员**，从「快速入门」开始，然后进入管理员线了解日常运维和配置。如果你是**普通用户**，从「架构设计」了解整体概念后，直接跳到用户线查看你的操作面板和客户端使用方法。

## 快速上手

- **[快速入门](/docs/quick-start/)** — 从零开始安装、初始化和验证 PicoAide 部署
- **[架构设计](/docs/architecture/)** — 理解系统设计理念、架构决策和组件关系

## 管理员文档

- **[安装与部署](/docs/install-deploy/)** — 生产环境安装、升级和数据迁移
- **[管理后台操作指南](/docs/admin-guide/)** — 用户、组、技能、认证、MCP 服务的完整管理流程
- **[认证与安全配置](/docs/auth-security/)** — Local、LDAP、OIDC 三种认证模式和白名单配置
- **[系统配置参考](/docs/configuration/)** — 全局配置字段、目录结构和 CLI 命令
- **[技能系统](/docs/skills/)** — 技能仓库管理、安装部署和生命周期管理
- **[故障排查](/docs/troubleshooting/)** — 常见问题的诊断步骤和解决方案

## 用户文档

- **[Web 面板操作指南](/docs/web-panel/)** — 对话、文件、频道、技能中心和定时任务管理
- **[浏览器扩展](/docs/browser-extension/)** — 安装授权和 AI 浏览器控制
- **[桌面客户端](/docs/desktop-client/)** — 安装连接、权限组和白名单配置

## 技术参考

- **[MCP 与 AI 集成](/docs/mcp-integration/)** — MCP 协议、Token 管理、工具调用和配置
- **[API 参考](/docs/api/)** — HTTP API、MCP SSE 和管理接口完整参考
- **[常见问题](/docs/faq/)** — 高频问题汇总
