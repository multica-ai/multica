# Sub-issues

Whether to split an issue, and how sub-issues and stages start, order and wake
work.

- [Before splitting: one issue, one PR](#before-splitting-one-issue-one-pr)
- [Sub-issues: todo starts work now, backlog parks it](#sub-issues-todo-starts-work-now-backlog-parks-it)
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
stages. The platform's sub-issue wakeup **wakes the parent assignee when a stage
closes while a later stage is waiting** — every sub-issue up to that stage has
reached a terminal status (`done`/`cancelled`) — and **once more when every
sub-issue, staged or not, is closed**. A completion that closes nothing is
silent. A sibling set with **no** stages wakes the parent once, when the *last*
sub-issue finishes. A parent in `backlog` is not woken; it catches up once it
leaves backlog. A member assignee gets an inbox notification instead of a run.

Advancement is agent-driven: the server only detects the closed barrier and
wakes the parent assignee, who then decides whether to promote the next stage's
`backlog` sub-issues to `todo`.

An agent cannot close a parent while sub-issues are open (`sub_issues_open`).

```bash
# Stage 1 runs now; later stages parked until promoted
multica issue create --title "Research A" --parent <id> --assignee <agent> --stage 1 --status todo
multica issue create --title "Research B" --parent <id> --assignee <agent> --stage 1 --status todo
multica issue create --title "Build"      --parent <id> --assignee <agent> --stage 2 --status backlog
multica issue create --title "Ship"       --parent <id> --assignee <agent> --stage 3 --status backlog
```

When both Stage 1 sub-issues finish you (the parent assignee) are woken by the
sub-issue wakeup; its `[WAKEUP]` block lists every stage and names the next one.
Inspect the layout, then promote the next stage:

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

# correct — stage them; Stage 1 runs, later stages park and are promoted as
# each stage's barrier closes
multica issue create --title "Step 1" --parent <issue-id> --assignee <agent> --stage 1 --status todo
multica issue create --title "Step 2" --parent <issue-id> --assignee <agent> --stage 2 --status backlog
multica issue create --title "Step 3" --parent <issue-id> --assignee <agent> --stage 3 --status backlog
```
