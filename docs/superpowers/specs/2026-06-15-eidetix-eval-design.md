# Eidetix with/without Eval — Protocol Design

**Status:** Design, pre-execution (gated on a populated graph)
**Date:** 2026-06-15
**Decision inputs (from Naman):** measure **all three** (quality+grounding, consistency, efficiency); knowledge source = **real partner Marketing graph**; run substrate = **full Multica stack, true e2e**.

## Question

Does giving Multica agents the Eidetix shared-context graph measurably improve
their work versus running them cold? "Improve" is decomposed into three
independently-measured axes.

This eval answers the **value** question (does shared context help outcomes),
NOT the **plumbing** question (does the server inject context correctly) — the
latter is already covered by the Go test suite.

## The controlled comparison

The feature's own per-project `enabled` flag is the experimental knob. Between
the two arms, **only** Eidetix presence changes; model, temperature, agent
instructions, issue text, provider, and runtime are held identical.

- **Arm WITH:** project bound to the Marketing graph, `enabled = true` → claim
  response carries the `eidetix` MCP server + `multica-eidetix` skill.
- **Arm WITHOUT:** same project, `multica project eidetix disable <project>` →
  no server, no skill. Cold agent.

Counterbalance arm order and interleave runs so model/endpoint drift averages
out. Pre-register the task suite + judge rubric before any run (no post-hoc
task selection).

## GATE A — graph must be populated → PASSED (2026-06-16)

Confirmed populated: the Marketing graph holds ~150 NodeOps/CreateOS marketing
documents (inventory shared by the team; spot-read via the Notion source).
Anchor docs verified: `BRAND.md v1.1`, `BRAND-SURFACE-GATES.md` (the NodeOps
contamination ban), CreateOS positioning one-pagers/fact-sheets, `personas.md`,
`competitors.md`, the 8-phase launch playbook, per-channel platform playbooks.

The task suite (`tasks.json`) is finalised against real, objectively-gradeable
facts from these docs — chiefly the canonical CreateOS one-liners, the
"execution layer" (not "orchestration") positioning, the `createos.sh` domain,
and the hard banned-term list (NodeOps, DePIN, $NODE, orchestration, …). These
are high-contrast: an unaided agent typically reaches for "orchestration",
mentions NodeOps, or uses the old domain, while a graph-aided agent complies —
so the WITH/WITHOUT signal is sharp and checkable.

`evals/eidetix/probe_graph.py` remains a pre-run sanity check: run it once with
the real token to confirm `recall` actually surfaces these facts before
spending the agent budget.

## Task suite

~12 marketing tasks representative of NodeOps/CreateOS work, **each constructed
so the graph's known facts would change a good answer** (brand voice, prior
positioning decisions, audience facts, past campaign outcomes). Examples
(finalised from probe output):
- Draft a launch announcement for <feature> in our established voice.
- Write positioning copy for <audience/persona>.
- Propose a campaign brief for <initiative>, consistent with prior decisions.
- Rewrite this off-brand draft to match our guidelines.

Each task ships as a Multica issue on the bound project, assigned to the eval
agent. A task with no graph-relevant facts is a control (expect ~0 lift — its
presence guards against judge bias toward the WITH arm).

## Metrics

### Arm 1 — Quality + factual grounding (LLM-judged, blind)
- For each task × arm × trial, a strong judge model (e.g. Opus) scores the
  agent output on: (a) **overall quality** 1–5; (b) **factual grounding** =
  fraction of the relevant graph facts correctly used, minus contradictions of
  known facts. The judge is given the graph's ground-truth facts as the rubric
  key but is **blind to which arm produced the output**.
- Also run **pairwise A/B preference** (judge picks the better of the two arm
  outputs for the same task, blind/randomised order) — more robust than
  absolute scores.
- Report: mean quality and grounding per arm with 95% CIs; pairwise win-rate.

### Arm 2 — Consistency across agents
- Run each task through **N = 3–5 agents** (distinct agents on the project, or
  N trials) per arm. Measure inter-output agreement:
  - embedding pairwise cosine similarity of the N outputs;
  - variance of the per-output grounding score.
- Hypothesis: shared memory converges agents on the team's facts → higher
  similarity, lower grounding variance in the WITH arm.
- Report: mean pairwise similarity and grounding-variance per arm.

### Arm 3 — Efficiency / cost (from the stack, not judged)
- Pull per-task from `task_usage`: `input_tokens`, `output_tokens`,
  `cache_read_tokens`, `cache_write_tokens`. From `task_message`: message/turn
  count and tool-call count (proxy for re-discovery: web/search/file calls the
  agent makes to rebuild context the graph already holds).
- This axis is **directionally honest, not assumed**: Eidetix can *reduce*
  re-discovery (fewer turns/tool-calls to get facts) while *adding* tokens
  (recall payload injected) and latency. Report the net delta on each
  dimension; do not pre-judge the sign.
- Report: median Δtokens, Δturns, Δtool-calls (WITH − WITHOUT) per task, with
  the distribution.

## Scale & cost

Default: 12 tasks × 2 arms × 3 trials = 72 agent runs for arms 1+3; the
consistency arm reuses the trials (N=3 per arm = the same 72) plus optionally a
few extra agents. Each run is a full provider session — real token cost. Scale
is a budget decision (see asks). Start at this size, expand only if the signal
is noisy.

## Run topology (full stack, local)

Because the live CreateOS backend does NOT yet run this branch, the faithful
full-stack run is **local**:
1. Local backend on this branch, `MULTICA_EIDETIX_SECRET_KEY` set (done).
2. Local daemon running a **transport-verified provider** (Claude Code or
   OpenClaw — the only two confirmed to load remote-MCP `url` entries).
3. Provider API credentials available locally (Anthropic / OpenRouter).
4. Eidetix endpoint reachable (confirmed up, 401-gated).
5. One bound project; the Marketing token set via `multica project eidetix set`.

Alternative: deploy the branch to a CreateOS preview env and point the existing
`daemon-claude` runtime at it — heavier, defer unless local is impractical.

## Harness

`evals/eidetix/run_eval.py` (to build after Gate A):
1. For each (task, arm, trial): ensure the arm via the `enabled` flag, create
   the issue on the bound project, assign the eval agent, poll to completion.
2. Collect the output (latest agent `comment`/`task_message`) + the
   `task_usage` row + turn/tool-call counts.
3. Persist a row per run to `evals/eidetix/results/runs.jsonl`.

`evals/eidetix/judge.py` (or a Workflow fan-out):
- Blind quality + grounding scoring and pairwise preference over the collected
  outputs; embedding similarity for the consistency arm; aggregate to a report.
- The judging/aggregation stage is a natural multi-agent fan-out and could be
  run as a Workflow (opt-in) — but the agent *runs* themselves go through the
  real Multica stack, not the workflow.

## Confound controls (summary)

- Single variable between arms (the `enabled` flag).
- Blind, randomised-order judging; multiple trials; report CIs.
- Interleaved arm execution to average model/endpoint drift.
- Pre-registered task suite + rubric.
- At least one graph-irrelevant control task (expect ~0 lift) to detect judge
  bias toward the WITH arm.

## Partner constraints (confirmed by Eidetix team, 2026-06-15)

- **Token scope:** the Marketing token supports all 8 MCP tools (read + write) — the loop's `get_schema`/`ingest_traces` path is authorised.
- **Endpoint/transport:** `https://eidetix.nodeops.xyz/mcp/sse` + `Authorization: Bearer <token>` (matches the integration).
- **No rate limits.** **Concurrency cap = 4.** Latency target < 1s/request.
- Eval impact: the runner is **serial** (one agent in flight), so it never approaches the cap. Benign for the eval.
- **Production impact (note, not an eval issue):** per-project binding means every agent on a marketing project shares one token → one graph. With >4 marketing agents running concurrently, the 5th+ concurrent Eidetix session hits the cap. Fail-open means no task breaks — those agents just proceed without shared memory for that call. If the team routinely runs >4 concurrent marketing agents, revisit (client-side queueing / a higher cap) post-v0.

## Still pending from the Eidetix team
- **Graph inventory** (documents / entities / topics) — needed to finalise the task suite and the grounding key. (GATE A: also confirms the graph is populated.)
- **Trace schema** (`get_schema` output + `ingest_traces` shape) — to verify the write path; team is adding it to the doc.

## Open asks (gates before execution)

1. **Run the probe** (`probe_graph.py` with the Marketing token) and share the
   verdict + a redacted sense of what facts the graph holds → finalises the
   task suite. (Blocking.)
2. **Budget / scale** — confirm tolerance for ~72 real provider runs (more if
   we widen the consistency arm). Sets task/trial counts.
3. **Provider** — Claude Code or OpenClaw for the eval agent? (Must be a
   transport-verified one; others would silently drop the eidetix server and
   contaminate the WITHOUT-vs-WITH contrast.)
4. **Run location** — local full stack (recommended) vs deploy-to-preview.
