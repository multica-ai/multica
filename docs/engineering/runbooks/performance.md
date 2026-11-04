# Office performance checks (plan 12.4)

This runbook records the measured numbers for the company office and says which
thresholds the spec sets and whether they are met. The checks run against the
fixture data of `server/internal/service/office_perf_test.go` and the spike in
`server/internal/service/budget_test.go`.

## How to run

Both checks need the checkout's database (`.env.worktree` in a worktree). Load the
env file, then run:

```sh
cd server
set -a; . ../.env.worktree; set +a
MULTICA_OFFICE_PERF=1 go test ./internal/service -run TestOfficePerf -count=1 -v
MULTICA_BUDGET_SPIKE=1 go test ./internal/service -run TestBudgetGateAdmissionSpikeP99 -count=1 -v
```

Both tests are skipped unless their variable is set, so `make test` does not run them.

## Thresholds from the spec

| Check | Threshold | Where the spec sets it | Result |
|---|---|---|---|
| Claim gate admission, p99, 50 budgets, 10,000 queued tasks | under 2 ms | plan 11.6 spike check | Met after a fix (see below) |
| Digest with 50 squads and 200 initiatives | no latency threshold; caps: at most 20 squads in full, five recent done issues per squad | spec, "The digest" | Caps met; latency recorded, no threshold to compare |
| Board with 200 cards (pipeline list query) | no threshold set | plan 12.4 names the check only | Recorded |
| Brief length with the new sections | body at most 20,000 characters (column limit); headline at most three lines; at most five decisions; "readable in two minutes" | spec, "The brief" | Character cap met; reading target not met at the caps (see below) |

## Claim gate (plan 11.6 spike)

| Run | p50 | p99 | max | Outcome |
|---|---|---|---|---|
| Before migration 900 | 4.70 ms | 15.39 ms | 22.56 ms | Failed the 2 ms limit |
| After migration 900 (run 1) | 0.64 ms | 1.11 ms | 3.65 ms | Passed |
| After migration 900 (run 2) | 0.65 ms | 1.15 ms | 3.94 ms | Passed |

Cause. The admission calls `ListBudgetInflightTasks`, which filters `agent_task_queue`
by status and joins `agent` on the workspace. The agent's index only narrowed by
`agent_id`, so the planner read all 10,000 queued rows of that agent and dropped them
by status. `EXPLAIN ANALYZE` on the fixture showed 3.6 ms in that one node (`Rows
Removed by Filter: 10000`). The finished-costs query was already 0.03 ms through the
terminal-task index.

Fix. Migration `server/migrations/900_agent_task_inflight_index.up.sql` adds a partial
index `agent_task_queue_inflight_agent_idx (agent_id) WHERE status IN ('dispatched',
'running', 'waiting_local_directory')`, built `CONCURRENTLY` as one statement. The
down file drops it. No query text changed, so `make sqlc` is not needed.

## Digest and board (50 squads, 200 initiatives)

Setup: six floors, 50 squads with one in-progress and one done issue each, the default
pipeline, and 200 initiatives created through `InitiativeService.Create`. Setup took
1.2 s.

| Measure | Value |
|---|---|
| Digest `Compute`, median of 20 runs | 6.1 ms |
| Digest `Compute`, worst of 20 runs | 23.6 ms |
| Squads in full (cap 20) | 20; 30 omitted |
| Pipeline rows in the digest | 200 |
| Digest JSON size | 105,096 bytes |
| Board list query (`ListInitiatives`, 200 cards), median of 50 | 0.44 ms |
| Board list query, worst of 50 | 0.89 ms |
| `EXPLAIN ANALYZE` of the board query, execution | 0.09 ms (sequential scan of 200 rows, sort by `seq`) |

Finding, recorded and not changed. The digest's pipeline section lists every active or
operating initiative (spec, "The digest" and 3.7), so the JSON grows with the number of
initiatives, about 500 bytes each. The squad cap bounds the squad part only. The
Secretary reads this JSON, so a company with thousands of initiatives would need the
pipeline section capped too. That is a product decision, so it is recorded here.

## Brief length (phase 3 and 4 sections)

The measured body is `companyDayMaxBrief` in the same test file. It fills every section
that phase 3 and 4 add (Pipeline, Goals, Budget, People) with five decisions, one line
per floor, and five blockers, and it stays within the caps the spec sets.

| Measure | Value |
|---|---|
| Body size | 3,381 bytes (column cap 20,000 characters) |
| Words | 591 |
| Headings | 10 sections (the test's count printed 9, because it misses the first heading) |

The spec gives no number for "readable in two minutes". At a reading speed of 238 words
a minute (an assumption, not a spec value), two minutes is about 475 words. The
synthetic body at the caps is 591 words, about 2.5 minutes, so the reading target is
not met at the caps. The fix is either a numeric target in the spec or shorter caps on
the items, and that is a product decision. A live Secretary brief depends on the model
and is checked by hand in `runbooks/company-day.md`.

## Not measured

- Hardware: the numbers come from this host, Postgres 17 (pgvector image) in Docker
  and Go 1.25. They are not CI-class hardware.
- Live Secretary brief length (needs the model).
- Claim gate under concurrent claims (the spike measures one admission at a time).
