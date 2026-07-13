# Squad Source Map

This file records source evidence for `multica-squads/SKILL.md`.

Use this when the task requires exact source paths, edge-case behavior, tests, or contract verification.

## Object Model

### DB shape

Source:

```text
server/migrations/084_squad.up.sql                # base table: name, description, leader_id, creator_id
server/migrations/085_squad_archive.up.sql        # archived_at, archived_by columns
server/migrations/088_squad_instructions.up.sql   # instructions column
server/pkg/db/queries/squad.sql
packages/core/types/squad.ts
```

Key facts:

- `squad` stores `name`, `description`, `leader_id`, `creator_id` (084), archive
  metadata `archived_at`/`archived_by` (085), and `instructions` (088).
- `squad_member` stores `member_type`, `member_id`, and `role`.
- `member_type` is constrained to `agent` or `member`.
- issue `assignee_type` supports `squad`.

## CLI

Source:

```text
server/cmd/multica/cmd_squad.go
```

Commands:

```bash
multica squad list
multica squad get <squad-id>
multica squad create
multica squad update <squad-id>
multica squad delete <squad-id>
multica squad activity <issue-id> <outcome>

multica squad member list <squad-id>
multica squad member add <squad-id>
multica squad member remove <squad-id>
multica squad member set-role <squad-id>
```

Use `--help` for exact flags before writes.

## Create / Update

Source:

```text
server/internal/handler/squad.go                  # CreateSquad ~200-272, UpdateSquad ~287-364
server/pkg/db/queries/agent.sql                   # GetAgentInWorkspace ~15-17
server/pkg/db/generated/agent.sql.go              # getAgentInWorkspace ~1261
```

Contracts:

- create requires `leader_id` (squad.go:215-218);
- leader must be a workspace agent — both create (squad.go:230-237) and update
  (squad.go:333-338) validate via `GetAgentInWorkspace`;
- archived leader is NOT rejected at create/update: `GetAgentInWorkspace` is
  `WHERE id = $1 AND workspace_id = $2` (agent.sql:15-17) with no archived
  filter, so an archived agent can be set as leader here. Archived-leader fails
  closed later, at routing/dispatch — see the readiness gate (squad.go:945,
  isSquadLeaderReady → service.AgentReadiness at squad.go:1017), assignment
  validation (issue.go:2625-2627), and autopilot admission (autopilot.go:885-891);
- leader is auto-added as member with role `leader` (squad.go:258-263);
- updating `leader_id` auto-adds new leader as member if missing (squad.go:340-347).

## Leader Briefing

Source:

```text
server/internal/handler/squad_briefing.go         # buildSquadLeaderBriefing ~104, buildSquadRoster ~121, renderMemberRow ~169, agentSkillsRosterSegment, formatRosterRow
server/internal/handler/daemon.go                  # briefing injection ~1187, ~1530
```

Contracts:

- squad leader tasks append briefing to leader agent instructions
  (daemon.go:1187, 1530);
- briefing includes operating protocol, roster, and optional instructions
  (squad_briefing.go:104-117);
- `instructions` section appears only when non-empty (squad_briefing.go:110-112);
- archived agent members are skipped from roster (squad_briefing.go:178-179);
- agent member roster rows list assigned workspace skills via
  `loadSquadMemberSkillNames` (ListAgentSkillNamesByAgentIDs) and
  `agentSkillsRosterSegment` — "skills: a, b" or
  "no skills assigned"; builtin multica-* skills are excluded and human
  members carry no skills segment (squad_briefing.go renderMemberRow);
- no traced behavior injects `instructions` into every squad member.

## Issue Assignment

Source:

```text
server/internal/handler/issue.go                  # assignee validation ~2614-2632
server/internal/handler/squad.go                   # shouldEnqueueSquadLeaderOnAssign ~990, enqueueSquadLeaderTask ~1027
server/internal/service/task.go
```

Contracts:

- `assignee_type="squad"` routes to `squad.leader_id` (squad.go:1028-1050);
- backlog assignment does not immediately enqueue (squad.go:991-993);
- moving out of backlog can enqueue leader (squad.go:990-994 → isSquadLeaderReady);
- assignee change cancels existing issue tasks first;
- private leader access is checked at assign-time (issue.go:2629-2632) and at
  enqueue-time via `canEnqueueSquadLeader` (squad.go:1037);
- archived squad / archived leader rejected at assign-time (issue.go:2622-2627);
- pending task dedup is applied (squad.go:1042-1048).

## Comment / Mention

Source:

```text
server/internal/handler/comment.go                # comment triggers ~1057-1199, squad mention branch ~1352
server/internal/handler/squad.go                   # enqueueSquadLeaderTask ~986 (assign/backlog paths), lastTaskWasLeader ~915
server/internal/service/task.go                   # EnqueueTaskForSquadLeader
```

Contracts:

- commenting on a squad-assigned issue can wake the leader — the comment path
  computes triggers via `computeCommentAgentTriggers` (comment.go:1124), whose
  assigned-squad branch is `computeAssignedSquadLeaderCommentTrigger`
  (comment.go:1162-1199); the same computation backs the trigger-preview
  endpoint;
- explicit `mention://squad/<id>` resolves squad and adds the leader trigger
  (comment.go:1352-1391);
- squad mention does not fan out to members — enqueue targets `squad.LeaderID`
  only (comment.go:1104-1112, and squad.go:1007 on the assign/backlog paths);
- leader task uses `is_leader_task=true` (via `EnqueueTaskForSquadLeader`);
- leader self-trigger loops are guarded — same-leader / last-task-was-leader
  guards (comment.go:1173-1176, lastTaskWasLeader at squad.go:915) and member
  explicit-mention skip (comment.go:1177-1179).

## Autopilot

Source:

```text
server/internal/service/autopilot.go              # resolveAutopilotLeader ~617-655, dispatch ~88-111
server/internal/handler/autopilot.go              # save-time validateAutopilotAssignee ~845-893
```

Contracts:

- squad autopilot resolves executable agent from `squad.leader_id` —
  `resolveAutopilotLeader` squad branch (autopilot.go:639-651);
- readiness/admission checks target the leader: save-time validation rejects an
  archived squad/leader (handler/autopilot.go:881-891), and dispatch re-runs
  `resolveAutopilotLeader` + `AgentReadiness`;
- archived squad fails closed / skips dispatch — `errSquadArchived`
  (autopilot.go:644-645);
- `create_issue` keeps the issue assigned to the squad (autopilot.go:88-97);
- `run_only` creates task directly for leader (autopilot.go:99-106, dispatch via
  `resolveAutopilotLeader` at autopilot.go:284).

## Child-done Parent Trigger

Source:

```text
server/internal/handler/issue_child_done.go       # dispatchParentAssigneeTrigger ~246, triggerChildDoneSquad ~304
```

Contracts:

- when a child issue closes a stage barrier and the parent is assigned to a
  squad, the parent squad leader is triggered (triggerChildDoneSquad in
  issue_child_done.go);
- routing is leader-only — one `EnqueueTaskForSquadLeader` on the leader, no
  member fan-out (triggerChildDoneSquad / dispatchParentAssigneeTrigger);
- no self-trigger guard: a same-squad or shared-leader child still wakes the
  parent squad leader — the wake is a serial handoff onto the PARENT and is the
  only carrier of the stage-barrier "advance / wrap up" instruction (MUL-3969,
  mirrors the agent path from MUL-2808). Re-triggering is bounded only by
  `HasPendingTaskForIssueAndAgent` (idempotent per parent issue + agent).
- no leader-invocation gate: child-done does NOT re-check whether the child's
  completer can invoke the leader. The parent was already permission-checked at
  squad-assign time (`validateAssigneePair`), so waking its own leader is a
  coordination handoff, not a fresh invocation. Re-checking it here failed
  closed for the DEFAULT private leader (the child's completer is an
  agent/system actor with no resolvable human originator), stranding every
  process-squad pipeline after stage 1 while direct-to-leader-agent parents
  advanced fine (MUL-4063 / GH #4928). Agent and squad child-done now share one
  ungated path; any future invocation gate must be added to BOTH together.

## Private Leader Access

Source:

```text
server/internal/handler/agent_access.go           # canInvokeAgent ~48-108, canEnqueueSquadLeader ~261-267
server/internal/handler/squad.go                   # enqueueSquadLeaderTask gate ~955-974
```

Contracts (invocation gate, MUL-3963 — this is the *trigger* gate, distinct from
the view gate `canAccessPrivateAgent`):

- `canEnqueueSquadLeader` loads the leader and delegates to `canInvokeAgent`
  (agent_access.go:261-267);
- `canInvokeAgent` judges by the *effective invoking user*: a member actor is
  itself; an agent/system actor is the top-of-chain human originator
  (`originatorUserID`), which is `""` when none resolved (agent_access.go:48-54);
- the agent owner may always invoke their own agent (agent_access.go:57-59);
- `permission_mode != "public_to"` (i.e. private) is deny-by-default — no admin
  bypass, no A2A bypass; only the owner branch passes (agent_access.go:61-65);
- `public_to` consults the invocation-target allow-list: a `workspace` target
  admits any workspace member AND workspace-internal agent/system principals even
  with no resolved human (`workspaceBroad`); `member` targets require the
  resolved human to match; `team` targets are inert in V1 (agent_access.go:82-106);
- wired into `enqueueSquadLeaderTask` (squad.go:955-974): the squad
  assign/promote path denies the enqueue when the actor cannot invoke the leader
  (member authors are their own originator; agent-authored triggers pass `""`).
- NOTE: the child-done wake does NOT use this gate anymore — see "Child-done
  Parent Trigger" above (MUL-4063).

## Tests

Relevant test groups:

```text
server/internal/handler/squad_assign_trigger_test.go
server/internal/handler/squad_comment_trigger_test.go
server/internal/handler/squad_briefing_test.go
server/internal/handler/squad_private_leader_test.go
server/internal/handler/autopilot_private_leader_test.go
server/internal/handler/squad_no_action_test.go
```

Verification command:

```bash
go test ./internal/handler -run 'Test.*Squad|Test.*squad|Test.*Autopilot.*Squad|Test.*ChildDone.*Squad'
```
