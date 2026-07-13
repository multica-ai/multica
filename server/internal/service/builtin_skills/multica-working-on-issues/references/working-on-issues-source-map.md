# working-on-issues source map

Evidence layer for `SKILL.md`. Every contract the skill states is traced to a
current `file:line` here. Lines were re-derived against `feat/builtin-skills`
after the latest `main` merge; the prior skill cited pre-merge lines that have
since moved (see the "drifted" column). Re-confirm with the verification command
at the bottom before relying on an exact line.

## `multica issue pull-requests` — read PR links from Multica

| Behavior | File:line | Drifted from |
|---|---|---|
| CLI command `pull-requests <id>` (alias `prs`) | `server/cmd/multica/cmd_issue.go:105` | `:104` |
| `runIssuePullRequests` handler | `server/cmd/multica/cmd_issue.go:507` | new citation |
| Calls `GET /api/issues/<id>/pull-requests` | `server/cmd/multica/cmd_issue.go:522` | `:522` (unchanged) |
| API route registration | `server/cmd/server/router.go:480` | `:480` (unchanged) |
| Handler `ListPullRequestsForIssue` → `Queries.ListPullRequestsByIssue` | `server/internal/handler/github.go:466,471` | `:466` (unchanged) |
| Row → response mapper `issuePullRequestRowToResponse` | `server/internal/handler/github.go:149` | new citation |

The CLI resolves the issue ref, GETs the endpoint, and (for `--output json`)
prints the raw `{"pull_requests": [...]}` body. Only `--output` is accepted; the
default `table` shows `NUMBER STATE TITLE URL`.

## PR response shape

`GitHubPullRequestResponse` struct: `server/internal/handler/github.go:51`. JSON
fields the agent can read off each element of `pull_requests`:

- `number` (`json:"number"`, line 56)
- `html_url` (`json:"html_url"`, line 59)
- `title` (`json:"title"`, line 57)
- `state` (`json:"state"`, line 58) — the folded lifecycle enum (see below)
- `merged_at` (`json:"merged_at"`, line 63), `closed_at` (line 64)
- `mergeable_state` (`json:"mergeable_state"`, line 70) — mirrors GitHub; UI only
  surfaces `clean`/`dirty`, other values round-trip as unknown
- `checks_conclusion` (`json:"checks_conclusion"`, line 74) — aggregated
  `"passed"`/`"failed"`/`"pending"` or `null` (no observed suite)
- `checks_passed` / `checks_failed` / `checks_pending` (lines 78-80) — per-suite
  counts; `aggregateChecksConclusion` (line 183) folds them into
  `checks_conclusion`

There is **no** standalone `draft` or `merged` boolean in the response. The
PR lifecycle is encoded in the single `state` string by `derivePRState`
(`server/internal/handler/github.go:994`):

```
merged   → if PullRequest.Merged
closed   → else if PullRequest.State == "closed"
draft    → else if PullRequest.Draft
open     → otherwise
```

`derivePRState` is called when the webhook upserts the row
(`server/internal/handler/github.go:682`), so `state` is what the list endpoint
returns. "Is it merged?" = `state == "merged"` (or `merged_at != null`); "is it a
draft?" = `state == "draft"`. Combine with `checks_conclusion` for CI status.

## Two distinct webhook paths: link vs close-intent

Both run inside the `pull_request` webhook handler, gated by the workspace
auto-link flag (`workspaceAutoLinkPRsEnabled`, `github.go:1074`).

### Path 1 — link (title OR body OR branch)

- `extractIdentifiers` regex helper: `server/internal/handler/github.go:1028`
- driving regex `identifierRe` (`\b([a-z][a-z0-9]{1,9})-(\d+)\b`, case-insensitive):
  `server/internal/handler/github.go:490`
- call site: `server/internal/handler/github.go:727` —
  `extractIdentifiers(p.PullRequest.Title, p.PullRequest.Body, p.PullRequest.Head.Ref)`

Every `PREFIX-NUMBER` mention in **title, body, or branch** resolves to an issue
in the workspace and writes a link row (`LinkIssueToPullRequest`, ~`github.go:762`).
This is what `multica issue pull-requests` later reads back.

Drifted from the prior skill's `github.go:727` citation, which pointed at the old
call-site location for the link logic.

### Path 2 — close intent (title OR body only, keyword-adjacent)

- `extractClosingIdentifiers` regex helper: `server/internal/handler/github.go:1051`
- driving regex `closingIdentifierRe`
  (`\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)[:\s]+([a-z][a-z0-9]{1,9})-(\d+)\b`):
  `server/internal/handler/github.go:501`
- call site: `server/internal/handler/github.go:736` —
  `extractClosingIdentifiers(p.PullRequest.Title, p.PullRequest.Body)` (no branch arg)

Only a `PREFIX-NUMBER` immediately after a closing keyword
(`Closes`/`Fixes`/`Resolves`, optional `:` then whitespace) sets the link row's
`close_intent` flag — the gate that auto-advances the issue to `done` on merge.
`Fix MUL-1` closes; `Fix login MUL-1` does not (adjacency). Branch names are
deliberately excluded (function doc, `github.go:1044-1050`): a branch like
`mul-1/fix-login` links but must never declare close intent.

Drifted from the prior skill's `github.go:736` citation.

Net: a bare title prefix (`MUL-2759: ...`) or a branch ref links only;
`Closes MUL-2759` links **and** records close intent.

## Status side effects (enqueue contracts)

| Behavior | File:line | Drifted from |
|---|---|---|
| Create-time: agent-assigned, non-backlog issue enqueues immediately | `server/internal/handler/issue.go:2263-2264` | new citation |
| `shouldEnqueueAgentTask` returns false for `backlog` (parking lot) | `server/internal/handler/issue.go:2644-2648` | new citation |
| Backlog → non-backlog (not done/cancelled) enqueues on update | `server/internal/handler/issue.go:2537-2540` | `:2523` |
| Same contract in batch update | `server/internal/handler/issue.go:3021-3024` | new citation |
| Child → `done` posts a system comment on the parent | `server/internal/handler/issue_child_done.go:51` (`notifyParentOfChildDone`; doc comment at `:15`) | func def `:51` |

Creation with `--status todo` (or any non-backlog status) on an agent-assigned
issue fires the agent immediately; `--status backlog` parks it with the assignee
set but no trigger. Promoting `backlog → todo` later fires it then (update path,
line 2537).

## Metadata CLI

| Behavior | File:line |
|---|---|
| `multica issue metadata set <issue-id> --key --value [--type]` | `server/cmd/multica/cmd_issue_metadata.go:80,109-111` |
| `multica issue metadata delete <issue-id> --key` | `server/cmd/multica/cmd_issue_metadata.go:93,113` |
| API routes (PUT/DELETE `/metadata/{key}`) | `server/cmd/server/router.go:478-479` |

`--value` is JSON-parsed by default (bool/number sniff); `--type` forces
`string`/`number`/`bool`.

## Sessions = threads CLI / MCP (FIR-2021)

A session IS a comment thread (FIR-1874). Resume vs. fresh is decided server-side
by whether the trigger comment has a parent.

| Behavior | File:line |
|---|---|
| Fresh-vs-resume: `forceFreshSession = (parentComment == nil)` | `server/internal/handler/comment.go:1057` |
| Resume lookup keyed by (agent, issue) | `server/internal/handler/daemon.go:1729-1742`; query `server/pkg/db/queries/agent.sql:385` |
| CLI `issue comment resolve / unresolve` | `server/cmd/multica/cerebro_sessions.go` (POST/DELETE `/api/comments/{id}/resolve`) |
| CLI `issue session list / rename / handoff` | `server/cmd/multica/cerebro_sessions.go` |
| MCP `resolve_comment` / `unresolve_comment` / `list_sessions` / `rename_session` / `handoff_session` | `server/internal/cerebro/clitools/mcp_tools.go` |
| Resolve route (upstream) | `server/cmd/server/router.go:1541-1542` (`ResolveComment` / `UnresolveComment`) |
| Sessions routes (list / start-fresh / rename) | `server/cmd/server/router.go:2019,2023,2024` → `server/internal/cerebro/sessions/handler.go` |

`handoff` posts to `.../sessions/start-fresh` (resolves the thread + stores the
brief); `rename` PATCHes `.../sessions/{rootCommentId}` (sessionId path param IS
the thread root comment id). Omitting the brief flags lets the server
auto-summarise (`generateHandoff`).

<!-- CEREBRO-PATCH(handoff-start-new-skill): FIR-2021 — source map for the one-command handoff (--start-new / start_new). -->

One-command handoff (FIR-2021): `--start-new` (CLI) / `start_new: true` (MCP) /
`"start_new": true` (API body) makes `start-fresh` also open a new session and
start a fresh run on it — `Handler.startFreshSession` creates a new top-level
comment (the new session root, authored by the issue assignee) and enqueues a
`force_fresh_session` task via `TaskService.EnqueueTaskForIssueFromComment`. v1
targets the issue's assignee agent. Response adds a `fresh_run` object
(`root_comment_id`, `task_id`, `agent_id`). See
`server/internal/cerebro/sessions/handler.go`.

## Verification command

Re-derive any line above before depending on it:

```bash
cd server
# CEREBRO-PATCH(create-issue-workflow-source-map): FIR-2283 followup — issue create --workflow.
grep -n '"workflow"'                          cmd/multica/cmd_workflow.go cmd/multica/cmd_issue.go
grep -n 'WorkflowID \*string'                 internal/handler/issue.go
grep -n 'ActivateForIssue'                    internal/cerebro/workflows/handler.go internal/handler/issue.go
<!-- CEREBRO-PATCH(cerebro-workflow-agent-guidance): FIR-2937 workflow CLI/MCP source trace. -->
grep -n 'runWorkflow\|workflowCmd'            cmd/multica/cmd_workflow.go
grep -n 'registerWorkflowTools'               internal/cerebro/clitools/mcp_tools_workflow.go internal/cerebro/clitools/mcp_tools.go
grep -n 'pull-requests <id>'                 cmd/multica/cmd_issue.go
grep -n 'ListPullRequestsForIssue'           cmd/server/router.go internal/handler/github.go
grep -n 'func issuePullRequestRowToResponse\|type GitHubPullRequestResponse struct\|func derivePRState\|func extractIdentifiers\|func extractClosingIdentifiers\|closingIdentifierRe' internal/handler/github.go
grep -n 'extractIdentifiers(\|extractClosingIdentifiers(\|derivePRState(' internal/handler/github.go
grep -n 'prevIssue.Status == "backlog"\|func (h \*Handler) shouldEnqueueAgentTask' internal/handler/issue.go
grep -n 'func notifyParentOfChildDone'       internal/handler/issue_child_done.go
```
