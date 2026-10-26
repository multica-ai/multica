# NEX-1565 admission foundation

This change is not an H3 mitigation on its own. The production server does not
emit `execution_policy`. Do not deploy it as a security fix or advertise isolated
execution capability based on this change.

An authenticated claim may carry `execution_policy: "restricted"`. The daemon
rejects it before repo preparation, credential injection, provider discovery,
or environment setup. The same check runs immediately before backend Execute,
including resume fallback. No provider is treated as isolated. Unknown strings
and the empty string are rejected; omitted and null policies preserve existing
claims. `unrestricted` is a server decision, not proof of owner authorization.
Neither prompts nor metadata are inspected to infer permission.

The sink tests deliberately use a backend that writes an ephemeral marker when
started, regardless of the attack prompt. Restricted claims must make zero
Execute calls and leave no marker across repeated attempts, even with a resume
ID and permissive custom arguments. Unrestricted and legacy claims run normally.
This is admission testing, not an OS isolation or authenticated owner-action test.

## Required before activation

- Persist immutable origin on relay messages, sessions, issues and task lineage.
  Derive it from an authenticated dedicated relay principal; never accept an
  owner_direct string as authority. Mixed external context stays restricted.
- Existing task-scoped `mat_` credentials already exist in this source. Extend
  their authorization using server task origin, including issue/comment/attachment
  writes and downstream reads. Parent lineage alone cannot prevent persisted
  content from being read by an unrestricted task. Shared filesystem access also
  requires isolation, not just API provenance.
- Gate server claim, retry, rerun and reassignment. Withhold restricted tasks from
  every daemon until the server can attest support. A self-reported capability
  flag alone is insufficient. The legacy nil branch must never be used for relay
  or unknown-origin tasks. Old daemons ignore the new field entirely.
- Connect AIDO's dedicated structured relay input and rotate to fresh restricted
  sessions. Stop and reconcile queued/running legacy relay tasks before cutover.
  Do not resume their old native sessions or reuse writable workdirs as trusted.
- Verify authenticated owner instructions on a clean path, plus existing issue
  execution and retries, against a temporary DB and mock action sink. The local
  unrestricted test is not evidence of authenticated owner authorization.
- Provide a separate exact-payload, expiring, single-use owner action executor
  if restricted tasks must perform actions. Until then restricted tasks cannot
  execute any backend. OS isolation tests are a separate release gate.

No daemon restart or binary replacement is part of this patch. Before rollout,
record the target SHA/runtime/PID, affected queue, in-flight cancellation policy,
and recovery procedure in the issue. Recovery parks restricted work; it must not
route restricted claims to an older unrestricted daemon.

## Server quarantine candidate (not activated)

Migration 134 adds an immutable `is_external_relay` flag to newly issued PATs
and newly created chat sessions, plus a sticky task `execution_policy`.
A human credential can issue a relay-only PAT using the existing PAT API with
`is_external_relay: true`; machine credentials cannot issue PATs. Auth stamps
`external_relay` only after looking up that credential. Relay tokens cannot
use daemon or WebSocket auth, populate the shared human PAT cache, write issues,
comments or attachments, or mint replacement credentials.

Relay tokens can create/list/read/send quarantined chat sessions only. They
cannot use existing trusted sessions. New relay sessions have a constant title
and do not accept attachments. Internal task-token requests cannot list/read
these sessions. This prevents stored chat messages from being re-ingested by an
unrestricted task through the chat API. The queue trigger inherits restricted
policy from the session or parent, including retries and reruns, and never
permits a downgrade. Restricted task credentials are rejected on lookup.
Claim, stale-dispatch recovery, candidate enumeration, and start SQL exclude
restricted tasks for **every** daemon. No capability claim opts them back in.
They remain queued; no backend or action executor is available for them yet.
Normal authenticated owner sessions keep existing automation behavior. Sending
an owner message into a relay session does not upgrade it.

This is whole-session quarantine, not content classification or OS isolation.
The AIDO relay must use a dedicated relay PAT for **all** mixed-context traffic;
there is no safe model-driven owner/external classifier. Owner actions must use
a fresh normal authenticated session, without copying external instructions.
There is no exact-action grant executor in this candidate. Ordinary PATs remain
powerful; giving the relay an ordinary PAT bypasses the new boundary.

### Cutover and recovery gate

Do not deploy this as a mitigation until AIDO's source/configuration is connected
and an actual relay HTTP regression against this server is independently tested.
`nexai` (the audit source's actual remote) is not configured in this workspace;
the configured family-guardian-dashboard repository does not contain this relay.
No AIDO source or production configuration has been changed in this candidate.

Before activating, record affected relay session/task IDs (no message bodies or
secrets), runtime SHA and PIDs. Pause inbound relay; drain/cancel its old queued,
dispatched and running tasks and revoke their task tokens. Existing records have
no reliable origin and are **not** retroactively made safe by migration defaults.
Revoke the old broad relay PAT, invalidate all PAT caches, configure only the new
relay token, create fresh external sessions, discard old native resume IDs and
workdirs, and verify owner tasks still run on clean sessions. Already executing
processes cannot be stopped by claim SQL. Do not resume ingress before that
reconciliation and independent review. This requires an explicit operational
impact comment before daemon restart or binary replacement.

On failure pause ingress and keep external work parked. Do not fall back to a
broad PAT, trusted session or old server. Migration down refuses rollback while
relay credentials, sessions or restricted tasks remain. Reconciliation must
archive evidence securely and remove/revoke the affected data deliberately;
deleting a session alone never downgrades its retained task policy. Generic
human copy/paste and arbitrary host/shared filesystem reads are outside this
quarantine proof; external tasks never obtain host execution in the first place.
