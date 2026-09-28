# Provider quota pool holds

Design note for [#6255](https://github.com/multica-ai/multica/issues/6255), scoped to account-level admission and recovery. The broader agent/binding selection design is [#6650](https://github.com/multica-ai/multica/issues/6650). The implementation on this branch uses `provider_quota_pool`, `provider_quota_pool_agent`, and `provider_quota_pool_event`; pool membership is the opt-in rollout boundary. No production pools are created by the migration.

## Contract

A quota pool is an opaque identity for one provider account's usage window. It contains no credential or account secret. A pool may contain agents from more than one workspace or runtime when those agents draw from the same account. Pool membership is explicit: provider and model names cannot prove account identity, and one runtime may host agents from different quota windows.

A held pool admits no new execution. Running tasks continue. Existing queued tasks stay durable but cannot be claimed until release. An issue assignment or mention targeting a held agent returns a visible `provider_quota_held` outcome with the known reset time. It does not archive the agent, alter its invocation permissions, change its model, or silently move the task to another account.

## State

Use a global `provider_quota_pool` row with `id`, `owner_id`, display name, optional provider hint, and timestamps. A separate `provider_quota_pool_agent` mapping associates an agent with at most one pool in the current single-binding model. The membership points to the agent ID, not the runtime ID; a runtime alone can contain several model/account windows. Scope management to the pool owner and validate access to every mapped agent. A future binding model can move the mapping from agent to binding without changing pool identity or hold history.

Store a versioned hold row with pool ID, state (`held_exact`, `held_date`, `reset_unknown`, `probe_due`, `probing`, `probe_backoff`, `open`), source task ID, observed timestamp, reset timestamp or date plus time zone, probe task and attempt count, and revision. Keep an append-only transition/audit record for automatic and manual changes. Exact resets are UTC instants. Date-only resets are calendar dates in the provider account's zone and do not imply an exact hour. A later verified failure may extend the hold; duplicate completion events must be idempotent. Store the normalized failure reason on the audit event, never the raw provider error.

## Admission and execution

The server's existing `AgentReadiness` in `server/internal/service/agent_ready.go` is the shared early verdict for assignment, mentions, and autopilot. Add pool state to that verdict and surface the reset in the refusal response or durable issue notice. The handler and service versions of `shouldEnqueueAgentTask` must remain aligned. A held verdict should not be treated as a broken runtime: it has an automatic recovery path when the reset is known.

Early admission alone has a race with a hold created after enqueue. Filter `ListQueuedClaimCandidatesByRuntime(s)` and guard `ClaimAgentTask` in `server/pkg/db/queries/agent.sql` using the same pool state, so the daemon neither spins on held tasks nor claims one. Recheck in the start transaction for a hold committed after claim but before execution; return an unstarted task to its queue without consuming a concurrency slot. Serialize these decisions with a pool row lock so a hold and a start cannot both win. This covers issue, mention, chat, quick-create, autopilot, and squad work, which share the task queue. A task that was already started before a hold remains untouched. A queued task left behind by a concurrent hold waits for the pool to open and is woken once. No caller may create a second task merely because the first is waiting behind a pool hold.

Enqueue paths in `server/internal/service/task.go` are not one function: `enqueueIssueTaskWithCommentPlan`, `enqueueMentionTaskWithCommentPlan`, quick-create, chat, and rerun have separate writes. Centralize the pool admission check, then cover each entry point in tests. The database claim predicate is the final safety barrier if a caller misses the early check.

## Detection and release

On task finalization, only `agent_error.provider_quota_limit` may create an automatic pool hold. `agent_error.provider_capacity_or_rate_limit` takes bounded backoff and does not hold the account. Use structured provider reset data when available. The narrow `taskfailure.ParseQuotaResetHint` helper handles known exact and date-only text forms as a transition path; keep it guarded by the classified failure reason. Store the source text category and task ID, not credentials or raw logs. An unparseable reset becomes `reset_unknown` and needs an audited operator correction or provider observation.

A server scheduler, independent of agent runtimes, advances exact holds at `reset_at` and date-only holds to `probe_due` on the stated date. Neither transition opens the floodgates: only one bounded probe per pool may run at once. A successful probe opens the pool; a quota failure re-holds it with the new reset; transient provider capacity errors back off without declaring quota success. After release, wake queued tasks once and let the existing task dedup and issue state decide which one is still relevant. An operator may override a hold with an audited reason, but a manual release does not erase failure history.

## Operator API

These authenticated routes require a human actor. A pool owner can create a pool with `POST /api/provider-quota-pools` using `name`, `provider_hint`, and an IANA `timezone`; the migration creates no pools or memberships. `GET /api/provider-quota-pools` lists owned pools. `GET /api/provider-quota-pools/{poolId}` includes mapped agent IDs and recent audit events. `PUT` or `DELETE /api/provider-quota-pools/{poolId}/agents/{agentId}` changes one membership after checking management rights for the mapped agents. An agent can belong to only one pool.

`POST /api/provider-quota-pools/{poolId}/hold` accepts a nonempty `reason` and either an RFC 3339 `reset_at`, a `YYYY-MM-DD` `reset_date`, or neither for an unknown reset. `POST /api/provider-quota-pools/{poolId}/release` requires a nonempty `reason`. Both operations write an audit event. The release endpoint refuses an active probe; cancel its task first so its terminal state cannot reopen the pool later. All reset times are interpreted against the pool timezone where needed. Operators must keep credentials and raw error logs out of the reason field.

## Rollout and tests

1. Ship the schema, owner-scoped management and status API, and claim-time gate with no pool memberships. Seed explicit membership only after comparing the live agent inventory with the account roster. An unmapped agent keeps existing behavior. To roll back one pool, remove its memberships through the API; the audit ledger remains.
2. Enable manual hold on one test pool. Verify direct assignment, mention, chat, quick-create, and automation produce the intended visible wait/refusal; verify an already running task completes and queued work does not start. Release and confirm exactly one wakeup per task.
3. Enable automatic failure detection and reset reconciliation for that pool. Test exact reset, date-only reset, unknown reset, repeated failure, late completion, concurrent enqueue, scheduler restart, and a task whose issue was completed while held.
4. Expand by provider account pool. Alert on an unmapped quota failure, a hold with no reset, a probe loop, or an overdue queued task. Do not map a production agent until the pool owner and provider account are confirmed. An account-wide kill switch and automated alerting are future work; this branch provides per-pool membership removal and audited manual release.

This does not implement cross-provider failover, billing transitions, or mid-run migration. Those need the explicit binding and execution policies in #6650.
