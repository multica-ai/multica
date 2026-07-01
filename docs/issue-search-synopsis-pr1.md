# issue_search_synopsis PR1 notes

PR1 adds an offline, synthetic regression harness for the proposed issue search
synopsis work. It does not change production search behavior.

## Problem

When a workspace accumulates many related issues, search quality can degrade
because the index treats noisy material too uniformly:

- long comment threads can outweigh the latest final result
- meta discussion can match a query better than the executable issue
- superseded or cancelled child issues can be retrieved as if still current
- parent control-room intent and child execution intent can be confused

## Proposed direction

Index a canonical `issue_search_synopsis` document per issue. The synopsis
should prioritize:

- structured issue fields
- child status aggregation
- latest final or consolidation signal
- lifecycle flags such as `superseded`, `needs_decision`, and `true_blocked`
- compact `search_text` instead of full unweighted chatter

Search/rerank can then boost current-state signals and downrank stale,
cancelled, superseded, or chatter-heavy matches.

## PR1 scope

This PR only adds clean offline artifacts:

- synthetic issue graph fixture
- `issue_search_synopsis_v0` schema
- 20-query synthetic regression fixture
- local generator/evaluator script
- this PR note

No real workspace snapshots, real comments, member IDs, agent IDs, absolute
paths, or private artifact URLs are included.

Later PRs can add materialized synopsis storage and feature-flagged search
rerank against production data.

