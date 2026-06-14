# Agent Improvement Loop (Deterministic Tool Feedback Plane)

## Goal
Build a repeatable loop that observes where agents fail or stall, clusters those events, and promotes deterministic tools (dettools) where they reduce retries/loops.

This is intentionally aligned to Multica’s deterministic tool plane and fork-safe
agent workflow.

---

## Current status checkpoint

_Last updated: 2026-06-14 (verified against commit d78f4f7493dc34c1b8a0bd1b3201158844ce517d on branch ail-stage1-2-status-2026-06-14)_

### Completed in repo

- **Stage 1 — Implemented.** Telemetry is integrated into core task lifecycle in `TaskService` (`server/internal/service/task_stage1_telemetry.go`). Emits normalized JSONL lifecycle events with full env/config controls (`MULTICA_AIL_STAGE1_ENABLED`, `MULTICA_AIL_STAGE1_EVENTS_PATH`, `MULTICA_AIL_STAGE1_EMIT_CATEGORIES`, `MULTICA_AIL_STAGE1_CONFIG`). Tests pass.
- **Stage 2 code — Implemented (code only).** Capture/index logic exists in `server/internal/ail/stage2.go` (`RunStage2Capture`, `Stage2Config`, `Stage2Result`) with tests in `stage2_test.go`. Outputs: `diagnostics/stage2/stage2_index.jsonl` and `diagnostics/stage2/stage2_summary.json`.
- **Stage 8 promotion script — Implemented.** `scripts/stage8-promote.sh` moves prospect → production, updates `dettools/prospect/manifest.json`, runs `multica dettool import-file`, and appends `diagnostics/stage8-promotion.jsonl`.
- **AIL skills and runbooks** — `skills/agent-improvement-loop/{analyzer.md,evaluator.md,SETUP.md}` present.
- **Architecture choice rule 1 (Stage 1 always-on)** — honored via TaskService integration.

### Current verification status

- `go test ./internal/service -count=1` ✅ (`/home/ethanturk/multica/server`)
- `go test ./internal/ail -count=1` ✅ (`/home/ethanturk/multica/server`)
- `go test ./internal/service ./internal/ail -count=1` ✅
- `grep -rn "RunStage2Capture" --include="*.go"` — no production caller outside `stage2.go` itself ✅ (confirms wiring gap)
- `grep -rn "agent_improvement_capture|agent_improvement_analyze|agent_improvement_evaluate" --include="*.go"` — no results ✅ (confirms Stages 3–4 dettools absent)
- `dettools/prospect/manifest.json` has `items: []` ✅ (confirms Stage 6 scaffold absent)

### Outstanding (unimplemented gaps — one follow-up task each)

1. **Stage 2 wiring** — No CLI subcommand in `server/cmd/multica/`; no autopilot definition; `RunStage2Capture` has zero production callers.
2. **Stage 3** — No log-analysis Go code; `agent_improvement_analyze` dettool absent.
3. **Stage 4** — No candidacy evaluation code; `agent_improvement_evaluate` dettool absent; no `ready_for_candidate / ready_for_review / defer` logic.
4. **Stage 5** — No digest-reporting code; no `dettool.none` fail-safe path.
5. **Stage 6** — No candidate scaffold generator; `dettools/prospect/manifest.json` is empty.
6. **Stage 7** — No replay/evaluation harness; no determinism profile; no replay filters.
7. **Stage 8 diagnostics** — Missing `diagnostics/stage-summary.jsonl`, `diagnostics/candidate-decision.json`, `diagnostics/rerun-manifest.json`; no baseline telemetry comparator; no 30-day re-evaluation trigger.
8. **Architecture choice rules 2–4** — Depend on the Stage 2 wiring, Stage 3, and Stage 4 follow-up tasks above.

## Architecture choice (by stage)

1. **Stage 1 should be always-on** — yes, this belongs to normal agent runtime execution and should happen automatically on every task.
   - Every task lifecycle event, failure, attempt, and dettools invocation should already be emitted by the normal daemon/server paths.
   - Stage 1 is therefore mostly *instrumentation parity*: ensure the schema is normalized and complete, not a separate periodic job.

2. **Stage 2 should be scheduled**:
   - Yes, schedule it (nightly is a good default, e.g. `0 2 * * *` UTC) unless you want faster feedback.
   - Prefer running it as a dedicated **Multica Autopilot + agent** in `run_only` mode so it can reuse the deterministic tool plane and avoid issue noise.
   - Give it dedicated dettools for "window capture + indexing" (`agent_improvement_capture` etc.).
   - Run concurrency policy should usually be `skip` (or `queue`) with `run_only` so overlapping runs don’t duplicate output.

3. **Stage 3 should run immediately after Stage 2 in the same workflow** (Option A):
   - Single scheduled run does Stage 2 (capture/index) then Stage 3 (analysis) in sequence, so no cross-autopilot race.
   - Keep the Stage 3 runner idempotent by watermarking the index window it analyzed.

4. **Stage 4 should indeed be a deterministic tool + loop**:
   - Yes: make Stage 4 a dettool (stable schema, bounded runtime, stable error codes).
   - Pair with a dedicated agent loop that consumes its output and advances `ready_for_candidate / ready_for_review / defer` decisions.
   - This gives you human-in-the-loop control without letting model drift decide pipeline state.

---

## 1) Logging of agents

### What to log
Collect a stable, machine-friendly event stream at issue/task granularity.

Stage 1 telemetry writes normalized lifecycle records from the normal server
execution path to JSONL by default:

- default path: `~/.multica/agent-improvement-loop/stage1-events.jsonl`
- env override: `MULTICA_AIL_STAGE1_EVENTS_PATH`
- toggle: `MULTICA_AIL_STAGE1_ENABLED=false` (opt-out, only for incidents)
- emit-category override: `MULTICA_AIL_STAGE1_EMIT_CATEGORIES` (comma or space separated) 
  defaults to `agent_event,attempt_event,failure_event`

Canonical event categories:

 - `agent_event`
 - `attempt_event`
 - `failure_event`

At minimum:

- `task.id`, `task.status`, `attempt`, `max_attempts`
- `task.failure_reason` and final `result`
- `issue.id`, `provider/runtime`, `agent.id`, `workspace.id`
- `run.duration_ms`, `retry_count`, `next_retry_at`
- `error.message` and **normalized** stack/token indicators
- `tool_name` (if used), `dettool` and `tool_args_hash`
- `model` + `source` + `runtime_mode`

### Where to source
- Daemon run logs (for per-task lifecycle and completion failure reasons)
- Postgres-backed task tables:
  - `agent_task_queue` / task history
  - `chat_message` for chat-facing failures
- Daemon stdout/journal lines for transport-level failures
- Existing deterministic tool invocation envelopes (`dettools` result/error_code/retryable)

### Logging rule
Each agent attempt should emit one compact line/object with a deterministic schema.
No free-text parsing dependency in stage 2+.

Current Stage 1 emit points (phase 1): lifecycle + terminal transitions from
`task:queued`, `task:dispatch`, `task:running`, `task:completed`,
`task:failed`, and `task:cancelled`.

---

## 2) Log capture and indexing

### Ingestion sources
- Daemon logs (already collected)
- CLI/server logs (watchdog + scheduler)
- Task tables / run metadata exports
- Container logs where local runtimes execute
- PI/session artifacts and JSONL (for secondary evidence)

### Indexing output
Create a local index artifact (JSONL + optional SQLite index) per window:

- `agent_event`
- `attempt_event`
- `failure_event`
- `tool_event`
- `loop_signal`

Minimal canonical event shape:

```json
{
  "ts": "2026-06-14T09:22:10Z",
  "workspace_id": "...",
  "agent_id": "...",
  "issue_id": "...",
  "task_id": "...",
  "provider": "codex",
  "status": "failed|running|completed",
  "attempt": 2,
  "max_attempts": 3,
  "failure_reason": "agent_error",
  "error_signature": "E_FILE_READ",
  "loop_signature": "install_loop|test_loop|permission_loop",
  "dettools_used": ["repo_facts", "diff_summarize"],
  "source": "daemon|chat|task_db|container|pids",
  "raw_ref": "/path/to/source.log:12345"
}
```

---

## 3) Log analysis for repeated/difficult operations

### Candidate “pain” signals
- high retry ratio by `(workspace, agent, failure_reason)`
- attempts near `max_attempts`
- recurring loops (`loop_signature` repeated with short interval)
- repeated `status!=running` oscillation
- high time-to-first-token after task dispatch
- long-running tasks with no progress markers
- repeated user-facing clarification comments

### Analysis outputs
- `trouble_buckets` (ranked)
- `repeat_signatures` with count, recency, and representative examples
- `candidate_dettools` ranked by expected determinism gain

---

## 4) Evaluate situations for dettools

For each candidate bucket:
1. Is the failure domain structured and deterministic?
2. Can a deterministic function replace a common inference step?
3. Is required context available from task inputs + repository + runtime context?
4. Are outputs naturally machine-readable and replayable?
5. Is a tool failure mode safer than silent hallucination?

Gate:
- Only progress if expected false-positive risk is low.
- Prefer deterministic validators/parsers before generators.

Decision tags:
- `ready_for_candidate` (1-off candidate)
- `ready_for_review` (needs human)
- `defer` (not yet reliable)

---

## 5) Report possible dettools (human interaction here)

Produce a human-readable digest each run with:
- Top 5 pain signatures
- Confidence + risk score
- Suggested tool names and signatures
- Example input/output contract for each
- One-click generation package for human acceptance
- **Fail-safe behavior:** if signal count > 0 and `recommended_candidate_count = 0`, post an issue alert tagged `dettool.none` so humans see there is still work pressure but no safe deterministic opportunity yet.

Output target:
- Multica issue comment on a designated tuning issue
- Optional dashboard artifact in repo/workspace logs

---

## 6) Generate dettools for human indicated situations

When a human marks a candidate `approve`, generate a scaffold in a deterministic form:
- `name`, `description`, `input schema`, `machine_data`
- strict input validation (`DisallowUnknownFields` style)
- bounded IO and explicit error taxonomy (`INVALID_INPUT`, `MISSING_DEPENDENCY`, `TIMEOUT`)
- artifact outputs (if needed) instead of huge inline blobs

Scaffold policy:
- add under `dettools/prospect/` as a `*_generated.go` or `*_candidate.go`
- include short unit test harness + example invocation
- keep candidates in `dettools/prospect/manifest.json` with metadata (`status`, `human_approve_ref`, `owner`, `source_cluster_id`)
- append to an integration list for agent import

---

## 7) One-off dettool evaluation

Evaluation protocol:
1. Synthetic fixtures representing top pain signatures
2. Real issue/task replay sample from index
3. Metrics:
   - success-on-retry delta
   - reduction in failed retries
   - precision (tool returns actionable output)
   - invocation cost/cadence
4. Safety checks:
   - deterministic parse
   - stable schema
   - idempotent behavior on re-run

5. Reproducible re-run support:
   - Evaluation runs must support replay filters to rerun only a subset of logged events:
     - `event_ids`: explicit IDs to replay
     - `issue_ids`: specific issue/task filters
     - `agent_ids`: specific agents
     - `time_range`: `[start, end)` UTC window
     - `failure_reasons` + `loop_signature`
   - A failed evaluation can be rerun with the same filters and `determinism_profile` (tool args + env + git revision + input checksum) so the result is reproducible.

If pass thresholds → promote; if not, archive with reason.

---

## 8) Create reusable dettool and wire into agents

Promotion steps:
1. **Promote artifact**: Use transaction helper:
   `scripts/stage8-promote.sh --tool <tool> --approve-ref <ticket-or-pr> [--force] [--skip-import]`
   This moves source from prospect to production and updates manifest in one step.
2. **Verify manifest**: helper writes `status: promoted`, `promoted_at`, human approval/reference and optional `git_commit` in `dettools/prospect/manifest.json`.
3. **Import dettool** into workspace:
   `multica dettool import-file dettools/<tool>.go --output table`
4. **Update skills in repo folder** (`multica/skills/agent-improvement-loop/*`):
   - refresh `analyzer.md` and `evaluator.md` if either skill should now require the new tool
   - keep `SETUP.md` canonical for required/optional Stage-2..8 dettool lists
5. **Publish rollout config** for candidate usage:
   - stage tool visibility to selected agents only
   - keep fallback non-dettool behavior enabled during the first validation window
6. Archive diagnostic evidence bundle before/after promotion (never delete):
   - per-stage summary (`diagnostics/stage-summary.jsonl`)
   - candidate decision rationale (`diagnostics/candidate-decision.json`)
   - rerun manifest (`diagnostics/rerun-manifest.json`) with fingerprinted tool inputs
   - Stage-8 promotion log (`diagnostics/stage8-promotion.jsonl`)
7. Add telemetry after promotion and compare against pre-promotion baseline:
   - `dettool.hit_rate`
   - `tool_fail_rate`
   - `retry_ratio_after_tool`
8. Record a final immutable stage artifact under `/home/ethanturk/multica/diagnostics/` so future reviews can rerun the exact promotion evidence.

Keep diagnostics artifacts in a persistent path (never overwrite):
- `/home/ethanturk/multica/diagnostics/` (create if missing)
- one immutable JSONL per run and one indexed SQLite row per decision, if SQLite is enabled.

Guardrails:
- Keep tooling additive; no agent-specific hard dependency on single tool
- Always allow model fallback to plain workflow when dettool fails
- Re-evaluate every 30 days or after 2 failed promotion waves

---

## Suggested implementation sequence

1. Start with **Stage 1+2** (collection + indexing) for 7 days.
2. Stand up Stage 3 analyses weekly; auto-annotate top signatures.
3. Add Stage 5+6 human review loop.
4. Pilot 1 deterministic tool from Stage 3 signal.
5. Promote after 3 consecutive successful one-off evaluations.

---

## Minimal command workflow

- Review and ship index: `python`/`jq`/`dettools`-style aggregator run (first version can be cron-driven)
- Human gate review via normal issue workflow
- Candidate generation using deterministic import pipeline
- Promotion with `multica dettool import-file`

---

## Open architecture notes

- Use short structured signatures (`error_signature`, `loop_signature`) to keep clustering stable.
- Keep raw text logs immutable; keep index as a derived append-only artifact.
- Store only redacted identifiers in exported artifacts; never persist secrets.
