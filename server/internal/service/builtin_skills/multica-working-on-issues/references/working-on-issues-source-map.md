# working-on-issues source map

Evidence layer for `SKILL.md`; every contract traces to `file:line`,
re-derived against `feat/builtin-skills`' latest `main` merge (prior
citations drifted). Re-confirm exact lines.

## `multica issue pull-requests`

| Behavior | File:line | Drifted from |
|---|---|---|
| CLI `pull-requests <id>` (alias `prs`) | `server/cmd/multica/cmd_issue.go:105` |
| `runIssuePullRequests` handler | `cmd_issue.go:507`; GET `/api/issues/<id>/pull-requests` `:522`; route | `router.go:480` |
| `ListPullRequestsForIssue` → `Queries.ListPullRequestsByIssue`; mapper `issuePullRequestRowToResponse` | `github.go:205` |

CLI resolves the issue ref, GETs the endpoint; `--output json` prints the
raw `{"pull_requests": [...]}` body; `table` shows `NUMBER STATE TITLE URL`.

## PR response shape

`GitHubPullRequestResponse` struct (`github.go:58`). Fields off each
`pull_requests` element: `provider` (63),
`number` (67), `html_url` (70), `title`, `state` (`derivePRState`),
`merged_at`, `mergeable_state`, snapshot fields (`snapshot_available` 100,
`mergeable` 90, `merge_state_status` 94, `checks_rollup` 105,
`checks_total`/`checks_passed`/`checks_failed`/`checks_running` 111-114,
`failed_check_names` 118), `checks_conclusion` (108) — coarse
`"passed"`/`"failed"`/`"pending"` or `null`; GitHub derives it only from an
available current-head snapshot (242-254), self-hosted VCS via
`aggregateChecksConclusion` (275).

No standalone `draft`/`merged` boolean — lifecycle is the single `state`
from `derivePRState` (`github.go:1317`):

```text
merged → if PullRequest.Merged
closed → else if PullRequest.State == "closed"
draft  → else if PullRequest.Draft
open   → otherwise
```

`derivePRState` runs on webhook upsert (`1115`), so `state` is what the
list returns. "Merged?" = `state == "merged"` (`merged_at != null`);
"draft?" = `state == "draft"`; CI = `checks_conclusion`.

## Two webhook paths: link vs close-intent

Both run in the `pull_request` webhook in `server/internal/handler/github.go`,
gated by `workspaceAutoLinkPRsEnabled` (`github.go:1074`).

### Path 1 — link (title OR body OR branch)

- `extractIdentifiers` regex helper: `github.go:1028`
- driving regex `identifierRe`
  (`\b([a-z][a-z0-9]{1,9})-(\d+)\b`, case-insensitive): `github.go:490`
- call site: `github.go:727` — `extractIdentifiers(Title, Body, Head.Ref)`
- each `PREFIX-NUMBER` match → `LinkIssueToPullRequest` (~`github.go:762`)

**Reference-only (MUL-3739).**
`127_issue_pull_request_reference_only.up.sql`; the handler
computes `qualifyingIdents` (title/branch matches ∪ body `closingIdents`);
a link from a bare body mention not in that set is flagged
`reference_only = true`. `ListPullRequestsByIssue` and
`GetIssuePullRequestCloseAggregate` filter `AND NOT reference_only` — hidden
from CLI/UI list AND excluded from the auto-advance gate (must not silently
block `done` while invisible). Row still exists for edit-time tracking.

### Path 2 — close intent (title OR body only, keyword-adjacent)

- `extractClosingIdentifiers` regex helper: `github.go:1051`
- driving regex `closingIdentifierRe`
  (`\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)[:\s]+([a-z][a-z0-9]{1,9})-(\d+)\b`):
  `github.go:501`
- call site: `github.go:736` — `extractClosingIdentifiers(Title, Body)`
  (no branch arg)

Only a `PREFIX-NUMBER` immediately after a closing keyword
(`Closes`/`Fixes`/`Resolves`, optional `:` + whitespace) sets `close_intent`
— the gate auto-advancing the issue to `done` on merge. `Fix MUL-1` closes;
`Fix login MUL-1` does not (adjacency). Branches deliberately excluded
(doc, `github.go:1044-1050`): `mul-1/fix-login` links, never closes.

## Status side effects

- Create-time: non-backlog (`--status todo`) fires immediately; `backlog`
  parks; promoting `backlog → todo` later fires it.
- `cancelled` USED to call `CancelTasksForIssue` (old #940); MUL-4465 removed
  it from `UpdateIssue` and `BatchUpdateIssues` — no status flip ever cancels
  tasks now. `CancelTasksForIssue` fires only on deletion paths
  (`DeleteIssue`/`BatchDeleteIssues`), where the owning row goes away.
- `StartTask`/`CompleteTask` never write status; brief: `in_progress` then
  `in_review`; squad leader `in_review` only on confirmed re-trigger.
- Failed task may roll `in_progress` → `todo` when no active task remains.
- Child `done` notifies and wakes the parent, gated by the stage barrier.

## Sub-issue stages (barrier wake)

| Behavior | File:line |
|---|---|
| `issue.stage` column (nullable, `>= 1`) | `123_issue_stage.up.sql` |
| Barrier: wake fires when lowest unfinished stage is all-terminal; un-staged = one implicit stage | `server/internal/handler/issue_child_done.go` |
| `--stage` on `issue create`/`update` | `cmd_issue.go:328,350` |

Advancement is agent-driven: the parent assignee promotes the next stage's
`backlog` sub-issues and, when wrapping up, runs
`multica issue status <parent-id> in_review` — comment-triggered runs must
not change status unless asked.

## Metadata CLI

| Behavior | File:line |
|---|---|
| `multica issue metadata set <issue-id> --key --value [--type]` / `delete <issue-id> --key` | `server/cmd/multica/cmd_issue_metadata.go` |
| Routes (PUT/DELETE `/metadata/{key}`) | `server/cmd/server/router.go:478-479` |

`--value` is JSON-parsed by default (bool/number sniff); `--type` forces
`string`/`number`/`bool`.

## Custom properties CLI

| Behavior | File:line |
|---|---|
| `multica property list/get/create/update/archive/unarchive`; `issue property list/set/unset` (name→id) | `server/cmd/multica/cmd_property.go` |
| Definition CRUD, admin gate, agent-actor rejection, catalog icon, per-type validation | `server/internal/handler/property.go` |
| Routes (`/api/properties`, PUT/DELETE `/api/issues/{id}/properties/{propertyId}`) | `server/cmd/server/router.go` |

## Verification

Re-derive before depending on a line:

```bash
cd server
grep -n 'pull-requests <id>' cmd/multica/cmd_issue.go
grep -n 'ListPullRequestsForIssue' cmd/server/router.go internal/handler/github.go
grep -n 'func issuePullRequestRowToResponse\|type GitHubPullRequestResponse struct\|func derivePRState\|func extractIdentifiers\|func extractClosingIdentifiers\|closingIdentifierRe' internal/handler/github.go
grep -n 'extractIdentifiers(\|extractClosingIdentifiers(\|derivePRState(' internal/handler/github.go
grep -n 'qualifyingIdents\|reference_only\|ReferenceOnly' internal/handler/github.go pkg/db/queries/github.sql
grep -n 'issue.stage\|stageBarrier' internal/handler/issue.go internal/handler/issue_child_done.go
```
