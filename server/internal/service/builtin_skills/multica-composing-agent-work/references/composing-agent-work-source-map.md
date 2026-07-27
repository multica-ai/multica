# Composing agent work source map

## Built-in delivery

- `server/internal/service/builtin_skills.go` embeds every directory under
  `builtin_skills`, loads its `SKILL.md` and supporting files, and returns it
  from `BuiltinSkills`.
- `server/internal/service/task.go` appends built-in skills to workspace-bound
  skills in `LoadAgentSkillBundles`.
- `server/internal/handler/daemon.go` includes built-in skills in the claimed
  task payload.

## Agent, skill, and runtime inventory

- `server/cmd/multica/cmd_agent.go` registers `agent list`, `agent get`, and the
  `agent skills` subcommands.
- `server/pkg/db/queries/skill.sql` implements enabled agent-skill inventory via
  `ListAgentSkills`, `ListAgentSkillSummaries`, and
  `ListAgentSkillNamesByAgentIDs`.
- `server/cmd/multica/cmd_runtime.go` registers `runtime list`,
  `runtime usage <runtime-id>`, and `runtime activity <runtime-id>`.
- `runRuntimeUsage` calls `/api/runtimes/{id}/usage?days=N`.
- `server/pkg/db/queries/runtime_usage.sql` aggregates historical token usage by
  date, provider, and model. It does not read provider subscription allowance
  or reset-window data.

## Serial and parallel child work

- `server/cmd/multica/cmd_issue.go` exposes `issue create --parent --stage
  --status --assignee-id` and `issue children`.
- `server/internal/handler/issue_child_done.go` implements
  `stageBarrierClosed` and wakes the parent only after the lowest unfinished
  stage reaches terminal state.
- `server/internal/service/builtin_skills/multica-working-on-issues/SKILL.md`
  documents the exact `todo` versus `backlog` enqueue behavior and stage
  promotion workflow.

## Side-effect ownership and provider handoff

- `server/pkg/providerfailover/controlplane.go` defines deterministic
  `task_spawn` and `stage_promotion` effect keys.
- `server/internal/service/provider_failover.go` owns persisted provider
  handoff state and fallback dispatch.
- `server/internal/service/issue.go` claims the guarded `task_spawn` effect for
  agent-created child work.
- `server/internal/handler/issue.go` claims the guarded `stage_promotion`
  effect before activating staged children.
- Together those claims prevent original and fallback tasks from duplicating
  the same control-plane side effect.

## Deterministic routing policy

- `server/pkg/agentroute/policy.go` defines the pure workload, candidate,
  provider-capacity, and promoted skill-affinity inputs.
- `agentroute.Route` rejects unknown capacity and capability/authority gaps,
  preserves an emergency reserve, ranks eligible candidates deterministically,
  and chooses `solo`, `serial`, `bounded_parallel`, or
  `cross_provider_review`.
- The resulting assignments contain exactly one write-capable lead; explorers
  and cross-provider critics are read-only.
- `server/pkg/agentroute/policy_test.go` locks the fail-closed gates,
  aggregate parallel budget, evidence promotion boundary, topology rules,
  deterministic tie breaks, and cross-provider fallback ordering.
- This package has no task-admission or dispatch side effects. Capacity
  collection and service integration remain separate work.

## Skill conformance

- `server/internal/service/builtin_skills_test.go` enforces strict YAML,
  required frontmatter, body-size limits, supporting-file hygiene, and the
  composition-contract anchors for this skill.
