## Why

Multica already has the outline of an AI-native work system: issue-centric workflows, board/list views, project assignment, realtime task execution, and first-class agents. It does not yet have the structured planning foundation that makes Trello easy to run day-to-day, Linear strong for engineering teams, or Jira useful for more formal delivery workflows.

The biggest gap is not "missing more views." It is that work is not yet structured enough for humans and agents to plan, decompose, coordinate, and automate execution together. Without hierarchy, dependency management, labels, checklists, templates, saved views, and stronger follow/notification flows, Multica risks feeling powerful but operationally incomplete.

This change formalizes the product direction that best fits Multica: build the structured execution foundation first, then add planning cadence, then agent automation, and only later add selected enterprise or personal-productivity features.

## What Changes

- Define a phased product blueprint that prioritizes structured issue execution over Jira-style platform breadth or TickTick-style personal productivity depth.
- Establish Phase 1 as the core execution foundation: parent/sub-issues, dependency relationships, labels, checklists, templates, saved views, and stronger follower/notification flows.
- Establish Phase 2 as planning cadence: milestones, cycles, timeline/roadmap, estimates, and project health signals.
- Establish Phase 3 as Multica's differentiator: agent-driven triage, decomposition, execution triggers, blocker escalation, and project-level orchestration.
- Establish Phase 4 as selective expansion: custom fields, automation rules, reporting, permissions, auditability, and ecosystem integrations.
- Capture the implementation blueprint across data model, backend APIs, realtime/eventing, workspace UX, web UX, and agent orchestration layers.

## Capabilities

### New Capabilities
- `structured-execution-foundation`: Define the prioritized roadmap and implementation blueprint for Multica's issue-planning and agent-execution foundation.

### Modified Capabilities

## Impact

- `openspec/changes/structured-execution-foundation/specs/structured-execution-foundation/spec.md`
- Issue and project domain modeling in `/home/runner/work/multim/multim/server/`
- Mirrored issue and project workflows in `/home/runner/work/multim/multim/apps/workspace/` and `/home/runner/work/multim/multim/apps/web/`
- Realtime, notifications, and agent orchestration flows that depend on structured issue state
