# Kagent native text DEV dispatch

This path retains the Multica daemon's claims, task start, transcript drain and
terminal callbacks. It uses the existing alpha7 backend and local persistent
journal. It supports text chat only. It has not been deployed or qualified as a
coding runtime, distributed dispatcher or HA integration.

Set `MULTICA_KAGENT_ENABLED=1` on both server and daemon. The daemon also needs
`MULTICA_KAGENT_CONFIG` pointing to an absolute operator-owned JSON file:

```json
{
  "journal_dir": "/var/lib/multica/kagent",
  "kubernetes_endpoint": "https://approved-kubernetes-api.example",
  "kubernetes_token_file": "/run/secrets/kagent-management-token",
  "a2a_endpoint": "https://approved-alpha7-gateway.example",
  "a2a_token_file": "/run/secrets/kagent-caller-token",
  "namespace": "multica-dev",
  "harness": "dedicated-native-readonly",
  "model_config": "approved-dev-model",
  "agents": [
    {
      "workspace_id": "<Multica workspace UUID>",
      "agent_id": "<Multica agent UUID>"
    }
  ]
}
```

Use an approved dedicated harness with no broad operational tools. HTTPS and
system-trusted certificates are required. Credentials are read again per
request. Agent `custom_env` and `runtime_config` cannot supply endpoints or
transport credentials. All participating local processes must use the same
private persistent journal directory; it is not distributed fencing.

Discovery advertises `kagent` only with the environment opt-in. Registration
checks local configuration, not network reachability or remote readiness. Set
the agent's runtime to this daemon's Kagent runtime through the existing API and
save `runtime_config: {"mode":"native-text"}`. Kagent is not a custom CLI
protocol family, so it is intentionally excluded from runtime-profile creation.
The `agent_runtime.provider` database column has no enum constraint; no provider
migration is needed for this registration path. Model selection is managed by
the daemon's approved shared model configuration.

Claims omit the task credential and automatic platform skills for this explicit
mode. Transactional claim authorization and receipts remain unchanged (a task
token hash is still minted internally but its secret is not delivered). Explicit
skills, tools, attachments, repositories, environment/argument/model overrides,
issue/autopilot/quick-create tasks and other unsupported inputs are rejected
before remote dispatch. This contract has no Multica actions or local checkout.

At claim execution, the daemon verifies the workspace/agent allowlist, enrolls
or updates that agent's desired resource and reconciles readiness. It sends an
inline text-only brief containing agent instructions and workspace/project
context. It skips all local checkout, brief-file and transcript-file resume
gates. Conversation identity is workspace + agent + chat ID; attempt identity
is task ID + server attempt counter + exact `dispatched_at` claim generation. The journal stores opaque
remote session/task mappings without prompt bodies or credentials. Follow-ups
reuse that mapping, including after a daemon restart. Missing/mismatched resume
mappings fail closed; there is no automatic fresh-session retry.

Remote dispatch requires a server acknowledgement of
`kagent-process-cleanup-v1`. Unconfirmed remote cleanup fences automatic work.
Server orphan/offline/timeout recovery preserves this fence even when the
runtime's provider has since changed. The existing manual-rerun server escape
does not clear the backend journal's unresolved-attempt fence. An uncertain
send without an acknowledged task ID needs operator reconciliation; never
delete its journal to force a retry while remote effects may still be active.

Disable the opt-in or remove the enrollment to prevent subsequent dispatches;
this does not cancel an already executing turn. Normal task cancellation uses
the backend's bounded remote cancellation and terminal acknowledgement.
Create/update reconciliation happens at execution, not immediately on API
agent edits. Automatic agent archive/delete/drain resource cleanup is not wired;
the existing lifecycle deletion API requires confirmed drain, and shared
harness/model/session/snapshot resources must not be deleted by this path.

Remaining gates include DB-backed claim/recovery verification, live TLS and
authenticated endpoint qualification, actual Multica DEV task execution,
resource cleanup event wiring, retention, HA, streaming tools/usage, and any
Pi/checkout/credential/MCP parity work. Backend protocol tests and daemon
in-process HTTP tests do not satisfy those gates.
