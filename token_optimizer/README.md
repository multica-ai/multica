# Token Optimizer

Toolkit for reducing Multica agent context overhead by 30%+ without quality loss.

## Modules

| Module | Description |
|--------|-------------|
| `core/profiler.py` | Section-level token profiling |
| `core/compressor.py` | Pattern-based compression (v1) |
| `core/compressor_v2.py` | Section-replacement compression (v2, recommended) |
| `core/selective_context.py` | Task-aware skill/category filtering |
| `core/budget.py` | Configurable per-agent token budgets |
| `core/dashboard.py` | Per-run JSONL tracking + baseline comparison |
| `core/ab_test.py` | Quality validation framework |

## Quick Start

```bash
# Profile AGENTS.md
python -m token_optimizer.cli profile AGENTS.md

# Compress with section replacement (v2)
python -m token_optimizer.cli compress-v2 AGENTS.md -o AGENTS.optimized.md

# Full benchmark (profile + compress + selective + budget + A/B)
python -m token_optimizer.cli benchmark AGENTS.md

# Check token budget
python -m token_optimizer.cli budget AGENTS.md --max-tokens 30000

# View dashboard
python -m token_optimizer.cli dashboard <agent-id>
```

## Strategies

1. **Section-level compression** - Replace verbose sections with concise equivalents
2. **Selective skill injection** - Task keyword -> category matching; only inject relevant skills
3. **Configurable budgets** - Per-agent limits with P1-P5 priority trimming
4. **Token dashboard** - JSONL tracking, baseline comparison, over-budget alerts
5. **A/B quality validation** - 5 test categories, automated scoring (>=85% retention = PASS)

## Architecture

```
token_optimizer/
  __init__.py
  cli.py              # CLI entry point
  core/
    __init__.py
    profiler.py        # Section-level token profiling
    compressor.py      # Pattern-based compression (v1)
    compressor_v2.py   # Section-replacement compression (v2)
    selective_context.py # Task-aware skill filtering
    budget.py          # Configurable per-agent budgets
    dashboard.py       # Per-run tracking + baseline
    ab_test.py         # Quality validation framework
```

## Acceptance Criteria Mapping

| Criterion | Module | Validation |
|-----------|--------|------------|
| Baseline measurement | `profiler.py` | `profile` command |
| 30%+ reduction | `compressor_v2.py` | `benchmark` command |
| No quality degradation | `ab_test.py` | `ab-test` command (>=85%) |
| Configurable budget | `budget.py` | `budget` command |
| Token dashboard | `dashboard.py` | `dashboard` command |
