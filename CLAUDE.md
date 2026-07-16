# CLAUDE.md

Guidance for Claude Code when working in this repository. Keep this file short and authoritative: rules here should be hard to infer from code or easy to get wrong.

## Conventions

The source of truth for code naming, i18n glossary, and Chinese product voice is:

- `apps/docs/content/docs/developers/conventions.mdx`
- `apps/docs/content/docs/developers/conventions.zh.mdx`

Read it before editing translations in `packages/views/locales/`, naming routes/packages/files/DB columns/types, or writing Chinese UI/docs copy. Do not rely on `packages/views/locales/glossary.md`; it is only a redirect stub.

## Project Shape

Multica is an AI-native task management platform for small teams, with agents as first-class assignees that can own issues, comment, and change status.

- `server/`: Go backend, Chi router, sqlc, gorilla/websocket.
- `apps/web/`: Next.js App Router.
- `apps/desktop/`: Electron desktop app.
- `apps/mobile/`: Expo / React Native iOS app. Read `apps/mobile/CLAUDE.md` before touching it.
- `packages/core/`: headless business logic, API client, React Query hooks, Zustand stores.
- `packages/ui/`: atomic UI components only.
- `packages/views/`: shared business pages/components for web and desktop.
- `packages/tsconfig/`: shared TypeScript config.

Shared packages export raw `.ts` / `.tsx` and are compiled by consuming apps. Dependency direction is `views -> core + ui`; `core` and `ui` must stay independent.

## State Rules

Keep server state and client state separate.

- TanStack Query owns server state: issues, users, workspaces, inbox, agents, members, and anything fetched from the API.
- Zustand owns client/view state: filters, drafts, modals, tab layout, and navigation history. Current workspace identity is route-driven; platform stores/singletons may mirror slug/id only for headers, persistence namespaces, and reconnects.
- Shared Zustand stores live in `packages/core/`, never in `packages/views/` or app directories.
- React Context is for platform plumbing only, such as `WorkspaceIdProvider` and `NavigationProvider`.
- Only auth/workspace stores may call `api.*` directly. Other server interaction belongs in queries/mutations.
- Workspace-scoped query keys must include `wsId`.
- Optimistic updates only when ALL hold: outcome locally predictable, user stays on the same screen (no navigation), failure is rare, rollback is trivial. Canonical: status/assignee/toggle field patches — patch determinate caches, roll back on failure, invalidate uncertain projections on settle.
- Flows that navigate or confirm (create, delete, leave) must await the server before navigating or cleaning up; never optimistically remove an entity from cache.
- Chat/message send uses the pending-message pattern: render immediately with a visible pending state and retry on failure, not silent optimism.
- WebSocket events invalidate or patch Query cache for server data. They must never mirror server payload data into Zustand; clearing client-owned pointers (active session, selection, current workspace) is allowed only with a single responder and a self-initiated guard when this client can cause the event.
- Persist durable preferences/drafts/layout. Do not persist server data or ephemeral UI state.
- Zustand selectors must return stable references. Do not return freshly allocated objects/arrays from selectors without shallow comparison.
- Hooks that need workspace context should accept `wsId`; do not call `useWorkspaceId()` internally unless the hook is guaranteed to run under the provider.

## Package Boundaries

These are hard constraints:

- `packages/core/`: no `react-dom`, `localStorage` (use `StorageAdapter`), `process.env`, or UI libraries.
- `packages/ui/`: no `@multica/core` imports and no business logic.
- `packages/views/`: no `next/*`, no `react-router-dom`, no stores. Use `NavigationAdapter`, `useNavigation()`, and `<AppLink>`.
- `apps/web/platform/`: only place for Next.js navigation/platform APIs.
- `apps/desktop/src/renderer/src/platform/`: only place for `react-router-dom` navigation wiring.
- Every workspace under `apps/` and `packages/` must declare directly imported external packages in its own `package.json`.
- Shared dependencies use `catalog:` from `pnpm-workspace.yaml`; `apps/mobile/` pins Expo/React Native related versions directly.

## Sharing Rules

Web and desktop share business logic, hooks, stores, components, and views through `packages/core/`, `packages/ui/`, and `packages/views/`.

If the same logic exists in both web and desktop, extract it unless it depends on platform APIs:

1. Next.js, Electron, or router APIs stay in the app/platform layer.
2. Headless logic belongs in `packages/core/`.
3. Shared UI or business views belong in `packages/views/`.
4. Shared primitives belong in `packages/ui/`.

Mobile is independent. It may import types and pure functions from `@multica/core`, with `import type` for types, but owns its UI, state, hooks, providers, i18n, React version, build pipeline, and release cadence.

## Commands

Use the repo scripts as the source of truth. Common commands:

```bash
make dev              # auto-setup and start the app
make start            # start backend + frontend
make stop             # stop app processes for this checkout
make server           # run Go server only
make daemon           # run local daemon
make test             # Go tests
make sqlc             # regenerate sqlc code after SQL changes
pnpm install
pnpm dev:web
pnpm dev:desktop
pnpm build
pnpm typecheck
pnpm lint
pnpm test             # TS/Vitest tests through Turborepo
pnpm exec playwright test
pnpm ui:add badge     # shadcn/Base UI component into packages/ui
```

Worktrees share one PostgreSQL container and get isolated DB names/ports via `.env.worktree`. `make dev` auto-detects this. For manual setup use `make worktree-env`, `make setup-worktree`, and `make start-worktree`. `pnpm dev:desktop` additionally self-isolates per worktree (its own renderer port + app name) automatically, independent of `.env.worktree`.

CI runs Node 22, Go 1.26.1, and a `pgvector/pgvector:pg17` PostgreSQL service.

## Database and Migration Rules

These are hard requirements for every new or modified database design and production migration:

- Do not add database foreign keys (`FOREIGN KEY` / `REFERENCES`), cascading deletes, or cascading updates. Resolve relationships, validation, and dependent cleanup explicitly in application code. Use an application transaction when cleanup and the parent operation must commit or roll back atomically.
- Every index created by a migration must use `CREATE INDEX CONCURRENTLY` or `CREATE UNIQUE INDEX CONCURRENTLY`, including indexes on newly created tables. PostgreSQL rejects concurrent index creation inside a transaction or a multi-command string, so keep each concurrent index build in its own single-statement migration file. The repository migration runner executes migration files outside an explicit transaction to support this.

## Coding Rules

- TypeScript strict mode is enabled; keep types explicit.
- Go follows standard conventions: `gofmt`, `go vet`, checked errors.
- Code comments must be English.
- Prefer existing patterns/components over new parallel abstractions.
- Avoid broad refactors unless required by the task.
- For internal, non-boundary code, do not add compatibility layers, fallback paths, dual writes, legacy adapters, or temporary shims unless explicitly requested.
- API boundaries are different: installed desktop clients can talk to newer backends, so response parsing must follow the API compatibility rules below.
- If a flow or API is being replaced and the product is not live, prefer removing the old path instead of preserving both.
- New global pre-workspace routes must be a single word (`/login`, `/inbox`) or `/{noun}/{verb}` (`/workspaces/new`). Do not add hyphenated root routes like `/new-workspace`.
- Reserved slugs live in `server/internal/handler/reserved_slugs.json`. Edit it, run `pnpm generate:reserved-slugs`, and commit the generated `packages/core/paths/reserved-slugs.ts`.
- When changing CLI commands/flags, API fields, or product behavior documented by built-in skills under `server/internal/service/builtin_skills/*`, update the relevant `SKILL.md` and `references/*-source-map.md` in the same PR.

## API Compatibility

Frontend code must survive backend response drift, especially in installed desktop builds.

- Parse API JSON with `parseWithFallback` in `packages/core/api/schema.ts` and a zod schema. Do not cast network JSON to `T`.
- Endpoint responses consumed by UI logic must pass through a schema before returning.
- Downstream UI should optional-chain and default fields defensively.
- Prefer explicit boolean checks (`=== true`) over truthy/falsy checks on server fields.
- Do not pin critical affordances to one backend boolean; combine signals when possible.
- Server-driven enum switches need a `default` branch.
- When adding or changing an endpoint, add/update the schema and include a malformed-response test.

## Backend UUID Rules

In `server/internal/handler/`, always know where a UUID came from before using it in write queries.

- Resource path params that may be UUIDs or human-readable IDs must be resolved through loaders such as `loadIssueForUser`, `loadSkillForUser`, `loadAgentForUser`, or `requireDaemonRuntimeAccess`; subsequent writes use the resolved `entity.ID`.
- Pure UUID inputs from request boundaries use `parseUUIDOrBadRequest(w, s, fieldName)` and return immediately on `ok=false`.
- Trusted UUID round-trips from sqlc results or test fixtures use `parseUUID(s)`, which panics on invalid input.
- Outside handlers, `util.ParseUUID(s) (pgtype.UUID, error)` is the safe variant; always check the error.

## Web/Desktop Features

When adding a shared page or feature for web and desktop:

1. Put the page/component in `packages/views/<domain>/`.
2. Add platform wiring in both `apps/web/app/` and the desktop router, unless the desktop flow is a transition overlay.
3. Use `useNavigation().push()` or `<AppLink>` in shared code.
4. Use shared guards/providers such as `DashboardGuard` from `packages/views/layout/`.
5. Keep platform-only UI in the app or inject it through props/slots.
6. Hooks that need workspace context should accept `wsId`.

CSS for web/desktop is shared from `packages/ui/styles/`. Use semantic tokens such as `bg-background` and `text-muted-foreground`; avoid hardcoded Tailwind colors and duplicated base styles.

## Desktop Rules

Desktop routing has three categories:

- Session routes: workspace-scoped tab destinations such as `/:slug/issues`.
- Transition flows: pre-workspace one-shot actions such as create workspace or accept invite. These are `WindowOverlay` state, not routes.
- Error/stale states: stale workspace tabs should auto-heal by dropping stale tab groups, not render desktop error pages.

More desktop constraints:

- New pre-workspace desktop flows register a `WindowOverlay` type in `stores/window-overlay-store.ts`; do not add them to `routes.tsx`.
- `setCurrentWorkspace(slug, uuid)` from `@multica/core/platform` mirrors the active route for headers, storage namespaces, and reconnects; workspace route layouts own setting it.
- Code that leaves workspace context must call `setCurrentWorkspace(null, null)` explicitly.
- Workspace delete must await the server before navigation/cleanup. Workspace leave currently clears/navigates before mutation only to avoid the `member:removed` realtime race; treat that as known debt, not a reusable pattern.
- Cross-workspace navigation must go through the navigation adapter so it can call `switchWorkspace(slug, targetPath)`.
- Full-window desktop views outside the dashboard shell must mount `<DragStrip />` from `@multica/views/platform` as the first flex child. Interactive controls in the top 48px need `WebkitAppRegion: "no-drag"`.

## Mobile Rules

Read `apps/mobile/CLAUDE.md` before touching `apps/mobile/`. It contains the mandatory pre-flight process, import limits, parity rules, tech stack, UI rules, data helpers, realtime strategy, and mobile release flow.

Root-level reminders:

- Mobile shares only `@multica/core` types and pure functions.
- Mobile must match web/desktop product semantics: counts, permissions, enums/transitions, and data identity.
- Mobile may differ in UI/interaction when the phone context requires it.

## UI Rules

- Prefer shadcn/Base UI components over custom implementations. Add them with `pnpm ui:add <component>` from the repo root.
- Use design tokens and semantic classes; avoid hardcoded colors.
- Do not introduce extra local state unless the design requires it.
- Handle overflow, long text, scrolling, alignment, and spacing deliberately.
- If a component is identical between web and desktop, it belongs in a shared package.

## Testing

Tests follow the code:

| What is tested | Location |
| --- | --- |
| Shared business logic, stores, queries, hooks | `packages/core/*.test.ts` |
| Shared UI components, pages, forms, modals | `packages/views/*.test.tsx` |
| Platform wiring such as cookies, redirects, search params | `apps/web/*.test.tsx` or `apps/desktop/` |
| End-to-end flows | `e2e/*.spec.ts` |
| Backend | `server/` Go tests |

Rules:

- Never test shared component behavior in an app test file.
- `packages/views/` tests must not mock `next/*` or `react-router-dom`.
- Mock `@multica/core` stores with the Zustand callable-store shape (`selectorFn` plus `getState`).
- Mock `@multica/core/api` for API calls.
- E2E tests should use `TestApiClient` for setup/teardown.
- Prefer writing the failing test in the correct package before implementation when the change is behavioral.

## Verification

For code changes, run the narrowest useful checks while iterating, then run broader verification when risk justifies it or when asked.

Useful checks:

```bash
pnpm typecheck
pnpm test
make test
pnpm exec playwright test
make check
```

Do not claim verification passed unless you ran it. If you skip checks because the change is docs-only or the user asked not to run them, say so.

## Commits and Releases

- Commits should be atomic and use conventional prefixes: `feat(scope)`, `fix(scope)`, `refactor(scope)`, `docs`, `test(scope)`, `chore(scope)`.
- A production deployment requires a CLI release tag on `main`: create `v0.x.x`, push it, and let `release.yml` publish binaries and the Homebrew tap.
- Bump patch by default unless the user specifies a version.

## Domain Reminders

- All queries filter by `workspace_id`; membership gates access; `X-Workspace-ID` selects the workspace.
- Issue assignees are polymorphic: `assignee_type` plus `assignee_id` can reference a member or an agent.
