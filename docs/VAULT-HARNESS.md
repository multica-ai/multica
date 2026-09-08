# Vault Harness Snapshot
> slug: multica · generated: 2026-08-29 09:35

---

## Global

---
type: system
status: evergreen
created: 2026-06-30
tags:
  - harness
  - global
---

# Global Harness — 所有外接仓必遵守

> 工作区根目录存在 `.secondbrain` 时生效。

## 开干前（强制）

1. 读 `10-SYSTEM/HARNESS/registry.json`，匹配当前仓 `slug` / `path` / `remote`
2. 依次读：`global.md` → `profiles/{profile}.md` → `projects/{slug}.md`
3. Monorepo 子项目：读 registry 中 `subprojects[]` 对应 `projects/{slug}.md`
4. **Brain-first（强制倾向）**：优先 gbrain MCP；否则 `打包第二大脑上下文` / `/zbrain-pack` → `think-vault`；合成 + `[[笔记]]` + `缺口：…`。见 [[GBRAIN-LAYER]] · [[AI-CONTRACT]]
5. **Vibecoding**：先解析当前 **chat-id** → 读 `{项目根}/sessions/<chat-id>.md`（若有）；项目级读 `{项目根}/SESSION.md`。见 [[session-protocol]] · [[2026-07-06-map-vibecoding-scratchpad]]

## Vibecoding 会话内

- **「继续 / 推进」按会话 ID 路由**（见 [[session-protocol]]）：有 `sessions/<chat-id>.md` 只跑该文件 next；勿串台到项目 SESSION  
- 明示「继续项目 / 推进 SESSION」才读项目级 `SESSION.md`  
- openworld **monorepo**：子项目仍各有项目级 SESSION；会话文件放在该子项目 `sessions/`  
- **不读不写** `active-site.json`（防多会话污染）  
- `记 scratch：…` → 写入**当前 chat-id** 的会话文件（无则建）

## 收工（有实质推进时）

1. **PATCH** `sessions/<chat-id>.md`（本聊天 next/done）；仅当变更属仓级战略债时再 PATCH 项目 `SESSION.md`  
2. **追加** 根或项目 `worklog/yyyy-MM-dd.md`「已完成」  
3. **milestone** → 静默 `deposit-second-brain -Source milestone`  
4. **git push**（项目 SESSION/worklog 为换机续作；会话文件可一并提交）

## 多执行器（Cursor · Hermes · OpenClaw）

- 项目真相：**SESSION.md**；Cursor 聊天进度：**sessions/\<chat-id\>.md**（见 [[session-protocol]]）
- 知识沉淀：**Vault**（见 [[AUTO-DEPOSIT]]）
- Hermes 运行时 vs 第二大脑 Portfolio：见 [[SECOND-BRAIN-HERMES-COLLAB]]
- Hermes Kanban **默认关闭**；战略 todo 不写 Kanban

## 编码原则

- **最小 diff**：只改任务相关文件，不顺手重构
- **复用现有约定**：命名、目录、抽象与周边代码一致
- **可执行优先**：直接给能跑的结果，不过度设计
- **中文沟通，代码英文**：UI 文案按项目 locale（见 profile / project harness）

## 安全

- 禁止把 API 密钥、token、`.env` 写入 Vault 或 commit
- 破坏性命令先确认；优先 `trash` 而非 `rm`

## 沉淀（默认自动）

里程碑（修完 / 部署 / 定方案 / 踩坑解决）→ 静默 `deposit-second-brain -Source milestone`  
详见 `10-SYSTEM/AUTO-DEPOSIT.md`。

## 禁止

- 在代码仓复制 SecondBrain 全文规则（仅允许薄指针 `vault-harness.mdc`）
- 空壳交付、构建失败仍称完成
- 未经要求的 git commit / push

## 连接

- [[AI-CONTRACT]]
- [[2026-06-14-permanent-multi-ide-secondbrain]]
- [[session-protocol]]
- [[SECOND-BRAIN-HERMES-COLLAB]]

---

## Profile: multica-monorepo

---
type: system
status: active
created: 2026-08-28
tags:
  - harness
  - profile
  - multica
---

# Profile: multica-monorepo

Multica 产品仓：Go 后端 + pnpm/Turborepo 前端（Web / Desktop / Mobile）+ 本地 daemon。

权威编码规则在仓内 **`CLAUDE.md`**，本 profile 只固定 Agent 进仓后的加载顺序与硬边界。不要在 Vault 复制 CLAUDE 全文。

## 开干前

1. 读根目录 `CLAUDE.md`（命名、状态、包边界、迁移、API 兼容、Desktop/Mobile）
2. 读 `docs/KNOWLEDGE-BASE.md` → `docs/CODE-INDEX.md` → `SESSION.md`
3. 动 `apps/mobile/` 之前先读 `apps/mobile/CLAUDE.md`
4. 写代码 / 改 i18n / 起路由前读 `apps/docs/content/docs/developers/conventions.mdx`（及 `.zh.mdx`）

## 硬边界（违反即错）

- `packages/core/`：禁止 `react-dom`、`localStorage`、`process.env`、UI 库
- `packages/ui/`：禁止 `@multica/core` 与业务逻辑
- `packages/views/`：禁止 `next/*`、`react-router-dom`；路由走 `NavigationAdapter`
- 服务端状态归 TanStack Query；Zustand 只放客户端/视图状态
- 数据库：**禁止** `FOREIGN KEY` / `REFERENCES` / cascade；索引必须 `CREATE [UNIQUE] INDEX CONCURRENTLY`，且单独成迁移文件
- 前端解析 API 用 `parseWithFallback` + zod，禁止把网络 JSON 直接断言成 `T`
- 未经用户明确要求：禁止 git commit / push

## 构建门禁

按改动范围跑最窄检查，再按风险加宽。宣称完成前，相关门禁必须通过：

```bash
pnpm typecheck
pnpm test
make test
make check
```

Web UI 行为变更须在浏览器里走通真实交互，不能只截一张静态图。

## 命令入口

以仓库 `Makefile` / 根 `package.json` 为准：`make up` · `make status` · `make dev` · `pnpm dev:web` · `pnpm dev:desktop`。

## 连接

- 仓内：`CLAUDE.md` · `AGENTS.md` · `SESSION.md`
- Vault：`projects/multica.md`

---

## Project: multica

---
type: project
status: active
created: 2026-08-28
updated: 2026-08-28
tags:
  - harness
  - multica
project: multica
---

# Harness: Multica

## 项目

- **名称**：Multica — AI-native 任务管理；Agent 作为一等公民 assignee
- **路径**：`/Users/zhenhuachen/Projects/multica`
- **远程**：`multica-ai/multica` · https://github.com/multica-ai/multica（fork：`chenzh/multica`）
- **Profile**：`multica-monorepo`

## 架构（Monorepo）

| 模块 | 路径 | 说明 |
|------|------|------|
| API | `server/` | Go · Chi · sqlc · websocket |
| Web | `apps/web/` | Next.js App Router |
| Desktop | `apps/desktop/` | Electron，共享 views |
| Mobile | `apps/mobile/` | Expo / RN iOS |
| Docs | `apps/docs/` | Fumadocs |
| Core | `packages/core/` | 无 UI 的业务逻辑 / Query / Zustand |
| UI | `packages/ui/` | 原子组件 |
| Views | `packages/views/` | Web/Desktop 共享业务页 |
| AI 公司流水线 | `.ai-company/` | 卫星项目交付脚手架（与本 Harness 分层） |

## 仓内知识库 / 索引（Basic）

| 文件 | 职责 |
|------|------|
| `CLAUDE.md` | 产品编码单一权威 |
| `docs/KNOWLEDGE-BASE.md` | 知识库 MOC |
| `docs/CODE-INDEX.md` | 代码地图 |
| `AGENTS.md` | Agent 入口（指向 CLAUDE + Harness） |
| `.cursor/rules/vault-harness.mdc` | Vault 触发器 |
| `.cursor/rules/code-index.mdc` | 索引边界 |
| `.cursorignore` | 不索引路径 |
| `SESSION.md` | 项目续作真相 |

## 约定

- 写代码前读：`CLAUDE.md` → `KNOWLEDGE-BASE` → `CODE-INDEX` → `SESSION`
- 产品任务走 GitHub issues；**仓级战略债**只写 `SESSION.md`
- Cursor 多会话写 `sessions/<chat-id>.md`，禁止串台
- 卫星站交付走 `.ai-company/harness`，**不要**和本仓 SecondBrain Harness 混成一套 todo

## Tier 2（本仓扩展）

| 文件 | 职责 |
|------|------|
| `CLAUDE.md` / `apps/mobile/CLAUDE.md` | 编码与 Mobile 预检 |
| `apps/docs/content/docs/developers/conventions.mdx` | 命名 / i18n / 中文产品声音 |
| `.ai-company/` | AI 公司运营与卫星 harness |
| `.github/workflows/ci.yml` | 主 CI |
| `Makefile` | `up` / `status` / `test` / `check` |
| `worklog/` | 日流水 |

## 构建 / 验收

- TS：`pnpm typecheck` · `pnpm test`
- Go：`make test`
- 全量：`make check`
- Web UI：浏览器真实交互，不只截图

## 连接

- Vault MOC：`04-PROJECTS/2026-08-28-project-multica.md`
- [[repo-contract]] · [[session-protocol]] · [[profiles/multica-monorepo]]
