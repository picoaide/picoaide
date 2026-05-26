---
title: "PicoAide v1.0.0 正式发布"
description: "PicoAide 首个正式版本发布，提供完整的企业级 AI 工作平台"
date: 2026-05-06
draft: false
weight: 1
tags: ["发布"]
---

今天我们很高兴地宣布，PicoAide v1.0.0 正式发布。作为企业级 AI 工作平台，PicoAide 致力于让每位员工拥有自己的 AI 操作助手，同时确保企业数据的边界得到守护。

PicoAide 的核心理念是「AI 的力量应当被释放，但数据的边界必须被守护」。通过私有化部署、原生 Linux 沙箱隔离和 Provider 注册表认证，企业可以在不将数据暴露给外部的情况下，让 AI 真正进入日常工作流程。

## 核心功能

PicoAide v1.0.0 提供了完整的平台功能。**原生沙箱隔离**方面，每个用户拥有独立的 AI 操作助手容器，通过 overlayfs + network namespace 实现隔离，使用 `picoaide-br` 网桥（100.64.0.0/16）和 iptables DROP 规则确保容器间完全隔离，不依赖 Docker。**MCP 三层中继架构**实现了 AI 与外部工具的桥梁，提供 browser、computer、agent 三个 MCP 服务，通过 SSE + WebSocket 双协议架构，支持浏览器标签页控制、桌面环境操作和平台内置工具。AI 可以自主完成网页浏览、表单填写、文件处理等复杂任务。**认证可插拔**方面，采用 Provider 注册表模式，通过 `init()` 自动注册认证源，支持 Local、LDAP、OIDC 三种模式，新增认证源无需修改核心代码。

## 技术架构

PicoAide 采用 Go 语言开发，使用 Gin 框架提供 HTTP 服务，SQLite（xorm + modernc）记录状态。采用双层 Gin 引擎架构：内部处理器仅暴露沙箱所需的最小 API 子集，外部处理器包含完整路由，通过源 IP 自动分发。配置管理采用展平键值对存储在 SQLite 中，支持实时修改和认证源切换自动清理。MCP 协议层支持标准的 `initialize`、`tools/list`、`tools/call` JSON-RPC 方法，协议版本为 `2024-11-05`，同时支持 Legacy SSE 和 Streamable HTTP 传输层。agent 服务支持聚合所有工具来源（平台、浏览器、桌面、第三方 MCP）。

## 客户端生态

浏览器扩展（PicoAide Helper，Chrome Manifest V3）让 AI 能够控制浏览器标签页，提供 19 个 browser 操作工具。桌面客户端（Python PyQt）让 AI 能够控制用户的桌面环境，提供 15 个 computer 桌面工具和 6 个权限组。两项客户端均通过 WebSocket 与 PicoAide 服务端建立连接，通过 ServiceHub 管理连接生命周期。同时还提供 6 个平台内置工具（picoaide_*）和支持第三方 MCP 服务器的代理集成。

## 开始使用

现在就开始体验 PicoAide。访问 [快速开始](/docs/quick-start/) 指南，几分钟内即可完成部署。源码和安装包可在 [GitHub](https://github.com/picoaide/picoaide) 获取。如果你在使用过程中遇到问题，请查阅 [文档](/docs/) 或在 GitHub 上提交 Issue。
