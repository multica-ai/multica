# multica — Overview

> **Navigation aid.** This article shows WHERE things live (routes, models, files). Read actual source files before implementing new features or making changes.

**multica** is a typescript project built with next-app, chi, organized as a monorepo.

**Workspaces:** `@multica/desktop` (`apps/desktop`), `@multica/docs` (`apps/docs`), `@multica/web` (`apps/web`), `@multica/core` (`packages/core`), `@multica/eslint-config` (`packages/eslint-config`), `@multica/tsconfig` (`packages/tsconfig`), `@multica/ui` (`packages/ui`), `@multica/views` (`packages/views`), `server` (`server`)

## Scale

323 API routes · 36 database models · 182 UI components · 155 library files · 16 middleware layers · 85 environment variables

## Subsystems

- **[Auth](./auth.md)** — 6 routes — touches: auth, db, upload
- **[Payments](./payments.md)** — 4 routes — touches: auth, db, upload, cache, payment
- **[X-User-ID](./x-user-id.md)** — 1 routes — touches: auth, db
- **[Active-task](./active-task.md)** — 2 routes — touches: auth, db, upload
- **[Activity](./activity.md)** — 2 routes — touches: auth, db, upload
- **[Agent](./agent.md)** — 1 routes — touches: auth, db, queue
- **[Agents](./agents.md)** — 8 routes — touches: auth, db, upload
- **[Archive](./archive.md)** — 2 routes — touches: auth, db, upload
- **[Archive-all](./archive-all.md)** — 1 routes — touches: auth, db, upload
- **[Archive-all-read](./archive-all-read.md)** — 1 routes — touches: auth, db, upload
- **[Archive-completed](./archive-completed.md)** — 1 routes — touches: auth, db, upload
- **[Assignee-frequency](./assignee-frequency.md)** — 1 routes — touches: auth, db, upload
- **[Attachments](./attachments.md)** — 4 routes — touches: auth, db, upload
- **[Auth_test](./auth_test.md)** — 1 routes — touches: auth
- **[Autopilot](./autopilot.md)** — 3 routes — touches: auth, db, payment
- **[Autopilots](./autopilots.md)** — 7 routes — touches: auth, db, upload
- **[Batch-delete](./batch-delete.md)** — 1 routes — touches: auth, db, upload
- **[Batch-update](./batch-update.md)** — 1 routes — touches: auth, db, upload
- **[Chat](./chat.md)** — 8 routes — touches: auth, db, upload
- **[Child-progress](./child-progress.md)** — 1 routes — touches: auth, db, upload
- **[Children](./children.md)** — 2 routes — touches: auth, db, upload
- **[Cli-token](./cli-token.md)** — 1 routes — touches: auth, db, upload
- **[Client_test](./client_test.md)** — 5 routes — touches: auth
- **[Cmd_auth](./cmd_auth.md)** — 3 routes — touches: auth, db
- **[Cmd_issue](./cmd_issue.md)** — 1 routes — touches: auth, upload
- **[Comment](./comment.md)** — 1 routes — touches: auth, db, queue, upload
- **[Comments](./comments.md)** — 8 routes — touches: auth, db, upload
- **[Config](./config.md)** — 1 routes — touches: auth, db, upload
- **[Cookie](./cookie.md)** — 1 routes — touches: auth
- **[Csp_test](./csp_test.md)** — 1 routes
- **[Daemon](./daemon.md)** — 16 routes — touches: auth, db, upload
- **[Daily](./daily.md)** — 1 routes — touches: auth, db, upload
- **[Deregister](./deregister.md)** — 1 routes — touches: auth, db, upload
- **[Files](./files.md)** — 6 routes — touches: auth, db, upload
- **[Heartbeat](./heartbeat.md)** — 1 routes — touches: auth, db, upload
- **[Hub](./hub.md)** — 1 routes — touches: auth, db
- **[Hub_test](./hub_test.md)** — 1 routes — touches: auth
- **[Import](./import.md)** — 1 routes — touches: auth, db, upload
- **[Inbox](./inbox.md)** — 8 routes — touches: auth, db, upload
- **[Invitations](./invitations.md)** — 8 routes — touches: auth, db, upload
- **[Issue](./issue.md)** — 8 routes — touches: auth, db, queue, upload
- **[Issues](./issues.md)** — 22 routes — touches: auth, db, upload
- **[Leave](./leave.md)** — 2 routes — touches: auth, db, upload
- **[Mark-all-read](./mark-all-read.md)** — 1 routes — touches: auth, db, upload
- **[Me](./me.md)** — 2 routes — touches: auth, db, upload
- **[Members](./members.md)** — 6 routes — touches: auth, db, upload
- **[Messages](./messages.md)** — 2 routes — touches: auth, db, upload
- **[Pending-task](./pending-task.md)** — 2 routes — touches: auth, db, upload
- **[Ping](./ping.md)** — 2 routes — touches: auth, db, upload
- **[Pins](./pins.md)** — 4 routes — touches: auth, db, upload
- **[Projects](./projects.md)** — 5 routes — touches: auth, db, upload
- **[Reactions](./reactions.md)** — 4 routes — touches: auth, db, upload
- **[Read](./read.md)** — 3 routes — touches: auth, db, upload
- **[Reorder](./reorder.md)** — 1 routes — touches: auth, db, upload
- **[Restore](./restore.md)** — 2 routes — touches: auth, db, upload
- **[Route](./route.md)** — 1 routes
- **[Runs](./runs.md)** — 2 routes — touches: auth, db, upload
- **[Runtime](./runtime.md)** — 2 routes — touches: auth, db, cache
- **[Runtimes](./runtimes.md)** — 13 routes — touches: auth, db, upload
- **[Search](./search.md)** — 1 routes — touches: auth, db, upload
- **[Skills](./skills.md)** — 12 routes — touches: auth, db, upload
- **[Subscribers](./subscribers.md)** — 2 routes — touches: auth, db, upload
- **[Summary](./summary.md)** — 1 routes — touches: auth, db, upload
- **[Task-runs](./task-runs.md)** — 2 routes — touches: auth, db, upload
- **[Tasks](./tasks.md)** — 14 routes — touches: auth, db, upload
- **[Timeline](./timeline.md)** — 2 routes — touches: auth, db, upload
- **[Tokens](./tokens.md)** — 3 routes — touches: auth, db, upload
- **[Trigger](./trigger.md)** — 2 routes — touches: auth, db, upload
- **[Triggers](./triggers.md)** — 4 routes — touches: auth, db, upload
- **[Unread-count](./unread-count.md)** — 1 routes — touches: auth, db, upload
- **[Unsubscribe](./unsubscribe.md)** — 2 routes — touches: auth, db, upload
- **[Update](./update.md)** — 4 routes — touches: auth, db, upload
- **[Upload-file](./upload-file.md)** — 1 routes — touches: auth, db, upload
- **[Uploads](./uploads.md)** — 1 routes — touches: auth, db, upload
- **[Usage](./usage.md)** — 5 routes — touches: auth, db, upload
- **[Use-realtime-sync](./use-realtime-sync.md)** — 27 routes
- **[Workspace-id](./workspace-id.md)** — 1 routes — touches: auth, db
- **[Workspaces](./workspaces.md)** — 10 routes — touches: auth, db, upload
- **[Ws](./ws.md)** — 1 routes — touches: auth, db, upload
- **[Infra](./infra.md)** — 11 routes — touches: auth, db, upload, cache, payment
- **[Api](./api.md)** — 8 routes — touches: auth, db, upload

**Database:** unknown, 36 models — see [database.md](./database.md)

**UI:** 182 components (react) — see [ui.md](./ui.md)

**Libraries:** 155 files — see [libraries.md](./libraries.md)

## High-Impact Files

Changes to these files have the widest blast radius across the codebase:

- `encoding/json` — imported by **67** files
- `log/slog` — imported by **59** files
- `net/http` — imported by **59** files
- `path/filepath` — imported by **32** files
- `packages/core/types/index.ts` — imported by **24** files
- `packages/views/common/actor-avatar.tsx` — imported by **22** files

## Required Environment Variables

- `ALLOWED_ORIGINS` — `.env.example`
- `APP_ENV` — `server/internal/auth/cookie.go`
- `APPLE_TEAM_ID` — `apps/desktop/scripts/package.mjs`
- `AWS_ENDPOINT_URL` — `server/internal/storage/s3.go`
- `CLAUDE_CONFIG_DIR` — `server/internal/daemon/usage/claude.go`
- `CLOUDFRONT_DOMAIN` — `.env.example`
- `CLOUDFRONT_KEY_PAIR_ID` — `.env.example`
- `CLOUDFRONT_PRIVATE_KEY` — `.env.example`
- `CODEX_HOME` — `server/internal/daemon/execenv/codex_home.go`
- `COOKIE_DOMAIN` — `.env.example`
- `CORS_ALLOWED_ORIGINS` — `apps/web/next.config.ts`
- `ELECTRON_RENDERER_URL` — `apps/desktop/src/main/index.ts`
- _...41 more_

---
_Back to [index.md](./index.md) · Generated 2026-04-15_