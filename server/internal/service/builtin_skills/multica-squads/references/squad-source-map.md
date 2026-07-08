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

## Comment Catch-up Guidance

Source:

```text
server/internal/daemon/prompt.go                  # assignment prompt starts with --recent 10
server/internal/daemon/execenv/runtime_config.go  # runtime workflow starts with --recent 10
server/internal/daemon/execenv/reply_instructions.go
```

Contracts:

- assignment-triggered agents must catch up on comments, but the required
  first read is bounded: `multica issue comment list <issue-id> --recent 10
  --output json`;
- older threads remain reachable through the `Next thread cursor` emitted on
  stderr and the matching `--before` / `--before-id` flags;
- comment-triggered cold starts read the triggering thread first and use
  `--recent 10` only for cross-thread background.

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
server/internal/handler/squad_briefing.go         # buildSquadLeaderBriefing ~104, buildSquadRoster ~121, renderMemberRow ~169
server/internal/handler/daemon.go                  # briefing injection ~1187, ~1530
```

Contracts:

- squad leader tasks append briefing to leader agent instructions
  (daemon.go:1187, 1530);
- briefing includes operating protocol, roster, and optional instructions
  (squad_briefing.go:104-117);
- `instructions` section appears only when non-empty (squad_briefing.go:110-112);
- archived agent members are skipped from roster (squad_briefing.go:178-179);
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

- when child issue completes and parent is assigned to squad, parent squad
  leader can be triggered (triggerChildDoneSquad at issue_child_done.go:304);
- routing is leader-only — one `EnqueueTaskForSquadLeader` on the leader, no
  member fan-out (issue_child_done.go:214-216, 344);
- loop guards skip same squad, same effective leader, and shared-leader
  cross-squad cases (issue_child_done.go:229-235, effectiveChildAgentOwner ~367,
  childAssigneeIsSquad ~387).

## Private Leader Access

Source:

```text
server/internal/handler/agent_access.go           # canAccessPrivateAgent ~25-40, canEnqueueSquadLeader ~82-91
server/internal/handler/squad.go                   # enqueueSquadLeaderTask gate ~1037
```

Contracts:

- public leaders pass — `canAccessPrivateAgent` returns true when
  `agent.Visibility != "private"` (agent_access.go:26-28);
- agent-to-agent traffic is allowed — `actorType == "agent"` short-circuits
  (agent_access.go:29-31);
- private leader access for members is limited to owner/admin or agent owner
  (agent_access.go:32-39);
- system triggers are treated like agent triggers for squad leader enqueue:
  `canEnqueueSquadLeader` remaps `actorType == "system"` to `"agent"` before
  delegating to `canAccessPrivateAgent` (agent_access.go:87-90). This is wired
  into `enqueueSquadLeaderTask`, which denies the enqueue when the actor cannot
  access the leader (squad.go:1037).

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
