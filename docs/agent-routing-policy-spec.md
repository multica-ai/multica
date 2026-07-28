# Agent routing policy

## Scope

This slice defines both the pure, deterministic policy used to choose an agent
model configuration/topology and the transactional task-admission caller that
uses it. It does not fetch provider quota or promote newly discovered models.
An out-of-band collector must publish current owner-plan capacity, and an
operator must explicitly configure validated candidates on each opted-in
agent.

The policy consumes:

- workload risk, urgency, dependency/parallelism shape, and required
  skills/tools/authority;
- candidate provider, model, thinking mode, measured quality/latency, supported
  capabilities, and forecast plan use;
- provider-plan remaining capacity and emergency reserve;
- optional skill affinity records that only influence ranking after independent
  promotion with an evidence revision.

It emits:

- `solo`, `serial`, `bounded_parallel`, or `cross_provider_review`;
- exactly one write-capable lead and zero or more read-only collaborators;
- a deterministic primary ranking and bounded cross-provider fallback order;
- machine-readable rejection reasons for candidates that failed a hard gate.

## Safety invariants

1. Unknown or malformed capacity is unavailable.
2. Ordinary work cannot spend a provider below its configured reserve.
3. Emergency work may consume reserve, but cannot exceed reported remaining
   capacity.
4. Missing skills, tools, authority scopes, protected-role approval, or usage
   forecasts make a candidate ineligible.
5. Experimental/rejected affinity data and promoted data without an evidence
   revision never affect ranking.
6. Cross-provider review requires high-value uncertainty and a distinct eligible
   provider. Parallel and review collaborators are read-only.
7. Fallbacks are cross-provider only and deterministic; execution remains the
   existing exactly-once failover subsystem's responsibility.
8. Admission never changes agent identity, instructions, skills, MCP access,
   custom environment, or authority. It may only override the selected runtime,
   model, thinking level, service tier, provider runtime config, and custom
   arguments for one task.
9. Candidate runtimes must be online, fresh, in the same workspace, and owned
   by the same valid human as the source agent. Protected identities are never
   automatically rebound.
10. Capacity is owner-scoped across workspaces. Admission locks the current
    capacity rows and reserves forecast usage in the same transaction as the
    task route. Every terminal transition releases that forecast reservation.
11. An opted-in task is fenced at insert and cannot be claimed while admission
    is pending. A sweeper repairs the insert-to-admission crash window.

## Transactional admission

Two rollout flags compose:

- `adaptive_agent_routing=false`: resolve an opted-in fence onto the original
  route without evaluating candidates.
- `adaptive_agent_routing=true`,
  `adaptive_agent_routing_active=false`: shadow evaluation; persist the decision
  while executing the original route.
- both `true`: reserve capacity and persist the selected per-task route. If no
  ordinary candidate can preserve reserve, defer for five minutes and
  re-evaluate with a fresh snapshot. Priority-4 emergency tasks may consume
  reserve through the policy's explicit emergency path.

Self-host starts shadow-first. Roll back the actuator immediately with
`FF_ADAPTIVE_AGENT_ROUTING_ACTIVE=false`; disable evaluation with
`FF_ADAPTIVE_AGENT_ROUTING=false`.

The capacity collector upserts `provider_plan_capacity` by
`(owner_id, provider)`. Snapshots older than 30 minutes, unknown snapshots, and
runtime heartbeats older than three minutes fail closed. `remaining_permille`
is the provider-reported current-window remainder; `reserve_permille` is the
operator floor; `reserved_inflight_permille` is maintained by task admission
and terminal-state triggers.

Owner scope is atomic only inside one database. When separate Multica
deployments consume the same paid provider plans, their publishers must use
disjoint allocation shares that sum to 1000 permille (for example, HCX 700 and
LEVER 300). Each publisher floors its share of observed remaining capacity and
ceils its share of the global reserve. This deliberately conservative split
prevents independent databases from each admitting against the full plan
window. Rebalance shares through reviewed configuration; never publish the
same unpartitioned allowance to both databases.

Agents opt in through `runtime_config.adaptive_routing`:

```json
{
  "adaptive_routing": {
    "enabled": true,
    "risk": "medium",
    "required_skills": ["bot-architecture"],
    "required_tools": ["github"],
    "required_authority": [],
    "candidates": [
      {
        "id": "codex-balanced",
        "runtime_id": "00000000-0000-0000-0000-000000000001",
        "model": "gpt-current",
        "thinking_level": "high",
        "quality_bp": 8800,
        "latency_penalty_bp": 100,
        "expected_use_permille": 40,
        "supported_skills": ["bot-architecture"],
        "supported_tools": ["github"]
      },
      {
        "id": "claude-balanced",
        "runtime_id": "00000000-0000-0000-0000-000000000002",
        "model": "claude-current",
        "thinking_level": "high",
        "quality_bp": 9000,
        "latency_penalty_bp": 120,
        "expected_use_permille": 45,
        "supported_skills": ["bot-architecture"],
        "supported_tools": ["github"]
      }
    ],
    "affinities": [
      {
        "skill": "bot-architecture",
        "provider": "claude",
        "status": "promoted",
        "score_bp": 300,
        "evidence_revision": "eval-2026-07"
      }
    ]
  }
}
```

Quality, latency, forecast use, and promoted affinities are governance inputs;
they are not inferred by the request-time path. Model discovery may propose a
candidate, but promotion stays evidence-backed and reversible.

Automatic admission always dispatches one executor. The topology recommendation
remains available to the composition meta-skill, which creates explicit bounded
serial/parallel/review work with one write owner rather than silently spawning
additional plan-consuming branches.

## Acceptance criteria

- Unit tests prove all safety invariants above.
- Real-database tests prove the insert fence, atomic route+reservation,
  concurrent headroom preservation, terminal release, shadow non-mutation,
  stale-capacity failure, and foreign-owner/protected exclusions.
- Rankings and tie breaks are stable regardless of candidate input order.
- More post-task headroom is preferred when quality and other evidence are
  equal.
- The package has no database, network, clock, or task-dispatch side effects.
