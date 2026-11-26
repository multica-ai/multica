# Squads

A squad is a workspace routing and coordination object. It is not an agent and
it does not run work by itself.

- [Debugging a squad that did or did not run](#debugging-a-squad-that-did-or-did-not-run)
- [Core model](#core-model)
- [CLI](#cli)
- [Squad fields](#squad-fields)
- [Creation and leader membership](#creation-and-leader-membership)
- [Leader briefing](#leader-briefing)
- [Member brief](#member-brief)
- [Handoff mode](#handoff-mode)
- [Issue assignment behavior](#issue-assignment-behavior)
- [Comment and mention behavior](#comment-and-mention-behavior)
- [Autopilot behavior](#autopilot-behavior)
- [Common wrong assumptions](#common-wrong-assumptions)

## Debugging a squad that did or did not run

Inspect first:

```bash
multica issue get <issue-id> --output json
multica squad get <squad-id> --output json
multica squad member list <squad-id> --output json
multica issue comment list <issue-id> --roots-only --summary --compact --output json
multica issue comment list <issue-id> --thread <thread-id> --tail 30 --compact --output json
```

The two comment reads are a sequence: scan the roots first, then open the
threads that look relevant — mention triggers, failure reasons, and user
instructions usually live in the replies, which the roots scan never returns.

## Core model

Squad-routed work runs through the squad's `leader_id` agent. Consequences:

- assigning an issue to a squad routes to the leader;
- mentioning a squad routes to the leader;
- squad-assigned autopilot resolves to the leader;
- squad members are not automatically fanned out;
- squad `instructions` go into the leader briefing and into every agent
  member's member brief on the squad's issues;
- `handoff_mode` decides whether a member's reply goes back to the leader
  (`leader`, default) or members hand off to each other (`direct`).

## CLI

Squad commands:

```bash
multica squad list --output json
multica squad get <squad-id> --output json
multica squad create --name <name> --leader <agent-name-or-id> --output json
multica squad update <squad-id> --instructions "<squad coordination policy>" --output json
multica squad update <squad-id> --handoff-mode leader|direct --output json
multica squad briefs <squad-id> [--agent <agent-name-or-id>] [--output json]
multica squad delete <squad-id>
```

`squad briefs` prints the exact squad section each agent receives (leader
briefing or member brief). Both `--handoff-mode` and `briefs` require
permission to manage the squad (its creator, or a workspace owner/admin).

Member commands:

```bash
multica squad member list <squad-id> --output json
multica squad member add <squad-id> --member-id <id> --type agent|member --role <role> --output json
multica squad member remove <squad-id> --member-id <id> --type agent|member
multica squad member set-role <squad-id> --member-id <id> --member-type agent|member --role <role> --output json
```

Squad leader evaluation command:

```bash
multica squad activity <issue-id> action|no_action|failed --reason "<why>" --output json
```

`activity` is a write: it records the leader's evaluation decision on an issue.
Use it only when acting as the squad leader after evaluating a trigger.

Which issue it accepts: **the issue your current turn is running on**. The
target issue does NOT need to be assigned to your squad — a `@squad` mention on
an issue owned by an individual agent, or a leader task bound to a child issue,
all record fine. What the server checks is your task's leader flag and stamped
squad id, not the issue's assignee. A leader woken by a stage barrier runs on
the PARENT issue, so record against the parent, not the child you just read;
passing an unrelated issue id is rejected and the error names the issue you
should have used.

If the call fails, do not exit silently — the comment prohibition on `no_action`
only applies once the recording succeeded. Post one short comment with the
outcome instead, and only when this turn has not already commented: on the
`action` path your delegation comment is already that record.

Issue/comment commands often needed with squads:

```bash
multica issue get <issue-id> --output json
multica issue update <issue-id> --help
multica issue comment list <issue-id> --roots-only --summary --compact --output json
multica issue comment add <issue-id> --help
```

Comment reads stay bounded — the scan-then-expand sequence above, never one
unbounded pull.

## Squad fields

- `id` — squad UUID.
- `workspace_id` — workspace the squad belongs to.
- `name` — display name; unique per workspace.
- `description` — human-facing metadata/display text. Do not assume runtime
  prompt impact.
- `instructions` — squad-level instructions added to the squad leader briefing
  and to the member brief of every agent member.
- `handoff_mode` — `leader` (default) or `direct`. See
  [Handoff mode](#handoff-mode).
- `avatar_url` — optional squad avatar URL.
- `leader_id` — agent ID of the squad leader; the runtime target for
  squad-routed work.
- `creator_id` — creator of the squad.
- `archived_at` / `archived_by` — archive metadata. Archived squads are rejected
  by assignment/autopilot routing paths.
- `member_count` — list response count of squad members.
- `member_preview` — list response preview of squad members.

Use `instructions` for squad coordination policy: squad responsibility,
delegation expectations, when to ask humans, and review/handoff rules. The
leader and every agent member read it, so write it for both.

## Squad member fields

- `member_type` — `agent` or `member`.
- `member_id` — ID of the agent or workspace member.
- `role` — roster role label. A non-empty `role` appears in the leader briefing
  roster. It does not create scheduling, permissions, or routing behavior.

## Creation and leader membership

Creating a squad requires `leader_id`. The leader must be a workspace agent.
Create/update does not reject an archived leader: the lookup only checks the
agent exists in the workspace. An archived leader fails closed later, at
routing/dispatch — assignment, autopilot admission, and the comment/mention
readiness gate all reject an archived leader before any task is enqueued.

On create, the backend attempts to add the leader as a squad member with role
`leader`. When updating `leader_id`, if the new leader is not already a member,
the backend adds the new leader as a squad member with role `leader`.

## Leader briefing

For squad leader tasks, Multica appends a squad leader briefing to the leader
agent instructions. The briefing includes:

- Squad Operating Protocol;
- Squad Roster;
- Squad Instructions, only when `instructions` is non-empty.

Roster entries include member name, member type, mention markdown, and non-empty
role. For agent members the roster also lists their assigned skills
(`skills: a, b`, or `no skills assigned` when the agent has none) so the leader
can delegate by capability instead of guessing from the role label; human
members carry no skills segment. Built-in `multica-*` skills are not listed —
only the workspace skills explicitly attached to the agent. Archived agent
members are skipped from the briefing roster.

In `direct` handoff mode the briefing adds a "Handoff Mode: Direct" section:
the leader assigns the first step and handles what comes back to it.

## Member brief

When a non-leader agent member claims a run on an issue assigned to its squad,
Multica appends a "Squad Member Brief": the member's role, the rest of the
roster with mention markdown, the squad instructions, and how to hand off under
the squad's handoff mode. It never contains the leader's Squad Operating
Protocol.

## Handoff mode

`leader` (default): a member reply with no explicit `@agent` / `@squad` mention
wakes the leader, as described under
[Comment and mention behavior](#comment-and-mention-behavior); one that
mentions another agent or squad runs only that target (see
`references/mentions.md`).

`direct`, for a comment written by a member's run on the squad's own issue:

- mentioning another squad member runs only that member; the leader is not
  woken;
- mentioning a person (and no agent) wakes nobody; the person replies;
- a reply that names no next owner goes back to the leader;
- the issue goes back to the leader instead of the named member when the same
  two members have already passed it back and forth 3 rounds, or the issue has
  taken 20 member handoffs in 24 hours;
- a failed member run is recovered by the leader, not by the member that
  handed the work over.

Each routed handoff is recorded as a `squad_handoff` activity on the issue
(`kind` dispatch / member / human / leader, the target, `round` when above 1,
and `reason` when a guard sent it back). These limits are fixed.

## Issue assignment behavior

Issues can be assigned to squads with:

```text
assignee_type = "squad"
assignee_id = <squad-id>
```

Current behavior:

- assignment routes work to `squad.leader_id`;
- it does not enqueue every squad member;
- assignment while status is `backlog` does not immediately start work;
- moving a squad-assigned issue out of `backlog` can trigger the leader;
- changing assignee cancels existing tasks for the issue before enqueueing the
  new assignee path;
- parent issue status is agent-managed (same model as direct agent assignment):
  the leader's first assignment turn should move the parent to `in_progress`
  and keep it there while members work; the leader moves the parent to
  `in_review` only when a later re-trigger confirms the overall goal is met.
  When the run carries a `[Project workflow]` block, the parent stays at its
  current step instead, and the leader moves it on with that block — its
  Step done command, or the status its status map gives for the step's
  finished work.
  Completing a leader task (including the first dispatch) does not itself
  change issue status;
- that status authority is granted only when the issue's `assignee_type` /
  `assignee_id` point at THIS squad. The leader briefing is injected on every
  leader path, including an `@squad` mention on an issue owned by a plain agent
  — on those paths the protocol instead carries an explicit "do not change this
  issue's status".

The status names above are fixed built-in keys, not categories.

Assignment validation rejects a missing type/id pair, non-existent squad,
archived squad, archived leader, and private leader when the actor cannot access
it.

## Comment and mention behavior

If an issue is assigned to a squad, a new comment can wake the squad leader. This
is leader routing, not member fan-out.

A worker's reply also wakes the assigned leader without an explicit mention. If
the leader's current run started before the reply arrived, the reply is
delivered in a follow-up run after that run completes, so the worker does not
need to mention the leader again. An explicit `@agent` mention of the leader on
an issue assigned to its squad also runs it as the leader, so
`multica squad activity` works on that turn too.

Squad mention format:

```md
[@Squad Name](mention://squad/<squad-id>)
```

Current behavior: resolve the squad, read `leader_id`, enqueue a leader task,
and use the current comment as the trigger comment. It does not enqueue every
squad member.

## Autopilot behavior

Autopilots can be assigned to squads. For `assignee_type = "squad"`:

- executable agent resolves from `squad.leader_id`;
- admission/readiness checks run against the leader;
- archived squads fail closed / skip dispatch;
- run attribution records squad id where applicable.

For `create_issue` autopilots, the created issue keeps `assignee_type = "squad"`
and `assignee_id = <squad-id>`, while the actual executing agent is the resolved
leader. For `run_only` autopilots, no issue is created; the task is created
directly for the resolved leader agent.

## Side effects

These actions can trigger agent work or mutate durable state:

- creating a squad;
- updating squad fields;
- changing `leader_id`;
- changing `handoff_mode` (changes who later comments wake);
- adding/removing members;
- changing member roles;
- assigning an issue to a squad;
- moving a squad-assigned issue out of backlog;
- commenting on a squad-assigned issue;
- mentioning a squad;
- creating or triggering squad-assigned autopilots;
- recording squad activity with `multica squad activity`;
- deleting/archiving a squad.

## Common wrong assumptions

- A squad is not an agent.
- Squad work routes to `leader_id`, not every member.
- Squad mention routes to the leader, not every member.
- Squad assignment routes to the leader, not every member.
- Squad autopilot resolves to the leader as executable agent.
- `instructions` are not leader-only: agent members receive them too.
- A member's reply does not always wake the leader: in `direct` handoff mode a
  member-to-member or member-to-person handoff skips it.
- `description` has no proven runtime prompt effect.
- `role` is roster context, not automatic scheduling.
- Backlog assignment does not immediately start work.
- First leader dispatch is not parent completion — parent stays `in_progress`
  until the leader later confirms the overall goal and moves it to `in_review`
  (under a `[Project workflow]` block: stays at its step until the leader
  moves it on through that block).
- The server does not auto-flip parent status when child issues finish; it only
  wakes the leader with an explicit ask (including `in_review` when wrapping up).
- Getting the leader briefing does NOT imply status authority. A squad
  `@`-mentioned into an issue assigned to someone else is a guest: roster and
  delegation rules yes, `multica issue status` no.
