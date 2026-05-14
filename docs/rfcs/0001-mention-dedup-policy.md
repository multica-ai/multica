# RFC: Per-mention agent task enqueue (drop @mention coalescing dedup)

- Issue: [MUL-1913](mention://issue/9f54962b-e055-43eb-a649-1b16db52fea2)
- Status: Accepted
- Date: 2026-05-09

## Background

When a member @mentions an agent on an issue (or a member comments on an
issue assigned to an agent), the trigger path enqueues an `agent_task_queue`
row. Today both paths short-circuit when the same agent already has a
`queued` or `dispatched` task on the same issue:

- `server/internal/handler/comment.go` `enqueueMentionedAgentTasks` — @mention
  trigger
- `server/internal/handler/issue.go` `shouldEnqueueOnComment` — assignee-on-
  comment trigger

Both call `Queries.HasPendingTaskForIssueAndAgent` and skip enqueue when it
returns true. The intent was a coalescing queue: rapid-fire comments fold
into a single pending task, and when that task picks up it reads all the
latest comments anyway.

## Problem

The coalescing model has three user-visible costs:

1. **No UI feedback for the merged comment.** A second @mention does not
   create a task, so no queued banner appears and there is no toast saying
   "merged into pending task". Users perceive the @mention as lost.
2. **Trigger comment provenance is lost.** Only the first trigger comment
   is recorded on the task; subsequent triggers are not referenced by any
   task. Auditing "what made this run happen" fails.
3. **Distinct intents collapse.** When two @mentions live in different
   threads with different requests ("add a test" vs. "fix copy"), folding
   them into one task forces the agent to disambiguate, and the user cannot
   cancel one without cancelling both.

Different threads and different mention text are strong signals that the
two triggers are distinct intents — coalescing throws that signal away.

## Decision

**Adopt option C: every @mention or assignee-comment trigger creates its
own task.** No `(issue, agent)` dedup at enqueue time.

Per-(issue, agent) execution stays serial because `ClaimAgentTask`
(`server/pkg/db/queries/agent.sql`) refuses to dispatch a queued row when
the same agent has another `dispatched` or `running` row on the same
issue. Multiple queued rows pile up safely and drain in
`(priority DESC, created_at ASC)` order. This is a coordination-side
property — `FOR UPDATE SKIP LOCKED` locks the row being claimed, not the
`(issue, agent)` key — and relies on the daemon today never invoking
`ClaimAgentTask` concurrently for the same agent. Tightening that into a
real DB-level guarantee (e.g. an advisory lock keyed on `(issue, agent)`)
is out of scope for this RFC.

### Mutual exclusion between on_comment and @mention paths

Without `(issue, agent)` dedup at enqueue time, a single member comment
that @mentions the assignee would otherwise enqueue twice with identical
`trigger_comment_id`: once via the on_comment path
(`shouldEnqueueOnComment` → `EnqueueTaskForIssue`) and once via the
@mention path (`enqueueMentionedAgentTasks` → `EnqueueTaskForMention`).
Same trick applies to a plain reply that inherits the assignee mention
from the thread root.

The on_comment gate gains a `commentMentionsAssignee` clause that uses the
same effective-mention computation as the @mention path
(`shouldInheritParentMentions` for inheritance). When the @mention path
will enqueue for the assignee, on_comment skips. The two paths become
mutually exclusive on a `(comment, assignee)` pair.

### Considered alternatives

- **A. Keep coalescing, add a UI hint** ("merged into pending task"). Fixes
  visibility but not provenance and not the distinct-intent case.
- **B. Allow up to N queued tasks per (issue, agent), coalesce above N.**
  Combines the worst of both — still loses the Nth+1 trigger comment, and
  introduces a magic number.
- **C. No dedup, every trigger creates a task. (Chosen.)**

## Out of scope

- **True rapid-fire duplicate suppression.** A user double-clicks @ within
  a second; both create tasks and the agent runs twice on identical
  context. Acceptable cost — the agent reads its own previous comment in
  the second run and can early-exit. We may revisit by having the
  scheduler skip a queued task when "no relevant comments since the
  previous task for this (issue, agent) completed", but that is a
  follow-up, not a blocker for this RFC.
- **Cross-agent dedup.** Different agents on the same issue continue to
  run in parallel; nothing changes there.
- **Queued banner UI.** Already shipped in
  [MUL-1897](mention://issue/14fdefb4-3a36-4406-a840-1f6700ac95b5).

## Implementation

1. Remove the `HasPendingTaskForIssueAndAgent` short-circuit in
   `enqueueMentionedAgentTasks`.
2. Remove the `HasPendingTaskForIssueAndAgent` short-circuit in
   `shouldEnqueueOnComment`. The function reduces to the
   assignee-readiness check (`isAgentAssigneeReady` + non-backlog status).
3. Add the `commentMentionsAssignee` clause to the on_comment gate so
   the on_comment and @mention paths are mutually exclusive on a
   `(comment, assignee)` pair (see "Mutual exclusion" above).
4. Drop `HasPendingTaskForIssueAndAgent` and the unused
   `HasPendingTaskForIssue` from `server/pkg/db/queries/agent.sql`. Re-run
   `make sqlc`.
5. Tests:
   - `TestRepeatedMentionsEnqueueSeparateTasks` — two @mentions on an
     unassigned issue produce two `queued` rows with distinct
     `trigger_comment_id` values.
   - `TestAssigneeMentionDoesNotDoubleEnqueue` — a member comment that
     @mentions the assignee on an assigned issue produces exactly one
     `queued` row (the mention path), not two.
6. No migration needed. No frontend changes needed: the queued banner
   already aggregates over `ListActiveTasksByIssue`, so multiple queued
   rows render correctly.

## Risks

- **Cost:** users who comment frequently on agent-assigned issues will
  trigger more runs than today. Mitigated by per-(issue, agent) serial
  execution — no extra concurrency, just more sequential work — and by
  the future "skip if no relevant comments" optimization noted above.
- **Replay:** the second queued task reads issue state that the first
  task may have already addressed. Agents already need to read recent
  comments and judge whether work is still required; this RFC does not
  change that contract.
