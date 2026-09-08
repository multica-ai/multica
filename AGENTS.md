# Repository Guidelines

This file provides guidance to AI agents when working with code in this repository.

> **Single source of truth:** This file is a concise pointer document.
> All authoritative architecture, coding rules, and conventions
> live in **CLAUDE.md** at the project root. Read that file first.
> Use `Makefile`, `package.json`, and `pnpm-workspace.yaml` as the
> source of truth for the full command list.

<!-- zbrain-harness:start -->
## zbrain / SecondBrain (generated)

This repo is joined to SecondBrain. **Before coding:**

1. Read `CLAUDE.md`, then `docs/KNOWLEDGE-BASE.md` and `docs/CODE-INDEX.md`
2. Continue via `SESSION.md` (project) or `sessions/<chat-id>.md` (per-chat)
3. Snapshot: `docs/VAULT-HARNESS.md` · triggers: `.cursor/rules/vault-harness.mdc` · `.cursor/rules/company-harness.mdc`
4. Do not duplicate Vault or Company OS rules here — resync with `同步项目规范到全部外接仓库` / `rollout-harness-tier0.sh`

Gateway on a machine with the Vault cloned: `zb status` / `zb advance`
<!-- zbrain-harness:end -->

## Quick Reference

### Architecture

Go backend + monorepo frontend (pnpm workspaces + Turborepo) with shared packages.

- `server/` - Go backend (Chi router, sqlc, gorilla/websocket)
- `apps/web/` - Next.js frontend (App Router)
- `apps/desktop/` - Electron desktop app
- `apps/mobile/` - Expo / React Native iOS app (read `apps/mobile/CLAUDE.md` first)
- `apps/docs/` - Fumadocs documentation site
- `packages/core/` - Headless business logic (Zustand stores, React Query hooks, API client)
- `packages/ui/` - Atomic UI components (shadcn/Base UI, zero business logic)
- `packages/views/` - Shared business pages/components
- `packages/tsconfig/` - Shared TypeScript config
- `packages/eslint-config/` - Shared ESLint config

### State Management (critical)

- **React Query** owns all server state (issues, members, agents, inbox, workspace list)
- **Zustand** owns client/view state (view filters, drafts, modals, desktop tab state); current workspace identity is route-driven and only mirrored for platform plumbing
- All Zustand stores live in `packages/core/` - never in `packages/views/` or app directories
- WS events update React Query for server data; store writes are only for clearing client-owned pointers with a single responder/self-event guard

### Package Boundaries (hard rules)

- `packages/core/` - zero react-dom, zero localStorage, zero process.env
- `packages/ui/` - zero `@multica/core` imports
- `packages/views/` - zero `next/*`, zero `react-router-dom`, use `NavigationAdapter` for routing
- `apps/web/platform/` - only place for Next.js APIs

### Database Migrations (hard rules)

- Never add database foreign keys or cascading actions. Enforce relationships and perform dependent cleanup explicitly in the application layer, using transactions when the operation must be atomic.
- Every index created by a migration, including unique indexes and indexes on new tables, must use `CREATE [UNIQUE] INDEX CONCURRENTLY`. Keep each concurrent index build in its own single-statement migration file.

### Commands

```bash
make dev              # Auto-setup + start everything
pnpm typecheck        # TypeScript check
pnpm test             # TS unit tests (Vitest)
make test             # Go tests
make check            # Full verification pipeline
```

See CLAUDE.md for the authoritative rules and common commands.

### AI 公司规范（与产品代码分开）

本仓 **两套规范** 并存：

| 范围 | 权威文档 |
|------|----------|
| Multica **产品代码**（Go/TS/apps/packages） | 根目录 `CLAUDE.md` + [conventions.mdx](apps/docs/content/docs/developers/conventions.mdx) |
| **AI 公司 OS**（portfolio、DoD、.harness、examples） | `.ai-company/docs/29-harness-layout.md` · 硅谷纪律 `docs/30-silicon-valley-doc-standards.md` |

- 改全公司规则 → `.ai-company/docs/` + `sync-company-norms.sh`
- 复制新产品种子 → `.ai-company/examples/<slug>/`（见 `examples/README.md`）
- 安装交付脚手架 → `.ai-company/harness/install.sh`
