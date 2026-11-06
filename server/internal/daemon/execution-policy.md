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
AIDO relay source has since been published in family-guardian-dashboard at
`relay-base/nex1565-prod-4d7d302`; its candidate PR #1 (`196170c`) connects the
dedicated credential and passes a synthetic actual-relay HTTP regression.
The nexai root repository is not the relay source. No production configuration
has been changed. Both candidates remain unsuitable for activation because
quarantining every AIDO session also parks authenticated owner conversation.

### Message-level authority design gate

The router subsequently declined device signing and per-action owner grants.
The selected alternative is to remove external-reading capability from the
owner lane while retaining automatic actions, and use a separate text-only
worker for external material. Owner-pasted text is an explicitly accepted
residual risk. The earlier grant proposal below is historical, not a new gate.

Pre-implementation audit found a blocking capability: the current owner agent
uses Claude's default built-in tools with bypassPermissions and no tool
allowlist. Bash/file reads, inherited user environment and same-user filesystem
access are not separated by strict MCP configuration or removal of custom env.
Arbitrary Python/shell can implement both actions and external reads. Codex's
auto-approval path does not supply an equivalent separation either. Do not
relax restricted admission based on removing a named Gmail tool or env key.
See relay PR #1 `docs/NEX-1565-capability-split-audit.md` for sanitized agent
inventory, synthetic counterexample and the precise unverified acceptance
criteria. Runtime implementation stopped per the router's explicit stop rule;
an OS/service capability boundary needs a separate scope decision first.

See family-guardian-dashboard PR #1, `docs/NEX-1565-message-authority-design.md`.
The current AIDO request authenticates a bearer but supplies a single content
string; typed and pasted identical bytes have indistinguishable provenance.
An owner principal, caller origin field, or device signature alone does not
make embedded external instructions authoritative. A device signature proves
the device sent a payload, not where all text inside it originated.

Do not remove the sticky session/parent policy to implement turn toggling.
External history, native resume state and workdirs must not flow into an
unrestricted turn merely because the next message is authenticated as owner.
The proposed replacement requires immutable message envelopes, registered
device authentication with replay protection, exact single-use owner action
grants checked at the side-effect sink, and separated execution contexts.
Free-form arbitrary CLI automation cannot simultaneously be promised as
unchanged and treated as an exact owner-authorized action without a new grant
contract. This product/authentication scope needs a router decision first.

External summaries require a separate capability-limited text-only worker,
not an exception to the current daemon admission rejection. No such worker
exists in this candidate. It must receive only scoped snapshots, lack host
tools/filesystem/credentials and arbitrary network access, and return inert
text that cannot mint tasks or trigger relay-local Gmail execution. Test real
response completion, action count zero, and negative isolation cases before
enabling it. This is a design proposal, not an implementation or a security
proof. Keep existing SQL/daemon rejection and rollback guards until the new
boundary is implemented and independently validated.

### OS boundary stage 1 implementation status

The router has now authorized a dedicated non-admin macOS identity as stage 1,
with network egress restrictions deferred to stage 2. `_aido1565` UID/GID 490
was created after the issue's operational impact comment. Production home/state
permissions, relay and daemon were not changed. No launchd service was installed.
`deploy/aido-stage` contains a disabled plist plus runtime configuration and
rollback gates; it is not a running or registered Multica runtime.

Relay PR #1 adds a private persistent NEX-1564 nonce bridge, send/write-only
action service and explicit primary-user factory, bounded text/display adapter,
calendar-note timestamp sanitization and a synchronous Unix-user/UDS staging
probe. Real new-user access to 8 Gmail paths, main home, relay state and nonce DB
was denied without reading credential bytes. Mock send: valid=1,
forged/missing/replay=0. The mock text adapter returns a reply with no owner
history injection. These are component proofs, not a connected production
model/daemon/relay integration test.

Dedicated agent/runtime registration, minimum task-token API scope, model login,
restricted transport/display wiring and restored owner attachment/partial/stop
integration are still release gates. Keep admission rejection unchanged. Do not
hand the new user a broad PAT or assume the current default task token's scope
has been narrowed by an OS user boundary. See relay
`docs/NEX-1565-os-stage.md` for precise verified/unverified behavior.

Subsequent candidate code implements the API scope gate in migration 135:
immutable agent/runtime binding, sticky is_aido_owner task-token flag, human
admin binding API/CLI, current-task message reads and same-session attachment
download only. Existing internal tokens retain their behavior. Binding removal,
runtime movement, token downgrade and unsafe rollback are guarded. This is a
server capability policy, not OS UID attestation. No live binding was created.
Relay's isolated owner mode and external display transport are opt-in and
disabled in production. See `docs/NEX-1565-owner-integration.md` in relay PR #1.
The remaining gate is real provisioning plus the connected UID490 daemon/DB
regression, rather than treating component tests as that full proof.

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
