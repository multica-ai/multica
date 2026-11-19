# Continuation detector pilot

A consumed wakeup proves that a receiver was scheduled. A terminal task proves
that an attempt ended. Neither proves that its remaining work was delivered.
This opt-in server monitor preserves an explicit remainder and produces one
private inbox incident when it has no live receiver, valid wait or attested
result. It never dispatches, cancels or retries tasks, or repeats an external
merge, deployment or call. Existing platform retry policy is unchanged.

## Scope and authority

The first slice is operator-managed, limited to **one project**, and disabled
by default. No agent receives database credentials or a new public write API.
An operator with the existing server database access uses `cmd/continuation`
to enroll a handoff, adopt a successor, confirm a scoped wait, or attest a
result. Ordinary wakeups are not silently enrolled. This is intentionally a
bounded pilot, not a claim of workspace-wide protection.

The implementation was based on server commit
`e1fe10d6694848e35dd1aa00e08cd11d93e545ac`; a deployment must verify its own
binary commit and schema. A locally installed CLI version is not that evidence.

## Durable contract

The logical incident key is `(workspace_id, issue_id, chain_id, remainder_id,
generation)`. `issue_continuation` holds the current contract and CAS version;
its history preserves explicit amendments and resolved generations.
`issue_continuation_incident` has a unique logical key and stable inbox ID.
That same incident row is its durable delivery outbox, with pending,
suppressed and delivered states. Delivered incident rows are immutable.
Automatic receiver adoption does not change generation. An operator can
explicitly rearm monitoring for a new live manual receiver after delivery;
the previous incident and unresolved contract remain in history.

The states are `awaiting_event`, `receiver_queued`, `receiver_running`,
`resolved`, and `needs_reconcile`. A failed, cancelled, missing or unknown
receiver, an expired deadline, a changed wakeup revision, and completed work
without a coverage receipt require reconciliation. An unknown external outcome
never authorizes a repeat operation. An already live receiver may itself be
reconciling that outcome and suppresses a new signal until it ends.

The 30-second existing server scheduler reads authoritative PostgreSQL task
rows, including atomic platform retry descendants. It does not rely on a
terminal event being delivered, an LLM response, the product worktree, or an
online execution host. Manual successors must be explicitly adopted; an
unrelated task or the issue's general `in_review` status does not cover this
remainder. The source run's result and a non-empty output are not receipts.

A once-wakeup can be disabled while its current-revision receipt is still
pending. With no explicit `disabled_at`, that receipt remains a valid event
wait, or identifies its reserved receiver. Explicit disable and revision
changes still invalidate the binding; a pending receipt does not extend the
contract's deadline or hide a reserved receiver's terminal outcome.

A confirmed `ci`, `owner`, or `quota` wait has its own deadline and covers only
this key. Dispatch consumes the previous event wait. Resolution requires a
nondeleted comment on the same issue, an explicit `coverage_confirmed`
attestation, and a known external outcome. An agent's result comment must come
from the recorded receiver; the named human owner can attest their own result.
The operator is responsible for verifying the comment actually covers the
remainder. The system does not try to infer semantics from prose.

## Operator commands

Build with the repository's Go version, from `server/`:

```sh
go build -o /path/to/operator-tools/continuation ./cmd/continuation
```

Use the server's existing secret provisioning for `DATABASE_URL`; do not put
it in an artifact or give it to a task. Set
`MULTICA_CONTINUATION_PROJECT_ID` to the pilot project UUID. The CLI only reads
or changes the contract; notification delivery is performed by the server.

An enrollment JSON file has this shape (replace example UUIDs with verified
IDs; `due_at` is the explicit end of the event wait):

```json
{
  "workspace_id": "00000000-0000-4000-8000-000000000001",
  "issue_id": "00000000-0000-4000-8000-000000000002",
  "chain_id": "00000000-0000-4000-8000-000000000003",
  "remainder_id": "00000000-0000-4000-8000-000000000004",
  "generation": 1,
  "owner_id": "00000000-0000-4000-8000-000000000005",
  "source_task_id": "00000000-0000-4000-8000-000000000006",
  "wakeup_id": "00000000-0000-4000-8000-000000000007",
  "wakeup_revision": 1,
  "due_at": "2030-01-01T12:00:00Z",
  "wait_kind": "ci",
  "wait_until": "2030-01-01T12:00:00Z",
  "external_outcome": "none",
  "coverage_confirmed": false,
  "evidence": "Explicit handoff: finish the repair after its named CI event",
  "expected_version": 0,
  "new_generation": false
}
```

```sh
continuation -operation put -input handoff.json
continuation -operation get -input key.json
```

`key.json` contains only the four UUID fields from the logical key, omitting
generation. Get returns the current version and contract. For an amendment,
copy only the contract fields, add that version as `expected_version`, and
submit `put`. A conflict means read the current contract, not blindly retry.
Automatic receiver adoption can also advance the version.

To adopt a manual successor, set its verified `receiver_task_id` and remove the
old `wakeup_id` and `wakeup_revision` binding. Before incident delivery, keep
generation unchanged. After delivery from `needs_reconcile`, explicitly set
`new_generation: true` and increment generation by exactly one. Read the
current CAS version first, clear old waits and resolution receipts, and record
the verified handoff in `evidence`. The receiver must be a different, existing
live task on this issue; another live predecessor/successor blocks replacement.
This operation only adopts the task: it never creates or retries one. A new
manual receiver cannot silently reuse an already delivered generation.

To await a person or quota, explicitly clear the consumed wakeup/receiver and
record the wait kind, end time and evidence. To resolve, set
`result_comment_id`, `coverage_confirmed: true`, and the verified coverage in
`evidence`. A resolved remainder can also start a new generation for a new
request, with the same explicit flag/increment and no inherited resolution.
A wait alone does not rearm a delivered generation. Preserve an unknown
external outcome until it is verified; generation changes confer no retry
allowance and never clear delivery history or downstream recovery receipts.

## Delivery boundary and crash behavior

Reconciliation creates or updates one incident/outbox row. Delivery separately
rechecks the current generation, receiver, wait, result and private-project
membership under the same ledger lock used for amendments. Parent issue locks
precede wakeup/ledger work. Task rows are read without taking an inverse lock
against terminal writers. Issue/workspace deletion removes the private ledger
and incident with the owning resource.

The inbox insert uses the stable incident ID. That insert and the persisted
outbox ACK commit in **one PostgreSQL transaction**. A rollback before ACK
leaves neither; an ambiguous commit is resolved by reading the same receipt.
Archiving or deleting an inbox item does not reset its delivery budget.
This guarantees one durable inbox row for this incident; it does **not**
promise exactly-once websocket, push, email, or external transport delivery,
nor that the owner read it. A crash after commit may lose the live websocket
announcement; the ordinary inbox refresh/reconnect still reads the saved row.

The signal is addressed only to the recorded member, never public issue
comments or subscriber fanout. It carries references and a short explanation,
not raw task logs, model output or credentials. Private-project access is
checked again by the inbox insertion. This action-needed type ends an inbox
snooze; it has no preference group to accidentally mute the pilot's signal.

## Rollout, evidence and rollback

1. Apply migrations through the normal release path and independent review.
2. Set only `MULTICA_CONTINUATION_PROJECT_ID`; leave
   `MULTICA_CONTINUATION_NOTIFY=false`. Enroll a small set of verified remainders
   (at most 100 for the pilot). Inspect incidents, waits, generations and
   active-successor suppression before allowing delivery.
3. Set `MULTICA_CONTINUATION_NOTIFY=true` through the same release path.
   Measure persisted `terminal_at → notified_at`, and separately client
   `reconnected_at → visible_at`. The target is at most five minutes while the
   server is available. A 30-second cadence or a local test is not proof of
   the production target; record actual UTC timestamps, server SHA and load.
4. Set notify false to stop delivery. Clear the project flag to stop monitoring.
   Keep tables, amendments, incidents and deduplication keys. Rollback does
   not run destructive down migrations or reset any generation.

Rows are visited in batches of 100 with a 45-second job budget. Larger
backlogs, database outages, revoked owner access and client offline time are
explicit delivery limitations; inspect scheduler errors and pending outbox
age. Restoring a worktree does not resolve a failed remainder. Restore the
contract with a proven successor or result instead.

## Verification

Focused PostgreSQL tests cover failure before model output, completed without
coverage, confirmed waits/deadlines, CAS, consumed once-wakeups, a changed
revision, active manual/platform successors, concurrent reconciliation and
delivery, restart/offline/reconnect, failure between inbox write and ACK,
private access, deletion and flag rollback. Run against a disposable migrated
database:

```sh
go test -race -timeout 90s ./internal/continuation ./internal/inbox
go test -timeout 90s ./cmd/migrate ./internal/migrations
```

These tests do not assert production deployment or human receipt. The later
recovery implementation must separately prove execution safety and admission;
this detector provides no permission or API for automatic retries.
