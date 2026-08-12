# Squad Source Map

Source evidence for `multica-squads/SKILL.md`.

## Object model (DB)

```text
server/migrations/084_squad.up.sql                # name, description, leader_id, creator_id
server/migrations/085_squad_archive.up.sql        # archived_at, archived_by
server/migrations/088_squad_instructions.up.sql   # instructions
server/pkg/db/queries/squad.sql
packages/core/types/squad.ts
```

`squad` (084/085/088); `squad_member` (`member_type` ∈ `agent`|`member`,
`member_id`, `role`); issue `assignee_type` supports `squad`.

## CLI — `server/cmd/multica/cmd_squad.go`

`multica squad list|get <squad-id>|create|update <squad-id>|delete
<squad-id>|activity <issue-id> <outcome>`; `multica squad member
list|add|remove|set-role <squad-id>`.

## Create / Update

Source: `server/internal/handler/squad.go` (`CreateSquad` ~200-272,
`UpdateSquad` ~287-364), `server/pkg/db/queries/agent.sql`
(`GetAgentInWorkspace` ~15-17), `server/pkg/db/generated/agent.sql.go` ~1261.

- create requires `leader_id` (215-218); leader must be a workspace agent
  (230-237, 333-338 via `GetAgentInWorkspace`);
- archived leader NOT rejected (no filter); fails closed later: readiness
  (squad.go:945 → `AgentReadiness` 1017), assignment (issue.go:2625-2627),
  autopilot admission (autopilot.go:885-891);
- leader auto-added, role `leader` (258-263); update adds new leader (340-347).

## Leader briefing

Source: `server/internal/handler/squad_briefing.go` (`buildSquadLeaderBriefing`
~104, `buildSquadRoster` ~121, `renderMemberRow` ~169, `agentSkillsRosterSegment`),
injection in `server/internal/handler/daemon.go` (~1187, ~1530).

- briefing appended to leader instructions; includes protocol, roster,
  optional instructions (104-117);
- `ownsIssueStatus` arg (`squadOperatingProtocolFor` selects responsibility
  6): status grant (`squadParentStatusOwned`) only when `issue.assignee_type
  == "squad"` AND `assignee_id == squad.id`, else explicit prohibition
  (`squadParentStatusNotOwned`); quick-create passes `false`; injection is
  broader than authority — keyed off `is_leader_task`, fires also for
  `@squad` mentions on others' issues (MUL-3724);
- `instructions` section only when non-empty (110-112); archived members
  skipped (178-179); agent rows list skills via `loadSquadMemberSkillNames`
  → "skills: a, b" / "no skills assigned"; builtin `multica-*` excluded;
  humans carry none;
- every claim response carries `leader_role_resolved: true`; when the gate
  withholds the briefing (NULL `squad_id`, hard-deleted, leader swapped), the
  handler also clears `is_leader_task`, so the flag means "briefing injected"
  and the run degrades to an ordinary turn (MUL-5811); the daemon derives the
  leader role from that flag (plus `squad_id` for quick-create), never from
  the briefing text. Older servers omit the field; a daemon seeing it absent
  falls back to the legacy "`## Squad Operating Protocol` appears in
  instructions" inference (before #4951 no `is_leader_task` was sent).
- no code injects `instructions` into members.

## Issue assignment

Source: `server/internal/handler/issue.go` (assignee validation ~2614-2632),
`squad.go` (`shouldEnqueueSquadLeaderOnAssign` ~990, `enqueueSquadLeaderTask`
~1027), `server/internal/service/task.go`.

- `assignee_type="squad"` routes to `squad.leader_id` (1028-1050);
- backlog assignment does not enqueue (991-993); out-of-backlog moves can
  (990-994 → `isSquadLeaderReady`);
- assignee change cancels existing issue tasks first;
- private leader checked at assign (issue.go:2629-2632) and enqueue
  (`canEnqueueSquadLeader` squad.go:1037); archived squad/leader rejected at
  assign (issue.go:2622-2627); pending-task dedup applied (1042-1048);
- parent status agent-managed: assignment brief (`writeWorkflowAssignment`)
  requires `in_progress` on first turn, forbids unconditional `in_review`;
  `StartTask`/`CompleteTask` never write status; comment-triggered turns may
  name the protocol responsibility as exception.

## Comment / Mention

Source: `server/internal/handler/comment.go` (triggers ~1057-1199, squad
branch ~1352), `squad.go` (~986, `lastTaskWasLeader` ~915), `task.go`
(`EnqueueTaskForSquadLeader`).

- comment on squad-assigned issue wakes the leader via
  `computeCommentAgentTriggers` (1124) / `computeAssignedSquadLeaderCommentTrigger`
  (1162-1199); shared with trigger-preview;
- `mention://squad/<id>` resolves squad, adds leader trigger (1352-1391);
  enqueue targets `squad.LeaderID` only (1104-1112) — no fan-out;
  `is_leader_task=true`;
- self-trigger guards: last-task-was-leader (squad.go:915, 1007),
  member explicit-mention skip (1173-1176, 1177-1179).

## Autopilot

Source: `server/internal/service/autopilot.go`
(`resolveAutopilotLeader` ~617-655, dispatch ~88-111),
`server/internal/handler/autopilot.go` (save-time validation ~845-893).

- executable agent from `squad.leader_id` (639-651);
- save-time `validateAutopilotAssignee` rejects archived squad/leader
  (881-891); dispatch re-runs resolve + `AgentReadiness`; archived squad
  fails closed (`errSquadArchived`, 644-645);
- `create_issue` keeps the issue squad-assigned (88-97); `run_only` creates the
  task directly for the leader (99-106, dispatch at 284).

## Child-done parent trigger

Source: `server/internal/handler/issue_child_done.go`
(`dispatchParentAssigneeTrigger` ~246, `triggerChildDoneSquad` ~304).

- child closing a stage barrier wakes the parent squad leader — one
  `EnqueueTaskForSquadLeader`, no fan-out;
- no self-trigger guard (MUL-3969, mirrors MUL-2808); bounded by
  `HasPendingTaskForIssueAndAgent`;
- no leader-invocation gate: permission-checked at assign
  (`validateAssigneePair`); re-checking stranded process-squad pipelines
  (MUL-4063 / GH #4928). Agent + squad child-done share one ungated path —
  future gates must be added to BOTH.
- parent status not auto-advanced: the system comment asks the leader to
  continue or run `multica issue status <parent-id> in_review`; `done` is
  human/integration owned.

## Private leader access

Source: `server/internal/handler/agent_access.go` (`canInvokeAgent` ~48-108,
`canEnqueueSquadLeader` ~261-267), `squad.go` (gate ~955-974). Invocation gate
(MUL-3963) ≠ view gate `canAccessPrivateAgent`.

- `canEnqueueSquadLeader` delegates to `canInvokeAgent` (48-108); invoker:
  member = itself; agent/system = top-of-chain human (`originatorUserID`
  48-54, `""` when none);
- owner may always invoke (57-59); `permission_mode != "public_to"` is
  deny-by-default (61-65);
- `public_to` (82-106): `workspace` target admits members + internal
  agent/system principals (`workspaceBroad`); `member` requires resolved
  human match; `team` inert in V1;
- wired into `enqueueSquadLeaderTask` (955-974); child-done wake does NOT use
  this gate (MUL-4063).

## Tests

```text
server/internal/handler/squad_assign_trigger_test.go
server/internal/handler/squad_comment_trigger_test.go
server/internal/handler/squad_briefing_test.go
server/internal/handler/squad_private_leader_test.go
server/internal/handler/autopilot_private_leader_test.go
server/internal/handler/squad_no_action_test.go
```

Verification:

```bash
go test ./internal/handler -run 'Test.*Squad|Test.*squad|Test.*Autopilot.*Squad|Test.*ChildDone.*Squad'
```
