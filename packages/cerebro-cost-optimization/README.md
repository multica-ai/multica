# @multica/cerebro-cost-optimization

Cerebro fork-only agent cost-optimization. Single source of truth for the
savings the agent runtime can apply, and the per-workspace mode that controls
each one. FIR-2325.

## The three-state model

Feature flags are boolean. A cost saving is not — we want to measure before we
commit. So each saving has three modes:

- **off** — runs exactly as today. No measurement.
- **shadow** ("kun måling") — runs exactly as today, but additionally computes
  what the saving WOULD have saved. Zero behavior risk.
- **on** — the saving is active; we measure what it ACTUALLY saved against a
  baseline.

## The four savings (first version)

1. `snapshot_prompt` — issue + latest thread in the start prompt; agent skips
   fetching issue/comments each run (~40% of platform calls).
2. `bundled_read` — one combined "issue context" call instead of 4-5 separate.
3. `model_routing` — cheap model by default, escalate only when needed.
4. `prune_tool_results` — drop stale tool results from context mid-run.

## Runtime scope (`runtimeScope`)

A saving only saves where it can act. Two runtimes exist: the **daemon** (local
agents that claim tasks from the server, on a fixed plan) and the **gateway**
(cloud agents the server runs and bills per token).

- `snapshot_prompt`, `bundled_read` → **both** — applied server-side at task
  claim, so they cut platform calls for daemon and gateway alike.
- `model_routing`, `prune_tool_results` → **gateway only** — the server owns the
  model choice and the context window only in the gateway. On the daemon's fixed
  plan they change no cost.

The settings UI badges the gateway-only savings ("Cloud runtime only") with a
one-line note, so a daemon-only workspace sees a scoped toggle — not a dead
button, and no false promise that it touches local agents.

## Owns

- The `COST_SAVINGS` registry (keys, defaults, display metadata, metric).
- The `useSavingMode` accessor and the per-workspace override store.
- The settings UI (`./views` subpath): the three-state control per saving and
  the query/mutation hooks that wire it to the phase-2 endpoint.

## May NOT land here

- The runtime logic that applies a saving — that lives in the runtime (Go
  `server/` and Python `cerebro-inference/`).
- The measurement/attribution logic and the dashboard — separate phases.

## Imports

- Root export (`.`) — the registry + store — imports from `@multica/core` only.
- The `./views` subpath additionally imports `@multica/ui`,
  `@tanstack/react-query`, `lucide-react`, and `sonner` for the settings tab,
  mirroring `cerebro-feature-flags`. It may NOT import from `@multica/views`,
  other `cerebro-*` packages, or `apps/*`.

## Build plan (FIR-2325) — resumable across runs

The full feature spans four codebases and cannot finish in one agent run.
Build it phase by phase on the `feat/FIR-2325-cost-optimization` branch;
each phase is committed and pushed so the next run resumes from git.

- **Phase 1 — Registry contract (this commit).** TS-only leaf package: the
  4 savings, the `off|shadow|on` model, defaults, per-workspace store. No
  behavior change. DONE: `pnpm --filter @multica/cerebro-cost-optimization
  typecheck` passes.
- **Phase 2 — Server persistence.** Go endpoint + migration to store/read
  per-workspace saving modes (mirror the feature-flags overrides table).
  DONE: GET/PUT modes round-trip via the API.
- **Phase 3 — Settings UI (this commit).** "Cost optimization" settings tab
  (`./views`) with a three-state control per saving (off / shadow / on), wired
  to phase 2 and spliced into both `apps/web` and `apps/desktop`. Selecting the
  default mode clears the override (DELETE); any other mode writes it (PUT).
  Admin/owner-gated to match the server. DONE: a human can flip a saving's mode
  and it persists.
- **Phase 4 — Runtime apply + shadow measurement.** Apply each saving in the
  runtime when mode=on; compute would-have-saved when mode=shadow; record a
  per-run measurement row (baseline vs actual). DONE: a run emits a
  measurement row for each non-off saving.
- **Phase 5 — A/B + dashboard.** Randomized holdout (a share of runs run
  without the saving) and a per-saving dashboard: estimated-would-save vs
  measured-actual-save, in kroner. DONE: dashboard shows kr per saving.

Pipeline (set by Jesper on FIR-2325): build = Mia, adversarial test =
Charlotte, review = Tine, approval + merge = Sara. Mia gets Jesper's final
approval before any merge/deploy.
