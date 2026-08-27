# Autonomy Runbook (fork features)

One-page operations guide for the autonomy stack merged to `main` @ `035f46887`
(migrations 432–435, 437; PRs #39 #42 #44 #45 #46 #47). Behavior details live in
`docs/model-availability-fallback-design.md`.

## How autonomy works

A task normally runs to completion. When it doesn't, the platform escalates in
this order:

1. **Retry** — transient failures (`provider_network`, `provider_server_error`,
   model-unavailable, etc.) spawn a retry child in the same transaction as the
   fail. The retry re-resolves the model (see fallback below), so it lands on a
   working model instead of the dead one.
2. **Fallback** — `resolveConcreteModel` picks a *known-healthy* candidate up
   front: the tier primary, then the `fallback_concrete` chain, skipping any
   `unhealthy` model. `FailTask` marks the failed model `unhealthy`; `CompleteTask`
   marks it `healthy` again. Unhealthy rows also auto-recover after a TTL
   (default 10m) — no liveness probe needed.
3. **Auto-rerun** — if retries are exhausted on a transient model failure, exactly
   one auto-rerun fires (`auto_rerun_count`) instead of dead-ending the task.
4. **Help signal → inbox** — if the agent is genuinely blocked it emits
   `agent_requested_help` (a `help_signal` with `blocked_reason` / `needs` /
   `confidence` on `/fail` or `/complete`). That is **excluded from retry and
   auto-rerun** and produces an `agent_help_requested` inbox item
   (severity `action_required`) for the task originator. A human picks it up.

Pricing: `model_pricing_watcher` polls every 15m; a price breach flips the model
`unhealthy`/`pricing` with a **sticky downgrade** (`last_failure_at` pushed 365
days ahead) so it stays down until the price recovers.

## Mark a model unhealthy (manual override)

Use the CLI, or the API for automation:

```bash
# CLI (workspace defaults to current when --workspace omitted)
multica model-health set --workspace <ws-id> --model muse-spark --status unhealthy --reason pricing
multica model-health set --model mimo --status healthy          # clear
multica model-health get --global                               # inspect
```

```http
PUT /api/model-health
Content-Type: application/json

{ "workspace_id": "<ws-id>", "concrete_model": "muse-spark", "status": "unhealthy", "reason": "pricing" }
```

`GET /api/model-health[?workspace=...]` lists current health.

Marking a model unhealthy makes the resolver skip it for that workspace (and
globally for `NULL` workspace_id rows) on the next enqueue/retry. This is the
fastest way to route around an outage without touching per-agent config.

### Arm a drill fault without a session token

If the admin PAT is expired (REST returns 401) you can inject the same fault
directly in Postgres — the resolver reads `model_health` from the DB, so this is
functionally identical to `PUT /api/model-health`. Make it **sticky** (push
`last_failure_at` a year out) so the 10m stale-TTL can't auto-heal it during the
drill.

```bash
WS=dc85f04e-b671-457f-808f-b9a666ac6063   # multica-dev
docker exec multica-postgres-1 psql -U multica -d multica -c "
UPDATE model_health SET last_failure_at = now() + interval '365 days',
       status='unhealthy', reason='drill', consecutive_failures=1
WHERE concrete='hy3-free'
  AND (workspace_id IS NOT DISTINCT FROM '$WS'::uuid OR workspace_id IS NULL);
"
```

Disarm (flip healthy again, workspace + global):

```bash
docker exec multica-postgres-1 psql -U multica -d multica -c "
INSERT INTO model_health (workspace_id, concrete, status, reason, consecutive_failures, last_success_at, updated_at)
VALUES (NULL,'hy3-free','healthy',NULL,0,now(),now())
ON CONFLICT (workspace_id, concrete) DO UPDATE SET status='healthy', reason=NULL,
  consecutive_failures=0, last_success_at=now(), updated_at=now();
"
# repeat with workspace_id = '$WS'::uuid for the workspace-scoped row
```

## Set fallback chains

```bash
# Global (NULL workspace_id) and per-workspace overrides
multica model-map set-fallback --global --balanced qwen,muse-spark
multica model-map set-fallback --workspace <ws-id> --premium claude,kimi-k2.6
multica model-map get-fallback --global
```

API: `GET|PATCH /api/model-map/fallback` with body
`{ "balanced": ["qwen","muse-spark"], "premium": ["claude","kimi-k2.6"] }`.
The primary concrete per tier is still set via `multica model-map set` /
`PATCH /api/model-map`.

## Handle `agent_requested_help`

When an agent is stuck it raises an inbox item of type `agent_help_requested`.
To clear it:

1. Open the inbox item. Its `details` carries `task_id`, `agent_id`,
   `blocked_reason`, `needs`, and `confidence`.
2. Provide what the agent listed in `needs` (e.g. set the missing secret in the
   agent's `custom_env`, grant access, clarify the requirement).
3. Re-run the task (manual rerun, or let the agent retry once the blocker is
   gone). The new task run is a fresh attempt — it is not auto-retried because
   the original reason was `agent_requested_help`.
4. Resolve/close the inbox item once the agent completes.

If `needs` points at a missing `custom_env` secret, set `MULTICA_ENV_ENC_KEY` on
the server first (see `SELF_HOSTING_ADVANCED.md`) so the secret is stored
encrypted at rest.

## Quick reference

| Symptom | Action |
|---|---|
| Model outage stalling tasks | `model-health set --status unhealthy` (or let `FailTask` do it) |
| Need a standby model | `model-map set-fallback` |
| Price spike downgrading a model | confirm `model_health.reason='pricing'`, lower threshold or wait for recovery |
| Agent stuck, inbox item `agent_help_requested` | satisfy `needs`, re-run, close item |
| Disk filling on host | daily `docker image/container prune` (04:00, `com.scotthawes.docker-cleanup`) |
