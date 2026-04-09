# Repository Guidelines

This file provides guidance to AI agents when working with code in this repository.

## Project Context

Multica is an AI-native task management platform — like Linear, but with AI agents as first-class citizens.

- Agents can be assigned issues, create issues, comment, and change status
- Supports local (daemon) and cloud agent runtimes
- Built for 2-10 person AI-native teams
- The repo is a monorepo with a Go backend, a product workspace SPA, a marketing site, CLI tooling, and OpenSpec change artifacts

## Current Repository Shape

- `server/` — Go backend, CLI, daemon, migrations, sqlc queries, and generated DB code
- `apps/workspace/` — primary product app. Vite + React + TanStack Router
- `apps/web/` — Next.js 16 marketing/public site
- `e2e/` — Playwright end-to-end tests
- `openspec/` — spec-driven change proposals and evolving product requirements
- `scripts/` and root `Makefile` — local setup, verification, and worktree helpers

## Architecture

**Go backend + frontend monorepo.**

- `server/` — Go backend (Chi router, sqlc for DB, gorilla/websocket for real-time)
- `apps/workspace/` — Vite + React product workspace app
- `apps/web/` — Next.js 16 frontend (App Router) for marketing/public pages
- `e2e/` — Playwright end-to-end tests
- `scripts/` and root `Makefile` — local setup and verification

### Workspace App Structure (`apps/workspace/src/`)

The workspace frontend uses a feature-based architecture with most product code under `src/`.

```
apps/workspace/src/
├── components/   # Reusable UI primitives and app-level components
├── features/     # Business logic, organized by domain
├── hooks/        # Reusable hooks
├── lib/          # App-specific utilities
├── shared/       # Shared API client, types, router, logger, utilities
├── styles/       # Global and shared styles
├── test/         # Shared test utilities and setup
├── app-shell.tsx # Main authenticated shell
├── main.tsx      # App bootstrap
└── router.tsx    # Route tree and navigation setup
```

`apps/workspace` uses `@/` alias mapping to `src/`.

### Web App Structure (`apps/web/`)

The frontend uses a **feature-based architecture** with four layers:

```
apps/web/
├── app/          # Routing layer (thin shells — import from features/)
├── features/     # Business logic, organized by domain
├── shared/       # Cross-feature utilities (api client, types, logger)
├── test/         # Shared test utilities and setup
├── public/       # Static assets
```

**`app/`** — Next.js App Router pages. Route files should be thin: import and re-export from `features/`. Layout components and route-specific glue (redirects, auth guards) live here. Shared layout components (e.g. `app-sidebar`) stay in `app/(dashboard)/_components/`.

**`features/`** — Domain modules, each with its own components, hooks, stores, and config:

| Feature | Purpose | Exports |
|---|---|---|
| `features/auth/` | Authentication state | `useAuthStore`, `AuthInitializer` |
| `features/workspace/` | Workspace, members, agents | `useWorkspaceStore`, `useActorName` |
| `features/issues/` | Issue state, components, config | `useIssueStore`, icons, pickers, status/priority config |
| `features/inbox/` | Inbox notification state | `useInboxStore` |
| `features/realtime/` | WebSocket connection + sync | `WSProvider`, `useWSEvent`, `useRealtimeSync` |
| `features/modals/` | Modal registry and state | Modal store and components |
| `features/skills/` | Skill management | Skill components |

**`shared/`** — Code used across multiple features:
- `shared/api/` — `ApiClient` (REST) and `WSClient` (WebSocket) for backend communication, plus the `api` singleton.
- `shared/types/` — Domain types (Issue, Agent, Workspace, etc.) and WebSocket event types.
- `shared/logger.ts` — Logger utility.

`apps/web` uses `@/` alias mapping to the app root.

### State Management

- **Zustand** for global client state — one store per feature domain (`features/auth/store.ts`, `features/workspace/store.ts`, `features/issues/store.ts`, `features/inbox/store.ts`).
- **React Context** only for connection lifecycle (`WSProvider` in `features/realtime/`).
- **Local `useState`** for component-scoped UI state (forms, modals, filters).
- Do not use React Context for data that can be a zustand store.

**Store conventions:**
- One store per feature domain. Import via `useAuthStore(selector)` or `useWorkspaceStore(selector)`.
- Stores must not call `useRouter` or any React hooks — keep navigation in components.
- Cross-store reads use `useOtherStore.getState()` inside actions (not hooks).
- Dependency direction: `workspace` → `auth`, `realtime` → `auth`, `issues` → `workspace`. Never reverse.

### Import Aliases

Use `@/` alias:

- In `apps/web`, `@/` maps to `apps/web/`.
- In `apps/workspace`, `@/` maps to `apps/workspace/src/`.

Example imports:

```typescript
import { api } from "@/shared/api";
import type { Issue } from "@/shared/types";
import { useAuthStore } from "@/features/auth";
import { useWorkspaceStore } from "@/features/workspace";
import { useIssueStore } from "@/features/issues";
import { useInboxStore } from "@/features/inbox";
import { useWSEvent } from "@/features/realtime";
import { StatusIcon } from "@/features/issues/components";
```

Within a feature, use relative imports. Between features or to shared, use `@/`.

### Data Flow

```
Browser → ApiClient (shared/api) → REST API (Chi handlers) → sqlc queries → PostgreSQL
Browser ← WSClient (shared/api) ← WebSocket ← Hub.Broadcast() ← Handlers/TaskService
```

### Backend Structure (`server/`)

- **Entry points** (`cmd/`): `server` (HTTP API), `multica` (CLI — daemon, agent management, config), `migrate`
- **Handlers** (`internal/handler/`): One file per domain (issue, comment, agent, auth, daemon, etc.). Each handler holds `Queries`, `DB`, `Hub`, and `TaskService`.
- **Real-time** (`internal/realtime/`): Hub manages WebSocket clients. Server broadcasts events; inbound WS message routing is still TODO.
- **Auth** (`internal/auth/` + `internal/middleware/`): JWT (HS256). Middleware sets `X-User-ID` and `X-User-Email` headers. Login creates user on-the-fly if not found.
- **Task lifecycle** (`internal/service/task.go`): Orchestrates agent work — enqueue → claim → start → complete/fail. Syncs issue status automatically and broadcasts WS events at each transition.
- **Agent SDK** (`pkg/agent/`): Unified `Backend` interface for executing prompts via Claude Code or Codex. Each backend spawns its CLI and streams results via `Session.Messages` + `Session.Result` channels.
- **Daemon** (`internal/daemon/`): Local agent runtime — auto-detects available CLIs (claude, codex), registers runtimes, polls for tasks, routes by provider.
- **CLI** (`internal/cli/`): Shared helpers for the `multica` CLI — API client, config management, output formatting.
- **Events** (`internal/events/`): Internal event bus for decoupled communication between handlers and services.
- **Logging** (`internal/logger/`): Structured logging via slog. `LOG_LEVEL` env var controls level (debug, info, warn, error).
- **Database**: PostgreSQL with pgvector extension (`pgvector/pgvector:pg17`). sqlc generates Go code from SQL in `pkg/db/queries/` → `pkg/db/generated/`. Migrations in `migrations/`.
- **Routes** (`cmd/server/router.go`): Public routes (auth, health, ws) + protected routes (require JWT) + daemon routes (unauthenticated, separate auth model).

### Multi-tenancy

All queries filter by `workspace_id`. Membership checks gate access. `X-Workspace-ID` header routes requests to the correct workspace.

### Agent Assignees

Assignees are polymorphic — can be a member or an agent. `assignee_type` + `assignee_id` on issues. Agents render with distinct styling (purple background, robot icon).

## OpenSpec Workflow

- Non-trivial product changes should use `openspec/changes/<change-name>/`.
- Proposal, design, specs, and tasks are the source of truth during implementation.
- Use the existing `/opsx:propose`, `/opsx:apply`, `/opsx:explore`, and `/opsx:archive` prompts when the user asks for OpenSpec-driven work.
- Main specs live in `openspec/specs/`; active work lives in `openspec/changes/`.

## Commands

```bash
# One-click setup & run
make setup            # Install deps, ensure shared DB, run migrations
make start            # Start backend + workspace SPA + marketing site
make stop             # Stop app processes for the current checkout
make check            # Full verification for the current checkout
make db-down          # Stop the shared PostgreSQL container

# Frontend
pnpm install
pnpm dev:workspace    # Workspace SPA on FRONTEND_PORT (default 3000)
pnpm dev:web          # Alias for workspace SPA from the repo root
pnpm dev:marketing    # Marketing site on MARKETING_PORT (default 3001)
pnpm build            # Build both frontends
pnpm typecheck        # TypeScript check for both frontends
pnpm lint             # TypeScript-based lint commands for both frontends
pnpm test             # Frontend tests (Vitest)

# Backend (Go)
make dev              # Run Go server only
make daemon           # Run local daemon
make build            # Build server + CLI binaries to server/bin/
make cli ARGS="..."   # Run multica CLI (e.g. make cli ARGS="config")
make test             # Go tests
make sqlc             # Regenerate sqlc code after editing SQL in server/pkg/db/queries/
make migrate-up       # Run database migrations
make migrate-down     # Rollback migrations

# Run a single Go test
cd server && go test ./internal/handler/ -run TestName

# Run a single TS test
pnpm --filter @multica/workspace exec vitest run src/path/to/file.test.ts
pnpm --filter @multica/web exec vitest run path/to/file.test.ts

# Run a single E2E test (requires backend + frontend running)
pnpm exec playwright test e2e/issues.spec.ts

# Infrastructure
make db-up            # Start shared PostgreSQL (pgvector/pg17 image)
make db-down          # Stop shared PostgreSQL

# Worktrees
make worktree-env       # Generate .env.worktree with unique DB/ports
make setup-worktree     # Setup using .env.worktree
make start-worktree     # Start using .env.worktree
make check-worktree     # Run checks in a worktree
```

## CI Requirements

CI runs on Node 22 and Go 1.26.1 with a `pgvector/pgvector:pg17` PostgreSQL service. See `.github/workflows/ci.yml`.

## Worktree Support

All checkouts share one PostgreSQL container. Isolation is at the database level — each worktree gets its own DB name and unique ports via `.env.worktree`. Main checkouts use `.env`.

```bash
make worktree-env       # Generate .env.worktree with unique DB/ports
make setup-worktree     # Setup using .env.worktree
make start-worktree     # Start using .env.worktree
```

## Coding Rules

- TypeScript strict mode is enabled; keep types explicit.
- TypeScript in `apps/web` and `apps/workspace` uses 2-space indentation, double quotes, and semicolons.
- Prefer PascalCase for React components, camelCase for hooks and helpers, and colocated test files such as `page.test.tsx`.
- Go code follows standard Go conventions (gofmt, go vet). Use domain-oriented filenames like `issue.go` or `cmd_issue.go`.
- Do not hand-edit generated code in `server/pkg/db/generated/`.
- Keep comments in code **English only**.
- Prefer existing patterns/components over introducing parallel abstractions.
- Unless the user explicitly asks for backwards compatibility, do **not** add compatibility layers, fallback paths, dual-write logic, legacy adapters, or temporary shims.
- If a flow or API is being replaced and the product is not yet live, prefer removing the old path instead of preserving both old and new behavior.
- Treat compatibility code as a maintenance cost, not a default safety mechanism. Avoid "just in case" branches that make the codebase harder to reason about.
- Avoid broad refactors unless required by the task.
- When shared behavior exists in both frontends, prefer mirrored updates rather than allowing them to drift.

## UI/UX Rules

- Design work must follow the rules in `DESIGN.md`.
- Prefer shadcn components over custom implementations. Install missing components via `npx shadcn add`.
- **Feature-specific components** → `features/<domain>/components/` — issue icons, pickers, and other domain-bound UI live inside their feature module.
- Use shadcn design tokens for styling (e.g. `bg-primary`, `text-muted-foreground`, `text-destructive`). Avoid hardcoded color values (e.g. `text-red-500`, `bg-gray-100`).
- Do not introduce extra state (useState, context, reducers) unless explicitly required by the design. Prefer zustand stores for shared state over React Context.
- Pay close attention to **overflow** (truncate long text, scrollable containers), **alignment**, and **spacing** consistency.
- When unsure about interaction or state design, ask — the user will provide direction.

## Testing Rules

- **TypeScript**: Vitest with Testing Library. Shared test setup lives in each app's `test/` directory. Mock external/third-party dependencies only.
- **Go**: Standard `go test`. Tests should create their own fixture data in a test database.
- End-to-end tests live in `e2e/*.spec.ts`; `make check` will start missing services automatically, while direct Playwright runs expect the app to already be running.
- Add or update tests whenever you change handlers, CLI commands, daemon behavior, or SQL-backed flows.

## Commit & Pull Request Rules

- Use atomic commits grouped by logical intent.
- Conventional format with scopes:
  - `feat(workspace): ...`, `feat(web): ...`, `feat(server): ...`, `feat(cli): ...`
  - `fix(workspace): ...`, `fix(web): ...`, `fix(server): ...`, `fix(cli): ...`
  - `refactor(daemon): ...`
  - `test(scope): ...`
  - `docs: ...`
  - `chore(scope): ...`
- Keep PRs focused and include a short description, linked issue or PR number when relevant, screenshots for UI work, and notes for migrations, env changes, or CLI surface changes.
- Before opening a PR, run `make check` or the relevant frontend/backend subset.

## CLI Release

**Prerequisite:** A CLI release must accompany every Production deployment. When deploying to Production, always release a new CLI version as part of the process.

1. Create a tag on the `main` branch: `git tag v0.x.x`
2. Push the tag: `git push origin v0.x.x`
3. GitHub Actions automatically triggers `release.yml`: runs Go tests → GoReleaser builds multi-platform binaries → publishes to GitHub Releases + Homebrew tap

By default, bump the patch version each release (e.g. `v0.1.12` → `v0.1.13`), unless the user specifies a specific version.

## Minimum Pre-Push Checks

```bash
make check    # Runs all checks: typecheck, unit tests, Go tests, E2E
```

Run verification only when the user explicitly asks for it.

For targeted checks when requested:
```bash
pnpm typecheck        # TypeScript type errors only
pnpm test             # TS unit tests only (Vitest)
make test             # Go tests only
pnpm exec playwright test   # E2E only (requires backend + frontend running)
```

## AI Agent Verification Loop

When a full verification pass is required, run the full verification pipeline:

```bash
make check
```

This runs all checks in sequence:
1. TypeScript typecheck (`pnpm typecheck`)
2. TypeScript unit tests (`pnpm test`)
3. Go tests (`go test ./...`)
4. E2E tests (auto-starts backend + frontend if needed, runs Playwright)

**Workflow:**
- Write code to satisfy the requirement
- Run `make check`
- If any step fails, read the error output, fix the code, and re-run `make check`
- Repeat until all checks pass
- Only then consider the task complete

**Quick iteration:** If you know only TypeScript or Go is affected, run individual checks first for faster feedback, then finish with a full `make check` before marking work complete.

## E2E Test Patterns

E2E tests should be self-contained. Use the `TestApiClient` fixture for data setup/teardown:

```typescript
import { loginAsDefault, createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

let api: TestApiClient;

test.beforeEach(async ({ page }) => {
  api = await createTestApi();       // logged-in API client
  await loginAsDefault(page);        // browser session
});

test.afterEach(async () => {
  await api.cleanup();               // delete any data created during the test
});

test("example", async ({ page }) => {
  const issue = await api.createIssue("Test Issue");  // create via API
  await page.goto(`/issues/${issue.id}`);             // test via UI
  // api.cleanup() in afterEach removes the issue
});
```
