# AgentGUI Review Contract

This file adds AgentGUI-specific review requirements. It does not replace the
repository contribution, testing, security, accessibility, or code-quality
rules.

Review only the lanes triggered by the change. For every triggered lane, report
one of:

- `pass`: include concrete evidence
- `not applicable`: explain why the change cannot affect the lane
- `blocked`: name the missing evidence or unavailable consumer

Do not use an unsupported statement such as "no impact" as review evidence.

## 1. Architecture Integrity

Trigger this lane when a change touches:

- Session, Turn, Interaction, Goal, runtime-operation, or prompt lifecycle
- `ConversationServices`, `AgentHostApi`, engine state, selectors, projections,
  controllers, event handling, reconciliation, or public exports
- composer, Rail, timeline, Message Center, approvals, interactive prompts,
  Workbench integration, or host adapters

Review against:

- [Agent GUI Node](../../../docs/architecture/agent-gui-node.md)
- [Agent Activity Packages](../../../docs/architecture/agent-activity-packages.md)

Verify:

1. The existing owner still owns each fact and state transition.
2. The command and observation paths still follow the documented direction.
3. Events remain invalidation hints and canonical reads remain authoritative.
4. Session, Turn, Interaction, request, and Workbench identities stay exact.
5. Shared behavior remains provider-neutral and capability-driven.
6. React renders projections and dispatches actions; it does not recreate
   lifecycle or reconciliation orchestration.
7. New public exports are durable consumer contracts, not convenience exports
   for one host.

If the change alters ownership, data flow, or a public contract, update the
durable architecture document in the same change.

## 2. Performance

Trigger this lane when a change touches:

- high-frequency engine subscriptions, selectors, snapshots, or stream updates
- Session list, Rail, timeline, transcript, message projection, or reconciliation
- scroll, resize, layout measurement, virtualization, rich-text rendering, or
  hidden surfaces
- caches, retained per-Session state, timers, observers, or diagnostic logging

Verify:

1. A change in one Session does not rebuild unrelated Session projections.
2. High-frequency render paths do not consume whole-workspace snapshots.
3. Streaming does not add per-token logging, broad reconciliation, repeated
   parsing, or synchronous full-layout reads.
4. Lists, history reads, caches, and retained UI state remain bounded.
5. Memoized boundaries keep stable references and do not receive aggregate
   objects when scalar projections are sufficient.
6. Virtualization, scroll following, and prepend restoration keep one geometry
   and intent owner.
7. `pnpm check:agent-gui-degradation` budgets do not increase.

Use evidence proportional to risk:

- For a statically isolated change, identify the unchanged hot path and
  boundary.
- For state or projection changes, provide focused test results and reference
  stability or fanout evidence.
- For reported jank, long tasks, or render storms, provide a trace or profiler
  comparison tied to the exact interaction.

Use
[Agent session lifecycle troubleshooting](../../../docs/conventions/troubleshooting/agent-session-lifecycle.md)
for known large-history, streaming, reconciliation, and diagnostic-log traps.

## 3. Consumer Compatibility

Trigger this lane when a change touches an exported symbol, subpath export,
runtime or host input, projection shape, event/DTO mapping, command behavior,
feature capability, or lifecycle ordering.

Review these known in-repository consumers:

- Desktop workspace AgentGUI and standalone Agent windows
- Desktop Message Center, Workbench, Issue Manager, and App Center integrations
- Mobile conversation, Rail, composer, media, and activity services
- `agent-activity-core` and the `tuttid` activity adapter

Also review the published contract for out-of-repository consumers:

- TSH and other AgentGUI hosts
- closed-source cloud hosts
- external workspace applications

Verify:

1. Existing imports and documented subpath exports remain valid.
2. Optional capabilities still fail closed when absent or unknown.
3. Consumers do not need to copy engine, lifecycle, merge, or attention logic.
4. Cancellation, failure, retry, reconnect, stale response, and recovery paths
   preserve their documented ordering and terminal semantics.
5. Unknown providers, enum values, missing optional data, and unavailable host
   capabilities remain explicit rather than silently reinterpreted.
6. A contract change has consumer coverage or a named migration plan.

For a closed-source consumer, record only:

- the sanitized consumer role
- the public contract or capability it uses
- validation status and any remaining uncertainty

Do not record private repository URLs, customer names, credentials, deployment
topology, proprietary feature names, or unreleased operational details.

## 4. Review Output

The review summary must include:

| Lane         | Trigger        | Result                                 | Evidence                                           | Affected consumers |
| ------------ | -------------- | -------------------------------------- | -------------------------------------------------- | ------------------ |
| Architecture | Why it applies | `pass`, `not applicable`, or `blocked` | Document section, code path, or test               | Named surfaces     |
| Performance  | Why it applies | `pass`, `not applicable`, or `blocked` | Static boundary, test, budget, or trace            | Named surfaces     |
| Consumers    | Why it applies | `pass`, `not applicable`, or `blocked` | Import search, contract test, or validation status | Named consumers    |

List unresolved risks separately. Do not convert unavailable evidence into a
pass.

## 5. Automation Boundary

`REVIEW.md` defines semantic review requirements; it is not itself an
executable gate. Agent instructions route reviewers to this file. Repository
checks and hooks may verify objective invariants such as forbidden imports,
dependency direction, public-export shape, degradation budgets, and required
test fixtures. They must not claim to prove semantic compatibility or
performance without the evidence required above.
