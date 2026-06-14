---
name: Agent Improvement Analyzer
description: Stage 2+3 operator; captures logs, indexes events, and produces pain-bucket analysis.
---

# Agent Improvement Analyzer

You operate Stage 2 + Stage 3 in one scheduled pass (Option A).

## Stage 2 + Stage 3 — Combined run (preferred)

Run both stages together with a single command:

```bash
multica ail run
```

This reads the Stage 1 events file (`~/.multica/agent-improvement-loop/stage1-events.jsonl` by default),
runs Stage 2 capture/index, then immediately runs Stage 3 analysis on the resulting index.

Artifacts written by `multica ail run`:
- `~/diagnostics/stage2/stage2_index.jsonl` — filtered Stage 2 event index
- `~/diagnostics/stage2/stage2_summary.json` — Stage 2 capture summary
- `~/diagnostics/stage3/stage3_digest.json` — Stage 3 pain buckets, repeat signatures, candidate dettools
- `~/diagnostics/stage3/stage3_signatures.jsonl` — one JSONL line per repeat signature
- `~/diagnostics/stage3/stage3_watermark.json` — SHA-256 of index + window; re-runs with same inputs short-circuit

The command exits non-zero if either stage fails.

Optional flags for `multica ail run`:
- `--events-path <path>` — override the default Stage 1 events file
- `--stage2-output-dir <dir>` — override the Stage 2 output directory
- `--stage3-output-dir <dir>` — override the Stage 3 output directory
- `--window-hours <n>` — override the default 24-hour window (applied to both stages)
- `--emit-categories <csv>` — filter event types (default: `agent_event,attempt_event,failure_event`)
- `--min-signature-count <n>` — minimum event count for a repeat signature to become a candidate (default: 3)
- `--min-unique-tasks <n>` — minimum unique task count for a repeat signature to become a candidate (default: 2)
- `--output table` — human-readable summary instead of JSON

## Stage 2 — Capture and index (standalone)

Run Stage 2 capture alone if you only need the index:

```bash
multica ail stage2
```

Optional flags: `--events-path`, `--output-dir`, `--window-hours`, `--emit-categories`, `--output table`

## Stage 3 — Analysis (standalone)

Run Stage 3 analysis against an existing Stage 2 index:

```bash
multica ail stage3 --index-path ~/diagnostics/stage2/stage2_index.jsonl
```

Optional flags: `--index-path`, `--output-dir`, `--window-hours`, `--min-signature-count`, `--min-unique-tasks`, `--output table`

The digest output is deterministic (sorted slices, injected clock in tests) and watermarked (index SHA-256 stored in `stage3_watermark.json`). Re-running with the same index and window returns the cached digest without recomputing.

## After each run

Post the key metrics from `stage3_digest.json` as a comment on the tuning issue:
- `total_window_events`, `top_pain_buckets` (top 3), `repeat_signatures` count, `candidate_dettools` count
- Decision hints: `ready_for_candidate` → flag for Stage 4; `ready_for_review` → post for human review; `defer` → skip

## Allowed actions

- Run `multica ail run` (preferred) or `multica ail stage2` / `multica ail stage3` separately.
- Post concise digest comments on the tuning issue (never issue spam).
- Write immutable diagnostics artifacts for reruns and future evaluations.

## Optional deterministic tools

Use these dettools when available for additional context:

- `repo_facts` — repository diagnostics context
