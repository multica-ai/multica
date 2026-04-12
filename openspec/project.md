# Multica Project Context

## Product Overview

Multica is an AI-native task management platform for teams that work with both humans and coding agents.

- Agents are first-class teammates, not background automations.
- Users can assign issues to members or agents.
- Agents can comment, create follow-up issues, report blockers, and change status as work progresses.
- The product supports both local runtimes via a daemon and cloud runtimes.
- The primary product experience lives in the workspace app, while the web app is the marketing/public surface.

Core domain concepts:

- `issue` is the canonical persisted work item across database, API, frontend types, and realtime events.
- `agent` represents an AI worker that can execute assigned tasks.
- `runtime` represents the compute environment where an agent runs.
- `task` represents execution lifecycle state for agent work.
- `inbox_item` is a notification model, not a backlog or capture bucket.
- `workspace` is the primary tenant boundary.

Important product assumptions:

- Keep agent assignment and execution visible in product flows; this is a core differentiator.
- Preserve the distinction between member assignees and agent assignees.
- Treat inbox/notifications separately from work planning unless a change explicitly redefines that behavior.
- Prefer evolving the existing issue model over introducing parallel work-item models unless there is strong evidence the current model is insufficient.

## Repository Structure

This repository is a monorepo.

- `server/` — Go backend, CLI, daemon, migrations, SQL queries, generated database code.
- `apps/workspace/` — primary product app built with Vite + React.
- `apps/web/` — marketing/public site built with Next.js App Router.
- `e2e/` — Playwright end-to-end tests.
- `openspec/` — OpenSpec change proposals, specs, and tasks.
- `scripts/` and root `Makefile` — setup, verification, local dev, and worktree helpers.
- `docs/` — product and deployment documentation.

## Architecture

High-level system shape:

1. Browser clients call REST APIs through shared API clients.
2. The Go backend handles requests with Chi handlers.
3. Data access goes through sqlc-generated query bindings into PostgreSQL.
4. Realtime updates are pushed through gorilla/websocket.
5. Local or cloud runtimes execute agent tasks and report progress back to the server.

Data flow:

```text
Browser -> shared/api client -> Go HTTP handlers -> sqlc queries -> PostgreSQL
Browser <- WS client <- realtime hub <- handlers/task service
Daemon/runtime -> agent backend (Claude/Codex) -> task lifecycle -> realtime events
```

Multi-tenancy rules:

- Tenant isolation is workspace-based.
- Queries are scoped by `workspace_id`.
- Membership checks gate access.
- `X-Workspace-ID` is used to route requests to the correct workspace.

## Technology Stack

### Frontend

Workspace app (`apps/workspace`):

- Vite 8
- React
- TypeScript with strict mode
- TanStack Router
- Zustand for shared client state
- Tailwind CSS + shadcn-style UI primitives
- TipTap for rich text editing
- dnd-kit for drag-and-drop interactions
- Vitest + Testing Library for unit/component tests

Web app (`apps/web`):

- Next.js 16 App Router
- React
- TypeScript with strict mode
- Zustand
- Tailwind CSS + shadcn-style UI primitives
- Vitest + Testing Library

Common frontend libraries include:

- `lucide-react`
- `date-fns`
- `sonner`
- `react-day-picker`
- `react-resizable-panels`
- `recharts`

### Backend

- Go 1.26.1
- Chi router
- gorilla/websocket
- JWT auth via `golang-jwt/jwt`
- sqlc for typed SQL access
- pgx for PostgreSQL access
- Cobra for CLI commands
- AWS SDK v2 for storage/secrets integrations
- Resend for email delivery

### Database and Infrastructure

- PostgreSQL 17
- `pgvector` extension enabled
- Docker Compose for local PostgreSQL
- Shared PostgreSQL container across main checkout and worktrees

### Tooling

- pnpm workspaces
- Playwright for end-to-end tests
- Makefile-based development workflow
- OpenSpec for proposal/design/spec/task artifacts

## Frontend Conventions

### App Structure

Workspace app (`apps/workspace/src`) uses feature-based organization:

- `components/` — reusable UI primitives and app-level shared components.
- `features/` — product/domain logic organized by feature.
- `hooks/` — reusable hooks.
- `lib/` — app-specific utilities.
- `shared/` — cross-feature API, types, router, logger, utilities.
- `styles/` — global styles and shared CSS.

Web app (`apps/web`) also uses feature-based organization:

- `app/` — thin route shells.
- `features/` — business logic.
- `shared/` — cross-feature utilities.
- `test/` — shared test setup.

### Import Rules

- In `apps/workspace`, `@/` maps to `apps/workspace/src/`.
- In `apps/web`, `@/` maps to `apps/web/`.
- Use relative imports inside a feature.
- Use `@/` imports when crossing feature boundaries or importing shared code.

### State Management

- Use Zustand for shared application state.
- Use one store per feature domain.
- Do not use React Context for data that should live in a store.
- Stores must not call React hooks such as `useRouter`.
- Cross-store reads should use `useOtherStore.getState()` inside actions.

Expected dependency direction:

- `workspace` -> `auth`
- `realtime` -> `auth`
- `issues` -> `workspace`

Do not reverse these dependencies.

### UI and Design Rules

- Follow `DESIGN.md` for product design direction.
- Prefer shadcn components and existing UI primitives over custom replacements.
- Use design tokens such as `bg-primary`, `text-muted-foreground`, and `text-destructive`.
- Avoid hardcoded utility colors like `text-red-500` or `bg-gray-100` unless there is a strong reason.
- Pay attention to overflow, truncation, spacing, and alignment.
- Keep shared UI behavior mirrored across `apps/web` and `apps/workspace` when the same concept exists in both places.

### TypeScript Style

- TypeScript strict mode is enabled.
- Use explicit, stable types.
- Use 2-space indentation.
- Use double quotes.
- Use semicolons.
- Prefer PascalCase for React components.
- Prefer camelCase for hooks, utilities, and store actions.
- Keep route files and page shells thin when possible.

## Backend Conventions

- Organize handlers by domain in `server/internal/handler/`.
- Keep business workflows in services such as `server/internal/service/`.
- Do not hand-edit generated files in `server/pkg/db/generated/`.
- Edit SQL in `server/pkg/db/queries/` and regenerate with `make sqlc` when needed.
- Use standard Go conventions and formatting (`gofmt`, `go test`, `go vet` mindset).
- Prefer domain-oriented filenames like `issue.go`, `agent.go`, `task.go`.
- Keep comments in code in English only.

Auth and realtime expectations:

- Protected API routes rely on JWT-based auth.
- Middleware sets request identity headers.
- Realtime updates should preserve existing event contracts unless a change explicitly replaces them.

Compatibility and migration expectations:

- Prefer additive changes over high-risk rewrites.
- Do not add compatibility shims, fallback logic, or dual-write behavior unless explicitly required.
- If the product is not live and a flow is being replaced, prefer removing the old path over supporting both forever.

## Testing and Verification

Frontend:

- Use Vitest and Testing Library.
- Mock external or third-party dependencies only.

Backend:

- Use `go test`.
- Tests should create their own fixture data.

End-to-end:

- Use Playwright tests in `e2e/`.
- `make check` runs the full verification pipeline and can start missing services automatically.
- Direct Playwright runs usually expect the app to already be running.

Preferred verification commands:

```bash
make check
pnpm typecheck
pnpm test
make test
pnpm exec playwright test
```

Targeted commands:

```bash
pnpm --filter @multica/workspace exec vitest run src/path/to/file.test.ts
pnpm --filter @multica/web exec vitest run path/to/file.test.ts
cd server && go test ./internal/handler/ -run TestName
```

## Local Development Workflow

Main checkout:

```bash
cp .env.example .env
make setup
make start
make stop
```

Worktree workflow:

```bash
make worktree-env
make setup-worktree
make start-worktree
make stop-worktree
```

Important environment conventions:

- Main checkout uses `.env`.
- Worktrees use `.env.worktree`.
- Do not copy `.env` into a worktree.
- All checkouts share one PostgreSQL container, but use separate databases.

## Commit and PR Conventions

- Use atomic commits grouped by logical intent.
- Use conventional commit prefixes with scopes.

Examples:

- `feat(workspace): ...`
- `feat(web): ...`
- `feat(server): ...`
- `feat(cli): ...`
- `fix(workspace): ...`
- `fix(server): ...`
- `refactor(daemon): ...`
- `test(scope): ...`
- `docs: ...`
- `chore(scope): ...`

Before opening a PR, prefer running `make check` or the relevant targeted subset.

## OpenSpec Guidance

- Use OpenSpec for non-trivial product or architecture changes.
- Change artifacts live under `openspec/changes/<change-name>/`.
- Main specs live under `openspec/specs/`.
- Proposal, design, spec, and tasks should stay aligned.
- When describing product changes, ground them in the existing domain model rather than inventing parallel abstractions without justification.

When writing OpenSpec artifacts for this project:

- Treat `issue` as the current canonical work item unless the change explicitly replaces that model.
- Preserve agent execution and assignment semantics in any workflow-related proposal.
- Treat notifications/inbox and work planning as distinct concerns unless the change intentionally merges them.
- Keep `apps/workspace` as the primary product shell unless there is a clear reason to move product behavior elsewhere.
- Avoid bundling unrelated infra rewrites into product-facing changes.

## What to Preserve When Making Changes

- Workspace-scoped multi-tenancy.
- First-class agent assignment and runtime execution visibility.
- Existing auth, routing, and realtime contracts unless the change explicitly covers their replacement.
- Feature-based frontend structure.
- Minimal, focused changes over broad refactors.