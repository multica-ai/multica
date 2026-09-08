# Controlled concurrency candidate

This opt-in server/daemon protocol extends run-owned rooms with an immutable
launch manifest, native capacity admission, and one transactional board writer.
An ordinary profile, prompt, task token, PAT, or request header cannot grant
controller authority. Unenrolled issues keep their existing behavior.

## Configuration and enrollment

The API requires `MULTICA_CONTROLLER_TOKEN_SHA256` (SHA-256 of an independently
generated `mct_` bearer), `MULTICA_CONTROLLER_WORKSPACE_ID`, and an explicit comma
separated `MULTICA_CONTROLLER_ALLOWED_HOST_IDS`. Keep the bearer exclusively in
the controller's private credential file. `MULTICA_BIND_HOST=127.0.0.1` limits a
local test server to loopback; the default remains all interfaces.

The native daemon additionally takes `MULTICA_CONTROLLER_DAEMON_TOKEN`, a
separately provisioned `mdt_` token bound to that workspace and daemon ID. Its
ordinary login still bootstraps registration. The bound token is used only for
native task and runtime endpoints and is filtered from provider environments.
Token issuance currently uses the deployment's trusted provisioning path; this
candidate does not give task workers a token-issuance endpoint.

`GET /api/controller/capabilities` advertises protocol version 1.
`GET /api/controller/profiles/{id}` returns the immutable definition digest.
`POST /api/controller/issues/{id}/enroll` accepts only a drained issue, exact
scope, explicit source path and repository commit allowlist, accepted profile /
runtime / host targets, seven native status mappings, select Progress and text
Current Picture properties, and a measured shared capacity budget with expiry.
Profiles require an explicit model; custom runtime-profile commands are refused.
The digest covers saved executable inputs, enabled MCP bindings, workspace skill
contents/files, and embedded product instructions/skills. Claim constructs its
payload from the same typed snapshot it validates; start refuses later drift.

## Controller operations

- `GET /api/controller/issues/{id}`: native issue, controller revision, evidence
  version, bounded timeline, native runs and immutable launch manifests.
- `POST .../launch`: exact action/attempt and full request replay, native durable
  queue insert and manifest commit in one transaction. Retry requires the failed
  parent, preserves original queue age, and respects durable `not_before`.
- `PUT .../projection`: issue and evidence CAS; native status, Progress, Current
  Picture, metadata, event and outbox commit together. Queued requires native
  waiting work; Working requires a running native attempt with a session ID.
- `POST .../stop`: revoke launch/effect authority, atomically project Blocked and
  the stop reason, then cancel native attempts. Replaying the exact event retries
  cancellation without another projection revision.
- `POST .../release`: after stop or a terminal owner decision, require all native
  runs drained, all executing effects reconciled, and the final projection read
  back; then remove the controller guard while preserving every immutable receipt.
  Re-enrollment establishes a fresh scope and fresh action identities.
- `GET .../outbox` and `POST .../outbox/ack`: recover committed projection work
  after restart and acknowledge matching native readback. This is not a receipt
  that a particular browser received a WebSocket message.
- `POST .../effects`: bounded reservation / begin / acknowledgment ledger with
  resource fences and permanent operation identity. Executing ambiguity is held.

The reconciled Python `controller_run.py` is an explicit-cohort replacement for
the legacy rollout writer. `--plan` sends GET requests only. `--apply` journals
the exact request in SQLite before transmission, replays ambiguous outcomes, and
marks success only after native readback. Never run both writers on the same
cohort. Existing owner Needs You and terminal gates remain authoritative.

## Release limits

This is a bounded enrollment version. STOP immediately and permanently revokes
the current enrolled authority epoch. Once the final projection is acknowledged,
native work is drained, and executing effects are reconciled, release removes the
guard without deleting historical receipts. Policy renewal uses release followed
by enrollment with a fresh scope and fresh action identities; it is never inferred
from a worker response. Ordinary protected issue writes, deletion, runtime teardown
and unbound orphan recovery are refused while enrollment owns them.

The effect endpoint advertises `external_effect_gateway=false`. This is a
durable lease ledger, not an external-write gateway or an OS sandbox. Native
provider executables and host configuration remain part of the trusted host.
Local tests, a running local candidate, installed app activation, SaaS backend
deployment and observed production behavior are separate evidence lanes.
