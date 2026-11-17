# 🎭 PLAN.md — MultiCa Backend: Go → Swift/Hummingbird Migration Program

> Epic tracker: **HAB-365** (Multica) · Incident that lit the fuse: `multibrain/docs/POSTGRES-PROCESS-NAMING-2026-08-26.md`
> Canon: swift-canon `hummingbird.md` + `opentelemetry.md` · Style: spellbinding-code

## Why Swift (the thesis this program tests)

The 2026-08-26 incident's follow-ups (default `application_name`, env-sized pools, pool
telemetry) were 3 code changes + test cycles in 147k LOC of Go. The bet: in Swift they're
a few lines at the point of pool construction, and the whole stack converges on Andromeda's
toolchain (one language, one canon, one set of skills). Phase 0 exists to prove it measurably.

## Phase 0 — Orchestrion (this session) — ✅ SHIPPED 2026-08-26 (HAB-374)

Verified live on :3640 vs :5442 — /health, /api/pool-stats, /api/census
(Go 25 named + Swift 2 named side by side), /hud.json, /metrics,
/api/events SSE streaming 🌙 heartbeats. `swift build` + 9/9 `swift test` green.

Side-by-side, zero risk to the running Go stack (untouched, port 3637):

- [x] `server-swift/` SwiftPM package: Hummingbird 2.25.1 + PostgresNIO (versions pinned to Andromeda's resolved set where shared)
- [x] **PoolOfSouls** — actor-based PG pool: min/max conns from env (`DATABASE_MAX_CONNS`/`MIN`), warm-up, idle pruning, acquire/empty-acquire counters (pgxpool `Stat()` parity)
- [x] **application_name by default** (HAB-364's ask, done in Swift first): runtime param injected unless URL already carries one — visible in `pg_stat_activity`
- [x] 📊 Telemetry sampler: 15s cadence, emoji logs, ring-buffer history (OTLP export listed for Phase 1)
- [x] Endpoints: `/health`, `/metrics`, `/api/pool-stats`, `/hud.json`, `/dashboard`
- [x] Dashboard: self-contained dark HTML, live-polling, sparkline, connections-by-app-name (shows Go `multica-server` and Swift `multica-swift-server` side by side)
- [x] HUD contract: `/hud.json` compact shape for an AndromedaHUD tile (SwiftUI tile = Phase 1)
- [x] Swift Testing tests: config precedence (URL app-name wins, env clamps), ring buffer

## Phase 1 — Read the Story (next)

- Read-only API slice over the SAME database: `/api/v9/projects`, `/api/v9/issues` list/get
- Route parity harness: golden-file diff Go vs Swift responses (json shape lock)
- AndromedaHUD SwiftUI tile consuming `/hud.json`
- OTLP metrics export (canon opentelemetry.md bootstrap)
- SQLC-equivalent: query layer discipline decision (hand-rolled `Codable` row decoders vs generated)

## Phase 2 — Write the Story

- Auth (session/token middleware parity), write paths for issues/comments
- Migrations runner (171 migrations are idempotent SQL — run under Swift, byte-identical)
- WebSocket/realtime (`hummingbird-websocket`) for the events channel

## Phase 3 — The Daemons

- Scheduler, daemon claim/heartbeat pollers (the pool-pressure source!) — actor model fits
  natively; revisit pool sizing with real telemetry from Phase 0-2 data
- Integrations, autopilot, cloudruntime

## Phase 4 — Cutover

- Shadow traffic: Swift serves reads behind Go, diffed; then writes; then Go retires
- launchd `com.multica.stack` swaps `./bin/server` for the Swift binary; rollback = one line

## Honest risks

- 147k LOC Go + 135k tests won't all move; Phase gates decide, not vibes
- PostgresNIO API churn (pin exact version)
- Daemon pollers are the perf-critical path that justified pgxpool=25 — measure before judging
