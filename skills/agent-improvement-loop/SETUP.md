---
name: Agent Improvement Loop - Setup
description: Configure staged loop agents for nightly telemetry, dettool prospecting, and evaluation feedback.
---

# Agent Improvement Loop Setup

This folder contains the dedicated skills used to run and evolve the agent improvement loop.

## Agents

Create dedicated agents:

- `Agent Improvement Analyzer` (skills: `analyzer`)
- `Agent Improvement Evaluator` (skills: `evaluator`)

Each agent should run only as part of Autopilot schedules defined below.

## Deterministic tools to import

Keep base production dettools imported from repo catalog:

- `pipeline_state_parse`
- `repo_facts`
- `diff_summarize`
- plus the Stage-4 evaluation tool(s) once promoted.

Staging and production folders are both repo-local:

- `dettools/*.go` (production catalog)
- `dettools/prospect/*.go` (candidates, not imported)

## Stage 6 candidate generation

Generate prospect candidates only after a human approval reference exists:

```bash
multica ail stage6 \
  --candidate-json <stage5-tool-contract.json> \
  --human-approve-ref <issue-or-comment-ref> \
  --owner <team-or-person>
```

Alternative from a Stage 3 digest:

```bash
multica ail stage6 \
  --stage3-digest diagnostics/stage3/stage3_digest.json \
  --tool <suggested_name> \
  --human-approve-ref <issue-or-comment-ref> \
  --owner <team-or-person>
```

The command writes `dettools/prospect/<tool>_candidate.go`, a matching `_test.go`, and a `candidate` entry in `dettools/prospect/manifest.json`. Keep candidates in prospect until Stage 8 promotion.

## Autopilot bootstrap

### Stage 2 + 3 (nightly)

**Autopilot agent prompt (current):** `MULTICA_AIL_TUNING_ISSUE_ID=<issue-id> multica ail run` — runs Stage 2 capture + Stage 3 analysis in one process (Option A), then writes and posts the Stage 5 digest. Stage 3 artifacts: `stage3_digest.json`, `stage3_signatures.jsonl`, `stage3_watermark.json` under `~/diagnostics/stage3/`. Stage 5 artifacts: `stage5_digest.json`, `stage5_watermark.json` under `~/diagnostics/stage5/`. The digest issue can also be supplied with `--digest-issue <issue-id>`.

```bash
AUTOPILOT_NAME="Agent Improvement Loop Stage2-3"

multica autopilot create \
  --title "$AUTOPILOT_NAME" \
  --description "Run nightly Stage 2 capture/index + Stage 3 analysis (Option A same workflow), then post stage summary on tuning issue." \
  --agent "Agent Improvement Analyzer" \
  --mode run_only

# add schedule later with returned AUTOPILOT_ID
multica autopilot trigger-add "<AUTOPILOT_ID>" --cron "0 2 * * *" --timezone "UTC"
```

### Manual intervention trigger

Use normal manual trigger while tuning:

```bash
multica autopilot trigger <AUTOPILOT_ID>
```

## Stage 8 promotion routine

Use the single transactional helper before moving/importing manually:

```bash
scripts/stage8-promote.sh --tool <tool_name> --approve-ref <issue-or-pr> [--force] [--skip-import] [--dry-run]
```

The script performs all Stage 8 steps atomically:

1. Move candidate source from `dettools/prospect/` into `dettools/` (candidate name normalization supported).
2. Update `dettools/prospect/manifest.json` (`status`, `promoted_at`, `human_approve_ref`, `tool_name`, optional `git_commit`).
3. Run `multica dettool import-file dettools/<tool>.go --output table` (unless `--skip-import`).
4. Validate required skill files are present in the repo:
   - `skills/agent-improvement-loop/analyzer.md`
   - `skills/agent-improvement-loop/evaluator.md`
   - `skills/agent-improvement-loop/SETUP.md`
5. Append an immutable, replayable promotion record to:
   - `diagnostics/stage8-promotion.jsonl`

Useful flags:

- `--candidate dettools/prospect/<tool>_candidate.go` (if you need to override auto-detection)
- `--diagnostics /path/to/file` (override audit output path)
- `--required-skill-file <path>` (repeatable)
- `--force` (overwrite existing production file after backup)

## Skill update rule

When Stage 4 promotes a candidate to reusable dettool:

1. Move candidate source `dettools/prospect/*.go` to `dettools/*.go`.
2. Refresh `dettools/README.md` if catalog grows.
3. Update this `/skills` folder definitions if `analyzer.md` / `evaluator.md` needs the new dettool in required imports.
4. Keep diagnostics bundles under `diagnostics/` for evaluation history.

Apply in this order on every promotion:

1. prospect -> production (`dettools/prospect` -> `dettools`)
2. `multica dettool import-file`
3. skill file update (`/skills/agent-improvement-loop/*`)
4. append immutable diagnostics entry
