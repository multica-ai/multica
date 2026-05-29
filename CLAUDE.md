# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Conventions reference

The single source of truth for **code naming, the i18n translation glossary, and the Chinese voice guide** is the docs site:

- **`apps/docs/content/docs/developers/conventions.mdx`** (English)
- **`apps/docs/content/docs/developers/conventions.zh.mdx`** (Chinese)

Read that page before:

- Writing or editing translations (`packages/views/locales/`)
- Naming a new route, package, file, DB column, or TS type
- Writing Chinese product copy (UI strings, error messages, docs)

The legacy `packages/views/locales/glossary.md` is now a stub redirecting to the docs page; do not rely on it.

## Project Context

Multica is an AI-native task management platform — like Linear, but with AI agents as first-class citizens.

- Agents can be assigned issues, create issues, comment, and change status
- Supports local (daemon) and cloud agent runtimes
- Built for 2-10 person AI-native teams

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

What lives where for sharing purposes is documented in *Sharing Principles* below — read it once.

### Key Architectural Decisions

**Internal Packages pattern** — all shared packages export raw `.ts`/`.tsx` files (no pre-compilation). The consuming app's bundler compiles them directly. This gives zero-config HMR and instant go-to-definition.

**Dependency direction:** `views/ → core/ + ui/`. Core and UI are independent of each other. No package imports from `next/*`, `react-router-dom`, or app-specific code.

**Platform bridge:** `packages/core/platform/` provides `CoreProvider` — initializes API client, auth/workspace stores, WS connection, and QueryClient. Each app wraps its root with `<CoreProvider>` and provides its own `NavigationAdapter` for routing.

**pnpm catalog** — `pnpm-workspace.yaml` defines `catalog:` for version pinning. All shared deps use `catalog:` references to guarantee a single version across all packages. When adding new shared deps (including test deps), add to catalog first.

### State Management

The architecture relies on a strict split between server state and client state. Mixing them is the most common way to break it.

- **TanStack Query owns all server state.** Issues, users, workspaces, inbox — anything fetched from the API lives in the Query cache. WS events keep it fresh via invalidation; no polling, no `staleTime` workarounds.
- **Zustand owns all client state.** UI selections, filters, drafts, modal state, navigation history. Stores live in `packages/core/` (never in `packages/views/`) so they're shared.
- **React Context** is reserved for cross-cutting platform plumbing — `WorkspaceIdProvider`, `NavigationProvider`. Don't reach for it for general state.
- **Auth and workspace stores are the only stores allowed to call `api.*` directly**, because they manage critical state that must exist before queries can run. They're created via factory + injected dependencies, registered by the platform layer.

**Hard rules — these are how the architecture stays coherent:**

- **Never duplicate server data into Zustand.** If it came from the API, it belongs in the Query cache. Copying it into a store creates two sources of truth and they will drift.
- **Workspace-scoped queries must key on `wsId`.** This is what makes workspace switching automatic — the cache key changes, the right data appears, no manual invalidation needed.
- **Mutations are optimistic by default.** Apply the change locally, send the request, roll back on failure, invalidate on settle. The user shouldn't wait for the server.
- **WS events invalidate queries — they never write to stores directly.** This keeps the cache as the single source of truth and avoids race conditions.
- **Persist what's worth preserving across restarts** (user preferences, drafts, tab layout). **Don't persist ephemeral UI state** (modal open/close, transient selections) or server data.

**Common Zustand footguns to avoid:**

- Selectors must return stable references. Returning a freshly built object or array on every call (e.g. `s => ({ a: s.a, b: s.b })` or `s => s.items.map(...)`) triggers infinite re-renders. Either select primitives separately or use shallow comparison.
- Hooks that need workspace context should accept `wsId` as a parameter, not call `useWorkspaceId()` internally — this lets them work outside the `WorkspaceIdProvider` (e.g. in a sidebar that renders before workspace is loaded).

## Sharing Principles

The monorepo splits into two share zones:

- **Web and desktop** share business logic, components, hooks, stores, and views through `packages/core/`, `packages/ui/`, and `packages/views/`. Existing model — keep using it.
- **Mobile (`apps/mobile/`) is independent.** It shares only **types and pure functions** from `@multica/core/`, with `import type` for types (zero runtime coupling). UI, state, hooks, providers, i18n, React version, build pipeline, release cadence — all mobile-owned.

Mobile is locked to the React version that Expo SDK / React Native ships (which lags React main by 6-12 months). Coupling mobile to the root `catalog:` React would block mobile from upgrading on its own schedule.

See `apps/mobile/CLAUDE.md` for the mobile rules and tech-stack baseline.

## Commands

```bash
# One-command dev (auto-setup + start everything)
make dev              # Auto-creates env, installs deps, starts DB, migrates, launches app

# Explicit setup & run (if you prefer separate steps)
make setup            # First-time: ensure shared DB, create app DB, migrate
make start            # Start backend + frontend together
make stop             # Stop app processes for the current checkout
make db-down          # Stop the shared PostgreSQL container

# Frontend (all commands go through Turborepo)
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
make cli ARGS="..."   # Run multica CLI (e.g. make cli ARGS="config")
make test             # Go tests
make sqlc             # Regenerate sqlc code after editing SQL in server/pkg/db/queries/
make migrate-up       # Run database migrations
make migrate-down     # Rollback migrations

# Run a single TS test (works for any package with a test script)
pnpm --filter @multica/views exec vitest run auth/login-page.test.tsx
pnpm --filter @multica/core exec vitest run runtimes/version.test.ts
pnpm --filter @multica/web exec vitest run app/\(auth\)/login/page.test.tsx

# Run a single Go test
cd server && go test ./internal/handler/ -run TestName

# Run a single E2E test (requires backend + frontend running)
pnpm exec playwright test e2e/tests/specific-test.spec.ts

# Mobile (Expo) — two environments only: dev and staging
pnpm dev:mobile                  # Metro, dev env       (reads apps/mobile/.env.development.local)
pnpm dev:mobile:staging          # Metro, staging env   (reads apps/mobile/.env.staging)
pnpm ios:mobile                  # Native build + install dev-client to iOS Simulator, dev env
pnpm ios:mobile:staging          # Native build + install dev-client to iOS Simulator, staging env
pnpm ios:mobile:device           # Native build + install dev-client to USB iPhone, dev env
pnpm ios:mobile:device:staging   # Native build + install dev-client to USB iPhone, staging env
# Daily flow: run `pnpm dev:mobile:staging` (or :dev). Only re-run `ios:mobile*` when
# native code or any expo-*/react-native-* dependency changes (lockfile drift counts).

# Desktop build & package
pnpm --filter @multica/desktop build      # Compile TS → JS (reads .env.production)
pnpm --filter @multica/desktop package    # Package into .app/.dmg/.exe (current platform only)

# shadcn — config lives in packages/ui/components.json (Base UI variant, base-nova style)
pnpm ui:add badge                # Adds component to packages/ui/components/ui/

# Infrastructure
make db-up            # Start shared PostgreSQL (pgvector/pg17 image)
make db-down          # Stop shared PostgreSQL
make db-reset         # Drop + recreate current env's DB, then re-run migrations (local only; stop backend first)
```

### CI Requirements

CI runs on Node 22 and Go 1.26.1 with a `pgvector/pgvector:pg17` PostgreSQL service. See `.github/workflows/ci.yml`.

### Worktree Support

All checkouts share one PostgreSQL container. Isolation is at the database level — each worktree gets its own DB name and unique ports via `.env.worktree`. Main checkouts use `.env`.

`make dev` auto-detects worktrees and handles everything. For explicit control:

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
- Unless the user explicitly asks for backwards compatibility, do **not** add compatibility layers, fallback paths, dual-write logic, legacy adapters, or temporary shims **for internal, non-boundary code** (a function calling another function in the same package, a component reading its own state, a store helper, etc.).
- This rule does **not** apply at API boundaries: the desktop app cannot assume the backend it talks to has the same shape as the one it was built against (older desktop installs will outlive any given server build). API response handling must follow the rules in **API Response Compatibility** below — that is a defensive boundary, not a legacy shim.
- If a flow or API is being replaced and the product is not yet live, prefer removing the old path instead of preserving both old and new behavior.
- Avoid broad refactors unless required by the task.
- New global (pre-workspace) routes MUST use a single word (`/login`, `/inbox`) or a `/{noun}/{verb}` pair (`/workspaces/new`). NEVER add hyphenated word-group root routes (`/new-workspace`, `/create-team`) — they collide with common user workspace names and force endless reserved-slug audits. Reserving the noun (`workspaces`) automatically protects the entire `/workspaces/*` subtree.
- The reserved-slug list lives in **one** place: `server/internal/handler/reserved_slugs.json`. The Go side embeds the JSON; `packages/core/paths/reserved-slugs.ts` is generated from it by `pnpm generate:reserved-slugs`. Edit the JSON, run the generator, commit both. CI re-runs the generator and fails on any drift, so a stale TS file cannot land.

### API Response Compatibility

The desktop app installed on a user's machine is older than any backend it talks to: a user on 0.2.26 will hit a server running 0.3.x, then 0.4.x, then beyond. Every response shape is a contract that **will** drift, and the frontend must survive drift without white-screening. Three concrete incidents already happened from violating this — #2143, #2147, #2192.

When writing code that consumes an API response, follow these rules:

- **Parse, don't cast.** Untyped JSON crossing the network is not `T`. Use `parseWithFallback` in `packages/core/api/schema.ts` with a `zod` schema and an explicit fallback. On validation failure it logs a warning and returns the fallback; it never throws into the UI.
- **No bare `as` casts on response bodies.** Every endpoint method whose response is consumed by UI logic must run through a schema before returning.
- **Optional-chain and default everywhere downstream.** Treat every field as possibly missing. Use explicit boolean checks (`=== true`) over truthy/falsy negation, which silently treats `undefined` and `null` as `false`.
- **Don't pin a UI affordance to a single backend field.** If a button or indicator depends on exactly one boolean from the server, a backend bug deletes it. Combine signals (cursor presence, page length, etc.) so the affordance stays available in the worst case.
- **Enum drift downgrades, not crashes.** A new server-side enum value should render a generic fallback. `switch` statements on server-driven strings must have a `default` branch.
- **When you add or change an endpoint:** add the schema in the same PR, and write at least one test that feeds a malformed response through it (missing field, wrong type, `null` array). The test fails closed if a future change breaks the contract.

This is not premature defense — it is the *only* defense for an installed-app architecture. CSR-only browser apps can ship a fix in minutes; an Electron build sitting on a developer's laptop cannot.

### Backend Handler UUID Parsing Convention

Every Go handler in `server/internal/handler/` follows these rules. The convention exists because `util.ParseUUID` used to silently return a zero UUID on invalid input, which caused #1661 — a `DELETE` returning 204 success while the SQL `DELETE` matched zero rows.

- **Resource path params that accept either a UUID or a human-readable identifier** (e.g. `chi.URLParam(r, "id")` for an issue, which accepts both `MUL-123` and a UUID) MUST be resolved through the dedicated loader (`loadIssueForUser` / `loadSkillForUser` / `loadAgentForUser` / `requireDaemonRuntimeAccess`). After resolution, all subsequent DB calls — especially `Queries.Delete*` / `Queries.Update*` — MUST use `entity.ID` from the resolved object. Never round-trip the raw URL string through `parseUUID` for a write query.
- **Pure-UUID inputs from request boundaries** (URL params that are always UUIDs, request body fields, query params, headers) MUST be validated with `parseUUIDOrBadRequest(w, s, fieldName)`. On invalid input it writes a 400 and returns `ok=false` — return immediately.
- **Trusted UUID round-trips** (sqlc-returned UUIDs being passed back into queries, test fixtures) use `parseUUID(s)` which calls `util.MustParseUUID` and panics on invalid input. A panic here means an unguarded user-input string slipped in — that is a real bug. `chi`'s `middleware.Recoverer` translates the panic into a 500 so the process keeps running.
- **`util.ParseUUID(s) (pgtype.UUID, error)`** is the only safe variant outside the handler package. Always check the error.

When adding a `Queries.Delete*` or `Queries.Update*` call, ask: "Where did this UUID come from?" If the answer is "raw user input that hasn't been validated," route it through `parseUUIDOrBadRequest` or a loader first.

### Dependency Declaration Rule

Every workspace (`apps/` and `packages/` directories) must explicitly declare all directly imported external packages in its own `package.json`. Relying on pnpm hoist to resolve undeclared imports (phantom deps) is prohibited — it causes production build failures when pnpm creates peer-dep variants.

- Use `"pkg": "catalog:"` to reference the shared version from `pnpm-workspace.yaml`.
- CI enforces this via `eslint-plugin-import-x/no-extraneous-dependencies`.
- Exception: `apps/mobile/` uses pinned versions (not `catalog:`) for packages tied to its own React/Expo version.

### Package Boundary Rules

These are hard constraints. Violating them breaks the cross-platform architecture:

- `packages/core/` — zero react-dom, zero localStorage (use StorageAdapter), zero process.env, zero UI libraries. **Shared Zustand stores live here**, even view-related ones (filters, view modes) — stores are pure state, not UI.
- `packages/ui/` — zero `@multica/core` imports (pure UI, no business logic).
- `packages/views/` — zero `next/*` imports, zero `react-router-dom` imports, zero stores. Use `NavigationAdapter` for all routing.
- `apps/web/platform/` — the only place for Next.js APIs (`next/navigation`).
- `apps/desktop/src/renderer/src/platform/` — the only place for react-router-dom navigation wiring.

### The No-Duplication Rule (web + desktop)

**If the same logic exists in both web and desktop, it must be extracted to a shared package.**

This applies to everything between web and desktop: components, hooks, guards, providers, utility functions. The decision process:

1. Does this code depend on Next.js or Electron APIs? → Keep in the respective app.
2. Does it depend on `react-router-dom` or `next/navigation`? → Keep in app's `platform/` layer.
3. Everything else → belongs in `packages/core/` (headless logic) or `packages/views/` (UI components).

When the two apps need different behavior for the same concept (e.g., different loading UI), extract the shared logic into a component with props/slots for the differences. Don't duplicate the logic.

### Cross-Platform Development Rules (web + desktop)

When adding a new page or feature for web/desktop:

1. **New page component** → add to `packages/views/<domain>/`. Never import from `next/*` or `react-router-dom`.
2. **Wire it in both apps** → add a route in `apps/web/app/` (Next.js page file) AND in the desktop router. **Exception**: pre-workspace transition flows (create workspace, accept invite) are NOT routes on desktop — they're `WindowOverlay` state. See *Desktop-specific Rules → Route categories*.
3. **Navigation** → use `useNavigation().push()` or `<AppLink>`. Never use framework-specific link/router APIs in shared code.
4. **Shared guards/providers** → use `DashboardGuard` from `packages/views/layout/`. Don't create separate guard logic per app.
5. **Platform-specific UI** → if a feature is web-only or desktop-only, keep it in the respective app. Use props slots (`extra`, `topSlot`) on shared layout components to inject platform-specific UI.
6. **New hooks that need workspace context** → accept `wsId` as parameter instead of reading from `useWorkspaceId()` Context, so they work both inside and outside `WorkspaceIdProvider`.

### CSS Architecture (web + desktop)

Web and desktop share the same CSS foundation from `packages/ui/styles/`.

- **Design tokens** → use semantic tokens (`bg-background`, `text-muted-foreground`). Never use hardcoded Tailwind colors (`text-red-500`, `bg-gray-100`).
- **Shared styles** → `packages/ui/styles/`. Never duplicate scrollbar styling, keyframes, or base layer rules in app CSS.
- **`@source` directives** → both apps scan shared packages so Tailwind sees all class names.

## Mobile-specific Rules

Rules for `apps/mobile/` live in `apps/mobile/CLAUDE.md`. Read it before touching anything in `apps/mobile/` — it covers what may be imported from `@multica/core/`, the React version policy, the build/release pipeline, and the locked tech-stack baseline.

## Desktop-specific Rules

These rules apply to `apps/desktop/` only. Web has different constraints (URL bar, SSR, no tabs) and doesn't share these concerns. Every rule in this section was added after a concrete bug — treat them as enforced, not suggestions.

### Route categories

Every path in the desktop app falls into exactly one category. Choosing the wrong one reproduces bugs we've already fixed.

- **Session routes** — workspace-scoped pages (`/:slug/issues`, `/:slug/settings`). Rendered by the per-tab memory router under `WorkspaceRouteLayout`. These are legitimate tab destinations.
- **Transition flows** — pre-workspace / one-shot actions (create workspace, accept invite). **NOT routes.** They live as `WindowOverlay` state, dispatched when the navigation adapter sees `push('/workspaces/new')` or `push('/invite/<id>')`. The shared view (`NewWorkspacePage`, `InvitePage`) is the content; the overlay wrapper supplies platform chrome.
- **Error / stale states** — "workspace not available", tabs pointing at a revoked workspace. **NOT pages.** `WorkspaceRouteLayout` auto-heals by dropping the stale tab group from the store; the user never lands on an explicit error screen. Web keeps `NoAccessPage` (shareable URL makes the error state meaningful); desktop has no URL bar so stale = heal silently.

**Adding a new pre-workspace flow on desktop**: register a new `WindowOverlay` type in `stores/window-overlay-store.ts`. Do NOT add it to `routes.tsx`. If a shared view needs the flow on both platforms, add the route on web (`apps/web/app/(auth)/...`) AND the overlay type on desktop — the shared view component is identical.

### Workspace context

`setCurrentWorkspace(slug, uuid)` from `@multica/core/platform` is the single source of truth for the active workspace. `WorkspaceRouteLayout` sets it on mount; unmount does NOT clear it. Code that leaves workspace context (leave/delete workspace, force-navigate to overlay) must call `setCurrentWorkspace(null, null)` explicitly.

### Workspace destructive operations

Leave / Delete workspace flows must follow this order, otherwise concurrent refetches race and the renderer hard-reloads:

1. Read destination from cached workspace list.
2. `setCurrentWorkspace(null, null)`.
3. `navigation.push(destination)`.
4. THEN `await mutation.mutateAsync(workspaceId)`.

### Tab isolation

Tabs are grouped per workspace in `stores/tab-store.ts`. The TabBar shows only the active workspace's tabs; cross-workspace tab leakage is impossible by construction (no flat global tabs array).

Cross-workspace `push(path)` is detected by the navigation adapter (`platform/navigation.tsx`) and translated into `switchWorkspace(slug, targetPath)` — NOT a navigation within the current tab's router. Don't bypass the adapter; always go through `useNavigation()` from shared code.

### Drag region (macOS)

Every full-window desktop view (anything outside the dashboard shell) must mount `<DragStrip />` from `@multica/views/platform` as the first flex child of the page root, otherwise users can't drag the window. Interactive UI inside the top 48px needs `WebkitAppRegion: "no-drag"` to stay clickable.

## UI/UX Rules

- Prefer shadcn components over custom implementations. Install via `pnpm ui:add <component>` from project root — adds to `packages/ui/components/ui/`. All components use Base UI primitives (`@base-ui/react`), not Radix.
- Use shadcn design tokens for styling. Avoid hardcoded color values.
- Do not introduce extra state (useState, context, reducers) unless explicitly required by the design.
- Pay close attention to **overflow** (truncate long text, scrollable containers), **alignment**, and **spacing** consistency.
- **If a component is identical between web and desktop, it belongs in a shared package.** Do not copy-paste between apps.

## Testing Rules

### Where to write tests

Tests follow the code, not the app. This is the most important testing principle in this monorepo:

| What you're testing | Where the test lives | Why |
|---|---|---|
| Shared business logic (stores, queries, hooks) | `packages/core/*.test.ts` | No DOM needed, pure logic |
| Shared UI components (pages, forms, modals) | `packages/views/*.test.tsx` | jsdom, no framework mocks |
| Platform-specific wiring (cookies, redirects, searchParams) | `apps/web/*.test.tsx` or `apps/desktop/` | Needs framework-specific mocks |
| End-to-end user flows | `e2e/*.spec.ts` | Real browser, real backend |

**Never test shared component behavior in an app's test file.** If a test requires mocking `next/navigation` or `react-router-dom` to test a component from `@multica/views`, the test is in the wrong place — move it to `packages/views/` and mock `@multica/core` instead.

### Test infrastructure

- `packages/core/` — Vitest, Node environment (no DOM)
- `packages/views/` — Vitest, jsdom environment, `@testing-library/react`
- `apps/web/` — Vitest, jsdom environment, framework-specific mocks
- `e2e/` — Playwright
- `server/` — Go standard `go test`

All test deps are in the pnpm catalog for unified versioning.

### Mocking conventions

- Mock `@multica/core` stores with `vi.hoisted()` + `Object.assign(selectorFn, { getState })` pattern (Zustand stores are both callable and have `.getState()`).
- Mock `@multica/core/api` for API calls.
- In `packages/views/` tests: never mock `next/*` or `react-router-dom` — those don't exist here.
- In `apps/web/` tests: mock framework-specific APIs only for platform-specific behavior.

### TDD workflow

1. Write failing test in the **correct package** first.
2. Write implementation.
3. Run `pnpm test` (Turborepo discovers all packages).
4. Green → done.

### Go tests

Standard `go test`. Tests should create their own fixture data in a test database.

### Go backend quality gate

When changing Go backend code under `server/`, database migrations, sqlc queries,
background workers, webhooks, external integrations, task queues, or state
synchronization flows, follow the repository skill at
`docs/agent-skills/go-backend-quality/SKILL.md` before implementation.

The practical bar is:

- Write the failing test first unless the change is mechanical or docs-only.
- Keep handlers thin; put business workflows in services or focused helpers.
- Enforce invariants in the database with constraints, transactions, locks, or leases.
- Design for multiple `multica-server` replicas; no process-local shared truth.
- Make webhook, sync, worker, and queue processing idempotent under retries.
- Use fake clients for external integration tests; do not depend on live services.
- Run `make sqlc` after query changes and include generated files.

### E2E tests

E2E tests should be self-contained. Use the `TestApiClient` fixture for data setup/teardown:

```typescript
import { loginAsDefault, createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

let api: TestApiClient;

test.beforeEach(async ({ page }) => {
  api = await createTestApi();
  await loginAsDefault(page);
});

test.afterEach(async () => {
  await api.cleanup();
});

test("example", async ({ page }) => {
  const issue = await api.createIssue("Test Issue");
  await page.goto(`/issues/${issue.id}`);
});
```

## Commit Rules

- Use atomic commits grouped by logical intent.
- Conventional format: `feat(scope)`, `fix(scope)`, `refactor(scope)`, `docs`, `test(scope)`, `chore(scope)`.

## Minimum Pre-Push Checks

```bash
make check    # Runs all checks: typecheck, unit tests, Go tests, E2E
```

Run verification only when the user explicitly asks for it.

For targeted checks when requested:
```bash
pnpm typecheck        # TypeScript type errors only
pnpm test             # TS unit tests only (Vitest, all packages)
make test             # Go tests only
pnpm exec playwright test   # E2E only (requires backend + frontend running)
```

## AI Agent Verification Loop

After writing or modifying code, always run the full verification pipeline:

```bash
make check
```

**Workflow:**
- Write code to satisfy the requirement
- Run `make check`
- If any step fails, read the error output, fix the code, and re-run
- Repeat until all checks pass
- Only then consider the task complete

**Quick iteration:** If you know only TypeScript or Go is affected, run individual checks first for faster feedback, then finish with a full `make check` before marking work complete.

## Release (Lilith fork)

Lilith ships only the desktop app; the `multica` CLI is bundled inside
the desktop binary via `apps/desktop/scripts/bundle-cli.mjs`, so it
follows the same release tag and isn't published separately.

Lilith ships only the desktop app; the `multica` CLI is bundled inside
the desktop binary via `apps/desktop/scripts/bundle-cli.mjs`, so it
follows the same release tag and isn't published separately.

1. On `main`, create a semver tag: `git tag v0.x.x` (or `git tag 0.x.x`
   — both shapes are accepted by `.gitlab-ci.yml`,
   `scripts/mirror-to-github.sh`, and the GitHub workflow).
2. Push to the GitLab origin: `git push origin v0.x.x`
3. GitLab CI's `mirror-to-github` job (`.gitlab-ci.yml`) force-pushes
   the current `main` and the new tag to `CopilotDemo/multica` on
   GitHub. The job authenticates via `GITHUB_MIRROR_DEPLOY_KEY` (see
   the one-time setup below).
4. GitHub Actions fires `.github/workflows/lilith-desktop-release.yml`:
   matrix build on macOS / Windows / Linux runners → upload installers
   + `latest-*.yml` (electron-updater metadata) to OSS → `ossutil ls`-
   verify each landed:
   - Installers → `oss://<bucket>/downloads/multica-desktop-<version>-<platform>-<arch>.<ext>`
   - Metadata  → `oss://<bucket>/downloads/latest-mac.yml`
                 `oss://<bucket>/downloads/latest.yml`
                 `oss://<bucket>/downloads/latest-arm64.yml`
                 `oss://<bucket>/downloads/latest-linux.yml`
   electron-builder generates the YML files locally during `package`;
   we just upload them. They contain `version`, per-file `sha512`, and
   the installer URL — the running desktop client reads them via
   electron-updater and decides whether to update.
5. GitLab CI's `notify-release` job (`.gitlab-ci.yml`) posts a Feishu
   card. (Runs after `mirror-to-github` succeeds so a Feishu card
   never claims a release that failed to mirror.)

### One-time GitLab → GitHub mirror setup

Required before `mirror-to-github` can run:

1. Generate a fresh SSH key pair locally:
   ```
   ssh-keygen -t ed25519 -N "" -C "gitlab-mirror" -f /tmp/gitlab_mirror_key
   ```
2. On `github.com/CopilotDemo/multica` → Settings → Deploy keys →
   **Add deploy key**: paste `gitlab_mirror_key.pub`, **tick "Allow
   write access"**.
3. On GitLab (this repo) → Settings → CI/CD → Variables → **Add
   variable**:
   - Key: `GITHUB_MIRROR_DEPLOY_KEY`
   - Value: contents of `gitlab_mirror_key` (the private half)
   - **Untick "Masked"** — GitLab can't mask multiline values, and
     the key body has newlines.
   - **Tick "Protected"** so the variable is only available on
     protected tags. Make sure release tags are configured as
     protected under Settings → Repository → Protected tags
     (`v?[0-9]+.[0-9]+.[0-9]+` matches both shapes).
4. Delete the local key copy: `rm /tmp/gitlab_mirror_key*`.

If a "File" type variable is preferred (safer — body never appears in
logs), point it at the file instead; the mirror script auto-detects
both shapes.

### Server-side proxy

The Go server exposes one route (see
`server/internal/handler/downloads.go`):

| Route | Upstream | Behavior |
| --- | --- | --- |
| `GET /api/downloads/<file>` | `s3://<bucket>/<prefix>/<file>` | Streamed unbuffered (no `io.ReadAll` — installers are 100MB+). Filename validated against path-traversal. Cache-Control depends on the file: `latest-*.yml` gets `max-age=60` so a republish is visible quickly; versioned installers get `immutable, max-age=31536000`. |

`<file>` is either an electron-updater metadata file (`latest-mac.yml`,
`latest.yml`, `latest-arm64.yml`, `latest-linux.yml`) or one of the
versioned installer binaries those YML files reference.

The bucket is **private** — the handler authenticates every request
via the AWS S3 SDK pointed at Aliyun OSS's S3-compatible endpoint
(same approach as `server/internal/storage/s3.go` uses for
attachments). Public-read on the bucket is not required and should
**not** be enabled.

#### Required env

| Var | Default | Notes |
| --- | --- | --- |
| `DOWNLOADS_OSS_BUCKET` | _(unset → route returns 503)_ | Bucket name only, no host. |
| `DOWNLOADS_OSS_ENDPOINT` | falls back to `AWS_ENDPOINT_URL` | Full S3-compat URL, e.g. `https://oss-cn-shanghai.aliyuncs.com`. |
| `DOWNLOADS_OSS_REGION` | falls back to `S3_REGION`, then `oss-cn-shanghai` | |
| `DOWNLOADS_OSS_PREFIX` | `downloads` | Directory inside the bucket. |
| `DOWNLOADS_OSS_ACCESS_KEY_ID` | falls back to `AWS_ACCESS_KEY_ID` | Read-only RAM user recommended. |
| `DOWNLOADS_OSS_ACCESS_KEY_SECRET` | falls back to `AWS_SECRET_ACCESS_KEY` | |

Most deployments only need `DOWNLOADS_OSS_BUCKET` since the AWS SDK
config + credentials are already set up for attachment storage.

To re-publish without a new build: overwrite
`oss://<bucket>/downloads/latest-*.yml` directly via `ossutil cp` —
clients pick up the change at next poll (≤ the YML's `max-age=60`
header + electron-updater's 1-hour poll interval).

Bump the patch version by default (`v0.1.12 → v0.1.13`) unless the
user specifies otherwise.

The installed desktop client checks for updates via electron-updater
configured with `provider: generic` pointing at
`https://multica.lilithgames.com/api/downloads`
(see `apps/desktop/electron-builder.yml`). It polls the
platform-specific `latest-*.yml`, downloads the installer referenced
there, verifies sha512, and prompts the user to install on next quit.
Wiring lives in `apps/desktop/src/main/updater.ts`.

## Multi-tenancy

All queries filter by `workspace_id`. Membership checks gate access. `X-Workspace-ID` header routes requests to the correct workspace.

## Agent Assignees

Assignees are polymorphic — can be a member or an agent. `assignee_type` + `assignee_id` on issues. Agents render with distinct styling (purple background, robot icon).
