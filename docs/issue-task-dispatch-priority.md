# Issue task dispatch priority

Multica applies issue priority when a runtime claims its next queued issue task. Priority changes which not-yet-started task receives an available slot; it never cancels, interrupts, or replaces a `dispatched`, `running`, or `waiting_local_directory` task.

## Ordering contract

Issue priorities map to the task queue as follows:

| Issue priority | Queue rank |
| --- | ---: |
| `urgent` | 4 |
| `high` | 3 |
| `medium` | 2 |
| `low` | 1 |
| `none` or an unknown legacy value | 0 |

The runtime candidate queries first exclude tasks when the same agent already has active work for that issue or chat, then order the globally eligible rows by:

1. task priority descending;
2. task creation time ascending;
3. task UUID ascending as the deterministic final tie-break.

`server/internal/service/task.go::ClaimTaskForRuntime` and `ClaimTasksForRuntimes` pass each candidate's exact ID to `claimTask`. Inside the agent capacity transaction, `server/pkg/db/queries/agent.sql::ClaimAgentTask` re-checks runtime health and the same serialization predicate for that exact row. If capacity or eligibility changed after the candidate snapshot, the attempt returns no task and the outer loop continues in global priority/FIFO order; it cannot silently substitute a lower-priority row from that agent. The singular and batch daemon poll APIs therefore share one final selector contract.

If the highest-priority candidate's agent has no free slot, the claim loop may select the next eligible agent. Capacity is checked while the agent row is locked. A lower-priority healthy run that already owns a slot stays active; a newly queued high-priority task can only win the next slot that becomes available.

## Entry-point funnel

Workspace issue dispatch entry points persist either the issue's current priority or, for an automatic retry, the parent task's priority snapshot before the runtime claim boundary:

| Entry point | Server funnel |
| --- | --- |
| Issue creation or assignment | `IssueService.maybeEnqueueOnAssign` → `TaskService.EnqueueTaskForIssue` |
| Promotion from backlog to an active status | `IssueService.WillEnqueueRun` → the same issue enqueue path |
| Agent-created sub-issue with an executable assignee/status | normal issue creation path |
| Explicit agent mention or comment wake | `EnqueueTaskForMention`, `EnqueueTaskForThreadParent`, or assignee fallback → `enqueueMentionTaskWithCommentPlan` |
| Autopilot `create_issue` | creates the issue, then uses the normal issue/squad enqueue path |
| Manual rerun | `RerunIssue` → `enqueueRerunTask`; the new task uses the issue's current priority |
| Automatic retry | `CreateRetryTask`; the child inherits the parent task priority |
| Deferred issue-media or retry promotion | preserves the stored task priority; due rows are promoted in priority/FIFO order |

The enqueue helpers call `priorityToInt` for issue-backed tasks. The claim log records `priority`, `priority_rank`, and `selection_reason=highest_priority_then_fifo` with the selected task, agent, and runtime IDs. Lower-priority work remains `queued` and visible through the task APIs; it is waiting for a slot, not terminally skipped.

## Eligibility gates

Priority never makes an ineligible issue executable:

- Assignment and status writes use `IssueService.WillEnqueueRun`. An issue in the built-in backlog or a custom backlog-category status does not enqueue. Moving between two backlog-category statuses also does not enqueue.
- A member assignee does not create an agent task.
- Archived agents, agents without a usable runtime, failed invocation-permission checks, and duplicate pending plans are rejected or coalesced before claim.
- Comments and explicit mentions are conversational triggers and intentionally work independently of issue status. A mention on a backlog issue is not backlog assignment promotion; it is an explicit request to run the mentioned agent.

## Paths outside issue-priority scheduling

These task kinds share runtime capacity but do not have workspace issue priority semantics:

- Autopilot `run_only` tasks have no issue and use their task kind's fixed priority.
- Direct chat tasks have no issue and use the chat priority policy.
- Quick-create tasks run before the requested issue exists and use the quick-create selection's task priority.
- Stale-dispatch reclaim re-delivers an already selected task. It orders recovery attempts by stored task priority but does not choose new issue work or preempt a healthy run.

Changing those policies requires a task-kind scheduling contract; it must not be inferred from a nonexistent issue priority.

## Regression coverage

- `service.TestIssueTaskClaimUsesPriorityThenFIFO` pins all five ranks and FIFO tie-breaking at the singular runtime claim boundary.
- `service.TestHighPriorityTakesNextFreeSlotWithoutPreemption` pins the arriving-high case and asserts the healthy low run and queued low successor are unchanged.
- `service.TestBatchClaimPrioritizesAcrossAgentsOnOneRuntime` pins priority selection across agents sharing one runtime when the batch has one available result slot.
- `service.TestRuntimeClaimSkipsBlockedPriorityHeadAcrossAgents` and its batch counterpart pin global ordering when one agent's highest queued row is blocked by per-issue serialization.
- `service.TestRuntimeScopedClaimNeverDowngradesExactCandidate` pins the race re-check: an exact candidate that becomes ineligible cannot silently dispatch a lower-priority row from the same agent.
- `service.TestSharedIssueDispatchFunnelsPreserveQueuePriority` pins assignment/status/sub-issue/autopilot-create-issue's shared issue funnel, mention/wake-comment's shared mention funnel, manual rerun, and automatic retry before those tasks reach the shared claim boundary.
- `handler.TestPreviewIssueTrigger_CreateAgentVsBacklog`, `TestPreviewIssueTrigger_MemberNoTrigger`, and `TestPreviewIssueTrigger_MatchesWritePath` pin the handler gates that keep backlog-category and human-assigned issues out of the task queue.
