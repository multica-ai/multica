# Sub-issues

Whether to split an issue, and how sub-issues and stages start, order and wake
work.

- [Before splitting: one issue, one PR](#before-splitting-one-issue-one-pr)
- [Sub-issues: todo starts once unblocked, backlog parks it](#sub-issues-todo-starts-once-unblocked-backlog-parks-it)
- [Incorrect to correct](#incorrect-to-correct)

## Before splitting: one issue, one PR

The default for an issue is one agent and one PR, however large. Sub-issues
cost more than they look: each child run starts without the discussion that
produced it, parallel PRs collide on shared files and migration numbers, and a
contract written before either side exists is only checked once both land.

Split only when every piece passes all of these:

- **It can be merged and verified on its own.** Someone can review and test
  it without the other pieces.
- **It touches separate code**, or another repository, so parallel PRs do not
  conflict.
- **Or it needs a different owner:** a human step (configuring an app,
  approving access), a different agent, a different team.

Slice by user-visible behavior, never by layer. Backend / frontend / API
contract sub-issues of one feature fail the first test. Work that does
parallelize well: unrelated bugs, separate repositories, independent research
directions, and large mechanical changes such as translations.

A PR that is large to review is not a reason to split. Organize it into
reviewable commits and put a review guide in its description.

When you do split, say in the parent comment why each piece is independent.
Each child's run gets a `[Parent issue]` block that points it at the parent,
so the child's description carries its scope and acceptance criteria; it does
not need to restate every decision made on the parent.

## Sub-issues: todo starts once unblocked, backlog parks it

On an agent-assigned issue, create status decides whether the assignee fires.
A non-backlog status (e.g. `todo`) enqueues the agent as soon as no earlier
stage or `--blocked-by` issue it waits on is still unfinished. `backlog` sets
the assignee without triggering, even after those waits end.

| Need | Create the child with |
|---|---|
| Children that run in parallel | `--status todo`, no stage |
| Children that run in order, each starting on its own | `--stage <N>` and `--status todo` |
| A step the parent reviews results before starting | that step's stage with `--status backlog` |
| A wait on an issue outside this parent, or on one sibling rather than a whole stage | `--blocked-by <issue>` (`references/dependencies.md`) |

Order sub-issues of one parent with stages, not with `--blocked-by` chains.

Parallel children — all start now:

```bash
multica issue create --title "..." --parent <issue-id> --assignee <agent> --status todo
```

### Stages: order sub-issues into barrier groups

`--stage <N>` (N >= 1) groups sub-issues under the same parent into ordered
stages. A staged sub-issue waits until every sub-issue in an earlier stage
reaches a done or closed category (including cancellation); `issue get` lists
those waits in `blocked_by` with `via: stage`. They are computed from the
current siblings and no dependency edges are stored, so moving, restaging or
deleting a sibling immediately changes the wait. A later stage created as
`todo` starts automatically once earlier stages end. Use this as the default.
Create the later stage as `backlog` when the parent must review results before
promoting it. Manual blockers combine with stage waits; a manual edge that
cycles through a stage is rejected.

The platform's sub-issue wakeup **wakes the parent assignee when a stage
closes while the next stage has backlog work to promote** — every sub-issue up
to that stage has reached a terminal status (`done`/`cancelled`) — and **once
more when every sub-issue, staged or not, is closed**. A completion that closes
nothing is silent. A sibling set with **no** stages wakes the parent once, when
the *last* sub-issue finishes. A parent in `backlog` is not woken; it catches up
once it leaves backlog. A member assignee gets an inbox notification instead of
a run.

```bash
# Stages start in order on their own; the parent reviews before Ship
multica issue create --title "Research A" --parent <id> --assignee <agent> --stage 1 --status todo
multica issue create --title "Research B" --parent <id> --assignee <agent> --stage 1 --status todo
multica issue create --title "Build"      --parent <id> --assignee <agent> --stage 2 --status todo
multica issue create --title "Ship"       --parent <id> --assignee <agent> --stage 3 --status backlog
```

Build starts once both Research sub-issues end. When Build ends, you (the parent
assignee) are woken by the sub-issue wakeup because Ship is parked; its
`[WAKEUP]` block lists every stage and names the next one. Inspect the layout,
then promote the next stage:

```bash
multica issue children <parent-id>   # sub-issues grouped by stage
multica issue status <child-id> todo # promote when its deps are met
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

Splitting one feature:

```text
# incorrect — sliced by layer; no piece can be verified alone
MUL-10 Backend: merge API          MUL-11 Frontend: merge button

# correct — one issue, one PR, reviewable commits
MUL-9: merge PRs from Multica      (api, then UI, then docs as separate commits)
```

Serial / phased sub-issues (don't start the whole chain at once):

```bash
# incorrect — all fire immediately, no ordering
multica issue create --title "Step 2" --parent <issue-id> --assignee <agent> --status todo
multica issue create --title "Step 3" --parent <issue-id> --assignee <agent> --status todo

# correct — stage them; each step starts when the previous one ends
multica issue create --title "Step 1" --parent <issue-id> --assignee <agent> --stage 1 --status todo
multica issue create --title "Step 2" --parent <issue-id> --assignee <agent> --stage 2 --status todo
multica issue create --title "Step 3" --parent <issue-id> --assignee <agent> --stage 3 --status todo
```
