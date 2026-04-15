# multica — Wiki

_Generated 2026-04-15 — re-run `npx codesight --wiki` if the codebase has changed._

Structural map compiled from source code via AST. No LLM — deterministic, 200ms.

> **How to use safely:** These articles tell you WHERE things live and WHAT exists. They do not show full implementation logic. Always read the actual source files before implementing new features or making changes. Never infer how a function works from the wiki alone.

## Articles

- [Overview](./overview.md)
- [Database](./database.md)
- [Auth](./auth.md)
- [Payments](./payments.md)
- [X-user-id](./x-user-id.md)
- [Active-task](./active-task.md)
- [Activity](./activity.md)
- [Agent](./agent.md)
- [Agents](./agents.md)
- [Archive](./archive.md)
- [Archive-all](./archive-all.md)
- [Archive-all-read](./archive-all-read.md)
- [Archive-completed](./archive-completed.md)
- [Assignee-frequency](./assignee-frequency.md)
- [Attachments](./attachments.md)
- [Auth_test](./auth_test.md)
- [Autopilot](./autopilot.md)
- [Autopilots](./autopilots.md)
- [Batch-delete](./batch-delete.md)
- [Batch-update](./batch-update.md)
- [Chat](./chat.md)
- [Child-progress](./child-progress.md)
- [Children](./children.md)
- [Cli-token](./cli-token.md)
- [Client_test](./client_test.md)
- [Cmd_auth](./cmd_auth.md)
- [Cmd_issue](./cmd_issue.md)
- [Comment](./comment.md)
- [Comments](./comments.md)
- [Config](./config.md)
- [Cookie](./cookie.md)
- [Csp_test](./csp_test.md)
- [Daemon](./daemon.md)
- [Daily](./daily.md)
- [Deregister](./deregister.md)
- [Files](./files.md)
- [Heartbeat](./heartbeat.md)
- [Hub](./hub.md)
- [Hub_test](./hub_test.md)
- [Import](./import.md)
- [Inbox](./inbox.md)
- [Invitations](./invitations.md)
- [Issue](./issue.md)
- [Issues](./issues.md)
- [Leave](./leave.md)
- [Mark-all-read](./mark-all-read.md)
- [Me](./me.md)
- [Members](./members.md)
- [Messages](./messages.md)
- [Pending-task](./pending-task.md)
- [Ping](./ping.md)
- [Pins](./pins.md)
- [Projects](./projects.md)
- [Reactions](./reactions.md)
- [Read](./read.md)
- [Reorder](./reorder.md)
- [Restore](./restore.md)
- [Route](./route.md)
- [Runs](./runs.md)
- [Runtime](./runtime.md)
- [Runtimes](./runtimes.md)
- [Search](./search.md)
- [Skills](./skills.md)
- [Subscribers](./subscribers.md)
- [Summary](./summary.md)
- [Task-runs](./task-runs.md)
- [Tasks](./tasks.md)
- [Timeline](./timeline.md)
- [Tokens](./tokens.md)
- [Trigger](./trigger.md)
- [Triggers](./triggers.md)
- [Unread-count](./unread-count.md)
- [Unsubscribe](./unsubscribe.md)
- [Update](./update.md)
- [Upload-file](./upload-file.md)
- [Uploads](./uploads.md)
- [Usage](./usage.md)
- [Use-realtime-sync](./use-realtime-sync.md)
- [Workspace-id](./workspace-id.md)
- [Workspaces](./workspaces.md)
- [Ws](./ws.md)
- [Infra](./infra.md)
- [Api](./api.md)
- [Ui](./ui.md)
- [Libraries](./libraries.md)

## Quick Stats

- Routes: **323**
- Models: **36**
- Components: **182**
- Env vars: **53** required, **32** with defaults

## How to Use

- **New session:** read `index.md` (this file) for orientation — WHERE things are
- **Architecture question:** read `overview.md` (~500 tokens)
- **Domain question:** read the relevant article, then **read those source files**
- **Database question:** read `database.md`, then read the actual schema files
- **Library question:** read `libraries.md`, then read the listed source files
- **Before implementing anything:** read the source files listed in the article
- **Full source context:** read `.codesight/CODESIGHT.md`

## What the Wiki Does Not Cover

These exist in your codebase but are **not** reflected in wiki articles:
- Routes registered dynamically at runtime (loops, plugin factories, `app.use(dynamicRouter)`)
- Internal routes from npm packages (e.g. Better Auth's built-in `/api/auth/*` endpoints)
- WebSocket and SSE handlers
- Raw SQL tables not declared through an ORM
- Computed or virtual fields absent from schema declarations
- TypeScript types that are not actual database columns
- Routes marked `[inferred]` were detected via regex and may have lower precision
- gRPC, tRPC, and GraphQL resolvers may be partially captured

When in doubt, search the source. The wiki is a starting point, not a complete inventory.

---
_Last compiled: 2026-04-15 · 86 articles · [codesight](https://github.com/Houseofmvps/codesight)_