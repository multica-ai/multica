# PR automation (MUL-7429, proposal B)

Workspace policy has two fields: identifier sources (`manual`, `title_branch`,
`all`) and `auto_complete`. No keyword grants completion eligibility after
migration. An issue completes only with at least one linked PR and every linked
PR merged. GitHub, Forgejo, Gitea and GitLab contribute to the same aggregate.

## Product contract

- Title/branch is the recommended source. `all` also treats ordinary description
  mentions as deliverables; `Closes` has no special meaning. Comments and commits
  are not inputs. Text edits reconcile the entire association set.
- Manual links persist across text edits. Removing a link persists an exclusion;
  restore removes that exclusion. Manual links resolve workspace issue UUIDs and
  require an authorized provider connection. Ambiguous automatic identifiers wait
  for manual resolution, without exposing other workspaces.
- Open, draft and closed-unmerged PRs block completion. Removing an abandoned PR
  can immediately complete the issue. Linking an already merged PR can too.
- Triage and terminal statuses are preserved, including custom terminal statuses.
  Reopening a completed issue sets its visible override to disabled and records
  why. Returning to workspace inheritance recomputes eligibility.
- Connection loss retains associations and blocks completion. Reconnection to the
  same VCS instance reuses the connection identity without retaining credentials.
- Any destination branch counts as merged. CI, deployment and acceptance are not
  additional gates; disable automation for issues that require further acceptance.

## API and CLI

Owners/admins preview and apply workspace policy:

- `GET /api/workspaces/{id}/pr-automation`
- `POST /api/workspaces/{id}/pr-automation/preview`
- `PUT /api/workspaces/{id}/pr-automation` with the preview `token`
- `POST /api/workspaces/{id}/pr-automation/sync` for a bounded metadata refresh

Metadata refresh stores PR observations without changing links or issue statuses,
including during legacy migration previews. Webhooks, policy apply and scheduled
reconciliation evaluate completion separately. Manual URL linking fetches before
locking, then commits metadata and the explicit link in one transaction before
publishing the target issue's completion, if eligible.

Preview/apply bodies contain `source` and `auto_complete`. Apply additionally
requires `token`. A changed evaluated impact or policy revision returns 409 and
requires another preview. Tokens are workspace-scoped. Unrelated issue edits do
not invalidate a preview.

Members can inspect and manage one issue through
`GET/PUT /api/issues/{id}/pr-automation`. A PUT accepts exactly one operation:
`disabled`, or `mode` (`manual`, `excluded`, `automatic`) with `pr_id` / `url`.
The API returns the policy, source of each association, exclusions and decision.

```sh
multica issue pr-automation MUL-123 --output json
multica issue pr-automation MUL-123 --disabled
multica issue pr-automation MUL-123 --disabled=false
multica issue pr-automation MUL-123 --link https://github.com/owner/repo/pull/12
multica issue pr-automation MUL-123 --exclude <pr-uuid>
multica issue pr-automation MUL-123 --restore <pr-uuid>
```

The existing `issue pull-requests` response remains compatible. Workspace policy
is stored separately from the settings JSON, so old clients saving unrelated
settings cannot erase it. The old auto-link switch is hidden after migration.

## Migration and recovery

Migrations 500–510 add policy/evidence/override state and concurrent indexes. No
workspace is opted in by deployment or startup. Deploy compatible webhook and API
workers everywhere before allowing migration. An administrator selects the source
and completion switch, reviews added/removed links and candidate completions, then
saves. They can migrate with completion off first. Existing terminals never reopen.

Preview covers mirrored PRs and existing associations; it does not discover every
historical PR in every repository. Explicit URL linking can fetch a missing PR.
Historical missing descriptions require authorized provider API reads. Until the
selected description source is synchronized, eligible completions wait. Sync uses
GitHub App credentials or the existing encrypted self-hosted provider token. It
never follows redirects with credentials or fetches an arbitrary submitted origin.

Ingestion serializes with policy application through a workspace advisory lock.
Migrated PR metadata and description evidence commit together before reconciliation;
issue row locks serialize completion against ordinary issue writes. Older events
cannot overwrite newer observations. Equal-timestamp conflicts preserve the old
observation and request an authoritative refresh. Ingestion failures return a
non-success webhook response; committed observations are repaired by the scheduler.

Each reconciliation writes at most 100 affected issues. The persisted policy,
canonical associations and issue state are its durable progress: remaining diffs
are recomputed under the latest policy on the next pass. A scheduler job runs every
minute, selects up to 20 workspaces by last check, has a 50-second timeout and bounded
retries, and refreshes at most three PRs per pass. Provider failures are retained
and rotated with a ten-minute retry interval. No startup-wide historical backfill
runs. Plans still read the workspace's mirrored PR inventory; large inventories
should be monitored for preview/reconciliation latency.

Status changes and completion evidence are committed with link writes. Publishing
uses the existing issue and parent notification paths after commit. Duplicate
reconciliation does not create another completion activity or wake for an already
terminal issue. Existing event-bus delivery semantics remain unchanged.

To roll back behavior, keep a compatible server and save the policy with completion
off. Do not run an older server against migrated workspaces, delete policy rows to
reactivate keywords, or automatically reopen completed issues. Inspect completion
activities before an explicitly authorized correction. Schema down migrations are
for controlled development rollback after compatible workers stop.
