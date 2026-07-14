---
name: multica-working-on-issues
description: Use when working on a Multica issue after the runtime has provided the trigger context — to apply the product contracts the runtime brief does not encode: how PR linking differs from close intent, how to read a linked PR's real state via the pull-requests CLI, which metadata keys are high-signal, what status changes trigger on the server, and how sub-issue create status (todo vs backlog) controls whether assigned agents start immediately.
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
This is the link that `multica issue pull-requests` reads back.

```text
MUL-2759: add built-in issue working skill        # title prefix → links
agent/matt/mul-2759-working-on-issues             # branch ref   → links
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
not observe a routable issue key in the PR title/body/branch.

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
- **`cancelled`** stops outstanding work; treat it as a user-driven decision.

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

<!-- CEREBRO-PATCH(create-issue-permission-skill): Document FIR-3266 outcomes in the canonical issue skill. -->
## Create issue permission outcomes

`multica issue create` and the same command with `--parent` use the single
`create_issue` permission. The server may allow the create, deny it with
`platform_action_denied`, or return `platform_action_pending` while an Approval
inbox request is open. Do not bypass a deny or create a second issue on retry;
pending retries must reuse the approval id.

<!-- CEREBRO-PATCH(create-issue-workflow-skill): FIR-2283 followup — document starting a new issue on an Issue workflow from the CLI/API. -->

## Start a new issue on an Issue workflow (`--workflow`)

An **Issue workflow** is a saved recipe that runs a plan → build → delivery-gate
loop on an issue. Attach one at create time so the issue starts on the loop in a
single call — the same thing the Create-issue modal's workflow picker does.

```bash
multica workflow list                       # find the recipe ID (only Issue workflows)
multica issue create --title "..." --workflow <workflow-id>
```

<!-- CEREBRO-PATCH(cerebro-workflow-agent-guidance): FIR-2937 workflow CLI/MCP management surface. -->
Agents can also manage and activate recipes without the web editor:

```bash
multica workflow get <workflow-id>
multica workflow create --file workflow.json
multica workflow update <workflow-id> --file workflow.json
multica workflow toggle <workflow-id> --enabled=false
multica workflow activate <workflow-id> <issue-id>
multica workflow for-issue <issue-id>
multica workflow delete <workflow-id>
```

Use `--stdin` instead of `--file` when piping the complete JSON document. MCP
exposes the same surface as `list_workflows`, `get_workflow`,
`create_workflow`, `update_workflow`, `toggle_workflow`, `activate_workflow`,
`get_active_workflow`, and `delete_workflow`.

- `--workflow` takes an Issue workflow recipe ID (from `multica workflow list`).
  Plain (standard) workflows are rejected.
- Activation is best-effort: the issue is always created. If the workflow could
  not start, the create still succeeds and a warning is printed on stderr; the
  JSON response carries `workflow_activated` / `workflow_activation_error`.
- **API parity:** `POST /api/issues` accepts the same `workflow_id` field, and
  the response echoes `workflow_activated` / `workflow_activation_error`.

<!-- CEREBRO-PATCH(handoff-start-new-skill): FIR-2021 — document the one-command handoff (--start-new / start_new). -->

## Sessions are comment threads (start fresh vs. resume)

A session IS a comment thread. A new top-level comment (no `--parent`) starts a
fresh agent session with no history; a reply (`--parent <root>`) resumes that
session with its full conversation. The thread root's resolved state is the
session's open/closed state.

```bash
# Start fresh / hand off work: post a NEW top-level comment (no --parent).
multica issue comment add <issue> --content-stdin   # ... = fresh session

# Close a session (resolve its thread root); reopen with unresolve.
multica issue comment resolve <root-comment-id>
multica issue comment unresolve <root-comment-id>

# Name a session, list named sessions.
multica issue session rename <issue> <root-comment-id> --name "..."
multica issue session list <issue>

# Hand off: close the thread AND store a carry-over brief. Omit the brief
# flags to auto-summarise the thread.
multica issue session handoff <issue> <root-comment-id> \
  --summary "..." --done "..." --remaining "..."

# One-command handoff: also open a NEW session and start a fresh run on it
# (the issue's assignee agent, no memory of the old thread). No need to post
# the new top-level comment yourself. --prompt sets the new session's opening
# message (default points the fresh run at the brief).
multica issue session handoff <issue> <root-comment-id> \
  --summary "..." --done "..." --remaining "..." --start-new
```

MCP equivalents: `resolve_comment`, `unresolve_comment`, `rename_session`,
`handoff_session` (pass `start_new: true` for the one-command handoff),
`list_sessions`; `add_comment` with no `parent_id` starts a fresh session, a
reply resumes it.

## Incorrect → correct

PR title (link the issue):

```text
Fix login redirect                  # incorrect — no issue key, won't link
MUL-2759: fix login redirect        # correct — links the PR
```

Serial sub-issues (don't start the whole chain):

```bash
# incorrect — both fire immediately
multica issue create --title "Step 2" --parent <issue-id> --assignee <agent> --status todo
multica issue create --title "Step 3" --parent <issue-id> --assignee <agent> --status todo

# correct — parked, promote in turn
multica issue create --title "Step 2" --parent <issue-id> --assignee <agent> --status backlog
multica issue create --title "Step 3" --parent <issue-id> --assignee <agent> --status backlog
```

## References

`references/working-on-issues-source-map.md` — accurate `file:line` for every
contract above: the `pull-requests` CLI and route, the PR response field list,
`derivePRState`, the two-path link (`extractIdentifiers`) vs close-intent
(`extractClosingIdentifiers`) proof, the backlog enqueue lines, child-done
notify, and the metadata CLI. Re-derive before depending on an exact line.
