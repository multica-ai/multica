---
name: multica-working-on-issues
description: "Use when acting on a Multica issue beyond what the brief covers: PR linking vs close intent, reading a linked PR's real state, metadata keys, status-change side effects, sub-issue todo vs backlog."
user-invocable: false
allowed-tools: Bash(multica *), Bash(git *), Bash(gh *)
---

# Working on Multica issues

Product contracts the runtime brief does not fully encode: PR linking vs close
intent, reading linked-PR state, metadata keys, status side effects, and
sub-issue enqueue behavior.

For building mention links, load `multica-mentioning` instead — not this skill.

Every contract below is traced to source in
`references/working-on-issues-source-map.md`.

## PR linking and close intent are two distinct contracts

The GitHub webhook runs two separate scans over an incoming PR. They are not the
same gate and they read different fields.

**Linking** scans the PR **title, body, OR branch** for a routable issue key
(`PREFIX-NUMBER`, e.g. `MUL-2759`). Each match writes an issue ↔ PR link row.
This is the link that `multica issue pull-requests` reads back — but see the
reference-only rule below: a key that appears **only** as a bare mention in the
body is linked yet hidden from that list.

```text
MUL-2759: add built-in issue working skill        # title prefix → links, shown
agent/matt/mul-2759-working-on-issues             # branch ref   → links, shown
```

**Close intent** is stricter and is a separate scan over **title or body only —
never the branch**. It fires only for a key placed immediately after a closing
keyword (`Closes` / `Fixes` / `Resolves`, optional `:` then whitespace). That
adjacency is what sets the link row's close-intent flag, the gate that
auto-advances the issue to `done` when the PR merges.

```text
Closes MUL-2759                                    # links AND records close intent
Fixes MUL-2759
Resolves MUL-2759
Fix login MUL-2759                                 # links only — keyword not adjacent
```

Consequence: a bare title prefix or a branch reference links the PR but does not
close the issue on merge. A closing keyword immediately adjacent to the issue key
records close intent; on merge, that close intent can move the linked issue to
`done`.

**Reference-only links (hidden from the PR list).** A key that appears **only**
as a bare mention in the body — no closing keyword, and not in the title or
branch — still writes a link row, but the row is flagged `reference_only` and
**excluded from `multica issue pull-requests`** (and the issue's right-side PR
list in the UI). This keeps passing mentions like `Related MUL-2759` or
`Follow up in MUL-2759` from surfacing an unrelated PR as if it were working on
that issue. To make a PR show up for an issue, put the key in the title, the
branch, or after a closing keyword in the body — not as a loose body reference.

```text
Closes MUL-2759 in the body                        # links and shown
Related to MUL-2759 in the body (no title/branch)  # links but reference_only → hidden
```

### Default for code-changing issue work

When an issue run changes code in a checked-out GitHub repo, the default handoff
is to open or update a PR before posting the final Multica issue comment, unless
the user explicitly asked for a local-only change or no PR. This is a default, not
an unconditional command: if no code changed, say no PR is needed; if PR creation
is blocked by auth, failing tests, or missing remote state, report that blocker
instead of pretending the run is complete.

Use a routable issue key in the PR title, body, or branch so the webhook can link
the PR back to the issue. If the PR should close the issue on merge, put the key
immediately after a closing keyword in the title or body, for example:

```text
MUL-2759: fix login redirect        # links only
Closes MUL-2759                     # links and records close intent
```

In the final issue comment, include the PR URL when a PR exists. If the task did
not produce a PR because no code changed or the user asked not to create one, say
that explicitly.

## Reading a linked PR's real state

When a step depends on PR state, query Multica's link table — do not infer it
from branch names, GitHub search, memory, or `pr_url` metadata (which can be
stale).

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

If the command returns no linked PRs after a PR was opened, the link scanner did
not observe a routable issue key in the PR title/body/branch — or the only match
was a bare body mention, which links as `reference_only` and is hidden from this
list (see the reference-only rule above).

## Metadata: durable custom state

Metadata is a free-form KV bag of durable issue state. Reading metadata is safe.
Writing a metadata key is a state mutation and should be tied to an explicit
task requirement to record that state for later readers or runs. Keys are
whatever your workflow needs — the platform curates no vocabulary; pick short
snake_case names and reuse them consistently within your workspace.

Never store secrets, tokens, or API keys in metadata.
Not metadata: logs or summaries; runtime bookkeeping such as timestamps,
attempt counts, or agent IDs; or other single-run details such as
files touched and investigation notes — those belong in the result comment.

```bash
multica issue metadata set <issue-id> --key <key> --value <value>
multica issue metadata delete <issue-id> --key <stale-key>
```

`--value` is JSON-parsed by default (bool/number are sniffed); pass `--type
string|number|bool` to force a type.

## Custom properties: typed workflow state

Workspaces may define custom issue properties (Severity, Environment, QA
Status, Reviewer, ...). Properties are the typed, user-visible sibling of
metadata: values are validated against the definition (select options, date
format, http(s) URL, member reference), visible in the issue sidebar, and
addressed by name.

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
- Property vs metadata: if the value is workflow state a human should see and
  filter by, and a definition exists, prefer the property. Metadata stays the
  free-form bag for durable custom issue state.
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

## Status changes have server side effects

A status change is not cosmetic — the server enqueues or skips agent work based
on it. These are the contracts, not advice:

A workspace may define custom statuses beyond the seven built-ins; when any
exist, the runtime brief's Available Commands section lists this workspace's
catalog. A custom status inherits its category's behavior in full, and each
built-in key below is also the name of its category — so read these bullets as
category rules. Two writes are literal-key exceptions, not category rules: the
failed-task rollback below writes the literal `todo` key, and a merged PR with
close intent writes the literal `done` key.

- **`backlog`** parks an agent-assigned issue: the assignee is set but no task
  fires. Moving `backlog → todo` (or any non-done/non-cancelled status) enqueues
  the assigned agent then.
- **`in_progress` / `in_review`** are agent-managed CLI mutations, not
  `StartTask` / `CompleteTask` side effects. The runtime brief asks agents to
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
- **`done`** on a child issue posts a system comment on its parent. If a PR
  carries close intent (`Closes MUL-XXXX`), it advances the issue to `done`
  itself on merge — you do not also need to flip it manually.
- **`cancelled`** is a terminal, user-driven decision to close the issue. Like
  `done` it enqueues no new agent work, but it does **not** stop tasks already in
  flight — a run in progress keeps going (MUL-4465). To stop a running task,
  cancel the task itself.
- **Failed issue-triggered tasks** may roll an issue from `in_progress` back to
  `todo` when no active task / retry remains — that is the main server-owned
  status write on the agent-run path.

## Claim ownership without duplicating a run

Assigning an active issue to an agent normally starts a run. When the work is
already underway and the write only records ownership or progress, pass
`--no-start` on every command in that flow — suppressing the assignment alone
does not suppress a later status update:

```bash
multica issue assign <issue-id> --to-id <agent-id> --no-start
multica issue update <issue-id> --assignee-id <agent-id> --no-start
multica issue status <issue-id> in_progress --no-start
```

Before self-assigning, check the target issue's comment history for an existing
claim. The server also suppresses a trusted self-assignment when the
exact target `(issue, agent)` pair already has a non-terminal task, but it
deliberately keeps same-agent handoffs to a fresh issue starting runs: cross-issue
serial chains and triage batches rely on that.

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

## Sub-issues: `todo` starts work now, `backlog` parks it

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

`--stage <N>` (N ≥ 1) groups sub-issues under the same parent into ordered
stages. The parent assignee is woken **once, when a whole stage finishes** —
i.e. every sub-issue in the lowest unfinished stage has reached a terminal
status (`done`/`cancelled`). A completion that does not close a stage is silent
(no comment, no wake). A sibling set with **no** stages is one implicit stage,
so the parent is woken once when the *last* sub-issue finishes — not on every
child.

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

`issue children --output json` reports per-stage `done` counts. A workspace may
define custom statuses beyond the 7 built-ins; a custom status counts as done
here when its category is `done` or `cancelled`, which is what `status_category`
on each child carries. Read `status_category` rather than matching `status`
against the built-in names.

Read each sub-issue's description before promoting and only promote items whose
stated dependencies are met; if a description conflicts with the parent's
breakdown, leave it `backlog` and comment to confirm first.

## Incorrect → correct

PR title (link the issue):

```text
Fix login redirect                  # incorrect — no issue key, won't link
MUL-2759: fix login redirect        # correct — links the PR
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

## References

`references/working-on-issues-source-map.md` — accurate `file:line` for every
contract above: the `pull-requests` CLI and route, the PR response field list,
`derivePRState`, the two-path link (`extractIdentifiers`) vs close-intent
(`extractClosingIdentifiers`) proof, the backlog enqueue lines, child-done
notify, the stage column / `stageBarrierClosed` barrier and the `--stage` /
`issue children` CLI, and the metadata CLI. Re-derive before depending on an
exact line.
