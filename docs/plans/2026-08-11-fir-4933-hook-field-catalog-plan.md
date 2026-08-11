---
artifact_contract: ce-unified-plan/v1
artifact_readiness: implementation-ready
execution: code
issue: FIR-4933
created: 2026-08-11
---

# Hook field catalog: one canonical spec for server and interface

## Problem frame

The Workflow hook editor's filter-field suggestions (`EVENT_FIELDS` +
`FIELD_DEFINITIONS` in `packages/cerebro-workflows/core/`) are a hand-written
list that has drifted from what the server gates actually put in the event
context. Three visible consequences:

1. The managed (`managed`) policies match on fields the catalog never listed
   (12 `message.*` fields from `server/internal/cerebro/commentguard/guard.go`,
   16 `wakeup.*` fields, `chain.*`, `status.*`, `failure.*`, `continuation.*`,
   `issue.terminal`), so those hooks render with generated labels and no proper
   input widgets in the editor.
2. The catalog offers fields no gate ever populates (`message.channel_id`,
   `message.body`, `issue.previous_status`, `wakeup.fire_at`, `workflow.step`,
   top-level `error`, top-level `task.id`, top-level `model`,
   bare `continuation`). A custom hook built on one of these never matches;
   `match()` fails closed on unknown fields with no warning
   (`server/internal/cerebro/workflows/conditions.go`).
3. The hook journal sanitizer (`hookRepositoryContextAllowlist` /
   `hookJournalSectionsByEvent` in `hook_repository.go`) drops exactly the
   sections and keys these conditions use, and has no entry at all for
   `before.issue.assigned` (so `CaptureEvent` returns
   `ErrHookEventNotRetainable` on every assignment). Because
   `RecordTestEvidence` requires `len(run.Result.Matches) > 0`, a conditional
   hook on these events can practically never clear the publish gate via the
   Test flow.

Already fixed on main (verified): FIR-4797 removed the `!knownField` redaction
in `hook-ux.ts` and the unknown-field rejection in `hook-step-panel.tsx` /
`hook-validation.ts`. This plan does not re-litigate those.

Scope boundary: no changes to managed policy semantics, no `fail_mode`
behavior changes, no merging of `Proposed` (message content) into the
condition context, no refactor of gate packages' event construction, no
changes to binding-scope coverage. `packages/cerebro-workflows/**` and
`server/internal/cerebro/**` are cerebro carve-outs — no CEREBRO-PATCH
markers needed for the files below; tests are zone-exempt everywhere.

## Settled decisions (KTD)

- **KTD-1 (user-approved): one canonical field manifest, generated TS.** The
  interface must not hand-maintain a field list next to the gates. Follow the
  existing action-catalog precedent: a JSON manifest embedded server-side, a
  `go:generate` target emitting
  `packages/cerebro-workflows/core/hook-field-catalog.generated.ts`, and a
  Go test failing when the generated file drifts (mirrors
  `TestHookActionCatalogIsGeneratedFromServerManifest`). Rejected alternative:
  expanding the hand-written `EVENT_FIELDS`/`FIELD_DEFINITIONS` lists — the
  issue explicitly showed that gap re-opens.
- **KTD-2: the catalog lists only fields the server actually supplies.**
  Dead suggestions are deleted, not "fixed up" in the editor. Rejected
  alternative: populate the dead fields server-side — message bodies do not
  belong in condition evaluation, and several (`workflow.step`,
  `issue.previous_status`) have truthful replacements (`workflow.step_status`,
  `status.from`).
- **KTD-3: `continuation` is exposed as `continuation.present` (and
  `continuation.kind`), matching what `task_completion_gate.go` already
  sends.** The bare `continuation` field (a map, never equal to a boolean) is
  removed; the `require-continuation` template is repointed.
- **KTD-4: unknown fields stay unredacted (FIR-4797) and get an explicit
  "(unknown field)" marker in summaries instead of borrowed security
  semantics.** No `<redacted>` for unfamiliar fields.
- **KTD-5: journal retention is extended only with non-sensitive operational
  fields.** Message booleans/ids, `continuation.*`, `chain.*`, `status.*`,
  `failure.reason` (+ postpone counts), `wakeup.*`, `assignment.*`, `actor.*`,
  workflow step fields are retained. Message content, prompts, and handoff
  prompts stay out of the journal.
- **KTD-6: `actor.type` / `actor.id` become base condition-context fields**
  in `hookConditionContext` (gates set `HookEvent.Actor`; the engine simply
  never merged it). One engine change fixes it for every event.

## Work

### U1 — Canonical field manifest (server)

Files:

- `server/internal/cerebro/workflows/hook_field_manifest.json` (new):
  `{version: 1, common: [...], events: [{type, fields: [...]}]}`; each field
  `{path, label, input: text|number|boolean|select, sensitive?, options?}`.
  Content = the verified gate truth below; every event in `HookEventCatalog`
  present, including `before.issue.assigned`.
- `server/internal/cerebro/workflows/hook_field_manifest.go` (new): embed,
  parse, validate (no duplicate paths per event, known input kinds, every
  event type known to `HookEventCatalog` and vice versa), accessor
  `HookFieldManifestCatalog()`, and `GenerateHookFieldTypeScript()` +
  `//go:generate` directive.
- `server/internal/cerebro/workflows/cmd/generate-hook-field-catalog/main.go`
  (new): `-output` writer, mirroring `generate-hook-action-catalog`.
- `packages/cerebro-workflows/core/hook-field-catalog.generated.ts`
  (generated): exports `HOOK_FIELD_CATALOG` (`common` + per-event field
  lists with labels/inputs) and `HOOK_FIELD_DEFINITIONS` map.

Verified context inventory (source: gate code on main):

| Event | Fields the gate supplies (beyond common) |
|---|---|
| `before.message.send` | `message.{agent_authored,has_recipient,has_active_wakeup,promises_continuation,thread_required,correct_thread,required_parent_id,no_action,is_sub_issue,mentions_initiator,mentions_agent,posted_on_parent}` |
| `before.agent.stop`, `before.task.complete` | `issue.{status,terminal}`, `continuation.{present,kind,evidence_id}` |
| `on.task.failure` | `failure.{reason,message,attempt,max_attempts}`, `task.{id,status}` |
| `before.issue.assigned` | `assignment.{agent_id,reason}`, `actor.{type,id}` |
| `before.issue.status_change` | `status.{from,to}`, `chain.{active,approved_for_done}` |
| `after.workflow.step_completed` | `workflow.{phase_id,block_id,block_type,step_number,step_status}` |
| `before.wakeup.create` | `wakeup.{trigger_type,trigger_enabled,active_count,max_active,min_interval_seconds,seconds_until_fire,has_last_fire,seconds_after_last_fire,loop_limit_enabled,consecutive_without_progress,max_without_progress,since_member_reply,since_status_change,since_progress_update,since_pull_request_update,expected_continuation}` |
| `on.wakeup.fire_failure` | `failure.{reason,message,consecutive_postpones,next_consecutive_postpone}`, `wakeup.{id,trigger_type,expected_continuation}` |
| `before.session.end` (handoff) | `handoff.{root_comment_id,start_new,prompt}` (prompt `sensitive`), `issue.status` |
| `before.prompt.assemble` | `prompt` (sensitive), `provider` |
| `before.session.start` | `provider`, `resuming`; `after.session.start`: `resuming` |
| `before/after.session.end` (runtime) | `status`, `error` |
| `after.tool.call` | `tool.{name,output}` (output `sensitive`), `call_id` |
| `on.tool.failure` | `tool.{name,argument_keys,error,call}`, `task.{id,status}` |
| `on.error` | `error`, `source` |

Common (engine base, `hookConditionContext`): `actor.type`, `actor.id` (new
via U3), `agent.id`, `agent.model`, `issue.id`, `session.id`, `project.id`,
`workflow.id`, `workspace.id`, `attempt`, `hook_depth`, `no_progress`.

Note: the step gate's `Context["workflow"]` replaces the base `workflow.id`
entry for `after.workflow.step_completed` (engine merges Context over base) —
documented behavior, catalog lists the step fields for that event.

Test scenarios:

- manifest parses; duplicate field path in one event panics; unknown event
  type panics; catalog event set == `HookEventCatalog` key set.
- regeneration output == committed generated TS (drift test, mirrors action
  catalog test in `hook_actions_test.go`).

### U2 — Interface consumes the generated catalog

Files:

- `packages/cerebro-workflows/core/hook-validation.ts`: derive
  `EVENT_FIELDS`/`COMMON_FIELDS` from `HOOK_FIELD_CATALOG`; keep
  `fieldsForEvents` signature.
- `packages/cerebro-workflows/core/hook-ux.ts`: build
  `FIELD_DEFINITIONS` from generated definitions; `fieldDefinition()` keeps
  its fallback for unknown fields but returns a `known: false` marker used by
  `conditionSummary` to append "(unknown field)" (KTD-4).
- `packages/cerebro-workflows/core/hook-types.ts`: add
  `before.issue.assigned` to `HookEventType` and `HOOK_EVENT_OPTIONS`
  (label/description from `HookEventCatalog` truth).
- `packages/cerebro-workflows/core/hook-ux.ts` templates:
  `require-continuation` → condition `continuation.present eq false`;
  `no-silent-failure` → replace dead `retry.pending` with
  `failure.attempt gte $event.failure.max_attempts` and retitle the
  description to "when a run exhausts its retries".
- Tests: `hook-validation.test.ts`, `hook-ux.test.ts`, `templates.test.ts` —
  update snapshot/expectations to catalog data; add a test asserting every
  template's condition fields exist in the catalog for the template's events
  (locks template drift).

Test scenarios:

- `fieldsForEvents(["before.message.send"])` includes `message.no_action` and
  no longer includes `message.channel_id`.
- summary for an unknown field renders the value plus the unknown marker, not
  `<redacted>`.
- every `HOOK_TEMPLATES` condition field resolves against the catalog.

### U3 — Engine exposes the actor (server)

Files:

- `server/internal/cerebro/workflows/hook_engine.go`: merge
  `"actor": {"type", "id"}` into `hookConditionContext`.
- `server/internal/cerebro/workflows/hook_engine_test.go`: assert
  `actor.type` matches for an event carrying `HookEvent.Actor`.

Test scenarios: condition `actor.type eq agent` on `before.message.send`
evaluates true against a guard-shaped event.

### U4 — Journal retention aligns with the fields (server)

Files:

- `server/internal/cerebro/workflows/hook_repository.go`:
  - add `HookBeforeIssueAssigned` to `hookJournalSectionsByEvent`
    (`assignment`, `actor`, `agent`, `issue`) — stops the
    `ErrHookEventNotRetainable` warn-log per assignment.
  - add missing sections to existing events: `continuation` on
    task-complete/agent-stop, `status`+`chain` on issue-status, `failure` on
    task-failure/wakeup-failure.
  - extend `hookJournalContextAllowlist` per KTD-5 (message facts,
    continuation, chain, status, failure reason(+counts), the 16 wakeup keys,
    assignment, actor, workflow step fields, task status).
- `server/internal/cerebro/workflows/hook_repository_test.go` (or sibling):
  capture → replay round-trip on a guard-shaped `before.message.send` event
  keeps the message facts.

Test scenarios: replayed journal event still matches a draft policy
conditioned on `message.agent_authored`.

### U5 — Drift locks tying managed policies and gates to the manifest

Files:

- `server/internal/cerebro/workflows/hook_field_manifest_test.go` (new):
  - every condition field used by `managedHookPolicies` (including `$event.*`
    value references) resolves against `manifest(event) ∪ common` for each of
    the policy's events.
  - generated-vs-manifest diff test (from U1).
- Gate-shape coverage tests, one per gate package, each stubbing the
  evaluator/store, capturing the emitted `HookEvent`, and asserting every
  flattened `Context` key is declared in the manifest for that event:
  - `server/internal/cerebro/commentguard/` (message.send)
  - `server/internal/cerebro/workflows/` (task complete/agent stop, task
    failure, issue assign)
  - `server/internal/cerebro/wakeup/` (wakeup create, fire failure)
  - `server/internal/cerebro/loops/` (status change, workflow step)
  - best-effort: `sessions` handoff event; skip `daemon` runtime events if
    fixture cost is high (advanced events, documented as follow-up).

Test scenarios: removing a field from `guard.go`'s context without updating
the manifest fails `commentguard`'s test; adding a condition on a new field
to `managed_policies.go` fails the manifest test.

## Dependencies and sequencing

U1 → U2 (TS needs the generated file). U1, U3, U4 independent of each other;
all before U5's assertions rely on the final field set (U5 last).

## Risks

- Generated-TS churn: first `go generate` must be run and committed; CI has
  no auto-generation. Mitigated by the drift test.
- Removing dead suggestions is a UX change for anyone who built hooks on
  them — their hooks already never matched, so no behavior change.
- Journal retention zip: retained events live 7 days; new keys are booleans,
  counts, ids and enums only (KTD-5), no content.

## Verification

- `cd server && go test ./internal/cerebro/workflows/... ./internal/cerebro/commentguard/... ./internal/cerebro/wakeup/... ./internal/cerebro/loops/...`
- `cd packages/cerebro-workflows && pnpm vitest run` (core tests incl. new
  catalog assertions)
- `cd server && go generate ./internal/cerebro/workflows/` leaves a clean
  tree
- `BASE_REF=origin/main bash scripts/validate-cerebro-patches.sh` → OK
  (expected: no upstream-zone files touched)
