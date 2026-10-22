# Qoder Cloud Agents runtime bridge (PoC)

This standalone service registers a `qoder_cloud` runtime in one Multica
workspace. Bind a Multica agent to that runtime and assign it an issue. The
bridge reads issue context using the claim's task-scoped token, creates a
managed Qoder session, sends the work, and reports messages and the final result.
No local coding-agent CLI, repository checkout, or shell execution is performed
by the bridge. Run it on a server that can reach both APIs.

## Issue status

For issues still assigned to the executing Qoder agent, the server moves
`todo` or `backlog` to `in_progress` when execution starts. A successful run
moves that issue to `in_review`, never directly to `done`, only if its revision
is unchanged and no newer task has been created for it. Any intervening issue
edit or reassignment prevents the completion transition. Custom status columns,
triage, and issues already in progress, review, or a terminal state are left
unchanged. Failed and cancelled runs do not advance the issue to review.

These transitions and their activity entries commit with the task transition;
connected clients receive status and activity updates. Existing runs started
before this support was deployed have no revision marker and are not advanced
automatically.

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
| `MULTICA_TOKEN` | Multica workspace administrator PAT (standalone mode only) |
| `MULTICA_WORKSPACE_ID` | Workspace UUID |
| `QODER_CLOUD_BASE_URL` | Qoder managed API base, including its gateway prefix (for example `https://api.qoder.com/api/v1/cloud`) |
| `QODER_CLOUD_TOKEN` | Qoder PAT with access to the configured agent/environment |
| `QODER_CLOUD_ENVIRONMENT_ID` | Existing managed Qoder Environment ID |
| `QODER_BRIDGE_STATE_DIR` | Dedicated persistent directory, writable by the service user |
| `QODER_BRIDGE_NAME` | Optional display name; defaults to `Qoder Cloud Agents` |

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
wide fallback credential is used. This is the standalone bridge's configuration;
managed connections set repository tokens in the web and desktop apps instead
(see below).

A project may bind one HTTPS GitHub repository and an optional branch ref.
Without one, the agent's default repository (`runtime_config.repository`) is
mounted, and failing that the workspace's only repository when a credential is
configured for it. Other workspace repositories are context only.

The prompt asks the agent to push a branch named `multica/<identifier>-<slug>`
and to prefix the pull request title with the issue identifier, so Multica's
GitHub integration links the pull request back to the issue.


## Execution and recovery

- Up to `max_concurrency` runs (default 1 for this binary, 3 for managed
  connections, at most 10) execute at once per bridge; further tasks stay
  queued in Multica. Deploy one instance per configuration with its dedicated
  persistent state volume; this is not an HA service.
- A follow-up run on the same issue continues the agent's previous Qoder session
  when that session is idle, uses the same Qoder agent and environment, and no
  other run is using it. Otherwise a fresh session starts with the current issue
  and triggering/coalesced comment context. Do not send additional messages
  directly to a bridge-owned session while its run is active.
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
  the remote session before deliberately retrying. Unconfirmed submissions expire
  after two minutes; all runs have a one-hour deadline. Cancellation is attempted
  before failure is reported. Failed cancellation remains in a durable cleanup
  queue, independently of the next task. Do not manually resend pending work.
- A lost **session creation** response can leave an unused idle session; retry
  does not automatically create a second session. The run fails with an explicit
  unconfirmed-creation error. An unused session whose ID was never received needs
  manual provider-side inspection; no task message is sent to it.
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


Workspace administrators can open **Runtimes → Add → Cloud services → Qoder Cloud Agents** to configure one
managed QCA connection per workspace, load environments from the PAT and check environment access, bind GitHub
repository tokens, and **Connect**. The Qoder Cloud Agents page can later **Pause** an idle runtime, resume it, or
**Disconnect** it. The menu entry only appears when the server has `MULTICA_VCS_SECRET_KEY` set. The existing project
resource picker selects the repository and starting branch. The managed form
offers Qoder's official regions, `https://api.qoder.com/api/v1/cloud` (Global) and
`https://api.qoder.com.cn/api/v1/cloud` (China); a PAT works only in the region
that issued it. Custom endpoints remain available to the standalone bridge only.

Server setup requires `MULTICA_VCS_SECRET_KEY` (a base64-encoded 32-byte encryption
key). Keep the key stable across restarts. Credentials and the run journal are
encrypted in `cloud_agent_connection`, so no local state directory is needed, and API
responses return only presence flags and repository names. Empty password fields
keep saved credentials; removing a repository removes its credential. Saving
creates an encrypted workspace/daemon-scoped credential with a one-hour lifetime.
The supervisor renews it only while the connection is enabled and its owner is an
administrator. Save and Stop revoke the old credential and invalidate its cache.
Membership/role checks also gate managed daemon requests. Removing or demoting
that administrator retires the bridge and schedules remote cancellation.

The API process supervises execution, restarts failed bridges, and resumes their
journals. One PostgreSQL advisory lock elects the active supervisor. Managed
journals are encrypted in PostgreSQL, including pending cancellations; API
instances do not need shared local disks. Standalone journals remain local.
Remote calls run outside database transactions under a bounded step lease;
Save/Stop may return a retryable conflict while a step is synchronizing.
Stop and configuration changes refuse active tasks; finish or cancel the tasks
first. Stopping keeps QCA/repository credentials for a later Save and start, but revokes the internal daemon token. Do not run a
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

The QCA migrations now use `573_qoder_connection` and
`574_qoder_connection_workspace_index` after rebasing onto upstream main.
If you ran an earlier draft with `564_qoder_connection` and
`565_qoder_connection_workspace_index`, with `509_qoder_connection` and
`510_qoder_connection_workspace_index`, with `479_qoder_connection` and
`480_qoder_connection_workspace_index`, or with `451_qoder_connection` and
`452_qoder_connection_workspace_index`, verify that the QCA table and its valid
unique index already exist, then rename only those two exact entries in
`schema_migrations` to the new names before running migrations. Preserve the
existing table, encrypted credentials, and journal. Do not mark upstream's
`451_agent_task_comment_thread` or `452_agent_task_pending_thread_unique` as
applied; the normal migration runner must still execute them.

Migration 575 revokes the early PoC's user-wide internal PATs and pauses existing
connections. Before upgrading, let old executions finish or cancel them while the
old bridge is still running. After migration, save the connection once to issue a
scoped daemon credential; QCA and repository secrets are preserved. The managed
daemon identity changes once during this upgrade; select the new managed runtime
for agents previously bound to the old PoC runtime. Old local
managed journals are not imported: archive/remove their private directories only
after confirming no old remote work remains. Keep the encryption key stable.

Workspace deletion first pauses its connection and cancels remote work. If a
step or provider cancellation is still pending, deletion returns a conflict and
must be retried; the encrypted recovery record is retained. Retired connections
are retried by the supervisor, and task prompts are removed after cleanup.
Catalog requests are limited to six per workspace per minute and ten pages per
request. A failed bridge restarts with bounded exponential backoff.

If lifecycle migration `566_qoder_lifecycle` was already applied, rename that
exact ledger entry to `575_qoder_lifecycle` as well; do not re-run its credential
revocation and connection pause. Do not rename upstream migrations sharing the
old numeric prefixes.
