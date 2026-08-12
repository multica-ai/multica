---
name: multica-working-on-issues
description: "Use when acting on a Multica issue beyond what the brief covers: PR linking vs close intent, reading linked-PR state, metadata keys, status side effects, and sub-issue enqueue behavior."
user-invocable: false
allowed-tools: Bash(multica *), Bash(git *), Bash(gh *)
---

# Working on Multica issues

Contracts the brief does not fully encode: PR linking vs close intent,
linked-PR state, metadata, status side effects, sub-issue enqueue. Mention links → `multica-mentioning`; `references/working-on-issues-source-map.md`.

## PR linking vs close intent

Two separate scans over an incoming PR, different fields.

**Linking** scans the PR **title, body, or branch** for a routable key
(`PREFIX-NUMBER`, e.g. `MUL-2759`), writing an issue↔PR link row.

```text
MUL-2759: add built-in issue working skill        # title prefix → links, shown
agent/matt/mul-2759-working-on-issues             # branch ref   → links, shown
```

**Close intent** = separate scan over **title or body only**; fires for a
key after `Closes` / `Fixes` / `Resolves` (optional `:` + whitespace) —
the gate auto-advancing the issue to `done` on merge.

```text
Closes MUL-2759      # links AND records close intent
Fixes MUL-2759
Resolves MUL-2759
Fix login MUL-2759   # links only — keyword not adjacent
```

**Reference-only (hidden from the PR list):** a key **only** as a bare body
mention — no closing keyword, not in title/branch — is flagged
`reference_only` and **excluded from `multica issue pull-requests`** and the
UI list: passing mentions never surface an unrelated PR.

### Default for code-changing issue work

Code-changing runs: open or update a PR before posting the final Multica issue comment. This is a default, not a hard rule — local-only fine
when asked. No code changed → say no PR needed; blocked → report the blocker. Use a routable issue key in the PR title, body, or branch; put it
after a closing keyword when it should close on merge. include the PR URL when a PR exists (or say why not).

## Reading a linked PR's state

Query Multica's link table — never infer from branches, search, memory, or
`pr_url` (stale):

```bash
multica issue pull-requests <issue-id> --output json
```

Returns `{"pull_requests": [...]}`; each element: `number`, `html_url`,
`title`; `state` — enum `merged`|`closed`|`draft`|`open` (merged wins,
then closed, then draft); `merged_at` (confirms merge); `provider`
(`github`, `forgejo`, `gitea`, `gitlab`); `mergeable_state`; snapshot
fields: `snapshot_available`, `mergeable`, `merge_state_status`,
`checks_rollup`, `checks_total`, `checks_passed`, `checks_failed`,
`checks_running`, `failed_check_names`, `snapshot_fetched_at`,
`snapshot_stale` (`snapshot_available == true` = enabled + current head;
only then `checks_rollup == null` = "no checks"); `checks_conclusion` —
`passed`|`failed`|`pending`|`null` (GitHub: snapshot; others: webhook
statuses).

"Merged?" → `state == "merged"` (`merged_at != null`); "draft?" → `state
== "draft"`; CI → `checks_conclusion`. Empty result = no routable key or a `reference_only` mention.

## Metadata: durable custom state

`pr_url` metadata (which can be large) is pre-existing data — not a write
recommendation; new keys are free-form.
Free-form KV bag; writing is a mutation — only for explicit requirements
that record state for later readers. Short snake_case; the platform curates no vocabulary. Never store secrets, tokens, or API keys. Not metadata: logs or summaries,
bookkeeping such as timestamps, attempt counts, or agent IDs, files touched and investigation notes — other single-run details belong in the result comment.

```bash
multica issue metadata set <issue-id> --key <key> --value <value>
multica issue metadata delete <issue-id> --key <stale-key>
```

`--value` JSON-parsed (sniffs bool/number); `--type string|number|bool` forces.

## Custom properties: typed workflow state

Typed sibling of metadata (Severity, Environment…): values validated
against the definition (select options, date format, URL), shown in the
sidebar. Read before writing: `multica property list` (catalog) /
`multica issue property list <issue-id>` (values); set by name — CLI
translates ids:

```bash
multica issue property set <issue-id> --name Environment --value staging
multica issue property set <issue-id> --name Platforms --value "iOS,Android"
multica issue property unset <issue-id> --name Environment
```

- Validation error lists legal options; catalog icon cosmetic; agents cannot
  create/edit definitions — propose in a comment.

## Status changes have server side effects

- **`backlog`** parks (assignee set, no task fires); moving to `todo` (or
  any non-done/non-cancelled status) enqueues the agent.
- **`in_progress`/`in_review`** are agent-managed CLI mutations, not
  `StartTask`/`CompleteTask` side effects; squad leaders: parent
  `in_progress` while members work, `in_review` on confirmed re-trigger.
- **`done`** on a child posts a system comment on its parent; close-intent
  PRs advance it on merge.
- **`cancelled`** is terminal; enqueues nothing new but does NOT stop
  in-flight tasks (MUL-4465).
- Failed issue-triggered tasks may roll `in_progress` → `todo` when no
  active task/retry remains.

## Sub-issues

Create status: `todo` starts work now, `backlog` parks it.

Parallel children — start:

```bash
multica issue create --title "..." --parent <issue-id> --assignee <agent> --status todo
```

Serial children — promote one at a time:

```bash
multica issue create --title "Step 2: ..." --parent <issue-id> --assignee <agent> --status backlog
multica issue status <child-id> todo   # promote when the previous step is truly done
```

### Stages

`--stage <N>` (N ≥ 1) groups sub-issues into stages; parent woken
**once when a whole stage finishes** (every sub-issue in the lowest unfinished
stage reached `done`/`cancelled`); non-closing completions are silent; no stages = one implicit stage (wake
on the *last* child). Advancement is agent-driven: the server wakes the parent assignee, who
promotes the next stage's `backlog` items.

```bash
# Stage 1 runs; later stages parked
multica issue create --title "Research A" --parent <id> --assignee <agent> --stage 1 --status todo
multica issue create --title "Research B" --parent <id> --assignee <agent> --stage 1 --status todo
multica issue create --title "Build"      --parent <id> --assignee <agent> --stage 2 --status backlog
multica issue create --title "Ship"       --parent <id> --assignee <agent> --stage 3 --status backlog
```

On "Stage N complete": inspect `multica issue children <parent-id>`, promote
only items whose dependencies are met
(`multica issue status <stage-2-child-id> todo`); on conflict: `backlog`.

## Incorrect → correct

```text
Fix login redirect                  # incorrect — no issue key, won't link
MUL-2759: fix login redirect        # correct — links the PR
```

```bash
# incorrect — all fire immediately
multica issue create --title "Step 2" --parent <issue-id> --assignee <agent> --status todo
multica issue create --title "Step 3" --parent <issue-id> --assignee <agent> --status todo
# correct — staged; later stages park
multica issue create --title "Step 1" --parent <issue-id> --assignee <agent> --stage 1 --status todo
multica issue create --title "Step 2" --parent <issue-id> --assignee <agent> --stage 2 --status backlog
multica issue create --title "Step 3" --parent <issue-id> --assignee <agent> --stage 3 --status backlog
```

## References

`references/working-on-issues-source-map.md` — `file:line` for every
contract: `pull-requests` CLI/route, PR response fields, `derivePRState`,
two-path link (`extractIdentifiers`) vs close-intent
(`extractClosingIdentifiers`), backlog enqueue, child-done notify, stage
column / `stageBarrierClosed`, `--stage` / `issue children` CLI, metadata
CLI.
