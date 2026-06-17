# Graphify token-cost benchmark

Measures how many tokens an agent spends answering a **navigation / orientation**
question ("where is X", "what connects to Y") via a Graphify graph query, versus
the naive cost of reading the source files to answer the same question.

It exists so the "Graphify is cheaper" claim is a re-runnable measurement, not a
one-off number. See the [graphify skill](../SKILL.md) for the tool itself.

## What it does

For each `(corpus, question)` in the config it:

1. builds a **code-only** knowledge graph offline (no API key — tree-sitter AST),
2. runs `graphify query` (budget-capped) and counts the answer's tokens,
3. sums the tokens of the source files the answer **cites** — the realistic cost
   an agent pays to answer *without* the graph ("naive: relevant files"),
4. sums the tokens of the **whole** corpus ("naive: load everything", worst case),
5. reports both ratios.

Token counts use `tiktoken` `cl100k_base` as a portable proxy. Absolute counts
differ slightly per model tokenizer; the **ratio** is what matters and is robust.

## Run

```bash
uv tool install graphifyy        # once: provides `graphify`
uv run --with tiktoken python3 .agents/skills/graphify/bench/graphify_bench.py \
    --config .agents/skills/graphify/bench/questions.example.json \
    --out /tmp/graphify-bench --budget 2000
```

Outputs `results.json` and `report.md` in `--out`. Paths in the config are
resolved relative to the directory you run from (run from the repo root).

## Baseline result (2026-06-17)

Run across three Firtal repos (firtal-cerebro, firtal-agents, firtal-data-registry),
7 navigation questions, budget 2000:

- **Realistic saving (vs. reading the relevant files): 8.8× – 59.7×.**
- Whole-subsystem framing (load everything) inflates to 100×–1700× — that is the
  worst case, not what a careful agent actually pays; we report the relevant-files
  number as the honest headline.

## Caveat

This measures the cost of **finding/understanding**, not editing. When an agent
must change code it still reads the real files — so the real-world saving on a
full workload depends on its find-vs-edit mix. Treat these as the upper bound on
the navigation portion of agent work.
