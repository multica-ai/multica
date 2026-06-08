# Cerebro Hatchet worker

A single Sliplane container — `multica-hatchet-worker` — that registers with
the self-hosted Hatchet instance at `runs.firtal.com` and hosts cron workflows
for cerebro-owned background jobs.

The image is built from `Dockerfile.hatchet-worker`; the binary lives at
`server/cmd/cerebro_hatchet_worker`. Both are marked CEREBRO-PATCH and tracked
in `docs/cerebro-patches.md`.

## Why a worker (and not one container per job)

Hatchet's model is: one long-running worker process picks up many workflow
runs. Adding a new daily cron does NOT mean adding a new container — it means
adding a `client.NewStandaloneTask(...)` registration inside this worker's
`main()` and redeploying the same image.

So as we accumulate more cerebro crons (`backfill_*`, `cleanup_*`, …) they
land here, alongside the FX refresh, on the same `multica-hatchet-worker`.

## Cron workflows currently registered

| Workflow name                     | Schedule           | Purpose                                                                  |
| --------------------------------- | ------------------ | ------------------------------------------------------------------------ |
| `cerebro-fetch-exchange-rates`    | `30 4 * * *` (UTC) | FIR-43 — daily USD→{DKK,EUR} from ECB via Frankfurter; powers FIR-40.    |

On startup the worker also runs the FX refresh once (bootstrap), so a fresh
deploy populates `cerebro_exchange_rates` within seconds rather than waiting
up to 24 h for the first cron tick.

If the fetch fails (Frankfurter down, network blip, …) the worker logs the
error and leaves the previously-cached rows in place. The display layer reads
the cached row and surfaces `fetched_at` as the freshness signal.

## Required environment

The worker **never holds a database credential.** It fetches rates and POSTs
them to the backend, which is the only writer of `cerebro_exchange_rates`
(FIR-43). So the worker needs an internal backend URL and a service key — not
`DATABASE_URL`.

| Variable                      | Source                                                     | Notes                                                                                          |
| ----------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| `HATCHET_CLIENT_TOKEN`        | Infisical `/runs/HATCHET_CLIENT_TOKEN_MULTICA`             | JWT scoped to the Hatchet tenant; the SDK derives the host from it (gRPC `runs-hatchet.internal:7077`). |
| `CEREBRO_FX_INGEST_URL`       | Internal URL of multica-backend                            | e.g. `http://multica-backend-8nnfh2.internal:8080/api/cerebro/exchange-rates`. Internal Sliplane network, no Cloudflare. |
| `CEREBRO_EXCHANGE_INGEST_KEY` | Shared secret (Infisical)                                  | Bearer token the worker presents. **The same value must be set on the multica-backend service** so it accepts the call. |

The backend (`multica-backend`) must have the matching
`CEREBRO_EXCHANGE_INGEST_KEY` set; when it is unset the ingest endpoint answers
direct loopback callers only and 404s everyone else (same policy as
`/api/cerebro/pricing`).

Optional overrides (rarely needed):

| Variable               | Default                                       | Notes                                                                |
| ---------------------- | --------------------------------------------- | -------------------------------------------------------------------- |
| `CEREBRO_FX_BASE`      | `USD`                                         | Base currency cost is stored in.                                     |
| `CEREBRO_FX_SYMBOLS`   | `DKK,EUR`                                     | Comma-separated targets to refresh. Add a symbol; redeploy.          |
| `CEREBRO_FX_ENDPOINT`  | `https://api.frankfurter.dev/v1/latest`       | Override only for testing or swapping data source.                   |
| `CEREBRO_FX_CRON`      | `30 4 * * *`                                  | UTC. ECB publishes daily ≈15:00 UTC; we settle and fetch the morning after. |

## First-time provisioning (Hatchet token)

The existing `HATCHET_CLIENT_TOKEN_PRICING` in Infisical `/runs` belongs to
the `ecommerce-pricing-engine` worker (a separate product) and must NOT be
reused — workers sharing a token register under the same identity in
Hatchet's scheduler.

A new token is provisioned for the Multica tenant (via the Hatchet API at
`runs.firtal.com` or the dashboard) and stored in Infisical at
`/runs/HATCHET_CLIENT_TOKEN_MULTICA`; reference it as `HATCHET_CLIENT_TOKEN` in
the Sliplane service env. Note Hatchet caps API tokens at 90 days, so this
token needs periodic rotation (re-mint + overwrite the Infisical key).

## Sliplane service

- **Project / server:** the worker must run on the **same Sliplane server** as
  `multica-backend` and `runs-hatchet` (server `Firtal Internal`). Internal
  `.internal` networking is server-scoped, so from there the worker reaches
  both the backend ingest URL and `runs-hatchet.internal:7077` without leaving
  the private network. Project `Multica` is the natural home.
- **Service name:** `multica-hatchet-worker`.
- **Image build:** from this repo, `Dockerfile.hatchet-worker`, root context.
- **Replicas:** 1 (Hatchet handles work assignment; horizontal scale is
  unnecessary for two cron ticks/day at this point).
- **Ports:** none. The worker is outbound-only.
- **Resources:** 0.1 vCPU / 128 MiB is plenty.
- **Restart policy:** always.
- **Env:** the three required vars above. Optional overrides only when needed.

## Verifying the deploy

1. **Container logs** show, in order: `starting hatchet worker`, then
   `bootstrap fx refresh complete` within ~5 seconds of start. (No database
   connection — the worker POSTs to the backend.)
2. **Hatchet dashboard** lists `multica-hatchet-worker` as a connected worker
   with one registered workflow (`cerebro-fetch-exchange-rates`).
3. **Database check** — `fetched_at` on the USD→DKK and USD→EUR rows in
   `cerebro_exchange_rates` advances to today's ECB reference date (written by
   the backend on the worker's POST, replacing the static seed from migration
   9066).
4. **Multica UI** — workspace settings → Visningsvaluta → "Kursdato" matches
   today's ECB date.

## Adding a new cron workflow

1. In `server/cmd/cerebro_hatchet_worker/main.go`, add another
   `client.NewStandaloneTask(...)` registration with its cron expression.
2. Pass it into `hatchet.WithWorkflows(...)` on the `NewWorker` call.
3. Build and redeploy the same Sliplane service (no new container needed).
