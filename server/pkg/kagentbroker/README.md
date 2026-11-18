# Kagent broker DEV prototype

**NOT READY FOR DEPLOYMENT.** Independent security review and TLS/network
qualification are required. No live cluster qualification has been performed.
This is a standalone prototype; it is not wired into Multica or its deployment.

The broker accepts one operator-configured namespace and deterministic agent
resource name (`multica-agent-<canonical UUID>`), agent-name annotation, harness,
and model. The policy is copied when the broker is constructed. It requires an
operator-configured ownership UUID. A separate ownership journal binds only the
UID returned by this broker's own successful create. Neither a caller assertion,
matching labels/token, nor a resource GET enrolls or adopts an existing resource.

Every accepted route requires exactly one `X-Kagent-Token` header value. That
token is sent **only in the TokenReview request body to the Kagent cluster**.
The review requests one exact audience and requires that audience alone in the
authenticated result. Username and service-account UID must both match the
configured identity. Missing review data, errors, identity changes and transport
failures deny access. There is no token cache, token logging or forwarded-user,
Host, cookie, bearer-header or impersonation identity trust. The reviewer has a
separate operator credential. Injected Reviewer/HTTP transports are trusted
operator plumbing; the standalone command always uses TokenReview.

## Surface

Only these exact paths are recognized; no queries, escaped paths, alternate
agent names, collection reads, watches, discovery or subresources are accepted:

| Method | Path | Behavior |
| --- | --- | --- |
| GET | `/apis/api.kagent.dev/v1alpha3/namespaces/<namespace>/agents/<name>` | Read, verify pins and restricted spec, reconstruct response |
| POST | `/agents/<namespace>/<name>` | Reconstruct SendMessage, GetTask or CancelTask |
| POST | Kubernetes collection path | Durable create intent, one POST, verified UID commit; exact committed replay |
| PATCH | Kubernetes resource path | Bound UID/token and caller RV; one conditional restricted merge patch |
| DELETE | Kubernetes resource path | Validate DeleteOptions, then deny |

Kubernetes DELETE remains disabled: UID/resourceVersion preconditions alone are
not trusted remote drain proof. Create and patch use the restricted real
KubernetesStore wire shapes, including empty `status: {}` on create and explicit
null clears on patch. Nonempty create status, unknown fields, unsafe capabilities,
missing preconditions and terminating resources are denied.

The mandatory ownership directory is separate from the send-attempt journal.
Explicit `-initialize-ownership` creates an **absent** private directory with
Uninitialized state. Omit that flag on restart. Existing directories cannot be
initialized again; missing, corrupt, unsupported-version or policy-mismatched
records fail startup. No migration or adoption of previously provisioned agents
is supported. Leave `-uid` unset; the journal establishes the UID. The ownership
UUID and all policy fields must remain identical across restarts. Policy changes
require a separately reviewed lifecycle, not editing or resetting this journal.

A create first checks the exact name and requires an actual upstream HTTP 404,
not a JSON Status claiming absence. It then fsyncs CreateIntent before one POST.
Only HTTP 201 with complete matching identity, owner token, allowed spec, UID,
resourceVersion and valid readiness data can publish Bound. Both intent and bound
records use a temporary file, file fsync, atomic rename and directory fsync. An
exclusive lifetime lock on a separate stable lock file prevents multiple owners
while records are replaced. File/lock modes must be 0600 and directory mode 0700;
symlink paths and multiply linked files are rejected. Supply a canonical absolute
path (on macOS, `/private/tmp/...`, not the `/tmp` symlink). Keep the directory and
its ancestors outside caller control and on a filesystem supporting flock,
atomic rename and fsync. These local guarantees do not coordinate independent
filesystems or resist operator rollback/replacement of the ownership directory.

Any POST transport error, conflict, malformed/unsafe result or restart before a
verified UID commit leaves CreateIntent fenced. There is no remote retry or GET
adoption, even when a live resource copies every annotation. Persistence errors
poison the running ownership fence, including uncertain rename/fsync outcomes.
A caller may replay a **committed** create with the exact canonical JSON digest
and ownership token: it performs a live GET, verifies the same bound UID and
restricted spec, and returns 201 without POST. JSON key order is immaterial; other
request differences are rejected. This returns current verified data, not cached
output. The parent caller's exact replay integration remains separate work; a
client must not infer ownership merely because GET succeeded after a lost create
response. CreateIntent has no automatic reset or recovery route.

PATCH requires Bound, caller UID/token and resourceVersion, then verifies the live
UID/token/spec and exact current resourceVersion. It issues the validated merge
patch once with the original preconditions and null leaves preserved. HTTP 409
is returned as a sanitized conflict; transport failures and invalid results are
not retried. The response must retain identity and match the requested owned
leaves. PATCH does not grant new ownership. GET preserves actual upstream 404
and validated generation/readiness conditions; it does not synthesize readiness
or reveal unbound existing resources. CreateIntent denies reads and execution.

An exact-resource GET precedes every A2A call. New sends verify the configured
UID, ownership UUID, labels, name, namespace, harness and model. Terminating
resources and unknown spec/template capabilities deny new work. No arbitrary
tools, skills, MCP, inline harness, template reference, cross-namespace/model
override or prompt reference is allowed. Only the configured harness/model name
references and inline description/systemPrompt text are accepted. This is a
capability-field allowlist, **not a zero-tool guarantee**: alpha7 includes built-in
`ask_user` even when no tools are configured. Reviewed shared harness/model
contents and runtime/network isolation remain necessary.

Already-bound GetTask/CancelTask require the original resource identity (including
UID and ownership UUID), but tolerate spec drift and deletionTimestamp. This
keeps recovery/cleanup available when capability verification would reject a new
send. **Kubernetes/TokenReview outages, deleted resources and UID replacement
still block recovery/cleanup.** Alpha7 routes interactions by namespace/name and
checks stored session Agent membership by namespace/name; its public API has no
UID precondition. A cached successful preflight cannot prove that the URL still
identifies the original Agent during an outage. Bypassing this check would require
independently qualified immutable routing/identity controls. No such controls are
implemented or claimed here. The remaining TOCTOU between a successful check and
dispatch also requires external immutability/isolation controls.

A2A requires JSON-RPC 2.0 with canonical UUID request IDs. SendMessage accepts one
bounded text part, ROLE_USER and exactly the real client configuration
(`returnImmediately: true`, `acceptedOutputModes: ["text/plain"]`). **A new session
must omit contextId.** A continuation can supply only a context recorded from a
correlated SendMessage through this broker. Unbound contexts and task IDs deny
before Kubernetes/A2A dispatch. Files, caller metadata, push notifications,
streaming, alternate routing and unknown request fields fail closed.

Only short in-memory transitions hold the binding mutex. GetTask and CancelTask
do not wait on send I/O or caller response writes. A session permits one send at
a time; concurrent sends for that session and replayed message IDs return 409
without waiting. All upstream requests inherit the caller's context/deadline.
Send reservations are made after preflight and rechecked atomically before the
single dispatch. A mandatory attempt journal fsyncs the caller's message ID before
network dispatch. It uses a cancellable disk-write gate outside the broker lock;
disk failures never authorize a send. A new session uses a broker-generated
upstream message UUID because alpha7's session-create deduplication otherwise
lets a caller select an existing accessible session using its old message ID.
Continuation messages retain the caller's ID within their already-bound session.
Independent initial sessions can dispatch concurrently.

A correlated, canonical task/context identity is retained **before** validating
optional task output or checking returned text for credentials. Thus malformed
history/status/artifacts or credential reflection cannot discard a known recovery
binding. Such a failed send leaves its session blocked for additional turns;
authenticated bound GetTask/CancelTask remain possible. A transport failure,
unparseable/oversized response, mismatched RPC identity, invalid task/context
identity or conflicting binding leaves acceptance uncertain and blocks all
subsequent sends. Known bindings are preserved. There is no retry, raw response
replay, implicit adoption, or automatic recovery reset. Message IDs are retained
as replay-denial tombstones, including successful sends. Requests rejected before
journal reservation consume no capacity. Cancellation after a durable reservation
may conservatively consume one record without dispatching; that record is never
removed or reused.

Task/context bindings are **memory-only**, while caller message replay records are
**durable**. Restart denies old task IDs and continuation contexts; loading a
message tombstone grants no task authority. Replaying a previously reserved caller
message ID is denied across restarts. Newly admitted initial sends use a random
broker-generated upstream ID, so they cannot select an old alpha7 session via
its message ID. The broker does not cache or replay output. After an ambiguous
send, callers must not invent a different message ID to retry the same logical
work; semantic deduplication across different IDs is impossible here.

The journal stores only a version header and up to 1024 canonical caller message
UUIDs. Creation fsyncs both the file and its directory; every reservation is
fsynced before dispatch. A lifetime nonblocking exclusive file lock rejects a
second owner. Torn/malformed/oversized records and group/world-readable files
fail startup. Write/sync failures poison the running send fence. Existing task
recovery does not use the disk gate. File locking is implemented for the named
Unix build targets; other platforms fail closed. Disk fsync itself cannot be
interrupted, but it holds no broker lock and the context is checked again before
dispatch. Protect the journal file, its parent directory and path from callers;
retain them across restarts. Deletion, replacement, rollback or manual rotation
can erase the replay fence and are not supported reset procedures.

Full task recovery after restart still needs a durable task/context/accepted-send
journal and an authenticated message-to-task lookup/proof-of-nonacceptance
protocol. In particular, a caller that never received a task ID cannot discover
a retained binding through the current routes. Those capabilities remain
deployment blockers.

### Bounded lifecycle and retirement blocker

At most 1024 durable send reservations (including successful and uncertain sends)
are retained per journal, across restarts. Task, context and busy maps are bounded
by those reservations. At capacity, additional sends return 403; bound GetTask and
CancelTask still work. No active, uncertain, or recovery binding is evicted.

Automatic retirement is deliberately unavailable. A terminal public task state
alone proves neither that the client durably saved the result nor that runtime
cleanup is settled; elapsed time also provides no recovery acknowledgement.
Safe retirement would need an authoritative settled terminal boundary, durable
client acknowledgement ending recovery rights, no in-flight session turn, and a
durable replay tombstone (or an enforced upstream replay-expiry contract) before
removing an attempt from the bounded admission ledger. The current client/broker
protocol exposes no such acknowledgement or expiry contract. Adding a timer or LRU would silently lose
recovery authority and permit replay. **The lifetime capacity/retirement problem
remains unsolved; this bound is an explicit DEV availability limitation.**

All input JSON rejects recursive duplicate keys (including escaped equivalent
keys), case-variant and unknown schema keys, invalid UTF-8, trailing data and
excessive nesting. Bodies are limited to 64 KiB and individual prompt/text values
to 16 KiB. Outbound requests are new requests with operator credentials and JSON
headers only; redirects are disabled, cookies are not persisted, and incoming
headers are never copied. Responses are bounded to 1 MiB and parsed before use.
Agent reads preserve validated nonnegative integer generation (at most 2^53-1)
and up to 32 unique conditions, with bounded type/reason/message, known condition
statuses, integer observedGeneration and RFC3339 transition times. Missing or
stale readiness data is never synthesized into Ready=True. Other status fields
are omitted. Only reconstructed Agent data or a validated text-only task result
is returned;
upstream status errors, RPC errors, headers and raw error bodies are not relayed.
Decoded credential-string reflection in returned fields is rejected, including
escaped JSON forms. Bounded optional A2A metadata, extension lists and history are
validated and removed rather than echoed. Status messages may carry the matching
taskId/contextId; message IDs are bounded strings as in the SDK, not assumed to
be UUIDs. Status timestamps are validated. Unsupported non-text parts still fail
closed: this does not implement ask_user/HITL or arbitrary extension semantics.

## Standalone command and qualification gaps

`go run ./cmd/kagent-broker -help` lists the required configuration flags. The
command requires `-dev-prototype`, exact identity/resource policy, three credential
files, private durable `-attempt-journal` and `-ownership-journal` paths, HTTPS
upstream origins, and a
listener TLS certificate/private key. It listens only on `127.0.0.1:8443`, with TLS 1.3 minimum and bounded HTTP timeouts.
Credentials are loaded once; restart with updated files to rotate them. Keep the
attempt and ownership journals intact during credential rotation. Do not put
bearer credentials in flags. Keep journals, upstream origins and configuration
outside caller control; missing policy fields fail startup.

Qualification must establish trust roots, peer identity, proxy behavior, DNS and
network egress, gateway isolation, least-privilege reviewer/resource credentials,
and reviewed harness/model contents. The preflight resource GET is not atomic
with alpha7 execution: alpha7 A2A has no UID/resourceVersion precondition. This
prototype cannot prevent an external writer from replacing/changing the resource
between validation and dispatch, or prove the insecure controller enforces agent
and context isolation internally. Those are deployment blockers, requiring
independent analysis and external immutability/isolation controls. The broker is
not a sandbox for generated code or built-in ask_user and does not qualify
controller security, task durability, availability, or runtime tool/network access.

Direct caller access to Kubernetes or A2A would bypass broker policy entirely.
Blocking that access, isolating gateway credentials and qualifying those network
controls are external deployment requirements, not properties this code proves.

Tests use in-memory transports and the real pkg/kagent clients. They do not
contact a cluster, resolve user agent CLIs, or publish anything.

## Offline source and fixture provenance

The reviewed source is `/tmp/kagent-src`, tag `v1.0.0-alpha7`, commit
`542e0a7a82f0f32d09c398f9c56319a3f617c93a`. Its `go/go.mod` pins
`github.com/a2aproject/a2a-go/v2 v2.6.0`. Relevant source:

- `go/core/internal/service/session/interactions.go`: `resolveSend` resolves a
  supplied context, `storedSession` checks namespace/name membership, and
  `taskSession` resolves task-to-session before Get/Cancel. `resolveSend` also
  deduplicates initial session creation using a request UUID derived from the
  message ID, which is why the broker generates the upstream initial ID.
- `go/core/internal/a2agateway/interactions.go`: send/cancel and settled-result
  recovery; public A2A requests have no Kubernetes UID precondition.
- `go/core/internal/database/task_history_test.go` and `task_store_test.go`:
  reconstructed history, message taskId/contextId and saved task boundaries.
- `go/api/a2a/metadata.go`: gateway task-created-at and timeline-position metadata.
- SDK `a2a/core.go`: actual JSON tags, enums and task/message/part serializers.

`testdata/alpha7-task.json` was serialized offline using the actual pinned SDK
and `testdata/generate.go`, not captured from a live cluster. It includes realistic
reconstructed history, non-UUID agent message IDs, taskId/contextId, timestamps,
text artifacts and extension metadata. Placeholder credential strings deliberately
exercise stripping of private optional content. The fixture is wrapped in real
SendMessage/GetTask/CancelTask envelopes and consumed through pkg/kagent clients
in the tests. Reconciler readiness is also exercised with its actual consumer.

To regenerate without changing server dependencies, copy `testdata/generate.go`
into a temporary Go module, require `github.com/a2aproject/a2a-go/v2 v2.6.0`, then
run `go run -mod=mod generate.go > alpha7-task.json`. The source file's build tag
keeps it outside default package builds. This regeneration requires the pinned
SDK and its dependencies locally or via the module proxy.
