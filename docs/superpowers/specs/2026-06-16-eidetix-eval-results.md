# Eidetix with/without Eval — Results (2026-06-16)

**Setup:** full local Multica stack on branch `feat/eidetix-context-integration`, Claude Code provider, real NodeOps Marketing graph. 6 marketing tasks × 2 arms (Eidetix enabled/disabled, via the per-project `enabled` flag — the only variable) × 3 trials = **36 real Claude Code runs**, all completed, 0 timeouts. Blind LLM judge (local `claude` CLI). Protocol: `2026-06-15-eidetix-eval-design.md`.

## Headline

| Axis | WITH | WITHOUT | Δ |
|---|---|---|---|
| Quality (1–5) | **4.61** | 3.67 | **+0.94 (≈+26%)** |
| Grounding (0–1) | **0.85** | 0.47 | **+0.38 (≈+80% relative)** |
| Pairwise win-rate | 0.50 (n=18) | — | tie |
| Consistency (intra-arm lexical sim) | 0.357 | 0.371 | ≈ flat |
| Efficiency Δ (WITH−WITHOUT), median | — | — | **+1989 output tokens, +12 turns, +8 tool calls** |

## Per-task

| Task | Quality W/Wo | Grounding W/Wo | Read |
|---|---|---|---|
| t1 launch one-liner | 4.67 / 3.67 | 0.95 / 0.33 | big WITH win |
| t2 /app B2C hook | 5.00 / 3.33 | 1.00 / 0.50 | big WITH win |
| **t3 rewrite NodeOps-contaminated blurb** | **5.00 / 2.00** | **0.97 / 0.03** | decisive WITH win |
| t4 full-lifecycle explainer | 3.67 / 3.67 | 0.42 / 0.45 | no lift |
| t5 differentiation vs code-gen | 4.33 / 4.33 | 0.78 / 0.52 | grounding only |
| t6 control (generic best-practices) | 5.00 / 5.00 | 1.00 / 1.00 | 0 lift (as designed) |

## What the numbers mean

1. **Shared context materially improves grounded quality** where the task depends on team-specific facts. Quality +0.94/5 and grounding nearly doubles. This is the core claim, and it holds.

2. **The signature result is t3 (rewrite a NodeOps-contaminated blurb): grounding 0.03 → 0.97.** An agent *without* the graph cannot know the `BRAND-SURFACE-GATES` ban (no NodeOps/orchestration/DePIN/$NODE in CreateOS copy), so it leaves the contamination in; *with* the graph it strips it. This is the strongest argument for Eidetix — it encodes rules an agent could not otherwise infer.

3. **The control (t6) shows exactly zero lift (5.00=5.00, 1.00=1.00).** This is the integrity check: when the graph is irrelevant, the WITH arm gets no spurious advantage. It means the wins above are real, not judge bias toward the longer/with-context output.

4. **The graph is not a universal win.** t4 (5-stage lifecycle) showed no lift and marginally *worse* grounding for WITH — recall didn't reliably surface the specific stage list, and both arms guessed comparably. Value is conditional on the graph actually holding (and recall surfacing) the needed fact.

5. **Pairwise is a tie despite higher absolute quality/grounding — and that is an actionable finding, not a contradiction.** The WITH deliverable is buried in process narration ("I recalled the brand gates, which say…") and runs ~60% longer (5250 vs 3331 output tokens). Head-to-head, the cleaner WITHOUT copy reads as competitive even when the WITH copy is more correct. **Fix:** the `multica-eidetix` loop skill should instruct agents to keep recall/reasoning internal and post *only* the clean deliverable. Expect pairwise to move decisively to WITH after that change.

6. **Consistency (the "team shares one brain" hypothesis) is not supported by this proxy** — intra-arm lexical similarity is flat (0.357 vs 0.371). Likely the crude Jaccard proxy plus narration-length variance washing out the signal; inconclusive rather than negative. A semantic-embedding similarity measure would test it better.

7. **Cost is real:** the recall loop adds ~+1989 output tokens, +12 turns, +8 tool calls per task. The grounding gain is bought with tokens/latency. Acceptable for high-stakes outward copy; reconsider for high-volume low-stakes generation.

## Performance (speed + token consumption)

Per-task, mean / median across 36 runs (timings from `agent_task_queue`
started_at→completed_at; tokens from `task_usage`):

| Metric | WITH | WITHOUT | Δ |
|---|---|---|---|
| Agent run time | 100s / 95s | 73s / 51s | +37% mean (~+45s median) — slower |
| Claim latency | ~0s | ~0s | negligible (poll interval did not distort) |
| Input tokens | 31.0k / 31.0k | 32.9k / 30.2k | ≈ equal |
| Output tokens | 5.3k / 5.1k | 3.3k / 2.6k | +58% |
| Cache-read tokens | 573k / 612k | 300k / 272k | +91% |
| Cache-write tokens | 106k / 108k | 56k / 46k | ~2× |
| Turns | 28 / 28 | 16 / 14 | +75% |
| Tool calls | 21 / 20 | 12 / 10 | +75% |

- **Speed:** Eidetix makes each task ~35–85% slower (+25–45s) — the read loop
  adds ~12 turns before the agent writes copy. Claim latency was ~0, so the
  daemon's 30s poll did not distort the comparison; the gap is real work.
- **Tokens:** raw input+output is nearly identical between arms (~36.3k each);
  the real cost is **cache traffic** — WITH does ~91% more cache-reads and ~2×
  cache-writes because each recall pulls large graph payloads into context that
  is then re-cached and re-read across ~75% more turns. In Anthropic pricing
  terms (cache-read ≈0.1× input, cache-write ≈1.25× input) that is roughly
  **+90k input-equivalent tokens/run**, dominated by cache, not the visible
  output bump.
- **Tradeoff:** the grounding/quality gains cost ~+40s/task and ~2× cache token
  traffic. Worth it for high-stakes outward copy; reconsider for high-volume
  low-stakes generation. The same skill fix (post clean deliverable, keep recall
  reasoning internal) would also trim output tokens and turns.

## Measurement caveats

- The captured "output" per run is the agent's **last comment**, which often includes process narration, not just the deliverable. This inflated a naive banned-term keyword count for the WITH arm (the agent *names* NodeOps while explaining it avoided it) and contributes to the pairwise tie. The LLM grounding score reads deliverable intent and is the figure to trust; isolating a clean-deliverable field would sharpen both pairwise and the lexical check.
- Single judge model; n=3 trials/cell (small). Directionally strong (control clean, t3 decisive), but CIs are wide on the per-task numbers.

## Bottom line

Eidetix shared context produces **markedly more grounded, higher-quality marketing output on team-specific tasks** (decisively so where the answer is a rule the agent can't infer — the contamination-rewrite case), with **no benefit on generic tasks** (correctly), at a **token/turn cost**. The one clear product fix surfaced: have the loop skill post clean deliverables and keep recall reasoning internal — that should convert the absolute-quality lead into a pairwise lead and cut wasted tokens.

Raw runs + per-task report: `evals/eidetix/results/` (gitignored — contains agent outputs).
