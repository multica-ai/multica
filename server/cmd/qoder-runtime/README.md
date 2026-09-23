# Qoder Cloud Agent runtime bridge (PoC)

This standalone service registers a `qoder_cloud` runtime in one Multica
workspace. Bind a Multica agent to that runtime and assign it an issue. The
bridge reads issue context using the claim's task-scoped token, creates a
managed Qoder session, sends the work, and reports messages and the final result.
No local coding-agent CLI, repository checkout, or shell execution is performed
by the bridge. Run it on a server that can reach both APIs.

## Build and configure

From `server/`:

```sh
go build -o qoder-runtime ./cmd/qoder-runtime
```

Provide these environment variables through your service manager or secret
store. Do not commit tokens to source control.

| Variable | Value |
| --- | --- |
| `MULTICA_SERVER_URL` | Multica server origin, without `/api` |
| `MULTICA_TOKEN` | Multica runtime owner's PAT with access to the workspace |
| `MULTICA_WORKSPACE_ID` | Workspace UUID |
| `QODER_CLOUD_BASE_URL` | Qoder managed API base, including its gateway prefix (for example `https://api.qoder.com/api/v1/cloud`) |
| `QODER_CLOUD_TOKEN` | Qoder PAT with access to the configured agent/environment |
| `QODER_CLOUD_ENVIRONMENT_ID` | Existing managed Qoder Environment ID |
| `QODER_BRIDGE_STATE_DIR` | Dedicated persistent directory, writable by the service user |
| `QODER_BRIDGE_NAME` | Optional display name; defaults to `Qoder Cloud Agent` |

Alternatively, copy `config.example.json` to a private location, set its fields,
and restrict it with `chmod 600`. File fields override the environment; missing
fields inherit environment values. Unknown keys are rejected to catch typos.

```sh
./qoder-runtime --config /private/path/config.json --check
./qoder-runtime --config /private/path/config.json
```

`--check` only reads the Multica workspace and Qoder environment. It does
not register a runtime, claim a task or start execution.

Start `./qoder-runtime`. Startup checks access to the Multica workspace and Qoder environment
before registering. Copy the `runtime_id` from its registration log and bind
an existing Multica agent:

```sh
multica agent update <agent-id> --runtime-id <runtime-id>
```

Configure the model, tools and skills on the Qoder Agent. GitHub resources require
an explicit `authorization_token` when creating a Qoder session, including public
repositories. Configure repository-scoped token files in the bridge JSON:

```json
"github_token_files": {
  "https://github.com/OWNER/REPO": "/path/to/private/github-token"
}
```

Each file must be a private regular file (0600) containing only the GitHub token.
Keep it outside the repository and grant only the repository permissions needed
for the task. The bridge reads it immediately before session creation and injects
it into an ephemeral request copy. Neither the token nor its file path enters the
run journal or prompt. Restart/resume reads the current file, allowing rotation.
Missing credentials fail the task without creating a Qoder session. No workspace
wide fallback credential is used. This is bridge-hosted configuration; Multica's
web UI does not yet manage GitHub credentials for this runtime.

A project may bind one HTTPS GitHub repository and an optional branch ref.
Workspace repository listings are context only, not implicit mounts.


## Execution and recovery

- One run executes at a time per bridge. Deploy one instance per configuration
  with its dedicated persistent state volume; this PoC is not an HA service.
- Each Multica run creates a fresh Qoder session. Do not send additional messages
  directly to a bridge-owned session while its run is active. Follow-up runs receive current
  issue and triggering/coalesced comment context, not the previous conversation.
- Progress uses cursor-paginated persisted events, polled every three seconds.
  Event IDs and opaque page cursors are tracked separately. A turn is successful
  only after its user message and `session.status_idle` with `end_turn`, without
  an unrecovered error. Thread-level idle does not complete the run.
- Cancelling/deleting a Multica run requests Qoder cancellation and waits for
  the session to become idle or terminated. Losing runtime access also requests
  cancellation. The session cancellation response alone is not proof of a stop.
- Shutdown leaves remote execution running. Restart with the same directory
  to resume reconciliation; do not delete the journal while work is active.
- State is written atomically with private file permissions. It includes task
  content, session ID, event cursor and pending outcome, but no API credentials.
  An OS file lock prevents concurrent use of the same directory.
- Submission intent is persisted before sending the user message. If the HTTP
  result is ambiguous, the bridge searches event history for that exact message
  and never automatically sends it twice. If no matching event appears, inspect
  the remote session and cancel the Multica run before deliberately retrying.
  Do not clear the journal or manually resend while reconciliation is pending.
- A lost **session creation** response can leave an unused idle session; retry
  can create another idle session, but work is submitted only to the session
  whose ID was durably recorded. These unused sessions need manual cleanup.
- Final callbacks retry through reconciliation. Transcript messages carry a
  task-scoped `idempotency_key` derived from the Qoder event ID. The server derives
  a stable message UUID and inserts once; accepted replays do not create another
  row or broadcast another event. Deploy the server changes in this branch with
  the bridge: older servers ignore that key and may duplicate transcript messages.

## Current boundaries

This is an issue-execution PoC. Chat, squad coordination, local directories,
attachments, arbitrary Git hosts, tag/commit checkout, full comment-history
hydration, interactive tool approval, custom client-side tools, terminal access,
file transfer and billing/usage conversion are not implemented. Set up a Qoder
Agent that can execute autonomously with its configured tools. `requires_action`
ends the Multica run as failed with the stop reason; approvals are not granted
implicitly.

Multica custom skills and execution overrides are rejected; built-in Multica
CLI skills are omitted because their local tools are unavailable. Qoder sessions
keep any artifacts they produce; textual result and PR/MR links are returned in
the final summary, without parsing them into structured Multica branch fields.

The bridge uses the existing daemon registration protocol with
`runtime_mode=cloud` and provider `qoder_cloud`. Existing daemons that omit the
mode still register as local. No cloud-node lifecycle or update API is advertised.

## Verification

```sh
go test -race ./internal/qoderruntime ./cmd/qoder-runtime
go vet ./internal/qoderruntime ./cmd/qoder-runtime
```

Tests use local HTTP fixtures and never access real accounts or execute an
installed coding-agent CLI. Live integration still requires a configured Multica
workspace and Qoder environment; it is not covered by these tests.

To exercise a running **local** Multica server with a simulated Qoder API:

```sh
MULTICA_QODER_INTEGRATION_CONFIG=/private/path/config.json \
  go test -race -tags=integration ./internal/qoderruntime -run '^TestLocalMultica$' -count=1 -v
```

This test requires a loopback Multica URL and a test workspace. It creates an
isolated runtime, agent and issue through the real authenticated API, simulates
a lost transcript response, verifies one persisted output, then cleans up the
issue/runtime and archives the fixture agent. Qoder credentials in the file are
not used; Qoder is an in-process HTTP fixture.

## Managed configuration in the web and desktop apps

The runtime page presents a **Qoder Cloud** card with live status, associated
agent count, **Configure credentials**, and **Add agent**. Credentials are grouped
into QCA connection and GitHub repository sections; saved PATs remain hidden until
the administrator chooses **Update credentials**. The quick-add dialog creates a
Multica agent linked to an existing QCA Agent and uses the configured environment.


Workspace administrators can open **Runtimes → Add remote runtime → Qoder Cloud Agent** to configure one
managed QCA connection per workspace, load environments from the PAT and check environment access, bind GitHub
repository tokens, save/start, or stop an idle runtime. The existing project
resource picker selects the repository and starting branch. The managed form
currently targets the official `https://api.qoder.com/api/v1/cloud` endpoint;
custom endpoints remain available to the standalone bridge only.

Server setup requires `MULTICA_VCS_SECRET_KEY` (a base64-encoded 32-byte encryption
key) and `MULTICA_QODER_STATE_DIR` (a persistent writable directory). Keep the key
stable across restarts. Credentials are encrypted in `qoder_connection`, and API
responses return only presence flags and repository names. Empty password fields
keep saved credentials; removing a repository removes its credential. Saving
creates an encrypted internal Multica service credential under the administrator
who saves the connection; superseded service credentials are revoked. Removing
that member stops the supervisor from running their connection.

The API process supervises execution, restarts failed bridges, and resumes their
journals. One PostgreSQL advisory lock elects the active supervisor; deployments
with multiple API instances must share the same persistent state directory.
Stop and configuration changes refuse active tasks; finish or cancel the tasks
first. Stopping keeps credentials for a later Save and start. Do not run a
standalone bridge for the same connection alongside the managed service. When
migrating, stop the standalone bridge at idle before saving the managed config.

This does not add chat support, session continuation, or delivery validation:
QCA's end-of-turn remains execution completion, not proof that a PR was created.


### Agent associations

A runtime holds the QCA connection and environment, not a fixed QCA Agent.
After entering a PAT in the Web/Desktop runtime dialog, available environments
load automatically; one available environment is selected automatically.
Names are displayed instead of requiring copied IDs. Archived resources are excluded.

In the Multica agent creation form, select the QCA Agent under execution settings.
For existing agents, use the runtime configuration tab. The association is stored
in `agent.runtime_config.qoder_agent_id` and copied into each run journal before
session creation. Several Multica agents can share a runtime with different QCA
Agents. Configure QCA models, tools and skills in Qoder; the Multica page links
existing QCA Agents rather than creating or editing those remote resources.

The API exposes `POST /api/workspaces/{id}/qoder/environments` (admin-only,
`{"qoder_token":"..."}`, blank reuses the saved PAT), and
`GET /api/workspaces/{id}/qoder/agents` (workspace members, saved PAT).
Only IDs and names are returned; provider configuration and credentials stay
server-side. Listing follows pagination and excludes archived resources.

When upgrading an earlier PoC configuration, stop idle bridges first, copy the
old fixed Agent ID to every associated Multica agent's runtime configuration,
and remove `agent_id` from standalone config files. Runtime identity and the
journal directory now depend on connection/environment, not the fixed Agent ID;
migrate an idle journal and daemon identity together to preserve existing runtime
bindings. Never move a journal with an active run.

### Updating an early development database

The QCA migrations now use `509_qoder_connection` and
`510_qoder_connection_workspace_index` after rebasing onto upstream main.
If you ran an earlier draft with `479_qoder_connection` and
`480_qoder_connection_workspace_index`, or with `451_qoder_connection` and
`452_qoder_connection_workspace_index`, verify that the QCA table and its valid
unique index already exist, then rename only those two exact entries in
`schema_migrations` to the new names before running migrations. Preserve the
existing table, encrypted credentials, and journal. Do not mark upstream's
`451_agent_task_comment_thread` or `452_agent_task_pending_thread_unique` as
applied; the normal migration runner must still execute them.
