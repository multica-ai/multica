# Active Codex chat guidance: validation evidence

Product source: `4183d882039fa24e5e2381ab46ff4c6f995e8986`.
The additional publication commit contains documentation and fixture screenshots
only. It does not change the reviewed implementation.

## Reproduce

Use the managed checkout environment from [CONTRIBUTING.md](../../../CONTRIBUTING.md),
then follow the [protocol and browser recipe](../chat-supplement.md).
The tests must use an isolated local PostgreSQL database and test-created agent
executables; no real agent account is needed.

After loading the checkout's `.env.worktree`, the publication verification ran:

```sh
go -C server test -race ./internal/handler ./internal/daemon ./pkg/agent ./cmd/server \
  -run '^Test(ChatSupplement|SupplementCodex|TaskSupplement|DirectChat_|DeleteRuntimeProfile_|DeleteAgentRuntime_.*DraftRestores|RuntimeGC)' \
  -count=1 -v
```

Result: **65 top-level tests passed in four packages**, no skipped tests and
no race reports. [Filtered execution output](go-results.txt) retains the test
names, package results and process/marker assertions without raw server logs.

The integration test uses real HTTP handlers, PostgreSQL, the daemon delivery
loop and a **fake Codex subprocess**. It checks the expected turn, original task,
one process, one injection despite an explicit retry, a durable receipt and the
marker in the persisted final answer. This is not a real Codex/model smoke test.

## Frontend checks and review provenance

The initial correction build (`b6b9286e5`) passed 254 focused core tests and
664 chat/locale tests, Web/Desktop typechecks, lint (existing warnings, no
errors), and one Chromium scenario covering both chat surfaces. The browser
simulates engine claim/ack; it proves UI/database behavior rather than Codex
transport. It drops the response after a real commit and checks idempotent
retry, plus pending → uncertain → accepted without reloading between claim
and acknowledgement.

The final workspace-response correction (`4183d8820`) passed 48 focused core
tests and 112 views tests, plus repository typecheck and lint. Independent
review then passed 44 focused core tests, 112 views tests and core typecheck.
The review loop stopped after its third verdict with no remaining significant
finding in the reviewed scope. These runs overlap and must not be summed as
unique test counts. Their original logs were inspected for this publication;
they were not rerun by the documentation commit.

The workspace regression covers success arriving after navigation, ambiguous
failure preserving the original request, newer drafts, logout, deleted workspace
storage and cleanup before mounting Chat. It does not introduce another send.

## Screenshots

These unedited captures are from the local Chromium fixture at `b6b9286e5`,
before the final storage-only workspace correction. They contain synthetic
chat content and no account credentials. “Before” means capability absent in
that fixture, not an older deployed version. They do not show a real model run.

| Interaction state | Capture |
| --- | --- |
| Before capability negotiation | [Capability absent](screenshots/before-capability.png) |
| Explicit action in full Chat | [Composer](screenshots/guidance-composer.png) |
| HTTP response lost, original request available for explicit retry | [Lost response](screenshots/guidance-response-lost.png) |
| Durable pending receipt | [Pending](screenshots/guidance-pending.png) |
| Claim without ack, updated live | [Uncertain](screenshots/guidance-uncertain-live.png) |
| Late positive engine acknowledgement | [Accepted](screenshots/guidance-accepted.png) |
| Shared floating window | [Floating Chat](screenshots/floating-guidance.png) |
| Refused submission keeps its draft | [Preserved draft](screenshots/guidance-rejected-draft.png) |

## Limits

Engine acceptance is not model compliance. An uncertain request is never
silently replayed. Clearing browser storage or changing device loses local
retry identity; explicitly treating an uncertain instruction as new can duplicate
it. Migrations roll back receipt metadata, so avoid rollback during delivery.

No real Codex binary/model/account, packaged Electron, production Next build,
full repository suite, production environment or loaded migration rollback was
validated. Hermes and other harnesses remain outside this contribution.

[SHA256SUMS](SHA256SUMS) records the integrity of these publication artifacts.
