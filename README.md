# Ship

> Lilith 基于开源项目 [multica-ai/multica](https://github.com/multica-ai/multica) 自研的 **AI Agent Harness 平台**。把 Claude Code / Codex / OpenCode 等 AI coding 工具，从单兵 CLI 升级为**团队级长周期任务执行系统**。

| | |
|---|---|
| 平台入口 | https://multica.lilithgames.com |
| 产品文档 | [飞书 Ship 平台使用文档](https://lilithgames.feishu.cn/docx/K5NwdtGsYoaLhExh9JAcxE2PnTb) |
| 上游仓库 | https://github.com/multica-ai/multica |

## 一句话

中间步骤（拉代码、根因分析、编译、跑测试）AI 自动跑；关键节点（出方案、出 diff、shelve CL）**强制人工 approve**。一次任务可跑几小时甚至跨天，只在检查点打扰人。

## 核心概念

| 概念 | 含义 |
|---|---|
| **Workspace** | 工作区，团队 / 数据 / 权限隔离单元，一个项目组一个 |
| **Agent** | 可被分配任务的 AI 队友，绑定一个 Runtime + Provider |
| **Runtime** | 真正执行 Agent 的机器（本地或云端），daemon 注册即生效 |
| **Provider** | Agent 后端 CLI：Claude Code / Codex / OpenCode / OpenClaw / Gemini / Cursor / ... |
| **Issue** | 平台中的任务单（看板卡片），可分配给人或 Agent |
| **Autopilot** | 定时 / 手动触发的自动化任务模板，做巡检、日报、扫描等 |

完整使用流程见[飞书 Ship 平台使用文档](https://lilithgames.feishu.cn/docx/K5NwdtGsYoaLhExh9JAcxE2PnTb)。

## 与上游的关系

- 上游 `multica-ai/multica` 是开源项目，由社区维护
- 我们 fork 在 `gitlab.lilithgame.com/devops/multica`，一**周同步一次**上游 main → 本地 develop
- 我们的内部改动以**扩展为主**（详见 [CONTRIBUTING.md → Scope: Extend, Don't Rewrite](CONTRIBUTING.md#scope-extend-dont-rewrite)）
- 适合上游的改动**也提一个 upstream PR**，合并后本地 diff 自动消失

## 仓库结构

```
server/                 Go 后端（Chi + sqlc + WebSocket）
apps/web/               Next.js 16（生产入口）
apps/desktop/           Electron 桌面端（开发体验）
packages/core/          headless 业务逻辑（store、API、permissions）
packages/views/         共享 UI 组件、页面（web 和 desktop 共用）
packages/ui/            原子组件（shadcn + Base UI）
deploy/k8s/             阿里云 ACK 部署（kustomize overlay）
```

技术栈：Next.js 16 + Go 1.26 + PostgreSQL 17 (pgvector) + Electron。

## 开发上手

```bash
make dev    # 一键起 DB + 后端 + 前端，自动处理 worktree
```

详见 [CONTRIBUTING.md](CONTRIBUTING.md)：

- [开发环境与数据库](CONTRIBUTING.md#first-time-setup)
- [Worktree 隔离](CONTRIBUTING.md#environment-files)
- [测试](CONTRIBUTING.md#testing)
- **[内部 PR 规范](CONTRIBUTING.md#submitting-changes-internal-contributors)** ← 提交代码前必读

## 部署

生产环境运行在阿里云 ACK + 阿里云 RDS PostgreSQL，部署由 release owner 操作，详见 [deploy/k8s/README.md](deploy/k8s/README.md)。普通开发者不需要碰部署，merge 到 develop 就好。

## 反馈

有问题、bug、改进建议：在 [Ship 平台](https://multica.lilithgames.com) 提一个 issue，或在内部飞书 Ship 群里讨论。
