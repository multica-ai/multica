# Wallts — Comprehensive Codebase Guide

> **Source**: `dwickyfp/multica` (fork of `multica-ai/multica`)
> **Purpose**: Enable any agent or developer to quickly understand, navigate, and contribute to the codebase.
> **Last updated**: 2026-06-02

---

## Table of Contents

1. [Architecture Overview](#1-architecture-overview)
2. [Data Flow Diagrams](#2-data-flow-diagrams)
3. [Repository Structure](#3-repository-structure)
4. [Backend (Go) Deep-Dive](#4-backend-go-deep-dive)
5. [Frontend (Next.js + Electron) Deep-Dive](#5-frontend-nextjs--electron-deep-dive)
6. [Database Schema Overview](#6-database-schema-overview)
7. [Auth System](#7-auth-system)
8. [WebSocket Architecture](#8-websocket-architecture)
9. [Daemon Protocol](#9-daemon-protocol)
10. [Configuration & Environment Variables](#10-configuration--environment-variables)
11. [Deployment & Infrastructure](#11-deployment--infrastructure)
12. [Rebranding Guide: Multica to Wallts](#12-rebranding-guide-multica-to-wallts)

---

## 1. Architecture Overview

Wallts (currently Multica) is an **AI agent orchestration platform** — it manages workspaces, issues, agents, tasks, chat, and real-time collaboration. The architecture follows a **Go backend + JavaScript monorepo** pattern.

### High-Level Components

```
+-------------------------------------------------------------+
|                        Clients                               |
|  +----------+  +----------+  +----------+  +-------------+  |
|  | Web App  |  | Desktop  |  |   CLI    |  | Agent Daemons| |
|  | (Next.js)|  |(Electron)|  |(multica) |  | (background) | |
|  +----+-----+  +----+-----+  +----+-----+  +------+-------+ |
+-------+-------------+-------------+---------------+----------+
        |             |             |               |
        |  HTTP/WS    |  HTTP/WS   | HTTP/WS       | HTTP/WS
        v             v             v               v
+-------------------------------------------------------------+
|                    Go Backend (Chi router)                   |
|  +----------+ +----------+ +----------+ +---------------+   |
|  | REST API | | WebSocket| |  Daemon  | |   Webhooks    |   |
|  |(150+ eps)| |  Hub     | |  WS Hub  | |(GitHub/Stripe)|   |
|  +----+-----+ +----+-----+ +----+-----+ +-------+-------+   |
|       +-------------+-----------+---------------+            |
|                     v           v                            |
|  +--------------------------------------------------------+ |
|  |          Internal Services & Business Logic            | |
|  |  Auth / Task Dispatch / Autopilot / Analytics / Storage| |
|  +----------------------+---------------------------------+ |
|                       v                                      |
|  +--------------+ +--------------+ +--------------------+    |
|  |  PostgreSQL  | |    Redis     | |  S3/Local Storage  |    |
|  |  (pgvector)  | | (realtime +  | |   (file uploads)   |    |
|  |              | |  rate-limit) | |                    |    |
|  +--------------+ +--------------+ +--------------------+    |
+-------------------------------------------------------------+
```

### Tech Stack

| Layer | Technology |
|-------|-----------|
| **Backend** | Go 1.26, Chi router, sqlc, pgx/v5, gorilla/websocket |
| **Frontend** | Next.js 16 (App Router), React 19, TypeScript 5.9 |
| **Desktop** | Electron (electron-vite), shared React frontend |
| **State** | React Query (server), Zustand (client) — 27 stores |
| **UI** | shadcn/UI + Base UI, Tailwind v4, OKLCH design tokens |
| **Database** | PostgreSQL 17 + pgvector, 48 tables, 111 migrations |
| **Cache/Realtime** | Redis 7 (Streams for cross-node WS relay) |
| **Storage** | S3 + CloudFront (prod) or local disk (dev/self-host) |
| **Monorepo** | pnpm 10.28 workspaces + Turborepo |
| **CI/CD** | GitHub Actions, GoReleaser, Docker multi-arch, Helm OCI |
| **i18n** | English, Chinese (Simplified), Korean (24 namespaces x 3 locales) |

---

## 2. Data Flow Diagrams

### 2.1 User Request to Agent Task Execution

```
User (Web/Desktop)
  |
  | POST /api/workspaces/:id/issues
  v
Chi Router -> Auth Middleware -> Workspace Middleware -> Issue Handler
  |
  | INSERT into `issue` + `agent_task_queue`
  | BROADCAST issue:created via WS
  v
Event Bus -> Redis Stream (cross-node relay)
  |
  | ws:scope:workspace:<id>:stream
  v
All connected clients (Web/Desktop) receive WS update
  |
  v
Daemon WebSocket receives `daemon:task_available` hint
  |
  | POST /api/daemon/runtimes/:id/tasks/claim  <-- HTTP = source of truth
  v
Daemon spawns agent CLI (Codex/Claude/Hermes/etc.)
  |
  | Agent gets `mat_` task token injected
  | Agent works autonomously...
  |
  | POST /tasks/:id/progress   (progress updates)
  | POST /tasks/:id/messages   (tool calls, text)
  | POST /tasks/:id/usage      (token counts)
  | POST /tasks/:id/complete   (final result)
  v
Backend updates DB -> broadcasts task:completed via WS
  |
  v
All clients see real-time progress + final result
```

### 2.2 WebSocket Event Flow

```
+------------+         +------------+         +------------+
|  Client A  |         |   Server   |         |  Client B  |
| (Web Tab)  |         |  (Go Hub)  |         | (Desktop)  |
+-----+------+         +-----+------+         +-----+------+
      |                      |                      |
      | -- WS connect ------>|                      |
      |                      |<-- WS connect ------ |
      |                      |                      |
      | <-- auth:success --- | <-- auth:success --- |
      |                      |                      |
      |  (user creates issue)|                      |
      | -- POST /issues ---->|                      |
      |                      |  INSERT DB           |
      |                      |--> Redis Stream ---->| (if multi-node)
      |                      |                      |
      | <-- issue:created -- | -- issue:created --> |
      |                      |                      |
      |    (daemon picks up) |                      |
      | <-- task:running --- | -- task:running -->  |
      | <-- task:progress -- | -- task:progress --> |
      | <-- task:completed - | -- task:completed -->|
```

### 2.3 Auth Flow

```
+----------+     +----------+     +----------+
|  Browser |     |  Server  |     |  Google  |
+----+-----+     +----+-----+     +----+-----+
     |                |                |
     | POST /auth/send-code            |
     | (email)        |                |
     |--------------->|                |
     |                | generate 6-digit code
     |                | store in DB (10min TTL)
     |                | send via email (Resend/SMTP)
     |<---------------|                |
     |                |                |
     | POST /auth/verify-code          |
     | (email + code) |                |
     |--------------->|                |
     |                | verify code    |
     |                | find/create user
     |                | sign JWT (HS256)
     |<---------------|                |
     | Set-Cookie: multica_auth (HttpOnly)
     | Set-Cookie: multica_csrf (readable)
     |                |                |
     | --- OR Google OAuth ---         |
     | GET /auth/google  |             |
     |--------------->|  redirect      |
     |                |--------------->|
     |<---------------|<---------------|
     |                | exchange code  |
     |                | userinfo       |
     |                | find/create user
     |                | sign JWT       |
     |<---------------|                |
     | Set-Cookie (same as above)
```

### 2.4 Monorepo Package Dependencies

```
apps/web ----------------+
    |                    |
    +-- @multica/core ---+-- (no dependencies - foundational)
    +-- @multica/ui -----+-- (no core import! - atomic)
    +-- @multica/views --+-- depends on core + ui
                         |
apps/desktop ------------+
    |
    +-- @multica/core
    +-- @multica/ui
    +-- @multica/views + Electron-specific IPC bridge
```

**Hard rules:**
- `packages/core/` = zero react-dom, zero localStorage, zero process.env
- `packages/ui/` = zero `@multica/core` imports
- `packages/views/` = zero `next/*`, zero `react-router-dom`, uses `NavigationAdapter`

---

## 3. Repository Structure

```
multica/
+-- server/                    # Go backend
|   +-- cmd/
|   |   +-- server/            # HTTP API server binary
|   |   +-- multica/           # CLI binary (cobra)
|   |   +-- migrate/           # DB migration CLI
|   |   +-- backfill_task_usage_hourly/
|   +-- internal/
|   |   +-- handler/           # ~130 HTTP handler files
|   |   +-- middleware/         # Auth, daemon_auth, CSP, ratelimit, logger
|   |   +-- auth/              # JWT, cookies, CSRF, token caches
|   |   +-- realtime/          # Client WS hub, Redis relay
|   |   +-- daemonws/          # Daemon WS hub
|   |   +-- events/            # In-process event bus
|   |   +-- service/           # Business logic (task dispatch, autopilot, cron)
|   |   +-- analytics/         # PostHog integration
|   |   +-- storage/           # S3 / local file storage
|   |   +-- metrics/           # Prometheus metrics
|   |   +-- daemon/            # Local daemon config, exec env
|   |   +-- cloudruntime/      # Cloud runtime provisioning
|   |   +-- mention/           # @mention parsing
|   |   +-- issueguard/        # Issue lifecycle guards
|   |   +-- skill/             # Skill template loading
|   +-- pkg/
|   |   +-- db/generated/      # sqlc-generated Go code
|   |   +-- db/queries/        # 34 SQL query definition files
|   |   +-- agent/             # Agent CLI adapters (13 agents)
|   |   +-- protocol/          # 90+ WS event types
|   |   +-- redact/            # Log/field redaction
|   +-- migrations/            # 111 migration pairs (up/down)
|
+-- apps/
|   +-- web/                   # Next.js 16 App Router
|   |   +-- app/
|   |   |   +-- (auth)/        # login, onboarding, workspaces/new, invitations
|   |   |   +-- (landing)/     # /, /about, /changelog, /download
|   |   |   +-- [workspaceSlug]/
|   |   |       +-- (dashboard)/  # issues, projects, agents, runtimes, etc.
|   |   +-- platform/          # Next.js-only APIs
|   +-- desktop/               # Electron app
|       +-- src/main/          # Main process (daemon-manager, updater, IPC)
|       +-- src/preload/       # Preload bridge (4 APIs)
|       +-- src/renderer/      # Renderer (shared React + desktop stores)
|
+-- packages/
|   +-- core/                  # Headless business logic (Zustand, React Query, API client)
|   +-- ui/                    # Atomic UI components (shadcn, zero business logic)
|   +-- views/                 # Shared business pages/components (routing-agnostic)
|   +-- tsconfig/              # Shared TypeScript config
|   +-- eslint-config/         # Shared ESLint config
|
+-- docker/                    # Docker entrypoint
+-- deploy/helm/               # Helm chart
+-- scripts/                   # Install, dev, check scripts
+-- e2e/                       # Playwright E2E tests
+-- docs/                      # Internal design docs
+-- .github/workflows/         # CI/CD (4 workflows)
|
+-- Dockerfile                 # Backend (Go)
+-- Dockerfile.web             # Frontend (Next.js)
+-- docker-compose.yml         # Dev (Postgres only)
+-- docker-compose.selfhost.yml # Full stack self-host
+-- Makefile                   # 31 targets
+-- turbo.json                 # Turborepo config
+-- .goreleaser.yml            # CLI release config
+-- .env.example               # 237-line env template
+-- CLAUDE.md                  # AI agent instructions
+-- SELF_HOSTING.md            # Self-hosting guide
+-- SELF_HOSTING_ADVANCED.md   # Advanced self-hosting
+-- CLI_AND_DAEMON.md          # CLI & daemon docs
```


---

## 4. Backend (Go) Deep-Dive

### 4.1 Entry Points

| Binary | Path | Purpose |
|--------|------|---------|
| **server** | `cmd/server/main.go` | HTTP API + WS server. Starts Chi router, Redis relay, event bus, background workers (runtime sweeper, autopilot scheduler, heartbeat scheduler, DB stats) |
| **multica** | `cmd/multica/main.go` | CLI (cobra). Subcommands: agent, auth, autopilot, config, daemon, issue, label, login, project, repo, runtime, setup, skill, squad, update, user, version, workspace |
| **migrate** | `cmd/migrate/main.go` | DB migration CLI: `migrate <up|down>` |
| **backfill** | `cmd/backfill_task_usage_hourly/main.go` | One-shot usage backfill utility |

### 4.2 HTTP Routes (150+ endpoints)

**Router:** `cmd/server/router.go` using Chi

**Global Middleware Stack:**
1. `chimw.RequestID` — request ID generation/propagation
2. `middleware.ClientMetadata` — `X-Client-Platform/Version/OS` to context
3. `middleware.RequestLogger` — structured slog logging
4. `chimw.Recoverer` — panic recovery
5. `middleware.ContentSecurityPolicy` — CSP header
6. `cors.Handler()` — origins from `CORS_ALLOWED_ORIGINS`/`FRONTEND_ORIGIN`
7. `obsmetrics.HTTPMetrics.Middleware` — Prometheus (conditional)

#### Public Endpoints (no auth)

| Method | Path | Notes |
|--------|------|-------|
| GET | `/health`, `/readyz`, `/healthz` | Health checks |
| GET | `/ws` | Client WebSocket |
| POST | `/auth/send-code` | Rate-limited 5/min |
| POST | `/auth/verify-code` | Rate-limited 20/min |
| POST | `/auth/google` | Google OAuth, rate-limited 5/min |
| POST | `/auth/logout` | Clear cookies |
| GET | `/api/config` | Public config |
| POST | `/api/contact-sales` | Rate-limited 5/hr |
| POST | `/api/webhooks/github` | HMAC-SHA256 verified |
| POST | `/api/webhooks/stripe` | Stripe signature verified |
| POST | `/api/webhooks/autopilots/{token}` | Token-in-URL auth |

#### Daemon Endpoints (`/api/daemon/*`) — DaemonAuth middleware

| Method | Path | Purpose |
|--------|------|---------|
| POST | `/register` | Upsert runtime rows |
| POST | `/deregister` | Remove daemon |
| POST | `/heartbeat` | Update last_seen, pull pending actions |
| GET | `/ws` | Daemon WebSocket |
| GET | `/workspaces/{id}/repos` | Workspace repos |
| POST | `/runtimes/{id}/tasks/claim` | Claim queued task |
| GET | `/runtimes/{id}/tasks/pending` | List pending tasks |
| POST | `/runtimes/{id}/update/{updateId}/result` | Report update result |
| POST | `/runtimes/{id}/models/{requestId}/result` | Report model list |
| POST | `/runtimes/{id}/local-skills/{requestId}/result` | Report skills |
| GET/POST | `/tasks/{id}/{action}` | start, progress, complete, fail, usage, messages, session, wait-local-directory |
| GET | `/issues/{id}/gc-check` | GC liveness checks |
| POST | `/runtimes/{id}/recover-orphans` | Orphan task recovery |

#### Authenticated User Endpoints (`/api/*`)

| Group | Endpoints |
|-------|-----------|
| User | `/api/me`, `/api/cli-token`, `/api/upload-file`, `/api/feedback` |
| Workspaces | Full CRUD on `/api/workspaces/*` |
| Invitations | `/api/invitations/*` |
| Tokens | `/api/tokens/*` |
| Billing | `/api/cloud-billing/*` |

#### Workspace-Scoped Endpoints (Auth + RequireWorkspaceMember)

| Domain | Count | Examples |
|--------|-------|---------|
| Issues | 30+ | CRUD, bulk update, dependencies, search, filters |
| Labels | 5+ | CRUD, assignment |
| Projects | 5+ | CRUD, resources |
| Squads | 5+ | CRUD, member management |
| Autopilots | 16 | CRUD, triggers, runs, deliveries |
| Agents | 14 | CRUD, task queue, runtime binding |
| Skills | 11 | CRUD, file management |
| Runtimes | 16 | CRUD, usage, health, custom pricing |
| Cloud Runtime | 12 | Provisioning, lifecycle |
| Chat | 11 | Sessions, messages, pending tasks |
| Inbox | 8 | Items, notifications |
| Dashboard | 4 | Stats, activity |
| Other | 10+ | Pins, attachments, comments, notification preferences, agent templates |

### 4.3 Key Internal Packages

| Package | Path | Purpose |
|---------|------|---------|
| **handler** | `internal/handler/` | ~130 files — all HTTP handlers |
| **middleware** | `internal/middleware/` | Auth, daemon_auth, workspace, ratelimit, CSP, logger |
| **auth** | `internal/auth/` | JWT, cookies/CSRF, CloudFront signer, PAT/daemon/membership caches |
| **realtime** | `internal/realtime/` | Client WS hub, Redis Streams relay, broadcaster interface |
| **daemonws** | `internal/daemonws/` | Daemon WS hub, notifier |
| **events** | `internal/events/` | In-process event bus |
| **service** | `internal/service/` | Business logic: task dispatch, autopilot scheduler, email, cron, squad |
| **analytics** | `internal/analytics/` | PostHog integration |
| **storage** | `internal/storage/` | File storage (local disk or S3) |
| **metrics** | `internal/metrics/` | Prometheus metrics |
| **daemon** | `internal/daemon/` | Local daemon config, exec env, agent lifecycle, skills, repocache |
| **daemon/execenv** | `internal/daemon/execenv/` | Agent CLI env setup (Codex home, memory, multi-agent config) |
| **cloudruntime** | `internal/cloudruntime/` | Cloud runtime provisioning |
| **mention** | `internal/mention/` | @mention parsing (issue links, member notifications, agent triggers) |
| **issueguard** | `internal/issueguard/` | Issue lifecycle guards (status transitions, permission checks) |
| **skill** | `internal/skill/` | Skill template loading |
| **db/generated** | `pkg/db/generated/` | sqlc-generated Go code |
| **db/queries** | `pkg/db/queries/` | SQL query definitions (34 files) |
| **agent** | `pkg/agent/` | Agent CLI adapters (13 agents) |
| **protocol** | `pkg/protocol/` | Daemon-server protocol types (90+ event types) |
| **redact** | `pkg/redact/` | Log/field redaction |



---

## 5. Frontend (Next.js + Electron) Deep-Dive

### 5.1 Monorepo Configuration

| File | Key Details |
|------|------------|
| `pnpm-workspace.yaml` | Workspaces: `apps/*`, `packages/*`. Catalog: React 19.2.3, zustand ^5, react-query ^5.96, zod ^4, tailwindcss ^4 |
| `turbo.json` | Tasks: `build` (^build), `dev` (persistent), `typecheck` (^typecheck). Global env: `DATABASE_URL`, `NEXT_PUBLIC_API_URL`, `NEXT_PUBLIC_WS_URL` |
| Root `package.json` | pnpm 10.28.2, Turbo v2.5.4 |

### 5.2 Web App (`apps/web/`) — Next.js 16 App Router

**Config:** `next.config.ts` rewrites `/api/*`, `/ws`, `/auth/*`, `/uploads/*` to Go backend.

**No native API routes** — all proxied to Go backend.

#### Route Tree

```
app/
+-- layout.tsx                          <-- Root (Inter/Geist_Mono/Source_Serif_4, ThemeProvider)
+-- (auth)/                             <-- No URL segment
|   +-- login/page.tsx                  --> /login
|   +-- onboarding/page.tsx             --> /onboarding
|   +-- workspaces/new/page.tsx         --> /workspaces/new
|   +-- invitations/page.tsx            --> /invitations
|   +-- invite/[id]/page.tsx            --> /invite/:id
+-- auth/callback/page.tsx              --> /auth/callback
+-- (landing)/                          <-- No URL segment
|   +-- page.tsx                        --> / (landing page)
|   +-- about/page.tsx                  --> /about
|   +-- changelog/page.tsx              --> /changelog
|   +-- contact-sales/page.tsx          --> /contact-sales
|   +-- download/page.tsx               --> /download
|   +-- usecases/[slug]/page.tsx        --> /usecases/:slug
+-- [workspaceSlug]/                    <-- Dynamic workspace scope
    +-- layout.tsx                      <-- Auth gate + workspace resolution
    +-- (dashboard)/
        +-- issues/ + [id]
        +-- my-issues/
        +-- projects/ + [id]
        +-- agents/ + [id]
        +-- autopilots/ + [id]
        +-- runtimes/ + [id]
        +-- skills/ + [id]
        +-- squads/ + [id]
        +-- members/[id]
        +-- inbox/
        +-- settings/
        +-- billing/
        +-- usage/
        +-- attachments/[id]/preview
```

**30 page files, 4 layouts, 3 route groups, i18n: en/zh-Hans/ko**

### 5.3 Desktop App (`apps/desktop/`) — Electron

| Layer | Key Files |
|-------|-----------|
| **Main** | `index.ts` (app entry, single instance, deep-link, WS origin strip), `daemon-manager.ts` (local daemon lifecycle, PAT mint, 15 IPC channels), `updater.ts` (auto-update), `cli-bootstrap.ts` (managed CLI download) |
| **Preload** | 4 APIs: `desktopAPI`, `daemonAPI`, `updater`, `electron` |
| **Renderer** | `App.tsx` (CoreProvider, deep-link auth), `routes.tsx` (per-tab memory router), `stores/tab-store.ts` (Zustand tabs), `stores/window-overlay-store.ts` |

**IPC channels:** ~30 total (daemon status, auth token, updater, local-directory, navigation-gesture)

### 5.4 State Management (`packages/core/`)

#### React Query (Server State)

- 13 query key factories (issues, workspace, chat, runtimes, autopilots, billing, projects, labels, pins, inbox, dashboard, GitHub, agents)
- 50+ mutations with optimistic updates pattern: `onMutate -> cancel -> snapshot -> patch cache -> onError rollback -> onSettled invalidate`
- Default: `staleTime: Infinity`, `gcTime: 10min`
- WS events invalidate React Query — never write directly to stores

#### Zustand Stores (27 total)

| Domain | Stores | Persisted |
|--------|--------|-----------|
| Auth | `createAuthStore()` | No |
| Config | `configStore` | No (vanilla) |
| Navigation | `useNavigationStore` | Yes |
| Modals | `useModalStore` (11 types) | No |
| Issues | `useIssueStore`, `useIssueViewStore`, `useIssueDraftStore`, `useIssueSelectionStore`, `useQuickCreateStore`, `useCreateModeStore`, `useIssuesScopeStore`, `useCommentCollapseStore`, `useCommentDraftStore` (30d TTL), `useRecentIssuesStore` (LRU 50ws/20issues), `myIssuesViewStore`, `actorIssuesViewStore` | Most yes |
| Chat | `createChatStore()` | Manual |
| Agents | `useAgentsViewStore`, `useTranscriptViewStore` | Yes |
| Projects | `useProjectViewStore`, `useProjectDraftStore` | Yes |
| Squads | `useSquadsViewStore` | Yes |
| Onboarding | `useWelcomeStore` | No |
| Runtimes | `useCustomPricingStore` | Yes |
| Feedback | `useFeedbackDraftStore` | Yes |

**Persistence:** `createWorkspaceAwareStorage()` namespaces by workspace slug. `registerForWorkspaceRehydration()` rehydrates on workspace switch.

#### API Client (`api/client.ts`)

- Native `fetch` (no axios), `ApiClient` class, ~100+ methods
- Auth: Bearer token OR cookie mode, CSRF from `multica_csrf` cookie
- Headers: `Authorization`, `X-Workspace-Slug`, `X-CSRF-Token`, `X-Request-ID`, `X-Client-Platform/Version/OS`
- Zod validation: `parseWithFallback()` for graceful degradation

### 5.5 UI Components (`packages/ui/`)

- ~65 shadcn/Base UI primitives (accordion to tooltip)
- Tailwind v4 CSS-first (no config file), CVA, tailwind-merge
- Design tokens: `styles/tokens.css` — full OKLCH color scale
- Markdown pipeline: react-markdown + rehype-raw/sanitize/katex + remark-gfm/math + Shiki code blocks
- **Hard rule:** zero `@multica/core` imports

### 5.6 Views (`packages/views/`)

Routing-agnostic business components using `NavigationAdapter`:

| Module | Key Components |
|--------|---------------|
| layout | `DashboardLayout`, `AppSidebar`, `DashboardGuard`, `WorkspaceLoader` |
| issues | `IssuesPage`, `IssueDetail`, `StatusIcon/Picker`, `PriorityPicker`, `AssigneePicker`, `LabelPicker`, `CommentCard/Input` |
| projects | `ProjectsPage`, `ProjectDetail`, `ProjectPicker` |
| agents | `AgentsPage`, `AgentDetailPage` |
| autopilots | `AutopilotsPage`, `AutopilotDetailPage` |
| runtimes | `RuntimesPage`, `RuntimeDetailPage` |
| skills | `SkillsPage`, `SkillDetailPage` |
| squads | `SquadsPage`, `SquadDetailPage` |
| editor | `ContentEditor` (TipTap 3), `TitleEditor`, `AttachmentCard/Preview` |
| chat | `ChatFab`, `ChatWindow` |
| search | `SearchCommand`, `SearchTrigger` |
| settings | `SettingsPage` (accepts `ExtraSettingsTab` for desktop injection) |
| onboarding | `OnboardingFlow`, `CliInstallInstructions` |
| auth | `LoginPage`, `useLogout` |



---

## 6. Database Schema Overview

**Engine:** PostgreSQL 17 + pgvector extension
**ORM:** sqlc (type-safe Go code generation from SQL)
**Migrations:** 111 pairs (up/down) in `server/migrations/`
**Query files:** 34 SQL files in `pkg/db/queries/`

### All 48 Tables

#### Users and Auth (5 tables)

| Table | Key Columns | Purpose |
|-------|------------|---------|
| `user` | id (UUID), email, name, avatar_url, created_at | User accounts |
| `verification_code` | id, user_id, code, expires_at | Magic login codes (6-digit, 10min) |
| `personal_access_token` | id, user_id, name, token_hash, expires_at | PATs (`mul_` prefix, SHA-256 stored) |
| `daemon_token` | id, workspace_id, agent_id, token_hash | Daemon auth tokens (`mdt_` prefix) |
| `task_token` | id, task_id, agent_id, token_hash, expires_at | Agent task tokens (`mat_` prefix) |

#### Workspaces and Members (3 tables)

| Table | Key Columns | Purpose |
|-------|------------|---------|
| `workspace` | id, name, slug, created_by | Workspace/org containers |
| `member` | id, workspace_id, user_id, role | Workspace membership (owner/admin/member) |
| `workspace_invitation` | id, workspace_id, email, role, status | Pending invitations |

#### Issues and Related (7 tables)

| Table | Key Columns | Purpose |
|-------|------------|---------|
| `issue` | id, workspace_id, title, description, status, priority, assignee_id, parent_id, position, created_by | Core issue/task entity |
| `issue_dependency` | id, issue_id, depends_on_id | Issue dependency graph |
| `issue_label` | id, workspace_id, name, color | Label definitions |
| `issue_to_label` | issue_id, label_id | Issue-label junction |
| `issue_reaction` | id, issue_id, user_id, emoji | Emoji reactions |
| `issue_subscriber` | id, issue_id, user_id | Notification subscriptions |
| `issue_pull_request` | id, issue_id, github_pr_id | Issue-PR linkage |

#### Projects (2 tables)

| Table | Key Columns | Purpose |
|-------|------------|---------|
| `project` | id, workspace_id, name, description, status | Project groupings |
| `project_resource` | id, project_id, resource_type, resource_id | Project-issue/PR linkage |

#### Agents and Runtimes (4 tables)

| Table | Key Columns | Purpose |
|-------|------------|---------|
| `agent` | id, workspace_id, name, model, system_prompt | Agent definitions |
| `agent_runtime` | id, agent_id, daemon_id, status, last_seen | Runtime instances |
| `agent_skill` | id, agent_id, skill_id | Agent-skill junction |
| `agent_task_queue` | id, agent_id, task_id, status, priority | Task queue |

#### Tasks and Usage (4 tables)

| Table | Key Columns | Purpose |
|-------|------------|---------|
| `task_message` | id, task_id, seq, type, content, tool_name | Task execution messages |
| `task_usage` | id, task_id, input_tokens, output_tokens | Token usage per task |
| `task_usage_hourly` | id, agent_id, hour, input_tokens, output_tokens | Hourly usage aggregation |
| `task_usage_hourly_dirty` + `task_usage_hourly_rollup_state` | — | Rollup tracking |

#### Skills (2 tables)

| Table | Key Columns | Purpose |
|-------|------------|---------|
| `skill` | id, workspace_id, name, description, content | Skill definitions |
| `skill_file` | id, skill_id, path, content | Skill files |

#### Squads (2 tables)

| Table | Key Columns | Purpose |
|-------|------------|---------|
| `squad` | id, workspace_id, name, description | Squad/team definitions |
| `squad_member` | id, squad_id, member_id, role | Squad membership |

#### Autopilots (4 tables)

| Table | Key Columns | Purpose |
|-------|------------|---------|
| `autopilot` | id, workspace_id, name, mode, schedule | Automation rules |
| `autopilot_run` | id, autopilot_id, status, started_at | Execution history |
| `autopilot_trigger` | id, autopilot_id, trigger_type, config | Trigger definitions |
| `webhook_delivery` | id, autopilot_id, status, request, response | Webhook delivery log |

#### Chat (2 tables)

| Table | Key Columns | Purpose |
|-------|------------|---------|
| `chat_session` | id, workspace_id, user_id, agent_id, title | Chat sessions |
| `chat_message` | id, session_id, role, content | Chat messages |

#### Inbox and Notifications (2 tables)

| Table | Key Columns | Purpose |
|-------|------------|---------|
| `inbox_item` | id, user_id, workspace_id, type, title, read | Notification items |
| `notification_preference` | id, user_id, workspace_id, channel, enabled | Per-channel prefs |

#### GitHub Integration (3 tables)

| Table | Key Columns | Purpose |
|-------|------------|---------|
| `github_installation` | id, installation_id, account_name | GitHub App installs |
| `github_pull_request` | id, repo, number, title, state, issue_id | PR tracking |
| `github_pull_request_check_suite` | id, pr_id, conclusion | CI status |

#### Other (8 tables)

| Table | Purpose |
|-------|---------|
| `attachment` | File attachments |
| `comment` | Issue comments (threaded via parent_id) |
| `comment_reaction` | Comment emoji reactions |
| `contact_sales_inquiry` | Sales contact form submissions |
| `daemon_connection` | Active daemon WebSocket connections |
| `feedback` | User feedback |
| `pinned_item` | Pinned items per workspace |


---

## 7. Auth System

### 7.1 Token Types

| Prefix | Kind | Storage | Use Case |
|--------|------|---------|----------|
| JWT (HS256) | Session token | Stateless (`JWT_SECRET`) | Web session, CLI handoff |
| `mul_` | Personal Access Token | `personal_access_tokens` (SHA-256 hash) | Human/CLI auth |
| `mdt_` | Daemon Token | `daemon_tokens` (hashed) | Daemon-machine auth |
| `mat_` | Agent Task Token | `task_tokens` (hashed) | Injected into agent process by daemon |
| `mcn_` | Cloud Node PAT | Multica Cloud Fleet API | Cloud-managed nodes |

### 7.2 Auth Middleware (`internal/middleware/auth.go`)

```
Request -> Strip X-Actor-Source (prevent spoofing)
       -> Extract token (Bearer header > multica_auth cookie)
       -> Cookie path requires CSRF for state-changing requests
       -> Branch by prefix:
           mat_ -> DB lookup
           mcn_ -> Cloud Fleet verify
           mul_ -> PATCache -> DB
           JWT  -> HS256 parse
       -> Stamp X-User-ID, X-Actor-Source in context
```

### 7.3 Daemon Auth (`internal/middleware/daemon_auth.go`)

- **No cookie fallback** (header only) — prevents CSWSH
- Context keys: `ctxKeyDaemonWorkspaceID`, `ctxKeyDaemonID`, `ctxKeyDaemonAuthPath`
- Shares PATCache with regular Auth middleware

### 7.4 CSRF Protection (`internal/auth/cookie.go`)

- Two cookies: `multica_auth` (HttpOnly) + `multica_csrf` (readable by JS)
- CSRF token: `hex(nonce).hex(HMAC-SHA256(nonce, authToken))` — bound to auth token
- SameSite=Strict, Secure flag derived from `FRONTEND_ORIGIN` scheme

### 7.5 Login Flows

1. **Magic Code:** 6-digit, 10min expiry, rate-limited 1/60s, `MULTICA_DEV_VERIFICATION_CODE` bypass for dev
2. **Google OAuth:** Code exchange -> userinfo -> find/create user -> JWT
3. **CLI Handoff:** `POST /api/cli-token` — cookie session -> fresh JWT

### 7.6 Token Caches (Redis-backed)

| Cache | TTL | Purpose |
|-------|-----|---------|
| PATCache | 10min (clamped to expiry) | PAT lookup, `Invalidate()` on revoke |
| DaemonTokenCache | Same pattern | Daemon token lookup |
| MembershipCache | 5min | Workspace membership existence |
| CloudPATCache | 60s | Cloud PAT (revocation latency trade-off) |



---

## 8. WebSocket Architecture

### 8.1 Client/Web UI WebSocket (`internal/realtime/hub.go`)

- **Endpoint:** `GET /ws`
- **Auth:** Cookie (`multica_auth` JWT) OR first-message `{"type":"auth","payload":{"token":"..."}}` within 10s
- **Query params:** `workspace_id` or `workspace_slug`
- **Hub model:** Rooms `map[scopeKey]to map[*Client]`
- **Scope types:** `workspace`, `user`, `task`, `chat`, `daemon_runtime`
- **Auto-subscribe:** workspace + user scopes on connect
- **Client frames:** `subscribe`, `unsubscribe`, `ping` -> `subscribe_ack`/`unsubscribe_ack`/`pong`
- **Slow client eviction:** non-blocking send, full channel -> evict
- **Dedup:** per-client LRU 128-event-id (ULIDs)
- **Timeouts:** pongWait=60s, pingPeriod=54s, readLimit=4096 bytes

### 8.2 Daemon WebSocket (`internal/daemonws/hub.go`)

- **Endpoint:** `GET /api/daemon/ws` (behind `DaemonAuth`)
- **Auth:** `Authorization: Bearer ***` header only (no cookies — CSWSH prevention)
- **Query:** `runtime_id` or `runtime_ids` (comma-separated)
- **Server->Daemon:** `daemon:task_available` (wakeup hint, best-effort)
- **Daemon->Server:** `daemon:heartbeat` -> ack with `RuntimeGone` flag
- **Hub:** Indexed by runtime ID for targeted dispatch, send buffer=16
- **Origin:** `CheckOrigin: true` (safe — auth header required)

### 8.3 Cross-Node Relay (`internal/realtime/redis_relay.go`)

- **Backend:** Redis Streams: `ws:scope:<type>:<id>:stream`
- **Envelope:** `event_id` (ULID), `event_type`, `scope`, `scope_id`, `workspace_id`, `actor_id`, `payload_json`
- **MAXLEN:** 10000 per stream
- **Heartbeat TTL:** 90s
- **Consumer idle grace:** 10min
- **Per-scope XREADGROUP consumer** (on-demand)
- **Modes:** `sharded` / `dual` / `legacy` (via `REALTIME_RELAY_MODE`)

### 8.4 Event Types (90+)

| Category | Events |
|----------|--------|
| **Issues** | `issue:created`, `issue:updated`, `issue:deleted` |
| **Comments** | `comment:created`, `comment:updated`, `comment:deleted` |
| **Reactions** | `reaction:added`, `reaction:removed` |
| **Tasks** | `task:queued`, `task:dispatch`, `task:running`, `task:progress`, `task:completed`, `task:failed`, `task:cancelled` |
| **Chat** | `chat:message`, `chat:done`, `chat:session_read`, `chat:session_deleted` |
| **Daemon** | `daemon:heartbeat`, `daemon:register`, `daemon:task_available` |
| **Inbox** | `inbox:created`, `inbox:updated`, `inbox:read` |
| **Skills** | `skill:created`, `skill:updated`, `skill:deleted` |
| **Agents** | `agent:created`, `agent:updated`, `agent:deleted` |
| **GitHub** | `github_installation:*`, `pull_request:*` |
| **Other** | Full CRUD lifecycle for all entities |



---

## 9. Daemon Protocol

### 9.1 Daemon Lifecycle

```
multica daemon start [--foreground]
  |
  +-- Load profile: ~/.multica/profiles/<name>/
  +-- Health check port: 19514 (default) or base+1+hash%1000
  +-- Auto-update: polls GitHub releases
  |
  v
  +-------------------------------------+
  |  Register -> Heartbeat Loop -> Work |
  |                                     |
  |  1. POST /api/daemon/register       |
  |     {DaemonID, AgentID, Runtimes[]} |
  |                                     |
  |  2. Connect WS /api/daemon/ws       |
  |     Listen for task_available hints  |
  |                                     |
  |  3. Heartbeat every 15s             |
  |     POST /api/daemon/heartbeat      |
  |     -> Receive pending actions       |
  |     -> RuntimeGone signal -> re-reg |
  |                                     |
  |  4. Poll for tasks every 3s         |
  |     POST /runtimes/:id/tasks/claim  |
  +-------------------------------------+
```

### 9.2 Task Execution Flow

```
Task Claimed
  |
  v
Spawn Agent CLI (Codex/Claude/Hermes/etc.)
  |  - Inject `mat_` task token as env var
  |  - Set up worktree in MULTICA_WORKSPACES_ROOT
  |  - Configure agent-specific env (CODEX_HOME, etc.)
  |
  v
Agent runs autonomously...
  |
  +-- POST /tasks/:id/progress    -> progress updates
  +-- POST /tasks/:id/messages    -> tool calls, text output
  +-- POST /tasks/:id/usage       -> token counts
  +-- POST /tasks/:id/session     -> session metadata
  |
  v
Task Completion
  +-- POST /tasks/:id/complete    -> success with result
  +-- POST /tasks/:id/fail        -> error with details
```

### 9.3 Key Protocol Payloads

```go
// Registration
DaemonRegisterPayload {
    DaemonID  string
    AgentID   string
    Runtimes  []RuntimeInfo{Type, Version, Status}
}

// Heartbeat acknowledgment
DaemonHeartbeatAckPayload {
    RuntimeID   string
    Status      string
    RuntimeGone bool  // signal to prune stale runtime
    Pending     struct {
        Update      []PendingUpdate
        ModelList   []PendingModelList
        LocalSkills []PendingLocalSkills
    }
}

// Task messages
TaskMessagePayload {
    TaskID   string
    Seq      int
    Type     string  // text | tool_use | tool_result | error
    Tool     string  // optional
    Content  string  // optional
}
```

### 9.4 Supported Agent CLIs (13)

| Agent | Binary | Env Var Override |
|-------|--------|-----------------|
| Claude | `claude` | `MULTICA_CLAUDE_PATH` |
| Codex | `codex` | `MULTICA_CODEX_PATH` |
| Cursor | `cursor-agent` | `MULTICA_CURSOR_PATH` |
| Copilot | `copilot` | `MULTICA_COPILOT_PATH` |
| Gemini | `gemini` | `MULTICA_GEMINI_PATH` |
| Hermes | `hermes` | `MULTICA_HERMES_PATH` |
| Pi | `pi` | `MULTICA_PI_PATH` |
| OpenCode | `opencode` | `MULTICA_OPENCODE_PATH` |
| OpenClaw | `openclaw` | `MULTICA_OPENCLAW_PATH` |
| Kimi | `kimi` | `MULTICA_KIMI_PATH` |
| Kiro | `kiro-cli` | `MULTICA_KIRO_PATH` |
| Antigravity | `antigravity` | `MULTICA_ANTIGRAVITY_PATH` |


---

## 10. Configuration & Environment Variables

### 10.1 Config Entry Points

| File | Purpose |
|------|---------|
| `.env` (from `.env.example`) | Server + web env vars (237 lines) |
| `docker-compose.selfhost.yml` | Full stack orchestration |
| `deploy/helm/multica/values.yaml` | Kubernetes deployment config |
| `Makefile` | Dev/build/deploy automation (31 targets) |
| `.goreleaser.yml` | CLI release config |
| `scripts/install.sh` | One-liner installer |
| `turbo.json` | Turborepo build config |
| `next.config.ts` | Next.js config (rewrites, proxy) |

### 10.2 Complete Environment Variables (100+)

#### Core Server

| Variable | Purpose | Default |
|----------|---------|---------|
| `DATABASE_URL` | PostgreSQL connection string | `postgres://multica:***@localhost:5432/multica?sslmode=disable` |
| `DATABASE_MAX_CONNS` | pgxpool max connections | 25 |
| `DATABASE_MIN_CONNS` | pgxpool min connections | 5 |
| `REDIS_URL` | Redis for rate-limiting, realtime, caching. Fail-open when unset | — |
| `PORT` | HTTP listen port | 8080 |
| `APP_ENV` | `production`/`staging`/`dev` | `production` |
| `JWT_SECRET` | **REQUIRED** — JWT signing secret | — |
| `LOG_LEVEL` | Server log verbosity | — |
| `METRICS_ADDR` | Prometheus listener (e.g. `127.0.0.1:9090`) | disabled |

#### Auth & Security

| Variable | Purpose | Default |
|----------|---------|---------|
| `AUTH_TOKEN_TTL` | Auth cookie lifetime (seconds) | 2592000 (30d) |
| `COOKIE_DOMAIN` | Cross-subdomain cookie domain | empty |
| `ALLOW_SIGNUP` | `false` disables registration | `true` |
| `ALLOWED_EMAILS` | Comma-separated email whitelist | — |
| `ALLOWED_EMAIL_DOMAINS` | Comma-separated domain whitelist | — |
| `DISABLE_WORKSPACE_CREATION` | `true` blocks workspace creation | — |
| `MULTICA_DEV_VERIFICATION_CODE` | Fixed 6-digit code for dev | — |

#### CORS & Origins

| Variable | Purpose |
|----------|---------|
| `CORS_ALLOWED_ORIGINS` | Comma-separated allowed origins |
| `ALLOWED_ORIGINS` | Alias for realtime hub (fallback chain) |
| `FRONTEND_ORIGIN` | Frontend URL (CORS, cookie SameSite, redirects) |
| `MULTICA_PUBLIC_URL` | Public internet URL for webhooks |
| `MULTICA_APP_URL` | App URL (fallback to FRONTEND_ORIGIN) |

#### Google OAuth

| Variable | Purpose |
|----------|---------|
| `GOOGLE_CLIENT_ID` | Google OAuth client ID |
| `GOOGLE_CLIENT_SECRET` | Google OAuth secret |
| `GOOGLE_REDIRECT_URI` | Override callback URL (auto-derived from FRONTEND_ORIGIN) |

#### Email (Resend — recommended)

| Variable | Purpose | Default |
|----------|---------|---------|
| `RESEND_API_KEY` | Resend.com API key. Empty = codes print to stdout | — |
| `RESEND_FROM_EMAIL` | Sender address | `noreply@multica.ai` |

#### Email (SMTP alternative)

| Variable | Purpose | Default |
|----------|---------|---------|
| `SMTP_HOST` | Activates SMTP mode when set | — |
| `SMTP_PORT` | SMTP port | 25 |
| `SMTP_USERNAME` | Auth user | — |
| `SMTP_PASSWORD` | Auth password | — |
| `SMTP_TLS` | TLS mode: `starttls`/`implicit`/`smtps`/`ssl` | `starttls` |
| `SMTP_TLS_INSECURE` | `true` = skip TLS verification | — |

#### S3 / CloudFront Storage

| Variable | Purpose |
|----------|---------|
| `S3_BUCKET` | Bucket name |
| `S3_REGION` | Region (default `us-west-2`) |
| `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` | Credentials |
| `AWS_ENDPOINT_URL` | Custom endpoint (MinIO/R2/etc.) |
| `CLOUDFRONT_KEY_PAIR_ID` | Signed-URL key pair |
| `CLOUDFRONT_PRIVATE_KEY` | Private key (base64 inline) |
| `CLOUDFRONT_PRIVATE_KEY_SECRET` | AWS Secrets Manager name |
| `CLOUDFRONT_DOMAIN` | CDN domain |

#### Local File Storage

| Variable | Purpose | Default |
|----------|---------|---------|
| `LOCAL_UPLOAD_DIR` | Upload directory when no S3 | `./data/uploads` |
| `LOCAL_UPLOAD_BASE_URL` | Base URL for local uploads | derived from backend port |

#### GitHub Integration

| Variable | Purpose |
|----------|---------|
| `GITHUB_APP_SLUG` | GitHub App slug |
| `GITHUB_WEBHOOK_SECRET` | Webhook verification secret |
| `GITHUB_TOKEN` | PAT for release info / skill repos |

#### Rate Limiting

| Variable | Purpose | Default |
|----------|---------|---------|
| `RATE_LIMIT_AUTH` | req/IP/min for send-code | 5 |
| `RATE_LIMIT_AUTH_VERIFY` | req/IP/min for verify-code | 20 |
| `MULTICA_TRUSTED_PROXIES` | Trusted CIDRs for XFF | — |

#### Analytics (PostHog)

| Variable | Purpose | Default |
|----------|---------|---------|
| `POSTHOG_API_KEY` | PostHog project key. Empty = no-op | — |
| `POSTHOG_HOST` | PostHog host | `https://us.i.posthog.com` |
| `ANALYTICS_DISABLED` | `true` = force no-op | — |

#### Realtime

| Variable | Purpose | Default |
|----------|---------|---------|
| `REALTIME_RELAY_MODE` | `sharded`/`dual`/`legacy` | — |
| `REALTIME_RELAY_SHARDS` | Number of Redis stream shards | — |
| `REALTIME_RELAY_STREAM_MAXLEN` | Max stream length | 10000 |
| `REALTIME_METRICS_TOKEN` | Bearer token for `/health/realtime` | — |

#### Daemon / Agent Runtime

| Variable | Purpose | Default |
|----------|---------|---------|
| `MULTICA_SERVER_URL` | Daemon->API WebSocket URL | — |
| `MULTICA_DAEMON_ID` | Daemon identifier | — |
| `MULTICA_DAEMON_PORT` | Daemon API port | — |
| `MULTICA_DAEMON_POLL_INTERVAL` | Task poll interval | `3s` |
| `MULTICA_DAEMON_HEARTBEAT_INTERVAL` | Heartbeat interval | `15s` |
| `MULTICA_DAEMON_MAX_CONCURRENT_TASKS` | Max parallel tasks | — |
| `MULTICA_DAEMON_AUTO_UPDATE` | Auto-update toggle | — |
| `MULTICA_WORKSPACE_ID` | Active workspace ID | — |
| `MULTICA_WORKSPACES_ROOT` | Root dir for workspaces | `~/multica_workspaces` |
| `MULTICA_TOKEN` | Auth token for CLI | — |
| `MULTICA_GC_ENABLED` | `false` = disable GC | — |
| `MULTICA_KEEP_ENV_AFTER_TASK` | `true` = preserve env between tasks | — |

#### Agent Path/Model Overrides

| Variable Pattern | Purpose |
|-----------------|---------|
| `MULTICA_{AGENT}_PATH` | Override binary path (CLAUDE, CODEX, COPILOT, OPENCODE, OPENCLAW, HERMES, GEMINI, PI, CURSOR, KIMI, KIRO, ANTIGRAVITY) |
| `MULTICA_{AGENT}_MODEL` | Override default model for agent |

#### Web (Next.js)

| Variable | Purpose | Default |
|----------|---------|---------|
| `NEXT_PUBLIC_API_URL` | API base URL for browser | — |
| `NEXT_PUBLIC_WS_URL` | WebSocket URL override | — |
| `NEXT_PUBLIC_APP_VERSION` | Build version | — |
| `NEXT_PUBLIC_ENABLE_CLOUD_RUNTIME` | `true` = show cloud runtime page | — |
| `FRONTEND_PORT` | Dev port | 3000 |
| `REMOTE_API_URL` | Server-side proxy target | — |
| `STANDALONE` | `true` = Next.js standalone output | — |

#### Desktop (Electron)

| Variable | Purpose |
|----------|---------|
| `ELECTRON_RENDERER_URL` | Dev renderer URL |
| `DESKTOP_RENDERER_PORT` | Dev port (default 5173) |
| `APPLE_TEAM_ID` | Apple notarization Team ID |

#### Docker / Self-Host Images

| Variable | Purpose | Default |
|----------|---------|---------|
| `MULTICA_IMAGE_TAG` | Image tag | `latest` |
| `MULTICA_BACKEND_IMAGE` | Backend image | `ghcr.io/multica-ai/multica-backend` |
| `MULTICA_WEB_IMAGE` | Web image | `ghcr.io/multica-ai/multica-web` |


---

## 11. Deployment & Infrastructure

### 11.1 Docker Compose (Self-Host)

**`docker-compose.selfhost.yml`** — 3 services:

| Service | Image | Ports | Volumes |
|---------|-------|-------|---------|
| postgres | `pgvector/pgvector:pg17` | internal only | `pgdata:/var/lib/postgresql/data` |
| backend | `ghcr.io/multica-ai/multica-backend:latest` | `127.0.0.1:8080:8080` | `backend_uploads:/app/data/uploads` |
| frontend | `ghcr.io/multica-ai/multica-web:latest` | `127.0.0.1:3000:3000` | — |

All ports bind `127.0.0.1` (security: Docker bypasses UFW/iptables on `0.0.0.0`).

**Quick start:**
```bash
make selfhost          # Pull images + start
make selfhost-build    # Build from source + start
make selfhost-stop     # Stop all
```

### 11.2 Helm / Kubernetes

**Chart:** `deploy/helm/multica/` (version 0.1.0)
**OCI:** `oci://ghcr.io/multica-ai/charts/multica`

**Templates:**
- `backend.yaml` — PVC (5Gi) + Deployment + Service (ClusterIP:8080)
- `frontend.yaml` — Deployment + Service (ClusterIP:3000)
- `postgres.yaml` — PVC (10Gi) + Deployment + Service (ClusterIP:5432)
- `ingress.yaml` — 2 Ingresses (frontend to :3000, backend to :8080)
- `configmap.yaml` — 17 env keys

**Secrets:** Pre-create `multica-secrets` with JWT_SECRET, POSTGRES_PASSWORD, RESEND_API_KEY, etc.

### 11.3 CI/CD Workflows

| Workflow | Trigger | What it does |
|----------|---------|-------------|
| `ci.yml` | Push/PR to main | Frontend (build/typecheck/lint/test), Backend (build/test with PG+Redis), Installer test (matrix: ubuntu+macOS) |
| `release.yml` | Tag `v*.*.*` | Verify -> GoReleaser (CLI) -> Docker backend (amd64/arm64) -> Docker web -> Helm chart -> Desktop (linux+windows) |
| `mobile-verify.yml` | Path-filtered push/PR | Mobile-only typecheck+lint |
| `desktop-smoke.yml` | Manual dispatch | Desktop build test (no publish) |

### 11.4 CLI Distribution

**GoReleaser:** 6 targets (darwin/linux/windows x amd64/arm64), CGO_ENABLED=0
**Homebrew:** `brew install multica-ai/tap/multica`
**Install script:** `curl ... | bash -s -- [--with-server]`

### 11.5 Makefile Targets (31)

| Category | Targets |
|----------|---------|
| Self-host | `selfhost`, `selfhost-build`, `selfhost-stop` |
| Setup | `setup`, `start`, `stop`, `check` |
| Database | `db-up`, `db-down`, `db-reset`, `migrate-up`, `migrate-down`, `sqlc` |
| Build | `build`, `test`, `dev`, `server`, `daemon` |
| Worktree | `worktree-env`, `setup-worktree`, `start-worktree`, `stop-worktree`, `check-worktree` |
| Cleanup | `clean` |



---

## 12. Rebranding Guide: Multica to Wallts

This section catalogs every location where "multica" appears and must be changed for rebranding to "Wallts".

### 12.1 Code-Level Changes

#### Backend (Go)

| Category | What to Change | File Pattern |
|----------|---------------|-------------|
| **Go module path** | `github.com/multica-ai/multica` -> new org path | `go.mod`, all Go imports |
| **Binary names** | `multica` CLI -> `wallts` | `cmd/multica/`, Makefile, scripts |
| **Token prefixes** | `mul_`, `mdt_`, `mat_`, `mcn_` -> `wal_`, `wdt_`, `wat_`, `wcn_` | `internal/auth/`, `internal/middleware/` |
| **Cookie names** | `multica_auth`, `multica_csrf` -> `wallts_auth`, `wallts_csrf` | `internal/auth/cookie.go`, frontend API client |
| **DB connection** | Default DB name `multica` -> `wallts` | `.env.example`, `scripts/ensure-postgres.sh` |
| **Redis stream prefix** | `ws:scope:` (no change needed) | `internal/realtime/redis_relay.go` |
| **Daemon health port** | Magic number 19514 (may want to change) | `internal/daemon/` |
| **Analytics** | PostHog event names, `multica` references | `internal/analytics/` |
| **Email templates** | "Multica" brand name in email copy | `internal/service/email*` |
| **CSP headers** | Any `multica` domains in CSP | `internal/middleware/` |

#### Frontend (TypeScript/React)

| Category | What to Change | File Pattern |
|----------|---------------|-------------|
| **Package names** | `@multica/core`, `@multica/ui`, `@multica/views` -> `@wallts/*` | All `package.json`, all imports |
| **npm scope** | `@multica` -> `@wallts` | `pnpm-workspace.yaml`, all packages |
| **App title/brand** | "Multica" -> "Wallts" in all UI text | `apps/web/`, `packages/views/`, i18n files |
| **i18n strings** | 24 namespaces x 3 locales = 72 files to update | `packages/views/locales/{en,zh-Hans,ko}/*.json` |
| **Cookie names** | `multica_auth`, `multica_csrf` in API client | `packages/core/api/client.ts` |
| **Deep-link scheme** | `multica://` -> `wallts://` | `apps/desktop/src/main/index.ts` |
| **Config paths** | `~/.multica/` -> `~/.wallts/` | Daemon config, desktop runtime loader |
| **Desktop IPC** | Channel names (if branded) | `apps/desktop/src/` |

#### Infrastructure

| Category | What to Change | File Pattern |
|----------|---------------|-------------|
| **Docker images** | `ghcr.io/multica-ai/multica-backend` -> new registry path | `docker-compose.selfhost.yml`, `.env.example` |
| **Helm chart** | Chart name, app name, image refs | `deploy/helm/multica/` (rename dir) |
| **GitHub org** | `multica-ai` -> new org | All GitHub Actions, GoReleaser, install scripts |
| **Homebrew tap** | `multica-ai/homebrew-tap` -> new tap | `.goreleaser.yml` |
| **Install script** | Binary name, org name, URLs | `scripts/install.sh`, `scripts/install.ps1` |
| **Env var names** | `MULTICA_*` -> `WALLTS_*` (100+ vars) | `.env.example`, all `os.Getenv()` calls |
| **CI/CD workflows** | Image names, org references | `.github/workflows/*.yml` |
| **README/docs** | All "Multica" references | `README.md`, `SELF_HOSTING.md`, `CLI_AND_DAEMON.md`, `docs/` |

### 12.2 Database Migrations

**No schema changes needed** — table/column names don't contain "multica". The rebrand is cosmetic (display names, env vars, binary names), not structural.

### 12.3 Search Patterns for Full Coverage

```bash
# Find all "multica" references (case-insensitive)
grep -ri "multica" --include="*.go" --include="*.ts" --include="*.tsx" \
  --include="*.js" --include="*.json" --include="*.yaml" --include="*.yml" \
  --include="*.md" --include="*.sh" --include="*.sql" --include="*.css" \
  --include="*.html" --include="*.env*" --include="Dockerfile*" \
  --include="Makefile" .

# Find env vars starting with MULTICA_
grep -r "MULTICA_" --include="*.go" --include="*.ts" --include="*.sh" .

# Find token prefixes
grep -rE "mul_|mdt_|mat_|mcn_" --include="*.go" .

# Find cookie names
grep -r "multica_auth\|multica_csrf" --include="*.go" --include="*.ts" .

# Find package imports
grep -r "@multica/" --include="*.ts" --include="*.tsx" .
```

### 12.4 Recommended Rebranding Order

1. **Go module path** (`go.mod`) + all imports — massive but mechanical
2. **Package names** (`@multica/*` -> `@wallts/*`) + all imports
3. **Token prefixes** (auth system) — requires DB migration for existing tokens
4. **Cookie names** — affects all active sessions (users must re-login)
5. **Env vars** (`MULTICA_*` -> `WALLTS_*`) — update all code + docs
6. **Binary names** (`multica` -> `wallts`) — update CLI, Makefile, scripts
7. **Docker/Helm images** — update registry paths + CI/CD
8. **UI strings & i18n** — 72 locale files
9. **Documentation** — README, guides, docs
10. **Deep-link scheme** (`multica://` -> `wallts://`) — desktop only

### 12.5 Breaking Changes to Handle

| Change | Impact | Migration |
|--------|--------|-----------|
| Cookie name change | All users logged out | Announce maintenance window |
| Token prefix change | Existing PATs/daemon tokens invalid | DB migration to update stored hashes? Or force regenerate |
| Env var rename | Self-hosted instances break | Provide migration script, backward compat shim |
| Docker image change | `docker pull` fails for old tags | Publish both old+new for one release cycle |
| Go module path | All forks must update imports | Major version bump recommended |

---

*End of guide. This document should be the single source of truth for understanding the Wallts (Multica) codebase.*
