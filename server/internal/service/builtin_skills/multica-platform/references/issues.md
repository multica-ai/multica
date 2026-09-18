# Issues

Product contracts the runtime brief does not fully encode.

- [PR linking and automatic completion follow workspace policy](#pr-linking-and-automatic-completion-follow-workspace-policy)
- [Reading a linked PR's real state](#reading-a-linked-prs-real-state)
- [Editing comments without overwriting concurrent work](#editing-comments-without-overwriting-concurrent-work)
- [Custom properties: typed workflow state](#custom-properties-typed-workflow-state)
- [Status changes have server side effects](#status-changes-have-server-side-effects)
- [Claim ownership without duplicating a run](#claim-ownership-without-duplicating-a-run)
- [Who else is running right now](#who-else-is-running-right-now)
- [Sub-issues: todo starts work now, backlog parks it](#sub-issues-todo-starts-work-now-backlog-parks-it)
- [Incorrect to correct](#incorrect-to-correct)

## Editing comments without overwriting concurrent work

Read the comment's current `revision`, then supply that positive value when
updating its body. Agent-authored bodies must use `--content-file`.

```bash
multica issue comment list <issue-id> --output json
multica issue comment update <comment-id> --content-file ./comment.md --expected-revision <revision>
```

If another editor changed the comment, the server rejects the stale revision.
Read the latest body and reconcile the edits before retrying; do not simply
advance the revision and overwrite the other edit. Authors can edit their own
comments; workspace owners and admins can edit any comment. Existing attachments
remain unchanged. Content edits have the same agent-trigger behavior as edits
in the app, so do not use an update as a silent bookkeeping operation.

## PR linking and automatic completion follow workspace policy

Before relying on a PR to complete an issue, inspect the effective policy:

```bash
multica issue pr-automation <issue-id> --output json
```

In migrated workspaces, settings select identifier sources: `manual` (explicit
links only), `title_branch`, or `all` (also ordinary description mentions).
`Closes` / `Fixes` / `Resolves` have no special completion meaning. Use a routable
issue key (`PREFIX-NUMBER`, such as `MUL-123`) in a selected source, or explicitly
link the PR:

```bash
multica issue pr-automation MUL-123 --link https://github.com/owner/repo/pull/12
```

With workspace automatic completion enabled, an issue completes only when at
least one PR is linked and **all linked PRs are merged**, across providers.
Open, draft and closed-unmerged PRs block completion. Triage and terminal issues
are preserved; disconnected providers or incomplete metadata can block progress.
The command reports the decision, associations and exclusions; inspect those
rather than assuming that a keyword or a merged PR is sufficient.

Manual links survive text edits. Removing a link persists an exclusion until
restored. Reopening a completed issue disables automatic completion for that
issue; restore workspace inheritance explicitly only when intended. Linking an
already merged PR or removing the last unmerged blocker can complete the issue
immediately. For work requiring acceptance after merge, disable automatic
completion for that issue before relying on PR links.

**Unmigrated workspaces retain the legacy contract.** Title/branch keys and body
closing keywords link; ordinary body mentions do not. Merge-time close intent
from `Closes` / `Fixes` / `Resolves` is required for automatic completion, subject
to the existing integration and issue gates. Late links do not grant retroactive
close intent. If the command is unsupported by the client/server, use
`multica issue pull-requests <issue-id>` and verify settings; do not assume the
new policy is active.

### Default for code-changing issue work

When an issue run changes code in a checked-out GitHub repo, the default handoff
is to open or update a PR before posting the final Multica issue comment, unless
the user explicitly asked for a local-only change or no PR. This is a default, not
an unconditional command: if no code changed, say no PR is needed; if PR creation
is blocked by auth, failing tests, or missing remote state, report that blocker
instead of pretending the run is complete.

Put a routable issue key in the PR **title** (preferred) or **branch**, then
verify the saved policy and actual association. With source `manual`, explicitly
link the URL. A description-only key links only when the selected source permits
it; adding a closing keyword does not override migrated workspace settings.

In the final issue comment, include the PR URL when a PR exists. If the task did
not produce a PR because no code changed or the user asked not to create one, say
that explicitly.

## Reading a linked PR's real state

When a step depends on PR state, query Multica's link table — do not infer it
from branch names, GitHub search, memory, or stale values left on the issue by
an earlier run.

```bash
multica issue pull-requests <issue-id> --output json
```

Returns `{"pull_requests": [...]}`. Each element exposes:

- `number`, `html_url`, `title`
- `state` — the PR lifecycle as a **single enum**, one of `merged`, `closed`,
  `draft`, `open`. There is no separate `draft` or `merged` boolean in the
  response; the server folds them into `state` (merged wins, then closed, then
  draft, else open).
- `merged_at` — non-null once merged; a second confirmation of `state: merged`.
- `provider` — `github`, `forgejo`, `gitea`, or `gitlab`.
- `mergeable_state` — mirrors GitHub (`clean` / `dirty` surfaced; other values
  round-trip as unknown; retained for compatibility).
- GitHub API snapshot fields: `snapshot_available`, `mergeable`,
  `merge_state_status`, `checks_rollup`, `checks_total`, `checks_passed`,
  `checks_failed`, `checks_running`, `failed_check_names`,
  `snapshot_fetched_at`, and `snapshot_stale`. `snapshot_available == true`
  means the feature is enabled and the snapshot matches the PR's current head.
  Only then does `checks_rollup == null` mean "no checks"; false means the
  snapshot feature is disabled, has not fetched yet, or only has an old head.
- `checks_conclusion` — coarse CI compatibility status: `passed`, `failed`,
  `pending`, or `null`. GitHub derives it from the current API snapshot;
  Forgejo/Gitea/GitLab derive it from webhook commit statuses. Backed by the
  provider-appropriate check counts.

So "is it merged?" is `state == "merged"` (or `merged_at != null`); "is it still
a draft?" is `state == "draft"`; coarse CI status is `checks_conclusion`.

If the command returns no linked PRs, inspect `issue pr-automation` first:
check the selected identifier sources, manual exclusions and ambiguous matches.
Correct a title/branch key if that source is enabled, or explicitly link the URL.
For legacy workspaces, title/branch keys or body closing keywords link; ordinary
body mentions do not. Do not add keywords blindly to force completion.

If the key is already written correctly and the list is still empty, stop editing
the PR blind: another no-op edit cannot fix an integration that never received the
event. Check the integration side instead — whether the app is installed on that
repository, whether the installation is bound to this workspace, whether
auto-linking is turned off for the workspace, and whether the event reached the
platform at all. A delivery that failed is not retried on its own, but it can be
redelivered once the receiving side is fixed. Report what you found in the result
comment rather than repeating the edit.

## Listing and ordering issues

`issue list` reads one page at a time, with a server maximum of 100 issues.
Advance `--offset` by the number of issues actually returned. If the server
cannot count matching issues, it returns `failed to count issues` as an error;
do not treat that failure as an empty or complete list. Older servers can
substitute the page length for a failed count, so that value alone is not proof
that all matching issues have been read.

`issue reorder` reads the issue's project-scoped status column before writing
its new position. When a legacy total is unavailable or no larger than its
page, it reads through an empty page. A failed request, malformed page, or
duplicate issue stops the operation before any position write. This protects
against truncated or repeated pages, but does not promise a snapshot across
concurrent edits. There is no CLI bulk-export or `--all` mode.

## Custom properties: typed workflow state

Workspaces may define custom issue properties (Severity, Environment, QA
Status, Reviewer, ...). They are the place for durable, typed issue state:
values are validated against the definition (select options, date format,
http(s) URL, member reference), visible in the issue sidebar, and addressed
by name.

- Read what exists before writing: `multica property list` shows the catalog;
  `multica issue property list <issue-id>` shows values set on the issue.
- Set values by property name and option name — the CLI translates to ids:

```bash
multica issue property set <issue-id> --name Environment --value staging
multica issue property set <issue-id> --name Platforms --value "iOS,Android"
multica issue property set <issue-id> --name Reviewer --value Bohan
multica issue property unset <issue-id> --name Environment
```

- A validation error lists the legal options — fix the value and retry.
- `actor` / `multi_actor` properties (Reviewer, Escalation contact, ...) hold
  workspace members only. `--value` takes a member name, email, UUID, short id,
  or an explicit `member:<uuid>`; `multi_actor` takes a comma-separated list
  (duplicates dropped, order kept, max 20).
- Definitions may include an optional catalog icon for visual identification;
  it does not change the property's type or value validation.
- Agents cannot create or edit property definitions (owner/admin humans only).
  If a needed property does not exist, propose it in a comment instead.
- Where state belongs: workflow state a human should see and filter by goes in
  a property; the stage the issue is at goes in its status; everything else —
  what you did this run, what you found — goes in the result comment.
- `issue list` filters and sorts by property with the same name addressing:

```bash
multica issue list --property "Impact=High" --property "Impact=Medium" --output json
multica issue list --property "QA Status=__none__" --status in_review --output json
multica issue list --sort property:Impact --direction desc --output json
```

- `--property` takes one `Name=Value` per flag. Repeating the same property
  matches ANY of its values; different properties must ALL match. Values are
  option names or ids (select types), `true`/`false` (checkbox), a member
  name/email/id (actor types), or the value itself for text, url, number,
  and date (`YYYY-MM-DD`). The reserved value `__none__` matches
  issues where the property is unset (works for every type; it is not
  index-backed, so use it for targeted audits rather than as a default
  listing filter). Only `=` is supported today; the `>=`, `<=` and `!=`
  spellings are reserved for comparison filters and are rejected.
- `--sort property:<name-or-id>` orders select properties by option order —
  an ordinal scale (Low < Medium < High) sorts by meaning — and number/date/
  text/url by value; issues without the property sort last either way.
  Archived properties and types without an order (multi_select, checkbox,
  actor kinds) are rejected up front.
- `issue list` and `issue get` return `properties` as a map of definition id
  to stored value. Add `--resolve-properties` in JSON mode to get the rows
  `issue property list` prints instead (name, type, stored value, display
  names); the CLI makes at most one catalog request for the whole page, so
  no `property list` call is needed:

```bash
multica issue list --status in_progress --output json --resolve-properties
multica issue get <issue-id> --resolve-properties
```

  Read `display` for a single value and `display_values` for a multi_select
  or multi_actor value; `value` keeps the stored ids.

## Status changes have server side effects

A status change is not cosmetic — the server enqueues or skips agent work based
on it. These are the contracts, not advice.

The rules below name fixed built-in status keys, not category-wide behaviors.
Custom statuses have only lifecycle semantics: unstarted, started, done
(successful terminal), or closed (cancelled terminal). They do not inherit
Backlog parking, In Review completion, Blocked failure, or In Progress recovery.
Use the built-in key when its special behavior is needed. Built-in definitions
cannot be edited or archived.

Archive a custom status only after moving every issue off it, including
completed/canceled issues. An occupied status returns HTTP 409 with code
`issue_status_in_use` and `issue_count`; it remains active. Use Settings >
View issues to inspect and move its issues, then retry. For terminal-status
replacement, preserve the lifecycle meaning (`done` to `done`, `closed` to
`closed`); do not reopen or cancel completed work just to retire a status.
Archival does not move issues automatically. Historical issues on previously
archived statuses remain readable via an explicit status filter.

- **`backlog`** parks an agent-assigned issue: the assignee is set but no task
  fires. Moving `backlog → todo` (or any non-done/non-cancelled status) enqueues
  the assigned agent then.
- **`in_progress` / `in_review`** are agent-managed CLI mutations, not automatic
  side effects of a task starting or finishing. The runtime brief asks agents to
  write the state the issue is in whenever their work changes it — not from
  the trigger type or the run's lifecycle, and not gated on being the
  assignee. Writes happen whenever the state changes, mid-turn included: a
  turn that advances the issue's own ask sets `in_progress` as soon as that
  is known, so the board shows the work while it runs; a blocker is recorded
  when it is hit; and the turn must not exit with a stale value — delivered
  the issue's own ask → `in_review`; work continues beyond the turn
  (dispatched sub-issues, partial delivery) → `in_progress`; stuck →
  `blocked`. A turn that produces none of the issue's own deliverable —
  answering a question, consulting on work owned elsewhere — writes nothing
  at any point. The kind of activity never decides this: research, design,
  planning, and review all count as the work exactly when they are what the
  issue asks for (a review-the-PR issue is being worked the moment reviewing
  starts). Questions, discussion, or acknowledgements never move the status.
  Squad leaders: dispatching members is not delivery — a dispatch turn
  leaves the parent `in_progress`, and it moves to `in_review` only when a
  later re-trigger confirms the overall goal is met.
- **`in_review`** is an accepted issue status. Some workflows use it while a PR
  is open and awaiting review; moving to it is an explicit mutation.
- **`done`** on a child issue posts a system comment on its parent. PR automation
  can advance it to `done` when the effective workspace/issue policy allows it;
  inspect `issue pr-automation` rather than assuming a closing keyword suffices.
  Do not also flip the status manually when automation already completed it.
- **`cancelled`** is a terminal, user-driven decision to close the issue. Like
  `done` it enqueues no new agent work, but it does **not** stop tasks already in
  flight — a run in progress keeps going. To stop a running task, cancel the
  task itself.
- **Failed issue-triggered tasks** may roll an issue from `in_progress` back to
  `todo` when no active task / retry remains — that is the main server-owned
  status write on the agent-run path.

## Claim ownership without duplicating a run

Assigning an active issue to an agent normally starts a run. When the work is
already underway and the write only records ownership or progress, pass
`--no-start` on every command in that flow:

```bash
multica issue assign <issue-id> --to-id <agent-id> --no-start
multica issue update <issue-id> --assignee-id <agent-id> --no-start
multica issue status <issue-id> in_progress --no-start
```

Before self-assigning, check the target issue's comment history for an existing
claim. The server also suppresses a trusted self-assignment when the exact
target `(issue, agent)` pair already has a non-terminal task, but it
deliberately keeps same-agent handoffs to a fresh issue starting runs:
cross-issue serial chains and triage batches rely on that.

## Who else is running right now

Nothing about concurrent runs is pushed into your prompt: the answer changes
while a turn is running, and most turns never need it. Ask the server on the
turns that do — before opening a PR against code a sibling issue also touches:

```bash
multica issue runs <issue-id> --active --output json     # in-flight runs on this issue
multica issue runs <issue-id> --siblings --output json   # ...and across the sub-issue family
```

`--active` drops the execution history and returns only `queued` / `dispatched`
/ `running` / `waiting_local_directory` runs. `--siblings` widens the same read
to the issue's family — its parent (or itself, when it has no parent) plus every
child of that parent — and labels each row with the issue it belongs to, which
is how you find another agent already working on a sibling sub-issue before you
open a second PR against the same code.

The family read returns a compact row — task, issue, agent, status, started —
not the full execution-log record. If you need a run's detail, follow the task
id with `multica issue run-messages`.

Rows come back running-first, newest-first within a status, and the family read
is capped at 20. When the cap truncates the answer the CLI prints a warning on
stderr — read it. Without that warning a short list means "nobody else is
there"; with it, the list proves nothing about the runs it did not return.

Both are advisory reads. Nothing here reserves an issue or serialises anything:
a run you see may finish a second later, and one you don't see may start a
second later. Coordinate through the issue's comments — the reads tell you whom
to coordinate with.

## Sub-issues: todo starts work now, backlog parks it

On an agent-assigned issue, create status decides whether the assignee fires
immediately. A non-backlog status (e.g. `todo`) enqueues the agent at create
time; `backlog` sets the assignee without triggering.

Parallel children — all start now:

```bash
multica issue create --title "..." --parent <issue-id> --assignee <agent> --status todo
```

Strictly serial children — park later steps, promote one at a time:

```bash
multica issue create --title "Step 2: ..." --parent <issue-id> --assignee <agent> --status backlog
multica issue status <child-id> todo   # promote when the previous step is truly done
```

Creating every serial step as `todo` enqueues the whole chain at once.

### Stages: order sub-issues into barrier groups

`--stage <N>` (N >= 1) groups sub-issues under the same parent into ordered
stages. The server **tries once to wake the parent assignee when a whole stage
finishes** — i.e. every sub-issue in the lowest unfinished stage has reached a
terminal status (`done`/`cancelled`); a notification that fails is not replayed.
A completion that does not close a stage is silent (no comment, no wake). A
sibling set with **no** stages is one implicit stage, so the parent is woken
once when the *last* sub-issue finishes — not on every child.

Advancement is agent-driven: the server only detects the closed barrier and
wakes the parent assignee, who then decides whether to promote the next stage's
`backlog` sub-issues to `todo`.

```bash
# Stage 1 runs now; later stages parked until promoted
multica issue create --title "Research A" --parent <id> --assignee <agent> --stage 1 --status todo
multica issue create --title "Research B" --parent <id> --assignee <agent> --stage 1 --status todo
multica issue create --title "Build"      --parent <id> --assignee <agent> --stage 2 --status backlog
multica issue create --title "Ship"       --parent <id> --assignee <agent> --stage 3 --status backlog
```

When both Stage 1 sub-issues finish you (the parent assignee) are woken with a
"Stage 1 complete" comment. Inspect the layout, then promote the next stage:

```bash
multica issue children <parent-id>             # sub-issues grouped by stage
multica issue status <stage-2-child-id> todo   # promote when its deps are met
```

`issue children --output json` reports per-stage `done` counts, including custom
statuses in terminal categories. When reading issue JSON, `status` is the exact
key; `status_category` retains the seven-value API enum for installed clients:
`backlog` / `todo` mean unstarted, `in_progress` / `in_review` / `blocked` mean
started, `done` means successful terminal, and `cancelled` means cancelled
terminal (the internal closed category). These values encode lifecycle, not
built-in automation behavior. Check `status_category` for `done` / `cancelled`
(or use the stage counts), not just the concrete `status` key, to recognize
terminal children.

Read each sub-issue's description before promoting and only promote items whose
stated dependencies are met; if a description conflicts with the parent's
breakdown, leave it `backlog` and comment to confirm first.

## Incorrect to correct

PR text with the `title_branch` source selected:

```text
Fix login redirect                  # incorrect — no issue key, won't link
Body-only "Part of MUL-123"         # won't link with this source; links with all
MUL-123: fix login redirect        # links when title matching is enabled
```

Serial / phased sub-issues (don't start the whole chain at once):

```bash
# incorrect — all fire immediately, no ordering
multica issue create --title "Step 2" --parent <issue-id> --assignee <agent> --status todo
multica issue create --title "Step 3" --parent <issue-id> --assignee <agent> --status todo

# correct — stage them; Stage 1 runs, later stages park and are promoted as
# each stage's barrier closes
multica issue create --title "Step 1" --parent <issue-id> --assignee <agent> --stage 1 --status todo
multica issue create --title "Step 2" --parent <issue-id> --assignee <agent> --stage 2 --status backlog
multica issue create --title "Step 3" --parent <issue-id> --assignee <agent> --stage 3 --status backlog
```
