---
name: multica-working-on-issues
description: "Use when working on a Multica issue after the runtime has provided the trigger context — to apply the product contracts the runtime brief does not encode: how PR linking differs from close intent, how to read a linked PR's real state via the pull-requests CLI, which metadata keys are high-signal, what status changes trigger on the server, and how sub-issue create status (todo vs backlog) controls whether assigned agents start immediately."
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
- `mergeable_state` — mirrors GitHub (`clean` / `dirty` surfaced; other values
  round-trip as unknown).
- `checks_conclusion` — aggregated CI: `passed`, `failed`, `pending`, or `null`
  when no check suite has been observed. Backed by `checks_passed`,
  `checks_failed`, `checks_pending` counts.

So "is it merged?" is `state == "merged"` (or `merged_at != null`); "is it still
a draft?" is `state == "draft"`; CI status is `checks_conclusion`.

If the command returns no linked PRs after a PR was opened, the link scanner did
not observe a routable issue key in the PR title/body/branch — or the only match
was a bare body mention, which links as `reference_only` and is hidden from this
list (see the reference-only rule above).

## Metadata: high-signal keys only

Metadata is durable issue state. Reading metadata is safe. Writing a metadata key
is a state mutation and should be tied to an explicit task requirement to record
that state for later readers or runs.

High-signal keys (reuse these names so queries stay consistent):

- `pr_url`
- `pr_number`
- `pipeline_status`
- `deploy_url`
- `external_issue_url`
- `waiting_on`
- `blocked_reason`
- `decision`

Not metadata: logs, summaries, files touched, timestamps, attempt counts,
investigation notes. Those belong in the result comment.

```bash
multica issue metadata set <issue-id> --key pr_url --value <url>
multica issue metadata delete <issue-id> --key <stale-key>
```

`--value` is JSON-parsed by default (bool/number are sniffed); pass `--type
string|number|bool` to force a type.

## Status changes have server side effects

A status change is not cosmetic — the server enqueues or skips agent work based
on it. These are the contracts, not advice:

- **`backlog`** parks an agent-assigned issue: the assignee is set but no task
  fires. Moving `backlog → todo` (or any non-done/non-cancelled status) enqueues
  the assigned agent then.
- **`in_review`** is an accepted issue status. Some workflows use it while a PR
  is open and awaiting review; moving to it is an explicit mutation.
- **`done`** on a child issue posts a system comment on its parent. If a PR
  carries close intent (`Closes MUL-XXXX`), it advances the issue to `done`
  itself on merge — you do not also need to flip it manually.
- **`cancelled`** is a terminal, user-driven decision to close the issue. Like
  `done` it enqueues no new agent work, but it does **not** stop tasks already in
  flight — a run in progress keeps going (MUL-4465). To stop a running task,
  cancel the task itself.

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
