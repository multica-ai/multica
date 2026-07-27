# Agent routing policy

## Scope

This slice defines the pure, deterministic policy used to choose an agent model
configuration and an execution topology. It does not fetch provider quota,
mutate agents, dispatch tasks, or promote newly discovered models. Callers must
supply validated candidates, live capacity snapshots, and promoted affinity
evidence.

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

## Acceptance criteria

- Unit tests prove all safety invariants above.
- Rankings and tie breaks are stable regardless of candidate input order.
- More post-task headroom is preferred when quality and other evidence are
  equal.
- The package has no database, network, clock, or task-dispatch side effects.
