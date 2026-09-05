# Multica 代码索引

> Agent 改代码前先定位模块；**勿**全仓盲搜 `node_modules` / `.next` / `dist`。  
> 索引边界见 [.cursorignore](../.cursorignore) · [.cursor/rules/code-index.mdc](../.cursor/rules/code-index.mdc)

---

## 1. 仓库地图

```text
multica/
├── server/                 # Go API · CLI · daemon · sqlc · migrations
├── apps/
│   ├── web/                # Next.js App Router
│   ├── desktop/            # Electron
│   ├── mobile/             # Expo / RN iOS（独立 UI/状态）
│   └── docs/               # Fumadocs
├── packages/
│   ├── core/               # Query · Zustand · API client
│   ├── ui/                 # 原子组件
│   ├── views/              # Web/Desktop 共享业务页
│   ├── tsconfig/
│   └── eslint-config/
├── e2e/                    # Playwright
├── scripts/                # 运维、harness print-status、ai-company
├── .ai-company/            # 卫星项目交付脚手架（另一套 harness）
├── docs/                   # 本仓 Harness 知识库（非产品站）
├── CLAUDE.md               # 编码权威
├── SESSION.md              # 项目续作真相
└── AGENTS.md               # Agent 入口
```

---

## 2. server/

| 路径 | 职责 |
|------|------|
| `server/internal/handler/` | HTTP；UUID 用 loader / `parseUUIDOrBadRequest` |
| `server/internal/service/` | 业务；含 builtin skills |
| `server/internal/store/` | sqlc 查询 |
| `server/migrations/` | 无 FK；CONCURRENTLY 索引单独文件 |
| `server/cmd/` | `server` · `migrate` · daemon 入口 |
| `server/internal/testutil/` | DB fixture + `Call(h, req).Want` |

**测试：** `(cd server && go test …)` 或根目录 `make test`。默认测试禁止解析真实 agent CLI。

---

## 3. packages/

| 包 | 改什么 | 禁止 |
|----|--------|------|
| `packages/core/` | hooks、stores、api schema、paths | react-dom · localStorage · process.env · UI |
| `packages/ui/` | shadcn/Base UI 原子组件 | `@multica/core` · 业务逻辑 |
| `packages/views/` | 共享页面/表单/layout | `next/*` · `react-router-dom` · stores |

Web/Desktop 新页面：先放 `packages/views/<domain>/`，再在 `apps/web/app/` 与 desktop router 接线。

---

## 4. apps/

| 目录 | 职责 | 先读 |
|------|------|------|
| `apps/web/` | Next 路由；平台 API 只在 `apps/web/platform/` | — |
| `apps/desktop/` | Electron；`WindowOverlay` 预工作区流 | CLAUDE Desktop 段 |
| `apps/mobile/` | 独立 UI/状态；只从 core 借 types/纯函数 | `apps/mobile/CLAUDE.md` |
| `apps/docs/` | 产品文档与 conventions | `developers/conventions.mdx` |

---

## 5. 测试落点

| 测什么 | 放哪 |
|--------|------|
| 共享逻辑 / store / query | `packages/core/*.test.ts` |
| 共享 UI / 页面 | `packages/views/*.test.tsx` |
| 平台接线 | `apps/web/` 或 `apps/desktop/` |
| E2E | `e2e/*.spec.ts` |
| 后端 | `server/` |

无 DOM 的 `.test.ts` 必须以 `// @vitest-environment node` 开头。

---

## 6. 决策树

| 你要改… | 去 |
|----------|----|
| Issue / inbox / agent 客户端逻辑 | `packages/core/` |
| 按钮、输入、token | `packages/ui/` |
| 看板/设置等业务页 | `packages/views/` |
| Next 路由或 cookie | `apps/web/` |
| Desktop 窗体/overlay | `apps/desktop/` |
| iOS 界面 | `apps/mobile/` |
| API / 迁移 / daemon | `server/` |
| 卫星站脚手架 | `.ai-company/harness/` |
| 本仓 Agent 续作 | `SESSION.md` |
