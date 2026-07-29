# Decision: The Table's truth is a server-authoritative query spec

Status: proposed

## Problem

The Table view computes group membership, group counts, hierarchy, facet counts, and export from whatever rows the browser has loaded. Because the browser cannot hold an arbitrary result set, `TABLE_STRUCTURE_MAX_WINDOW = 1000` caps what it loads and suspends structural features beyond that cap.

The consequence is that the product's behavior changes with the size of the result, not with the user's intent. The same filter and the same grouping produce accurate headers at 900 issues and a degraded surface at 1,100. Raising the cap moves the cliff without removing it, because the architectural premise — the frontend must own the complete result set to group or nest — is what forces the trade.

## Proposal

One canonical `IssueTableQuerySpec`, authoritative on the server. Everything that must be true of the *complete* result set derives from it; the frontend owns only presentation.

| Capability | Owner | Why |
|---|---|---|
| Scope, filter, search | Backend | Determines membership, total, and export |
| Sort and stable pagination | Backend | Order must hold across pages |
| Group membership, order, count | Backend | Must be true of the whole result |
| Hierarchy membership, child count | Backend | A parent may not be in the client's window |
| Facet counts and aggregates | Backend | Loaded rows do not represent the set |
| View mode, columns, width, density | Frontend | Changes presentation, not membership |
| Group and parent collapse | Frontend | Personal state; decides which branches to request |
| Selection | Frontend | Session state; cleared when membership changes |

Group headers and rows are separate queries. `POST /api/issues/table/groups` returns exact headers and counts; `POST /api/issues/table/rows` returns cursor-paged root and child rows for the groups and branches actually expanded and on screen; `POST /api/issues/table/facets` returns exact filter facets.

Group membership always uses the issue's own field. It no longer switches to the root parent's field depending on whether the tree happens to be fully loaded. Hierarchy nests a child under a parent only when both match the query spec and share a group key; a child whose parent is filtered out, or is in another group, renders as a root row of its own group.

Export replays the same rows query page by page and fails closed: it refuses to produce a partial CSV when the schema falls back, when the fingerprint or cursor drifts, when rows repeat, or when the final count does not equal the first page's total.

`multi_select` is not supported as a Group in the first version; it remains available as a Filter and a Column. `TABLE_STRUCTURE_MAX_WINDOW` and its structural-suspension logic are deleted once the new path is stable, rather than raised to a larger number.

The Table cuts over hard to the new endpoints. The endpoints are additive, so older clients keep using the legacy ones, but no second frontend Table truth is maintained — an API error must not silently fall back to frontend grouping.

## Alternatives considered

**Raise the 1,000-row cap.** Rejected. It relocates the cliff instead of removing it. Any cap large enough to feel safe is large enough to make the browser hold and sort a result set that the server can group in one indexed query.

**Materialize group and facet counts server-side into a cache.** Rejected for the first version. Counts must be exact and must reflect filters chosen a moment ago, so a materialized copy adds an invalidation problem to a query that indexes handle directly.

**Keep the frontend authoritative and stream the full result set to it.** Rejected. It preserves the premise that causes the problem — behavior would still degrade with size, just at a larger size — and it moves the cost onto every client rather than onto one indexed query.

**Migrate Board, List, and Swimlane in the same change.** Rejected as scope. Table is where the correctness problem is visible and where the query spec gets proven; the other surfaces follow once it holds.

## Acceptance criteria

A user grouping a view sees accurate group headers and counts immediately, whether the query matches 10, 1,001, or 100,000 issues, and loads rows within a group on demand. Group, hierarchy, and count semantics are never redefined in terms of a loaded window or a browser memory limit. Backend, `packages/core`, and `packages/views` tests pass; the 1,001-issue scenario is verified in a browser; nothing is materialized in full automatically.

## Risks

Sorting by a non-default or custom property can still scan and sort the full membership, bounded only by the query timeout.

A server-side streaming export or an asynchronous export job, the observability for these endpoints, staging SLOs at 100k and 1m issues, and a browser network smoke test are all outstanding.

The legacy `GET` handlers have not moved onto the shared filter compiler, so two compilation paths exist while they remain.

Deliberately not in the first version, though the interfaces leave room: multi-select grouping, multi-field and nested grouping, multi-field sort, high-cardinality text/number/date grouping, sum/average/min/max group footers, server-side saved views with permissions and team sharing, and URL-encoded temporary view overrides.
