---
name: multica-squads
description: "Use when creating, inspecting, updating, assigning to, or debugging a Multica squad."
user-invocable: false
allowed-tools: Bash(multica *)
---

# Multica Squads

## Quick start

Debugging squad run behavior — inspect first:

```bash
multica issue get <issue-id> --output json
multica squad get <squad-id> --output json
multica squad member list <squad-id> --output json
multica issue comment list <issue-id> --roots-only --summary --output json
multica issue comment list <issue-id> --thread <thread-id> --tail 30 --output json
```

scan the roots first, then open the threads — never unbounded pulls.
`--help`
before writes. No assign/comment/mention/update/delete/activity tests —
mutates state or triggers runs.

## Core model

A squad is not an agent: it is a workspace routing/coordination object.
Work runs through the squad's `leader_id` agent. Consequences:

- assigning an issue to a squad → routes to the leader;
- mentioning a squad → routes to the leader;
- squad-assigned autopilot → resolves to the leader;
- squad members are not automatically fanned out;
- squad `instructions` = leader briefing content, not member prompts.

## CLI

```bash
multica squad list --output json
multica squad get <squad-id> --output json
multica squad create --name <name> --leader <agent-name-or-id> --output json
multica squad update <squad-id> --instructions "<leader coordination policy>" --output json
multica squad delete <squad-id>
multica squad member list <squad-id> --output json
multica squad member add <squad-id> --member-id <id> --type agent|member --role <role> --output json
multica squad member remove <squad-id> --member-id <id> --type agent|member
multica squad member set-role <squad-id> --member-id <id> --member-type agent|member --role <role> --output json
multica squad activity <issue-id> action|no_action|failed --reason "<why>" --output json
```

recording squad activity: `multica squad activity` is a WRITE
— records the leader's evaluation; use
only as the squad leader after evaluating a trigger.

## Squad fields

`id` (UUID); `workspace_id`; `name` (unique per workspace); `description`
(human-facing metadata — do not assume runtime prompt impact); `instructions`
(leader briefing content, NOT injected into every member); `avatar_url`;
`leader_id` (runtime target for squad-routed work); `creator_id`;
`archived_at`/`archived_by` (archived squads rejected by assignment/autopilot
routing); `member_count`/`member_preview` (list responses).

Member fields: `member_type` (`agent` or `member`); `member_id`; `role`
(roster label — do not assume scheduling/permissions/routing).

`instructions` = leader-facing coordination policy: responsibility,
delegation, when to ask humans, review/handoff rules.

## Creation and leader membership

Create requires `leader_id` (a workspace agent). Archived leaders pass
create/update (existence check only) but fail closed at routing/dispatch. On
create (and on `leader_id` update when not already a member) the backend adds
the leader as a member with role `leader`.

## Leader briefing

Appended to the leader's instructions: Squad Operating Protocol; Squad Roster;
Squad Instructions (when non-empty). Roster: name, member type, mention
markdown, non-empty `role`; agent members list assigned workspace skills
(`skills: a, b` or `no skills assigned`); human members carry no skills
segment; builtin `multica-*` never listed; archived members skipped.

## Issue assignment

```text
assignee_type = "squad"
assignee_id = <squad-id>
```

Routes to `squad.leader_id`, never fan-out; `backlog` does not start work;
moving out of `backlog` can trigger the leader; changing assignee cancels
existing tasks first. Parent status is agent-managed: leader's first turn
moves parent to `in_progress` and keeps it while members work; `in_review`
only when a later re-trigger confirms the goal. Task completion never
self-changes status. Status authority only when assignee points at THIS squad;
other paths (e.g. `@squad` on a plain agent's issue) carry "do not change this
issue's status". Rejects: missing type/id, non-existent/archived squad,
archived leader, inaccessible private leader.

## Comment and mention

A comment on a squad-assigned issue can wake the squad leader (leader routing,
not fan-out). Mention format: `[@Squad Name](mention://squad/<squad-id>)` —
resolves the squad, reads `leader_id`, enqueues a leader task with the current
comment as trigger.

## Autopilot behavior

For `assignee_type = "squad"`: executable agent resolves from `squad.leader_id`;
admission/readiness checks run against the leader; archived squads fail closed;
attribution records the squad id. `create_issue` keeps
`assignee_type = "squad"`/`assignee_id` on the created issue; `run_only`
creates the task directly for the resolved leader.

## Handling complaints

Don't assume broken code or defend current behavior. Classify: expected /
config issue / product limitation / actual bug. If correct but product-wise
bad, propose a scoped change. Never silently change routing, fan-out,
briefing, autopilot, or comment-trigger behavior.

## Side effects

Side effects: squad create/update/delete, `leader_id` change, member
add/remove/role, issue assignment, out-of-backlog moves, commenting/
mentioning, squad autopilots, `multica squad activity`, archive. No test
actions without explicit authorization.

## Common wrong assumptions

Squad ≠ agent; work routes to `leader_id`, never fan-out; `instructions` =
leader briefing, not member prompts; `description` not proven prompt content;
`role` = roster context, not scheduling; backlog doesn't start work; first
dispatch ≠ parent completion; server never auto-flips parent status; briefing
≠ status authority — a squad `@`-mentioned into another's issue is a guest
(`multica issue status` no).

## References

Source paths, tests, edge cases, exact routing: `references/squad-source-map.md`.
