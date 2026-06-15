# Eidetix with/without eval

Measures whether the Eidetix shared-context graph improves agent outcomes,
across three axes: **quality + grounding**, **consistency**, **efficiency**.
Protocol + rationale: `docs/superpowers/specs/2026-06-15-eidetix-eval-design.md`.

Default scale: 6 tasks × 2 arms × 3 trials = **36 real Claude Code runs**, serial.

## Files
- `probe_graph.py` — GATE A. Confirms the real Marketing graph is populated.
- `tasks.json` — the 6-task suite. **Finalise from probe output** before running.
- `run_eval.py` — runs the matrix via the `multica` CLI + DB telemetry → `results/runs.jsonl`.
- `judge.py` — blind scoring + aggregation → `results/report.json`.

## Prerequisites (full local stack)
1. **Local backend on this branch**, with `MULTICA_EIDETIX_SECRET_KEY` set (already in `.env`). Start it: `make dev` (watch for `eidetix integration enabled`).
2. **A local daemon running a Claude Code runtime**, with the provider's API key available. The runtime must be online in the workspace. (Claude Code is transport-verified for remote MCP; do not substitute an unverified provider — it would silently drop the eidetix server and contaminate the WITH arm.)
3. **An eval agent** created on that Claude Code runtime — note its UUID (`multica agent list`).
4. **A bound project** with the real Marketing token set:
   `printf '%s' "$EIDETIX_TOKEN" | multica project eidetix set <project-id> --token-stdin --label Marketing`
5. **`multica` CLI built + configured**: `make build`; `multica config set server_url http://localhost:8080`; `multica config set workspace_id <ws>`; `multica login --token <PAT>`.

## Run order
```bash
# GATE A — must say POPULATED, else stop
pip install "mcp>=1.2"
export EIDETIX_TOKEN='<Marketing token>'
python3 evals/eidetix/probe_graph.py
# → then edit tasks.json so each task depends on facts the graph actually holds,
#   and fill graph_facts_expected (the judge's grounding key).

# Run the matrix (serial; ~tens of minutes depending on task length)
export EVAL_PROJECT='<bound project id>'
export EVAL_AGENT_ID='<eval agent uuid>'
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
export EVAL_TRIALS=3
python3 evals/eidetix/run_eval.py     # → results/runs.jsonl

# Score
pip install anthropic
export ANTHROPIC_API_KEY=...
python3 evals/eidetix/judge.py        # → results/report.json + printed summary
```

## Reading the result
- **Quality/Grounding WITH > WITHOUT** and **pairwise win-rate > 0.5** → shared context improves output, especially grounding (using the team's real facts).
- **Consistency WITH > WITHOUT** → agents converge on shared facts (the "one brain" claim).
- **Efficiency Δ** is reported signed (WITH − WITHOUT) and not pre-judged: Eidetix may cut turns/tool-calls (less re-discovery) while adding tokens (recall payload). Interpret net.
- **Control task `t6`** should show ~0 lift; if it shows large WITH lift, suspect judge bias and re-check blinding.

## Caveats
- `results/` holds agent outputs + token counts — gitignored by default; do not commit run artifacts.
- Real agent runs cost provider tokens. Start at the default scale; widen only if the signal is noisy.
- The token is read only from `EIDETIX_TOKEN`/the encrypted DB column — never commit it.
