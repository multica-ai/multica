# issue_search_synopsis PR1 offline regression

This directory contains a clean, synthetic regression harness for the first
`issue_search_synopsis` implementation slice. It does not change production
search behavior.

## Privacy gate

The fixtures in this directory are synthetic. They intentionally do not contain:

- real workspace snapshots
- real issue comments
- member IDs, agent IDs, workspace IDs, or project IDs
- local absolute paths
- private artifact URLs

The original failure mode is documented only at a product-behavior level: as
issue volume grows, long threads, meta discussion, superseded child issues, and
parent/child intent confusion can cause search to retrieve stale or noisy
issues. The proposed improvement is to index a canonical issue synopsis with
child status aggregation, latest final/consolidation signals, lifecycle flags,
and rerank penalties for superseded/cancelled/chatter-heavy matches.

## Files

- `issue_search_synopsis_v0.py` generates synopses and evaluates the synthetic
  20-query regression set.
- `synopsis_schema_v0.json` documents the offline synopsis shape.
- `synthetic_issues.json` is a small anonymized issue graph.
- `search_regression_20q.json` contains the fixed synthetic regression queries.

## Run

```bash
python3 tools/issue_search_synopsis/issue_search_synopsis_v0.py \
  --out issue_search_synopsis_out
```

Expected output includes:

- `issue_search_synopsis_out/synopsis.jsonl`
- `issue_search_synopsis_out/summary.json`

The evaluation compares a legacy-style text matcher against the synopsis rerank.
It is intended as a regression fixture for PR review, not as a production search
implementation.

