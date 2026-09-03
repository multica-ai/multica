# Workflow Engine V1 Design

**Status:** Approved design, implementation not started
**Date:** 2026-09-03
**Branch:** `feat/workflow-engine-v1`

## Goal

Add a server-owned, deterministic ordered workflow model on top of Multica's existing parent/child issue, `issue.stage`, issue status, revision, and run-dispatch primitives.

The V1 must let the server know what stage comes next, advance that stage without asking an LLM to decide workflow topology, preserve a durable audit trail, and leave all issues without an attached workflow behaving exactly as they do today.

## Existing behavior and problem

Multica already supports nullable integer `issue.stage`, staged child issues, backlog parking, stage-barrier detection, parent wakeups, optimistic issue revisions, status-driven run dispatch, and batch-aware child completion handling.

`server/internal/handler/issue_child_done.go` explicitly documents the missing capability: the server can detect that a stage barrier closed but has no declarative workflow model, so it cannot know whether the workflow is finished or whether a later stage must exist. That decision is currently handed back to the parent assignee agent.

Workflow Engine V1 closes only that gap. It does not replace the task queue, daemon, runtime, squad, autopilot, or issue hierarchy.

## Design principles

1. **Opt-in:** migration does not attach workflows to existing issues.
2. **Server-owned topology:** stage ordering comes from a persisted workflow snapshot, not a prompt.
3. **Reuse existing execution:** child issues remain normal Multica issues and are executed through existing assignment/run dispatch.
4. **Snapshot semantics:** an active run never changes because its template was edited later.
5. **Idempotent advancement:** duplicate completion callbacks cannot advance a run twice.
6. **Auditability:** every durable state transition is append-only and attributable.
7. **Backward compatibility:** without an active workflow run, current child-done comments and agent-driven advancement stay byte-for-byte behaviorally equivalent.
8. **YAGNI:** V1 is an ordered linear pipeline only.

## V1 scope

V1 includes workflow definitions, immutable definition versions, issue-bound workflow runs, append-only transitions, start/cancel/read APIs, deterministic stage promotion, blocked materialization state, final pending-review state, and tests for concurrency/idempotency/backward compatibility.

V1 does not create child issues from templates. The parent or another existing mechanism may materialize children before or during a workflow. V1 only controls which already-created stage is allowed to run next.

## Workflow definition

A definition is workspace-scoped and versioned. Editing a workflow creates a new version; it never mutates a version referenced by an existing run.

The persisted `definition` JSONB uses schema version 1:

```json
{
  "schema_version": 1,
  "stages": [
    {"key": "plan", "name": "Plan"},
    {"key": "implement", "name": "Implement"},
    {"key": "verify", "name": "Verify"},
    {"key": "review", "name": "Review"}
  ]
}
```

Array order is authoritative. The first item maps to `issue.stage = 1`, the second to stage 2, and so on. Stage keys are stable machine identifiers within one definition version; names are display labels.

Validation requires schema version 1, 1-32 stages, unique non-empty keys, unique stage positions by construction, and non-empty display names. No conditions, branches, loops, retries, executors, models, budgets, approval gates, or prompts are part of this schema.

## Data model

### `workflow_definition`

Stores one immutable version of a workspace workflow.

Required columns: `id uuid primary key`, `workspace_id uuid not null`, `name text not null`, `version integer not null`, `definition jsonb not null`, `created_by uuid not null`, `created_at timestamptz not null default now()`.

Constraints: `version >= 1`, unique `(workspace_id, name, version)`, and workspace-scoped indexes for list/read operations. There is no in-place update of `definition`; a logical edit inserts version N+1. Concurrent version creation for the same workspace/name is serialized with a transaction-scoped advisory lock before reading the current maximum version.

### `workflow_run`

Stores one execution of a definition against a parent issue.

Required columns: `id`, `workspace_id`, `issue_id`, `workflow_definition_id`, `definition_snapshot jsonb`, `status`, `current_stage`, `revision`, `started_by_type`, `started_by_id`, `started_at`, `completed_at`, `cancelled_at`, `created_at`, and `updated_at`.

One issue may have historical runs, but at most one active run. A partial unique index on `issue_id` covers statuses `running` and `blocked_materialization`.

`definition_snapshot` is the authoritative topology for the run even if the source definition is later deleted from user-facing lists or superseded by another version.

### `workflow_transition`

Append-only audit log for durable workflow state changes.

Required columns: `id`, `workspace_id`, `workflow_run_id`, `idempotency_key`, `kind`, `from_stage`, `to_stage`, `from_status`, `to_status`, `actor_type`, nullable `actor_id`, `payload jsonb`, and `created_at`. System transitions may omit `actor_id`; user/agent initiated transitions retain the authenticated actor ID.

A unique constraint on `(workflow_run_id, idempotency_key)` collapses duplicate external triggers. Transition rows are never updated or deleted by normal workflow operations.

## Run states

V1 states are:

- `running`: a declared stage is active or waiting for its currently materialized children to finish.
- `blocked_materialization`: the current stage closed, a later declared stage exists, but no child has been materialized for that later stage yet.
- `completed_pending_review`: the last declared stage closed. The workflow is complete, but the parent is not human-approved `done`.
- `cancelled`: workflow enforcement has been explicitly stopped.

`current_stage` is 1-based while `running`; when `blocked_materialization` it is the declared stage that must be materialized next; when completed it remains the final stage for audit/readability.

## Starting a workflow run

Starting is explicit; creating an issue never auto-attaches a workflow in V1.

The start operation loads the parent issue in the caller's workspace, loads the requested definition version, validates there is no active run, snapshots the definition, inspects the full child set, and performs validation before any mutation.

Start validation rules:

1. Every non-terminal child must have `stage` set.
2. Every staged child must have `1 <= stage <= len(definition.stages)`.
3. No non-terminal child in stage 2+ may already be outside the effective `backlog` category; otherwise ordered execution has already been violated and start returns 409.
4. Stage 1 must have at least one non-terminal child. V1 does not synthesize work.
5. A terminal historical child may be unstaged; it does not participate in workflow progression.
6. Parent status may not be terminal or effective backlog.

On successful start, the run and `started` transition are created transactionally. Any stage-1 child whose effective status is backlog is promoted to built-in `todo`; already-active stage-1 children are left unchanged.

After commit, promoted issue updates are published and normal status-driven run dispatch is evaluated using existing Multica issue trigger logic. Starting a workflow does not call a runtime or daemon directly.

## Stage advancement

The existing stage-barrier detector remains the source of the initial signal: a child transitions from non-terminal to terminal, the parent is non-terminal/non-backlog, and the staged frontier closes.

When that happens, the handler first looks for an active workflow run for the parent. This lookup/advance branch is evaluated before the legacy human-parent-assignee early return: a server-owned workflow must progress even when the parent is assigned to a member or is unassigned. Parent terminal/backlog guards still apply. If no active run exists, the handler executes the existing agent-driven child-done path unchanged, including its current human-assignee behavior.

If an active run exists, `WorkflowService.AdvanceFromClosedStage` performs one transaction:

1. Lock the `workflow_run` row `FOR UPDATE` and re-read its revision/status/current stage.
2. Re-read all child issues inside the transaction.
3. Re-evaluate that the run's current stage barrier is actually closed; a stale or out-of-order callback becomes a no-op.
4. Derive the next declared stage from `definition_snapshot`, never from currently materialized child stage numbers.
5. Reconcile forward through the declared stages, applying the terminal, blocked, promotion, or already-satisfied outcomes below.
6. Append one transition for each durable stage/state change, with an idempotency key derived from run ID, trigger kind, source/target stage, and resulting run revision.

This transaction is the serialization point for concurrent child completions. The workflow run revision increments once per successful durable transition.

### Advancement outcomes

For each next declared stage, the service inspects children with that exact `issue.stage`:

- If at least one non-terminal child exists, every such child must still be effective backlog. Those children are promoted to built-in `todo`, `current_stage` moves forward, and the run remains `running`.
- If children exist but all of them are already terminal, that stage is already satisfied. The service records the skipped/satisfied transition and continues scanning the next declared stage in the same transaction.
- If no child exists for the next declared stage, the run becomes `blocked_materialization` with `current_stage` set to that missing stage. It does not guess that the workflow is finished.
- If no declared stage remains, the run becomes `completed_pending_review` and the parent issue is moved to built-in `in_review` unless it is already in an effective `in_review` state. A terminal parent is handled earlier by reconciliation and cancels the run instead.

A later-stage non-terminal child found outside backlog is an invariant violation. Advancement stops without promotion and returns a conflict/error result for logging and API visibility rather than silently running stages out of order.

## Materialization resume

`POST .../workflow/resume` reconciles a `blocked_materialization` run after the stage named by `current_stage` has been materialized. In blocked state it inspects that current stage itself (not the previously closed stage): no children means it stays blocked; backlog children are promoted and the run returns to `running`; already-terminal children are satisfied and scanning continues; invalid active children cause conflict.

Resume is idempotent and is also safe to call on a `running` run to reconcile a barrier whose original callback was lost. It never changes a completed or cancelled run.

## Database transaction and issue side effects

Workflow state mutation and issue status promotion happen in one PostgreSQL transaction. New SQL queries must preserve the same status-change invariants as `UpdateIssueStatus`: workspace scoping, position recalculation, revision increment, `last_activity_at`, and `updated_at`.

The transaction must not enqueue agent runs, publish WebSocket events, or perform network I/O. It returns a `WorkflowAdvanceResult` containing the authoritative promoted/updated issue rows and workflow state.

After commit, the handler:

1. publishes normal `issue_updated` events for promoted children and any parent moved to `in_review`;
2. evaluates each promoted child through the existing `IssueService.WillEnqueueRun` predicate;
3. dispatches through the existing run path when that predicate says work should start;
4. emits the existing parent/system timeline messaging with workflow-aware wording rather than the old "decide whether another stage exists" instruction.

Workflow progression does not depend on a parent wake. For member-assigned parents, keep the existing no-automated-wake/no-noise policy; the workflow still advances server-side and remains visible through its run/transition APIs.

If post-commit publication or dispatch fails, database workflow state is not rolled back. Existing Multica recovery/idempotency mechanisms remain responsible for run dispatch recovery; workflow state is queryable and `resume` can reconcile.

## Cancellation and manual parent changes

Cancelling a workflow is explicit and idempotent. It changes only the run to `cancelled` and records a transition; it does not cancel active agent runs or rewrite child statuses.

Once cancelled, subsequent child completions use Multica's existing non-workflow stage-barrier behavior. A new workflow run may later be started after normal start validation succeeds.

V1 does not globally forbid users or integrations from changing the parent issue while a workflow runs. Every workflow start/advance/resume operation therefore re-loads the parent and fails closed when the parent is terminal or effective backlog. If the parent was deliberately parked and later restored, `resume` reconciles the workflow.

If the parent is already terminal when reconciliation occurs, the active workflow run is cancelled with reason `parent_terminal` rather than advancing child stages.

## Service boundary

Add a focused `WorkflowService` in `server/internal/service`. It owns definition validation, run lifecycle, transaction boundaries, state-machine rules, stage reconciliation, and returned side-effect descriptors.

HTTP handlers own authentication/authorization, request parsing, response formatting, WebSocket publication, timeline comments, and run dispatch. The daemon and runtime protocols are unchanged.

## HTTP API

Routes follow the existing workspace-scoped `/api/*` router group:

- `GET /api/workflow-definitions` — list latest definition versions in the current workspace.
- `POST /api/workflow-definitions` — create version 1 for a new name or the next immutable version for an existing name.
- `GET /api/workflow-definitions/{id}` — read one immutable version.
- `GET /api/issues/{id}/workflow` — return the active run, or the most recent historical run when none is active.
- `POST /api/issues/{id}/workflow/start` — start a requested definition version.
- `POST /api/issues/{id}/workflow/resume` — reconcile/promote a running or materialization-blocked run.
- `POST /api/issues/{id}/workflow/cancel` — cancel the active run.
- `GET /api/issues/{id}/workflow/transitions` — ordered audit history for the selected/latest run.

Definition mutation is human owner/admin only in V1. Definition reads use normal workspace membership. Start/resume use the same authenticated workspace/issue mutation boundary as existing issue operations so trusted task-token actors can participate. Cancel is human-only in V1.

Every loader/query includes `workspace_id`; an ID from another workspace must resolve as not found rather than leaking existence.

## Error and conflict behavior

Use explicit conflict results for topology/state violations instead of best-effort guessing.

Representative cases:

- active run already exists for issue -> 409;
- definition version not in caller workspace -> 404;
- malformed definition -> 400;
- stage value outside definition -> 409 on start/reconcile;
- later-stage non-terminal work already active -> 409;
- next declared stage has no children -> 200 with run state `blocked_materialization`, not an error;
- resume with no materialized children -> idempotent 200, still blocked;
- stale duplicate barrier callback -> internal no-op success;
- cancelled/completed run resume -> 409 for API callers, no-op for duplicate internal callbacks.

Unexpected database errors are logged with run/issue/workspace IDs but never include prompt content, credentials, or child descriptions in structured error fields.

## Observability

Structured logs include `workflow_run_id`, `workflow_definition_id`, `issue_id`, `workspace_id`, `from_stage`, `to_stage`, `from_status`, `to_status`, and transition kind.

The append-only transition table is the primary replay/audit surface in V1. Dedicated metrics, distributed tracing spans, cost accounting, and UI visualization are deferred.

## Expected implementation surface

The implementation should follow existing repository boundaries rather than introducing a new scheduler package.

Expected areas:

- new PostgreSQL migrations for the three workflow tables, constraints, and indexes;
- `server/pkg/db/queries/workflow.sql` plus sqlc-generated code;
- focused workflow domain/service files under `server/internal/service`;
- workflow HTTP handlers under `server/internal/handler`;
- route registration in `server/cmd/server/router.go`;
- a narrow integration hook in `server/internal/handler/issue_child_done.go` after the existing stage barrier closes;
- workflow-aware system-comment wording while retaining the legacy wording for non-workflow parents;
- service, handler, migration, workspace-isolation, and concurrency tests.

No daemon, runtime protocol, provider adapter, task queue schema, squad schema, autopilot schema, desktop packaging, or frontend component needs to change for V1.

## Compatibility contract

For a parent with no active workflow run, `notifyParentOfChildDone`, batch child-done handling, stage progress calculation, existing comments, and agent wake/dispatch behavior remain on the legacy path.

Existing staged issues are not migrated into workflow runs. Existing APIs continue to accept `stage` exactly as before.

## Test requirements

At minimum the implementation must prove:

1. valid definitions version correctly and invalid schemas are rejected;
2. workspace isolation holds for definitions, runs, and transitions;
3. start snapshots the definition and promotes only eligible stage-1 backlog children;
4. start rejects out-of-range stages and already-active later stages;
5. a workflow-free parent uses the exact legacy stage-barrier path;
6. closing stage N promotes declared stage N+1 and dispatch descriptors are returned once;
7. duplicate and concurrent callbacks produce one durable advancement;
8. an unmaterialized next stage becomes `blocked_materialization` instead of final;
9. resume promotes a newly materialized stage and is idempotent;
10. an already-terminal declared stage is skipped deterministically;
11. final stage completion records `completed_pending_review` and moves the parent to `in_review`;
12. cancelling stops workflow enforcement without cancelling child agent runs;
13. parent terminal reconciliation cancels the active workflow run;
14. batch child completion reconciles from the final committed sibling state;
15. SQL promotion preserves issue revision, position, activity timestamps, and workspace guards.

Concurrency tests must exercise two transactions racing to advance the same run, not only sequential duplicate calls.

## Rollout

The feature ships disabled by absence of workflow runs; no global feature flag is required for correctness. API exposure may still be guarded by an experimental server capability if maintainers prefer staged rollout.

Recommended rollout sequence:

1. merge schema/query/service primitives with no behavior change for legacy issues;
2. add start/read/cancel/resume APIs;
3. wire workflow-aware advancement behind active-run detection;
4. exercise real fork workloads and compare transition logs with expected stage order;
5. only then consider frontend workflow visualization or richer orchestration.

## Explicit non-goals for V1

- DAG branching or conditional edges;
- human approval records/gates;
- verifier agents or evidence evaluation;
- automatic rework loops;
- Project Brain / semantic memory;
- smart runtime/model routing;
- cost budgets or token ceilings;
- browser/visual QA;
- workflow-authored child issue templates;
- automatic retries or timeout policies;
- replacing squads, autopilots, tasks, runtimes, or daemon claims.

Each of those is a separate subsystem and must have its own design/spec cycle.

## Acceptance criteria

Workflow Engine V1 is complete when a workspace can create a versioned ordered definition, explicitly start it on a staged parent issue, and observe the server advance eligible child stages in definition order without asking an agent to decide whether another stage exists.

A missing declared stage must block rather than finish; materializing and resuming it must continue the same run. Concurrent/duplicate barrier events must not double-promote. Finishing the final declared stage must leave a durable audit trail and put the parent in `in_review`, never directly `done`.

Issues without an active workflow run must retain today's behavior and require no data migration or daemon/runtime changes.