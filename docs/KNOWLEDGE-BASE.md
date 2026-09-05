# Multica 知识库（Harness Basic）

> **slug:** `multica` · **Vault:** `10-SYSTEM/HARNESS/projects/multica.md`  
> Agent 进仓后 **60 秒内** 应读完本页 + [CODE-INDEX.md](./CODE-INDEX.md) + 根目录 `CLAUDE.md`。

---

## 1. 项目是什么

**Multica** 是面向小团队的 AI-native 任务管理平台：Agent 作为一等公民 assignee，可以认领 issue、评论、改状态。自托管，对接多种 agent CLI。

| 子系统 | 路径 | 说明 |
|--------|------|------|
| Go API | `server/` | Chi · sqlc · websocket · CLI · daemon |
| Web | `apps/web/` | Next.js App Router |
| Desktop | `apps/desktop/` | Electron，共享 `packages/views` |
| Mobile | `apps/mobile/` | Expo / React Native iOS |
| Docs | `apps/docs/` | Fumadocs 产品文档 |
| Core | `packages/core/` | Query / Zustand / API client |
| UI | `packages/ui/` | 原子组件，无业务 |
| Views | `packages/views/` | Web/Desktop 共享业务页 |

**远程：** https://github.com/multica-ai/multica  
**本机 fork：** https://github.com/chenzh/multica

---

## 2. 读文档顺序（写代码前）

1. [`CLAUDE.md`](../CLAUDE.md) — 编码、状态、包边界、迁移、API 兼容（单一权威）
2. [`AGENTS.md`](../AGENTS.md) — 短指针 + 常用命令
3. [CODE-INDEX.md](./CODE-INDEX.md) — 模块地图与索引边界
4. [`SESSION.md`](../SESSION.md) — 仓级 `phase` / `next` / `blockers`
5. 命名 / i18n / 中文文案 → `apps/docs/content/docs/developers/conventions.mdx`
6. 动 Mobile → `apps/mobile/CLAUDE.md`
7. 架构补充 → `apps/docs/content/docs/developers/architecture.mdx`

### Harness / 续作

1. [VAULT-HARNESS.md](./VAULT-HARNESS.md) — Vault 规范快照（`sync-all-harness` 生成）
2. Vault `04-PROJECTS/2026-08-28-project-multica.md` — 第二大脑项目 MOC
3. 两套 harness **不要混 todo**：
   - **本仓开发** = SecondBrain Harness（本页 / SESSION）
   - **卫星项目交付** = `.ai-company/harness`（复制到其他 git 仓的流水线脚手架）

---

## 3. 硬规则速查

完整条文在 `CLAUDE.md`。最容易写错的几条：

| 域 | 规则 |
|----|------|
| 状态 | Query = 服务端；Zustand = 客户端；WS 只补 Query，不把 payload 镜像进 Zustand |
| 包 | `core` 无 DOM/localStorage/env；`ui` 无 `@multica/core`；`views` 无 `next/*` / `react-router-dom` |
| DB | 无 FK / cascade；索引 `CREATE [UNIQUE] INDEX CONCURRENTLY`，一文件一条 |
| API | `parseWithFallback` + zod；handler 里 UUID 先分辨来源再写入 |
| Desktop | 预工作区流程用 `WindowOverlay`，不要塞进 `routes.tsx` |
| 提交 | 用户没要求就不要 commit / push |

---

## 4. 常用命令

```bash
make up                 # 启动本 checkout 环境
make status             # 本环境在跑什么（pid/commit 证明）
make dev                # 前台自动 setup + 启动
bash scripts/print-status.sh
pnpm typecheck
pnpm test
make test
make check
```

Worktree 用 `.env.worktree` 隔离端口和数据库，见 `CLAUDE.md` Commands。

---

## 5. 连接

- Vault harness：`10-SYSTEM/HARNESS/projects/multica.md`
- Profile：`10-SYSTEM/HARNESS/profiles/multica-monorepo.md`
- 协作薄指针：[SECOND-BRAIN-HERMES-COLLAB.md](./SECOND-BRAIN-HERMES-COLLAB.md)
