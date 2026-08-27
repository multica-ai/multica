# Model Availability Fallback + Pricing Reaction — Design (ponytail/minimal)

**Base:** `mine/my-fixes` @ `5277f891c` · **Scope:** `server/` (Go). No new services, no new goroutines required for the core loop.

## 1. Problem (grounded in code)

`model_tier_map` (migration `432`) maps `tier → concrete` as a single value. There is **no fallback chain** and **no health/pricing awareness**, so a model outage (hy3 `provider_network`, `muse-spark` 5xx, OpenRouter key limit) blocks every task on that tier until a human runs `PATCH /api/model-map`. Auto-retries make it worse: `CreateRetryTask` (`agent.sql:563`) copies `p.concrete_model` verbatim, so a retried task re-launches on the *same dead model*.

## 2. Schema changes

### 2.1 `model_tier_map` — add ordered fallback chain (migration `434`)
```sql
ALTER TABLE model_tier_map ADD COLUMN fallback_concrete TEXT[] NOT NULL DEFAULT '{}';
```
Keeps `concrete TEXT` as the **primary** (API `GET /api/model-map` stays `tier→concrete`, backward-compatible). `fallback_concrete` is the ordered chain tried after the primary. One array supports both "single fallback" and "list".

`PATCH /api/model-map` body gains an optional sibling map `fallback` → `{"balanced": ["qwen","muse-spark"]}`; primary path unchanged. `modelTierMapToResponse` adds `fallback` field (additive).

### 2.2 `model_health` — new table (migration `435`)
Per `(workspace_id, concrete)` (NULL = global), model-level availability state. Reused by **both** watchers.
```sql
CREATE TABLE model_health (
  workspace_id UUID REFERENCES workspace(id) ON DELETE CASCADE,
  concrete     TEXT NOT NULL,
  status       TEXT NOT NULL DEFAULT 'healthy',   -- healthy | unhealthy
  reason       TEXT,                              -- provider_error | pricing | capacity
  consecutive_failures INT NOT NULL DEFAULT 0,
  last_failure_reason  TEXT,
  last_failure_at      TIMESTAMPTZ,
  last_success_at      TIMESTAMPTZ,
  UNIQUE NULLS NOT DISTINCT (workspace_id, concrete)
);
```
**Recovery is implicit**: resolver treats `unhealthy` as usable again if `now - last_failure_at > healthTTL` (default 10 min) — no probe goroutine needed. A success on that model also flips it back to `healthy` (see §4).

### 2.3 `model_pricing` — new table (migration `436`)
```sql
CREATE TABLE model_pricing (
  concrete TEXT NOT NULL PRIMARY KEY,
  input_usd_per_mtok  NUMERIC,
  output_usd_per_mtok NUMERIC,
  threshold_input_usd_per_mtok NUMERIC,   -- per-model ceiling; NULL = no cap
  fetched_at TIMESTAMPTZ
);
```
Pricing watcher writes here; on breach it flips `model_health` to `unhealthy`/`pricing`.

### 2.4 `agent_task_queue` — observability (migration `437`)
```sql
ALTER TABLE agent_task_queue ADD COLUMN requested_concrete_model TEXT;
```
At enqueue, `requested_concrete_model` = the tier **primary**; `concrete_model` = the health-aware **chosen** candidate. `concrete_model_null` launch path (`daemon.go:5212`) is unchanged. `requested != concrete_model` ⇒ a fallback was used.

## 3. Resolver logic (`resolveConcreteModel`, `task.go:411`)

Replace single lookup with health-aware ordered selection:

```
candidates = [primary] + fallback_concrete[]   // from model_tier_map
for c in candidates:
    h = health(ws,c) ?? health(NULL,c)          // workspace override wins
    if h.status != 'unhealthy' (or expired stale): return c
    if c == primary: record only (do not early-return)
return primary   // all unhealthy → degraded best-effort, never deadlock
```

**Answer to "should the resolver try primary then fallback on failure?":** No launch-time try/catch. The resolver picks a *known-healthy* candidate **up front** (preemptive), so we avoid launching on a dead model at all. Reactivity is provided by the failure hook below, which marks the model unhealthy and causes the **retry** to re-resolve to a fallback. This is simpler than dual-path try/fallback and reuses the existing retry machinery.

Enqueue call site (`task.go:1499`) stays the same; add `RequestedConcreteModel: primary` to `CreateAgentTaskParams`.

## 4. Watcher triggers

### 4.1 Availability (reactive) — hook `FailTask` (`task.go:4915`)
No new goroutine. Inside the existing fail transaction, after `failureReason` is finalised (`:4930-4941`):
- If `failureReason ∈ {provider_network, provider_server_error, provider_capacity_or_rate_limit, model_not_found_or_unavailable}` **and** `parent.ConcreteModel.Valid`:
  - `markModelUnhealthy(ws, concrete, reason)` (upsert `model_health`, bump `consecutive_failures`, set `last_failure_at`).
  - Re-resolve for the retry child: `childConcrete = s.resolveConcreteModel(ctx, ws, tier)` — now skips the just-marked model → returns a fallback.
  - Pass `childConcrete` as a new nullable `concrete_model` override param to `CreateRetryTask` (default `p.concrete_model`). Mirror the same override in `MaybeRetryFailedTask` (`task.go:5657`) and the manual-rerun path (`task.go:6528`).
- On **task success/completion**: `markModelHealthy(ws, concrete)` (clears `unhealthy`). Recovery-on-success + TTL together remove the need for a liveness probe.

### 4.2 Pricing (proactive) — background poller
Model on `channel_media_reconciler.go` (`time.NewTicker` + DB lease, every `pricingPollInterval` = 15 min):
1. Distinct `concrete` from `model_tier_map`.
2. Fetch price (provider `/v1/models` or configured source). Upsert `model_pricing`.
3. If `price > threshold` → `model_health.status='unhealthy', reason='pricing'` + alert; else clear `pricing` unhealthiness.
4. **Webhook alternative** (future, optional): provider price-change POST → same upsert path. Poll is the minimal v1.

## 5. Metrics / alerts (slog + counters)
- `model_fallback_used{workspace,tier,requested,used,reason}` — fallback selected at enqueue/retry.
- `model_health_transition{concrete,from,to,reason}` — unhealthy/healthy flips.
- `model_pricing_breach{concrete,price,threshold}` — pricing alert.
- UI: extend `GET /api/model-map` (or add `GET /api/model-health`) to surface `status`/`reason`/`price` per model.

## 6. Acceptance criteria (for implementation)
1. `go build ./...` in `server/` passes; `sqlc generate` clean after migrations.
2. Unit: `resolveConcreteModel` returns fallback when primary `unhealthy`; returns primary when healthy; returns primary when **all** unhealthy (no deadlock); workspace health overrides global.
3. `FailTask` on `provider_network`/`provider_server_error`/`model_not_found_or_unavailable` marks `model_health` `unhealthy`; the created retry child's `concrete_model` differs from the parent's (fallback used), proving `CreateRetryTask` override works.
4. A subsequent success on that model flips `model_health` back to `healthy`.
5. Pricing poller marks over-threshold model `unhealthy`/`pricing`; resolver skips it (retry/enqueue uses fallback); dropping under threshold recovers it.
6. `concrete_model` launch path (`daemon.go:5212`) is byte-for-byte unchanged; `agent.Model` (the `PUT /api/agents/{id}` manual fix) untouched.
7. `requested_concrete_model != concrete_model` is observable in the DB and the `model_fallback_used` metric fires.

## 7. Out of scope (minimal)
- Live provider liveness probing (TTL + success-recovery suffice for v1).
- Per-workspace pricing sources, multi-provider routing, automatic tier re-budgeting beyond fallback selection.
