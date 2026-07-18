# Fallback Runtime Live Smoke - 2026-07-11

This note records the real-workflow validation for the optional fallback runtime
feature before opening an upstream PR. The smoke used a local Multica server
from this checkout, a real workspace, real agent configuration, real runtime
records, and real issues created through the HTTP API.

## Environment

- Backend: `http://localhost:18080`
- Database: local `multica-postgres-1`
- Workspace: `ecf8981b-259c-4d4b-8162-ea9a1bf1c9be`
- Workspace name: `Fallback Runtime QA 20260711`
- Issue prefix: `FBQ`
- Agent: `4ff87587-2686-4d94-9076-0440214c0afc`
- Agent name: `Fallback QA Agent 20260711`
- Primary runtime: `4ce3d2a6-88f0-4b22-ad89-6d143f91aa7d`
  (`qa-primary-clean`, `Primary QA Runtime Clean`)
- Fallback order:
  1. `1dcf1441-fac1-416f-a569-2e78f332683a`
     (`qa-fallback-one-clean`, `Fallback QA Runtime 1 Offline Clean`)
  2. `7526e486-14b3-40a7-9875-ee068df56023`
     (`qa-fallback-two-clean`, `Fallback QA Runtime 2 Online Clean`)

Agent configuration evidence:

```text
id                                   | name                       | primary_runtime_id
-------------------------------------+----------------------------+-------------------------------------
4ff87587-2686-4d94-9076-0440214c0afc | Fallback QA Agent 20260711 | 4ce3d2a6-88f0-4b22-ad89-6d143f91aa7d

fallback_order:
priority 0 -> 1dcf1441-fac1-416f-a569-2e78f332683a
priority 1 -> 7526e486-14b3-40a7-9875-ee068df56023
```

## Live Issues

All issues were assigned to the same fallback QA agent.

```text
issue  | issue_id                             | title
-------+--------------------------------------+---------------------------------------------------------------------------
FBQ-1  | 413433f5-021c-4d84-8616-9898cebde88a | Fallback QA 1: primary runtime failure resumes on fallback
FBQ-2  | 41bfe4c2-9ab1-4c58-aee6-2a3e07f49cef | Fallback QA 2: offline primary routes new task to fallback
FBQ-3  | 1e54e5e7-547e-49db-a407-a3693964a5b4 | Fallback QA 3: primary task failure queues fallback continuation
FBQ-4  | 8d8cd1de-ab28-4718-b9e7-a97aa49e4a7d | Fallback QA 4: primary failure completes on fallback in one live workflow
```

## Scenario Results

### FBQ-1: Sweeper Path

The primary runtime went stale before the task was claimed. The runtime sweeper
marked stale runtimes offline and failed the queued primary task.

```text
task_id                              | runtime          | status | error
-------------------------------------+------------------+--------+---------------------
97a54e8e-9664-47af-b5d3-a948df5f1738 | qa-primary-clean | failed | runtime went offline
```

Server log evidence:

```text
runtime sweeper: marked stale runtimes offline count=2
runtime sweeper: failed orphaned tasks count=1
```

This proves the sweeper path is active. It also proved that fallback runtimes
must keep heartbeating; otherwise a queued fallback task can also be swept.

### FBQ-2: Offline Primary Routes Directly To Fallback

The primary runtime was offline before enqueue. Fallback runtime 1 was also
offline, and fallback runtime 2 was online. The new task was routed directly to
fallback runtime 2 and completed through the daemon lifecycle.

```text
task_id                              | runtime               | status    | session_id              | work_dir
-------------------------------------+-----------------------+-----------+-------------------------+---------------------------------------
52e3f764-ebbc-46e6-a463-570002824ecc | qa-fallback-two-clean | completed | qa-session-fallback-002 | /tmp/multica-fallback-qa-fallback-two
```

Server log evidence:

```text
using fallback runtime for new task agent_id=4ff87587-2686-4d94-9076-0440214c0afc
primary_runtime_id=4ce3d2a6-88f0-4b22-ad89-6d143f91aa7d
fallback_runtime_id=7526e486-14b3-40a7-9875-ee068df56023
```

This proves fallback selection skips offline fallback runtime 1 and selects the
next eligible online runtime.

### FBQ-3: Primary Failure Queues Fallback Continuation

The primary task was claimed, started, and explicitly failed. Multica queued a
fallback task on fallback runtime 2 with the same session and work directory.
The fallback task was then intentionally left long enough for the runtime
sweeper to mark the runtime offline, so it failed as stale.

```text
task_id                              | runtime               | status | session_id             | work_dir                              | error
-------------------------------------+-----------------------+--------+------------------------+---------------------------------------+---------------------------------------------------------
155928a7-c8a8-4b83-8744-a0788553061a | qa-primary-clean      | failed | qa-session-primary-003 | /tmp/multica-fallback-qa-primary-003 | Fallback QA induced primary runtime failure after start
883abf9d-1cdc-42f3-9ff3-d68af0b1bdfb | qa-fallback-two-clean | failed | qa-session-primary-003 | /tmp/multica-fallback-qa-primary-003 | runtime went offline
```

Server log evidence:

```text
task fallback queued failed_task_id=155928a7-c8a8-4b83-8744-a0788553061a
fallback_task_id=883abf9d-1cdc-42f3-9ff3-d68af0b1bdfb
runtime_id=7526e486-14b3-40a7-9875-ee068df56023
```

This proves failed primary tasks create fallback continuations and preserve
handoff fields. It also repeats the operational heartbeat requirement.

### FBQ-4: Primary Failure Completes On Fallback

This was the full, tight end-to-end workflow. The primary runtime was online at
enqueue, claimed the task, started it, and failed. Multica queued a fallback
task on fallback runtime 2. Fallback runtime 2 claimed, started, and completed
the continuation while preserving session and work directory.

```text
task_id                              | runtime               | status    | session_id             | work_dir                              | error
-------------------------------------+-----------------------+-----------+------------------------+---------------------------------------+---------------------------------------------------------------
54969492-cdb4-4de0-b981-19acf5bc080b | qa-primary-clean      | failed    | qa-session-primary-004 | /tmp/multica-fallback-qa-primary-004 | Fallback QA induced primary runtime failure in tight workflow
e6373a84-512a-4205-a436-f941151731e7 | qa-fallback-two-clean | completed | qa-session-primary-004 | /tmp/multica-fallback-qa-primary-004 |
```

Completion result:

```json
{
  "output": "Fallback QA tight workflow completed on fallback two.",
  "session_id": "qa-session-primary-004",
  "work_dir": "/tmp/multica-fallback-qa-primary-004"
}
```

Server log evidence:

```text
task enqueued task_id=54969492-cdb4-4de0-b981-19acf5bc080b issue_id=8d8cd1de-ab28-4718-b9e7-a97aa49e4a7d
task claimed by runtime task_id=54969492-cdb4-4de0-b981-19acf5bc080b runtime_id=4ce3d2a6-88f0-4b22-ad89-6d143f91aa7d
task started task_id=54969492-cdb4-4de0-b981-19acf5bc080b issue_id=8d8cd1de-ab28-4718-b9e7-a97aa49e4a7d
task fallback queued failed_task_id=54969492-cdb4-4de0-b981-19acf5bc080b fallback_task_id=e6373a84-512a-4205-a436-f941151731e7 runtime_id=7526e486-14b3-40a7-9875-ee068df56023
task claimed by runtime task_id=e6373a84-512a-4205-a436-f941151731e7 runtime_id=7526e486-14b3-40a7-9875-ee068df56023
task started task_id=e6373a84-512a-4205-a436-f941151731e7 issue_id=8d8cd1de-ab28-4718-b9e7-a97aa49e4a7d
task completed task_id=e6373a84-512a-4205-a436-f941151731e7 issue_id=8d8cd1de-ab28-4718-b9e7-a97aa49e4a7d
agent status reconciled agent_id=4ff87587-2686-4d94-9076-0440214c0afc status=idle running_tasks=0
```

Timeline evidence for FBQ-4:

```text
created
task_failed
task_completed
agent comment: Fallback QA tight workflow completed on fallback two.
```

The timeline is understandable: one failure event is followed by one completion
event and one agent-authored completion comment. There was no duplicate failure
comment, silent disappearance, or stuck agent status in the final successful
workflow.

## Reliability Findings

- The fallback feature works in a real workflow with real issues, real runtime
  records, real daemon claim/start/fail/complete calls, and persisted task
  state.
- The fallback chain correctly skips an offline fallback and selects the next
  online fallback in priority order.
- Primary task failure queues a fallback continuation on the selected runtime.
- Session and work directory handoff fields were preserved across fallback in
  both FBQ-3 and FBQ-4.
- Runtime sweeper behavior is active and observable. Stale queued/running tasks
  can be failed when their runtime stops heartbeating, including fallback tasks
  that are not claimed before the stale window. That is operationally important
  and should be documented for daemon authors/operators.
- A cookie/header API probe for `/api/issues/{id}/timeline` was blocked by local
  auth extraction friction during validation, but the same timeline data was
  verified through the backing `activity_log` and `comment` rows and matched the
  server log sequence.

## Automated Verification

Required checks were run after the live workflow:

```text
cd /Users/blackthorne/Work/multica/upstream/server
go test ./...
PASS
```

```text
cd /Users/blackthorne/Work/multica/upstream
./node_modules/.bin/tsc --noEmit -p packages/views/tsconfig.json
PASS
```

```text
cd /Users/blackthorne/Work/multica/upstream
./packages/views/node_modules/.bin/vitest run agents
Test Files  1 passed (1)
Tests       2 passed (2)
```

```text
cd /Users/blackthorne/Work/multica/upstream
git diff --check
PASS
```

## PR Readiness Verdict

The fallback runtime feature is functionally proven for the core PR gate:
configured fallback order, primary enqueue, primary offline routing, primary
failure continuation, offline fallback skip, context/session/workdir
preservation, operator-visible DB/log/timeline evidence, sweeper behavior, and
green automated gates.

The only caveat is operational rather than a functional blocker: fallback
runtimes must heartbeat actively, because the sweeper can fail queued fallback
tasks once the selected fallback runtime is stale. That should be called out in
the PR description or daemon/operator docs.

## Quota And Context Handoff Rerun - 2026-07-18

The earlier smoke used real server and daemon lifecycle calls, but simulated
both runtime outcomes. This rerun exercised a current-branch daemon and a real
Codex provider against a freshly migrated PostgreSQL database. It copied the
read-only scenario from real workspace issue `SER-839` into isolated local
issue `FBT-3`; the cloud workspace and source issue were not mutated.

The primary runtime was a test-only OpenCode-protocol executable that emitted a
deterministic provider exhaustion event. The fallback runtime was the locally
authenticated Codex app-server (`codex-cli 0.144.1`). This distinction matters:
the exhaustion trigger was controlled, while the continuation was executed by
a real provider-backed Multica agent through the normal batch claim, workdir,
task-token, transcript, issue-comment, and completion paths.

```text
workspace  3e640f06-6217-453b-b095-009c236e507f  Fallback Runtime Battle Test
agent      5e4c4fb7-0225-4986-88ec-c76700dc6770  Fallback Battle-Test Agent
issue      21b18757-8e2d-4ad7-8e99-27ff1c1b6734  FBT-3
attempt 1  0f5c95ca-44be-4add-948c-267d5192d512  quota fixture  failed
attempt 2  bb8d0ee4-6ff4-4c2e-a7e5-cdb51477be44  real Codex     completed
```

Observed guarantees:

- Attempt 1 was classified as `agent_error.provider_quota_limit` and persisted
  a 15-minute cooldown for the primary runtime.
- Attempt 2 was created immediately on the configured Codex fallback. There
  were exactly two tasks, two distinct runtimes, and no attempt above the
  configured bound.
- The fallback reused the source workdir but started a fresh provider session.
- The source transcript was materialized at
  `.multica/fallback-context/0f5c95ca-44be-4add-948c-267d5192d512/transcript.jsonl`
  with mode `0444`.
- Generated runtime briefs contained only that relative file pointer and source
  task ID. The provider error text was not prompt-injected.
- The real Codex agent read the JSONL file, posted a bounded implementation and
  verification plan to `FBT-3`, and explicitly confirmed the canonical source
  failure category before the fallback task completed.
- Issue activity and Inbox history recorded source and destination runtime
  names/providers, canonical failure reason, attempts, and cooldown. Persisted
  history omitted the raw provider error.

The live run also found and fixed a claim-path gap before this proof succeeded:
the legacy per-runtime claim endpoint hydrated fallback transcripts, but the
machine-level batch claim endpoint used by current daemons did not. The shared
claim helper now applies the same handoff contract to both endpoints, with a
dedicated batch-claim regression test. This is why a real daemon workflow was a
necessary PR gate rather than relying only on the original handler test.

## Cooldown Recovery And Completion Audit - 2026-07-18

The same isolated workspace and agent were reused after the database was
round-tripped through the current migration set. Both QA runtimes were restored
to online status without starting the polling daemon, allowing the server's
real issue-enqueue path to be observed without either task being claimed.

```text
active cooldown
issue  3c22a7bd-662e-40dc-a368-5ebc03900336  FBT-4
task   95f0df76-87f0-43b5-8a12-ddd87babcd4c
route  c594e70d-b835-4d5d-8ebf-eb0060167cb2  real Codex fallback

expired cooldown
issue  76096f5e-26fb-48dd-be42-5d73a98e66e4  FBT-5
task   10454f78-bfdf-46c4-b6a9-daccb11ad723
route  028441ee-be75-4719-921f-4fed38115ab0  primary quota fixture
```

`FBT-4` proved that a new task bypasses an actively cooled primary. The
cooldown was then expired, and `FBT-5` proved that the primary immediately
became eligible again. The queued proof tasks were cancelled before the daemon
was restarted, so neither fixture performed another provider call.

The final requirement audit also added or strengthened database-backed tests:

- replaying one fallback failure event now produces exactly one Inbox row and
  one `inbox:new` event per recipient;
- a partial unique index makes that guarantee concurrency-safe for both
  `task_failed` and `task_fallback` rows;
- chat fallback preserves the original chat session, raises retry priority
  ahead of newer turns, and retains the worktree;
- squad fallback preserves the exact squad ID and leader-task role;
- the full Go suite passes under `-race` after the migration and notification
  compatibility fixtures were updated to model distinct historical tasks.

## Current Upstream CI Reproduction - 2026-07-18

The committed branch was verified against disposable services matching the
current upstream CI workflow: `pgvector/pgvector:pg17` and `redis:7-alpine`.
From a fresh PostgreSQL database, the exact backend sequence completed:

```text
go build ./...
go run ./cmd/migrate up
go test -race ./...
```

All migrations through `202_agent_fallback_runtimes` applied and every backend
package passed under the race detector. Frontend dependency installation was
lockfile-clean, self-host environment derivation passed through the installed
standalone Compose binary, and reserved-slug regeneration produced no diff.

The frontend typecheck, lint, and test matrix passed for all seven non-docs,
non-mobile packages. Its test totals included 987 core tests, 2,680 views
tests, 108 web tests, and 329 desktop tests. Production desktop and web builds
also passed when Next's official `NEXT_FONT_GOOGLE_MOCKED_RESPONSES` hook was
used to isolate font transport.

The unmodified local web build could not fetch the large Google Fonts response
set for `Noto Serif SC`, despite direct curl and Node HTTPS probes succeeding.
No source workaround was added for this machine-specific external dependency.
The exact unmocked build therefore remains an explicit upstream-CI delivery
gate after the approved PR branch is published.
