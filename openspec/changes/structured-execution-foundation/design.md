## Context

Multica's current product shape already supports issue-driven teamwork and agent execution, but its planning model is still thin compared with the tools it most naturally competes with. The repository already shows partial groundwork for several missing capabilities:

- `issue.parent_issue_id` exists in the canonical issue table, which gives a starting point for parent/sub-issue hierarchy.
- `issue_label`, `issue_to_label`, and `issue_dependency` tables already exist in the initial schema, but are not yet exposed as a complete product capability.
- `/home/runner/work/multim/multim/apps/workspace/` already implements the active issue flows that roadmap work should extend.
- Agent execution, inbox, activity, and realtime systems already depend on issue state, which means new planning structure should enhance the issue model rather than introduce a parallel work-item abstraction.

This design turns the strategic roadmap into an implementation blueprint that can be applied in phases without losing the core Multica differentiator: agents are teammates operating on the same structured work graph as humans.

## Goals / Non-Goals

**Goals:**

- Use the existing `issue` model as the foundation for structured planning rather than creating a second canonical work-item type.
- Sequence roadmap work so Multica first becomes reliable for day-to-day team execution, then for project planning, then for AI-native automation.
- Reuse existing schema groundwork for hierarchy, labels, and dependencies where possible.
- Keep the workspace app as the primary product shell for roadmap delivery.
- Define clear implementation boundaries for data model, backend contracts, frontend surfaces, and agent orchestration.

**Non-Goals:**

- Implement Jira-style fully customizable workflows, field builders, or marketplace-style extension systems in the first phase.
- Turn Multica into a personal productivity tool centered on habits, pomodoros, or personal routine tracking.
- Replace the existing issue model with a new epic/task tree model unrelated to current database and API contracts.
- Commit to exact UI mockups or migration timings for every later-phase feature in this change.

## Decisions

### 1. Build the roadmap around the existing `issue` graph

The roadmap will treat `issue` as the canonical execution object across storage, APIs, UI, realtime updates, and agent workflows. Hierarchy, labels, dependencies, checklists, templates, and planning metadata should extend that existing graph rather than introduce a second planning object that agents and humans must reconcile manually.

Why this approach:

- It matches the current product and repository direction, where issue data already powers both human workflows and task execution.
- It lets Multica productize schema capabilities that already exist (`parent_issue_id`, labels, dependencies) before inventing new abstractions.
- It keeps agent assignment, execution, and activity history attached to the same work record teams already understand.

Alternatives considered:

- Introduce a new planning-only object above issues: rejected because it would fragment work between planning and execution too early.
- Focus first on new views without strengthening issue structure: rejected because view diversity does not solve the coordination gap.

### 2. Phase 1 should productize structured execution primitives before phase-level planning

Phase 1 will center on the execution foundation that Trello and Linear both make easy to use: parent/sub-issues, dependencies, labels, checklists, reusable templates, saved views, and stronger following/notification behavior.

Why this approach:

- These capabilities directly improve human coordination and agent context quality.
- The repository already contains partial support for hierarchy and dependency-related storage, making this the highest-ROI next step.
- Teams need structured execution before milestones, cycles, or automation can work reliably.

Implementation shape:

- Productize `parent_issue_id` with clear parent/child UI, list filtering, and project-level rollups.
- Expose `issue_dependency` as typed relationships (`blocks`, `blocked_by`, `related`) with validation and visual summaries.
- Expose `issue_label` and `issue_to_label` as workspace-scoped labels usable in board, list, filter, and issue detail flows.
- Add checklist and template models as issue-adjacent structured metadata that agents can consume during execution.
- Add saved views and follow/watch mechanics to support recurring operational workflows and stronger notifications.

### 3. Phase 2 should add planning cadence as bounded project-planning objects

Milestones, cycles, estimates, timeline/roadmap, and project health should arrive after the execution foundation is stable. These capabilities should remain anchored to issues and projects rather than becoming an independent planning system.

Why this approach:

- Linear-like planning works best when the underlying issue graph is already structured.
- Milestones and cycles create more value when labels, dependencies, hierarchy, and templates already exist.
- This keeps the project-planning model focused and avoids premature Jira-style sprawl.

Implementation shape:

- Add milestone and cycle objects scoped to workspace/project planning.
- Link issues to milestones/cycles and expose aggregated project health, schedule, and estimation views.
- Introduce timeline/roadmap and saved planning views using issue-linked planning entities rather than bespoke roadmap cards.

### 4. Phase 3 should make agent orchestration event-driven on top of structured work

Multica's unique advantage is not cloning Trello, Linear, or Jira. It is using the structured work graph to let agents triage, decompose, start, escalate, and coordinate execution automatically. That automation should be layered on top of Phase 1 and Phase 2 metadata rather than bundled into the foundation before the structure exists.

Why this approach:

- Automation quality depends on reliable labels, checklists, dependencies, estimates, and project context.
- The backend already has task lifecycle and realtime eventing, which can evolve into orchestration without replacing the core execution model.
- It preserves the current differentiation of agents as visible teammates rather than hidden background jobs.

Implementation shape:

- Extend task and event flows so rules can react to dependency resolution, issue state transitions, templates, and project planning signals.
- Add agent-facing decomposition and triage flows that write back into the issue graph instead of producing disconnected plans.
- Add blocker escalation and orchestration logic that can operate at project scope using issue graph signals.

### 5. Phase 4 should selectively import Jira and TickTick strengths, not emulate them wholesale

After the structured execution and automation foundation is in place, Multica can add a smaller set of high-ROI enterprise and personal-productivity capabilities: custom fields, automation rules, reporting, permissions, audit logs, reminders, recurrence, and calendar integration.

Why this approach:

- These capabilities are useful only after the core work graph and execution loops are stable.
- Selective adoption avoids turning Multica into a heavyweight configuration platform or a personal-life organizer.
- It keeps product complexity aligned with the target customer: small AI-native teams.

Alternatives considered:

- Build Jira-style customization first: rejected because it delays the core execution improvements Multica needs most.
- Build TickTick-style personal productivity first: rejected because it does not strengthen team execution or agent coordination enough.

## Risks / Trade-offs

- [Roadmap breadth could create an oversized implementation change] -> Mitigation: treat this change as the source blueprint, then break execution into follow-up changes per phase or capability cluster.
- [Existing schema groundwork may not match the exact product UX needed] -> Mitigation: reuse current tables and fields where appropriate, but allow additive migrations when product semantics require stronger contracts.
- [Agent automation could outrun task structure quality] -> Mitigation: keep orchestration behind the structured execution and planning phases.

## Migration Plan

1. Use this change as the planning baseline for roadmap execution.
2. Split Phase 1 into follow-up implementation changes for hierarchy/dependencies, labels, checklist/templates, and saved views/following.
3. After Phase 1 is stable, create follow-up changes for milestones/cycles/timeline and project health.
4. Layer agent orchestration changes onto the structured work graph after planning signals exist.
5. Add enterprise and personal-productivity extensions only after the previous phases are shipping cleanly.

## Open Questions

- Should checklists be modeled as dedicated issue checklist items or by extending the existing `acceptance_criteria` JSON contract into a first-class interactive task list?
- Should saved views live purely client-side at first, or be persisted server-side so they can become shareable team views?
- Which Phase 1 slice should ship first: hierarchy/dependencies or labels/checklists?
