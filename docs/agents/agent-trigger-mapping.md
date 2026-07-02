# Agent Trigger Mapping — what wakes an agent and how

Authoritative reference for every event that starts a new agent run in Multica. Source-verified from `server/internal/handler/issue.go`, `server/internal/handler/comment.go`, and `server/internal/cerebro/wakeup/`. Read this before designing issue flows that involve sub-issues, delegation, or agent-to-agent coordination.

---

## Trigger 1 — Issue Assignment

**Handler:** `UpdateIssue()` — `server/internal/handler/issue.go`

**Gate:** `shouldEnqueueAgentTask()` (issue.go:2885):
```go
func (h *Handler) shouldEnqueueAgentTask(ctx context.Context, issue db.Issue) bool {
    if issue.Status == "backlog" {
        return false  // backlog = parking lot, no trigger
    }
    return h.isAgentAssigneeReady(ctx, issue)  // checks runtime exists, not archived
}
```

**Enqueue:** `TaskService.EnqueueTaskForIssue()`

**When it fires:**
- Assignee changes to an agent AND status is **not** `backlog` (any other status: `todo`, `in_progress`, `in_review`, etc.)
- OR status changes **from** `backlog` to any status that is not `done` or `cancelled` — separate code path at issue.go:2739, additionally guards `isAgentRunningOnIssue()`

**When it does NOT fire:**
- `status = "backlog"` — assignment is stored, no run enqueued
- Agent's runtime is missing or agent is archived

**Dedup:** `CancelTasksForIssue()` is called before re-enqueuing when the assignee changes — existing tasks are cancelled first, so a re-assign starts a fresh run.

**Implication for delegation:** `--assignee` + any non-backlog status is the complete trigger. No `@mention` in a comment on top of it is needed or correct — adding one creates a double-trigger (see below).

---

## Trigger 2 — Agent @mention in a comment

**Handler:** `CreateComment()` → `enqueueMentionedAgentTasks()` — `server/internal/handler/comment.go:1290`

**Mention parsing:** `util.ParseMentions(comment.Content)` extracts all `mention://agent/<uuid>` links from the body.

**Thread inheritance:** When replying in a thread, `shouldInheritParentMentions()` decides whether to inherit mentions from the thread root. Inheritance is suppressed when the reply explicitly @mentions only non-agent entities (members, issues), signalling the commenter is addressing someone else.

**Gates (all must pass):**
1. Agent has a runtime and is not archived (`GetAgentInWorkspace`)
2. Private-agent ownership check (`canTriggerPrivateAgent`)
3. `MentionTriggerGate.CanTriggerMention()` — CEREBRO-PATCH JEH-1917
4. `HasPendingTaskForIssueAndAgent` dedup — skips if agent already has a pending task for this issue
5. Human origin chain: `CommentDelegationContext()` resolves `OriginalUserID` by walking up to 10 issue hops via `ResolveHumanViaOrigin()`. If no human found: `delegationorigin.ErrMissingHuman` → routes to `PrivateAgentRunRequester.RequestPrivateAgentRun()` (TECH-3629). Not silently dropped.

**Enqueue:** `TaskService.EnqueueTaskForMentionFromComment()`

**Status gate:** None — @mention fires on any issue status, including `done` and `cancelled`.

**Loop risk:** If Agent A mentions Agent B and B replies with a mention back to A, both agents run indefinitely. `@mention` is for first delegation or human escalation only — never in replies, acknowledgements, or sign-offs.

---

## Trigger 3 — Member comment on agent's assigned issue

**Handler:** `CreateComment()` — `server/internal/handler/comment.go:1046`

**Conditions (all must be true):**
1. `authorType == "member"` — must be a human member posting
2. `!commentMentionsOthersButNotAssignee()` — comment must not be talking to someone else while ignoring the assignee
3. `!isReplyToMemberThread()` — must not be continuing a member-to-member conversation
4. `shouldEnqueueOnComment()` — agent has runtime, not archived, private-agent gate, and `HasPendingTaskForIssueAndAgent` dedup (allows enqueue when a task is running, skips if one is already pending)

**Enqueue:** `TaskService.EnqueueTaskForIssueFromComment()`

**Status gate:** None — fires for any issue status, including `done` (allows follow-up questions after completion).

**Scope:** Only the agent's own assigned issue. A comment on a parent issue does NOT reach agents on sub-issues.

---

## Trigger 4 — Parent agent notified when sub-issue changes status

**Handler:** `UpdateIssue()` → `notifyParentOfChildDone()` + `notifyParentOfChildStatus()` — `server/internal/handler/issue.go:2758` (MUL-2538)

When a sub-issue's status changes, the platform **posts a system comment on the parent issue** (if one exists). This system comment flows into Trigger 3 for the parent agent.

- `notifyParentOfChildDone()` — fires when sub-issue → `done`
- `notifyParentOfChildStatus()` — also fires for `in_review` and `blocked` — CEREBRO-PATCH FIR-2601
- `advanceOrchestrationOnChildDone()` — advances sub-issue waves — CEREBRO-PATCH FIR-2564

This is the reliable path for parent-agent notification on sub-issue completion. It requires a `--parent <issue-id>` relationship between the sub-issue and parent issue.

---

## Trigger 5 — Time-based wakeup

**Sweeper:** `RunSweeper()` — `server/internal/cerebro/wakeup/sweeper.go:9`

```go
func RunSweeper(ctx context.Context, svc *Service, interval time.Duration) {
    if interval <= 0 {
        interval = 30 * time.Second  // default
    }
    rows, err := svc.ClaimDueTime(ctx, 25)  // up to 25 per tick
    for _, row := range rows {
        go svc.Dispatch(context.Background(), row)
    }
}
```

- Default tick: **30 seconds**
- Claims up to **25** due wakeups per tick via `ClaimDueTimeWakeups` SQL
- Each dispatched in its own goroutine

**If runtime offline:** `Dispatch()` calls `postpone()`, resetting the wakeup to pending with a delay and incrementing `consecutive_postpones`.

**Constraints:** `fire_at` must be at least the per-workspace minimum interval ahead (`wakeup_min_interval_minutes`, default 5, hard floor 1 — set in Cerebro features; NOT a fixed 15). A too-soon `fire_at` is rejected with the exact current minimum in the error. One pending wakeup per agent+issue at a time.

---

## Trigger 6 — Status-based wakeup (non-done statuses)

**Listener:** `RegisterListeners()` — `server/internal/cerebro/wakeup/listener.go:18`

Subscribes to `protocol.EventIssueUpdated`. When `status_changed == true` and the new status is **not** `done`:

```go
rows, err := svc.ClaimIssueStatus(ctx, issueID, status, 50)
// SQL: WHERE watch_issue_id = issueID AND watch_status = status
```

Up to 50 matching wakeups are claimed and dispatched.

**When status = `done`:** `CancelByIssueID(issueID)` cancels pending wakeups **owned by** the done issue (`wakeup.issue_id = issueID`), then returns early without calling `ClaimIssueStatus`. Wakeups watching for `watch_status = "done"` are therefore not dispatched through this path — use Trigger 4 (platform system comment) instead for parent-agent notification on sub-issue completion.

**Typical use:** `--on-issue-status <sub-uuid> --watch-status in_review` fires the parent when the sub-issue reaches `in_review`.

---

## Trigger 7 — GitHub CI wakeup

**Listener:** `RegisterListeners()` — `server/internal/cerebro/wakeup/listener.go:42`

Subscribes to `protocol.EventPullRequestUpdated`. Extracts `linked_issue_ids` from event payload:

```go
ids := linkedIssueIDs(payload["linked_issue_ids"])
rows, err := svc.ClaimGithubCI(ctx, ids, 50)
// SQL: WHERE trigger_type = 'github_ci' AND watch_issue_id IN ids
```

**Typical use:** `--on-github-ci <issue-uuid>` fires when CI on a linked PR updates — avoids busy-waiting in a sleep loop.

---

## What does NOT trigger an agent

| Event | Why |
|-------|-----|
| Comment on parent issue | `shouldEnqueueOnComment()` only applies to the agent's own assigned issue |
| Comment on sub-issue | Parent agent's `shouldEnqueueOnComment` is not called for sub-issues |
| Bare issue link (`MUL-123`) | `util.ParseMentions` only extracts `mention://` scheme links |
| Agent mentions itself | Allowed for cross-issue A2A handoff; dedup via `HasPendingTaskForIssueAndAgent` |
| `status = "backlog"` on assignment | `shouldEnqueueAgentTask()` returns false |
| @mention without human origin | `CommentDelegationContext()` returns `ErrMissingHuman` → routes to run-request, not dropped |
| Second comment while task pending | `HasPendingTaskForIssueAndAgent` dedup prevents double-queuing |
| `--watch-status done` wakeup | `RegisterListeners()` returns early on `done` without calling `ClaimIssueStatus` — use Trigger 4 instead |

---

## The double-trigger pattern

**The mistake — assignment + @mention = two simultaneous tasks:**

```
Parent agent:
  multica issue create --assignee agent-b --status todo   ← Trigger 1 fires (shouldEnqueueAgentTask)
  post comment with mention://agent/agent-b               ← Trigger 2 fires (enqueueMentionedAgentTasks)
  → Agent B runs twice simultaneously, asks questions in both threads
```

The `HasPendingTaskForIssueAndAgent` dedup only blocks a third enqueue onward — it does NOT protect against two rapid separate triggers from different code paths.

**The fix — assignment is the complete trigger; parent relies on Trigger 4 for the return:**

```
Parent agent:
  multica issue create --assignee agent-b --status todo --parent <parent-uuid>
  ← Trigger 1 fires once
  exit (no comments on sub-issue, no cross-issue mentions)

Agent B:
  works exclusively on its own issue
  asks questions only on its own issue
  multica issue status <id> done
  ← notifyParentOfChildDone() posts system comment on parent (Trigger 4)
  ← Parent agent resumes via Trigger 3 (system comment → on-comment path)
```

---

## Rules of thumb

a. **Assignment is the complete trigger.** `--assignee` + any non-backlog status starts the agent. No `@mention` on top of it is needed or correct.

b. **Backlog = deferred start.** `--status backlog` assigns without triggering. Promote to `todo` when the prerequisite is done.

c. **Questions stay on the owning issue.** An agent never posts questions on a parent issue. Ask on the sub-issue and `@mention` the human there if needed.

d. **Parent-child coordination via `--parent`, not `--watch-status done`.** Set `--parent <parent-id>` on the sub-issue and the platform posts a system comment on the parent when the sub-issue completes.

e. **`@mention` = new run.** Use it for first delegation or human escalation only — never in replies, acknowledgements, or sign-offs.

f. **Status wakeup = for non-done status transitions.** `--watch-status in_review` works. `--watch-status done` does not fire via the wakeup system.
