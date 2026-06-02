# WALLTS — AI Agent Context

> **Primary context file for agents working on the WALLTS project.**
> This is the authoritative source for architecture, conventions, commands, and coding rules.
> Do NOT modify AGENTS.md, CLAUDE.md, or multica.md — those are upstream artifacts.
> Read this file first; only reference upstream files if you need a detail not covered here.

## Project Overview

WALLTS is a fork of [Multica](https://github.com/multica-ai/multica) — an AI-native task management platform where AI agents are first-class teammates.

- Agents can be assigned issues, create issues, comment, and change status
- Supports local (daemon) and cloud agent runtimes
- Built for 2-10 person AI-native teams

> **Note:** This codebase is maintained independently. Upstream `multica.md` and `CLAUDE.md`
> contain Multica-branded content. For WALLTS work, this file is the source of truth.

## Conventions Reference

The single source of truth for **code naming, the i18n translation glossary, and the Chinese voice guide** is the docs site:

- **`apps/docs/content/docs/developers/conventions.mdx`** (English)
- **`apps/docs/content/docs/developers/conventions.zh.mdx`** (Chinese)

Read that page before:

- Writing or editing translations (`packages/views/locales/`)
- Naming a new route, package, file, DB column, or TS type
- Writing Chinese product copy (UI strings, error messages, docs)

## Architecture

**Go backend + monorepo frontend (pnpm workspaces + Turborepo) with shared packages.**

- `server/` — Go backend (Chi router, sqlc for DB, gorilla/websocket for real-time)
- `apps/web/` — Next.js frontend (App Router)
- `apps/desktop/` — Electron desktop app (electron-vite)
- `apps/mobile/` — Expo / React Native iOS app. See `apps/mobile/CLAUDE.md`.
- `packages/core/` — Headless business logic (zero react-dom)
- `packages/ui/` — Atomic UI components (zero business logic)
- `packages/views/` — Shared business pages/components (zero next/* imports, zero react-router imports)
- `packages/tsconfig/` — Shared TypeScript configuration

### Key Architectural Decisions

**Internal Packages pattern** — all shared packages export raw `.ts`/`.tsx` files (no pre-compilation). The consuming app's bundler compiles them directly. Zero-config HMR, instant go-to-definition.

**Dependency direction:** `views/ → core/ + ui/`. Core and UI are independent of each other. No package imports from `next/*`, `react-router-dom`, or app-specific code.

**Platform bridge:** `packages/core/platform/` provides `CoreProvider` — initializes API client, auth/workspace stores, WS connection, and QueryClient. Each app wraps its root with `<CoreProvider>` and provides its own `NavigationAdapter` for routing.

**pnpm catalog** — `pnpm-workspace.yaml` defines `catalog:` for version pinning. All shared deps use `catalog:` references to guarantee a single version across all packages. When adding new shared deps (including test deps), add to catalog first.

### State Management

Strict split between server state and client state.

- **TanStack Query owns all server state.** Issues, users, workspaces, inbox — anything fetched from the API lives in the Query cache. WS events keep it fresh via invalidation; no polling, no `staleTime` workarounds.
- **Zustand owns all client state.** UI selections, filters, drafts, modal state, navigation history. Stores live in `packages/core/` (never in `packages/views/`) so they're shared.
- **React Context** is reserved for cross-cutting platform plumbing — `WorkspaceIdProvider`, `NavigationProvider`. Don't reach for it for general state.
- **Auth and workspace stores are the only stores allowed to call `api.*` directly**, because they manage critical state that must exist before queries can run.

**Hard rules:**

- **Never duplicate server data into Zustand.** If it came from the API, it belongs in the Query cache.
- **Workspace-scoped queries must key on `wsId`.** The cache key changes, the right data appears.
- **Mutations are optimistic by default.** Apply the change locally, send the request, roll back on failure, invalidate on settle.
- **WS events invalidate queries — they never write to stores directly.**
- **Persist what's worth preserving across restarts** (user preferences, drafts, tab layout). **Don't persist ephemeral UI state** or server data.

**Common Zustand footguns:**

- Selectors must return stable references. Returning a freshly built object or array on every call triggers infinite re-renders.
- Hooks that need workspace context should accept `wsId` as a parameter, not call `useWorkspaceId()` internally.

## Sharing Principles

- **Web and desktop** share business logic, components, hooks, stores, and views through `packages/core/`, `packages/ui/`, and `packages/views/`.
- **Mobile (`apps/mobile/`) is independent.** It shares only **types and pure functions** from `@multica/core/`, with `import type` for types (zero runtime coupling).

## Commands

```bash
# One-command dev (auto-setup + start everything)
make dev              # Auto-creates env, installs deps, starts DB, migrates, launches app

# Explicit setup & run
make setup            # First-time: ensure shared DB, create app DB, migrate
make start            # Start backend + frontend together
make stop             # Stop app processes
make db-down          # Stop the shared PostgreSQL container

# Frontend (Turborepo)
pnpm install
pnpm dev:web          # Next.js dev server (port 3000)
pnpm dev:desktop      # Electron dev (electron-vite, HMR)
pnpm build            # Build all frontend apps
pnpm typecheck        # TypeScript check (all packages + apps via turbo)
pnpm lint             # ESLint
pnpm test             # TS tests (Vitest, all packages + apps via turbo)

# Backend (Go)
make server           # Run Go server only (port 8080)
make daemon           # Run local daemon
make build            # Build server + CLI binaries to server/bin/
make cli ARGS="..."   # Run multica CLI
make test             # Go tests
make sqlc             # Regenerate sqlc code
make migrate-up       # Run database migrations
make migrate-down     # Rollback migrations

# Single tests
pnpm --filter @multica/views exec vitest run auth/login-page.test.tsx
pnpm --filter @multica/core exec vitest run runtimes/version.test.ts
cd server && go test ./internal/handler/ -run TestName

# E2E (requires backend + frontend running)
pnpm exec playwright test e2e/tests/specific-test.spec.ts

# Mobile (Expo)
pnpm dev:mobile                  # Metro, dev env
pnpm dev:mobile:staging          # Metro, staging env
pnpm ios:mobile                  # Native build + install dev-client to iOS Simulator

# Desktop build & package
pnpm --filter @multica/desktop build      # Compile TS → JS
pnpm --filter @multica/desktop package    # Package into .app/.dmg/.exe

# shadcn
pnpm ui:add badge                # Adds component to packages/ui/components/ui/

# Infrastructure
make db-up            # Start shared PostgreSQL (pgvector/pg17 image)
make db-down          # Stop shared PostgreSQL
make db-reset         # Drop + recreate current env's DB, then re-run migrations
```

### CI Requirements

CI runs on Node 22 and Go 1.26.1 with a `pgvector/pgvector:pg17` PostgreSQL service. See `.github/workflows/ci.yml`.

### Worktree Support

All checkouts share one PostgreSQL container. Isolation is at the database level — each worktree gets its own DB name and unique ports via `.env.worktree`.

```bash
make worktree-env       # Generate .env.worktree with unique DB/ports
make setup-worktree     # Setup using .env.worktree
make start-worktree     # Start using .env.worktree
```

## Coding Rules

- TypeScript strict mode is enabled; keep types explicit.
- Go code follows standard Go conventions (gofmt, go vet).
- Keep comments in code **English only**.
- Prefer existing patterns/components over introducing parallel abstractions.
- Unless explicitly asked, do **not** add compatibility layers, fallback paths, dual-write logic, legacy adapters, or temporary shims **for internal, non-boundary code**.
- If a flow or API is being replaced and the product is not yet live, prefer removing the old path instead of preserving both.
- Avoid broad refactors unless required by the task.
- New global (pre-workspace) routes MUST use a single word (`/login`, `/inbox`) or a `/{noun}/{verb}` pair (`/workspaces/new`). NEVER add hyphenated word-group root routes.
- The reserved-slug list lives in `server/internal/handler/reserved_slugs.json`. Edit the JSON, run the generator, commit both.

### API Response Compatibility

The desktop app installed on a user's machine is older than any backend it talks to. Every response shape is a contract that **will** drift.

- **Parse, don't cast.** Use `parseWithFallback` in `packages/core/api/schema.ts` with a `zod` schema and an explicit fallback.
- **No bare `as` casts on response bodies.** Every endpoint method must run through a schema before returning.
- **Optional-chain and default everywhere downstream.** Treat every field as possibly missing.
- **Don't pin a UI affordance to a single backend field.** Combine signals.
- **Enum drift downgrades, not crashes.** `switch` statements must have a `default` branch.
- **When you add or change an endpoint:** add the schema in the same PR, and write at least one test that feeds a malformed response through it.

### Backend Handler UUID Parsing Convention

- **Resource path params that accept either a UUID or a human-readable identifier** MUST be resolved through the dedicated loader (`loadIssueForUser` / `loadSkillForUser` / `loadAgentForUser` / `requireDaemonRuntimeAccess`).
- **Pure-UUID inputs from request boundaries** MUST be validated with `parseUUIDOrBadRequest(w, s, fieldName)`.
- **Trusted UUID round-trips** (sqlc-returned UUIDs, test fixtures) use `parseUUID(s)`.

### Dependency Declaration Rule

Every workspace must explicitly declare all directly imported external packages in its own `package.json`. Use `"pkg": "catalog:"` to reference the shared version.

### Package Boundary Rules

- `packages/core/` — zero react-dom, zero localStorage (use StorageAdapter), zero process.env, zero UI libraries.
- `packages/ui/` — zero `@multica/core` imports.
- `packages/views/` — zero `next/*` imports, zero `react-router-dom` imports, zero stores.
- `apps/web/platform/` — the only place for Next.js APIs.
- `apps/desktop/src/renderer/src/platform/` — the only place for react-router-dom navigation wiring.

### The No-Duplication Rule

If the same logic exists in both web and desktop, it must be extracted to a shared package.

### Cross-Platform Development Rules

When adding a new page or feature for web/desktop:

1. **New page component** → add to `packages/views/<domain>/`.
2. **Wire it in both apps** → add a route in `apps/web/app/` AND in the desktop router.
3. **Navigation** → use `useNavigation().push()` or `<AppLink>`.
4. **Shared guards/providers** → use `DashboardGuard` from `packages/views/layout/`.
5. **Platform-specific UI** → keep in the respective app.
6. **New hooks that need workspace context** → accept `wsId` as parameter.

### CSS Architecture

- **Design tokens** → use semantic tokens (`bg-background`, `text-muted-foreground`). Never use hardcoded Tailwind colors.
- **Shared styles** → `packages/ui/styles/`.
- **`@source` directives** → both apps scan shared packages.

## Mobile-specific Rules

Rules for `apps/mobile/` live in `apps/mobile/CLAUDE.md`. Read it before touching anything in `apps/mobile/`.

## Desktop-specific Rules

### Route categories

- **Session routes** — workspace-scoped pages. Rendered by the per-tab memory router.
- **Transition flows** — pre-workspace / one-shot actions. **NOT routes.** They live as `WindowOverlay` state.
- **Error / stale states** — auto-heal by dropping the stale tab group from the store.

### Workspace context

`setCurrentWorkspace(slug, uuid)` from `@multica/core/platform` is the single source of truth.

### Tab isolation

Tabs are grouped per workspace in `stores/tab-store.ts`.

### Drag region (macOS)

Every full-window desktop view must mount `<DragStrip />` from `@multica/views/platform`.

## UI/UX Rules

- Prefer shadcn components. Install via `pnpm ui:add <component>`. All components use Base UI primitives.
- Use shadcn design tokens for styling.
- Do not introduce extra state unless explicitly required.

## Testing Rules

### Where to write tests

| What you're testing | Where the test lives |
|---|---|
| Shared business logic | `packages/core/*.test.ts` |
| Shared UI components | `packages/views/*.test.tsx` |
| Platform-specific wiring | `apps/web/*.test.tsx` or `apps/desktop/` |
| End-to-end user flows | `e2e/*.spec.ts` |

### Test infrastructure

- `packages/core/` — Vitest, Node environment
- `packages/views/` — Vitest, jsdom, `@testing-library/react`
- `apps/web/` — Vitest, jsdom, framework-specific mocks
- `e2e/` — Playwright
- `server/` — Go standard `go test`

### TDD workflow

1. Write failing test in the **correct package** first.
2. Write implementation.
3. Run `pnpm test`.
4. Green → done.

## Commit Rules

- Use atomic commits grouped by logical intent.
- Conventional format: `feat(scope)`, `fix(scope)`, `refactor(scope)`, `docs`, `test(scope)`, `chore(scope)`.

## Pre-Push Checks

```bash
make check    # Runs all checks: typecheck, unit tests, Go tests, E2E
```

## CLI Release

1. Create a tag on `main`: `git tag v0.x.x`
2. Push the tag: `git push origin v0.x.x`
3. GitHub Actions triggers `release.yml`: GoReleaser builds → publishes to GitHub Releases + Homebrew tap

## Multi-tenancy

All queries filter by `workspace_id`. Membership checks gate access. `X-Workspace-ID` header routes requests.

## Agent Assignees

Assignees are polymorphic — can be a member or an agent. `assignee_type` + `assignee_id` on issues.
