# Claude Managed Agents runtime bridge (Phase 1)

This standalone service registers a `claude_managed` runtime in one Multica
workspace. Bind a Multica agent to that runtime, link it to an existing Claude
agent, and assign it an issue. The bridge reads issue context using the claim's
task-scoped token, creates a Claude Managed Agents session in a configured
environment, and reports messages and the final result back to Multica. No
local coding-agent CLI, repository checkout, or shell execution is performed by
the bridge. Run it on a server that can reach both APIs.

The bridge is modeled on the Qoder Cloud Agents bridge (`cmd/qoder-runtime`)
and keeps its structure; this document describes only the Claude behavior.

## Issue status

For issues still assigned to the executing agent, the server moves `todo` or
`backlog` to `in_progress` when execution starts. A successful run moves that
issue to `in_review`, never directly to `done`, only if its revision is
unchanged and no newer task has been created for it. Any intervening issue
edit or reassignment prevents the completion transition. Custom status columns,
triage, and issues already in progress, review, or a terminal state are left
unchanged. Failed and cancelled runs do not advance the issue to review.

These transitions and their activity entries (source `claude_managed`) commit
with the task transition; connected clients receive status and activity
updates.

## Build and configure

From `server/`:

```sh
go build -o claude-managed-runtime ./cmd/claude-managed-runtime
```

Provide these environment variables through your service manager or secret
store. Do not commit tokens to source control.

| Variable | Value |
| --- | --- |
| `MULTICA_SERVER_URL` | Multica server origin, without `/api` |
| `MULTICA_TOKEN` | Multica workspace administrator PAT |
| `MULTICA_WORKSPACE_ID` | Workspace UUID |
| `ANTHROPIC_API_KEY` | Workspace-scoped Anthropic API key (see below) |
| `ANTHROPIC_BASE_URL` | Optional API base; defaults to `https://api.anthropic.com/v1` |
| `CLAUDE_MANAGED_ENVIRONMENT_ID` | Existing Claude Managed Agents environment ID |
| `CLAUDE_MANAGED_STATE_DIR` | Dedicated persistent directory, writable by the service user |
| `CLAUDE_MANAGED_NAME` | Optional display name; defaults to `Claude Managed Agents` |
| `CLAUDE_MANAGED_VAULT_IDS` | Optional comma-separated vault IDs attached to every new session (see [Pull requests](#pull-requests)) |

Alternatively, copy `config.example.json` to a private location, set its fields,
and restrict it with `chmod 600`. File fields override the environment; missing
fields inherit environment values. Unknown keys are rejected to catch typos.
The file also accepts `max_concurrency`, `github_token_files` and `vault_ids`.

```sh
./claude-managed-runtime --config /private/path/config.json --check
./claude-managed-runtime --config /private/path/config.json
```

`--check` only reads the Multica workspace, the Claude environment and any
configured vaults. It does not register a runtime, claim a task or start
execution.

API base URLs must use HTTPS; plain HTTP is accepted only on loopback, for
local testing.

### Workspace-scoped API key

Requests carry the key as a Bearer token with `anthropic-version: 2023-06-01`
and `anthropic-beta: managed-agents-2026-04-01`. The bridge never sends
`anthropic-workspace-id`, so the key must belong to the Anthropic workspace
that owns the environment and agents. When Anthropic answers that a workspace
header is required, the error reports that a workspace-scoped key is needed.

## Binding a Multica agent

Start `./claude-managed-runtime`. Startup checks access to the Multica workspace
and the Claude environment before registering. Copy the `runtime_id` from its
registration log and bind an existing Multica agent:

```sh
multica agent update <agent-id> --runtime-id <runtime-id>
```

Link the Multica agent to a Claude agent through its runtime configuration:

```json
{
  "claude_agent_id": "agent_...",
  "repository": { "url": "https://github.com/OWNER/REPO", "ref": "main" }
}
```

`claude_agent_id` is required. `repository` is an optional default repository
with an optional branch. Multica checks both when the agent is saved and also
refuses the execution overrides listed under [What is rejected](#what-is-rejected). Several Multica agents can share one runtime with
different Claude agents. Configure the model, tools and skills on the Claude
agent; the bridge does not create or edit Claude agents or environments.

Set every tool on the Claude agent to the `always_allow` permission policy.
The bridge does not answer tool confirmations, so a run that stops for one
(an `always_ask` tool, or an `auto` decision that pauses) fails with
`Claude stopped: requires_action`. MCP toolsets default to `always_ask` and
need the policy set explicitly.

## Repository tokens

Each mounted GitHub repository needs an explicit authorization token, including
public repositories. Configure repository-scoped token files in the bridge JSON:

```json
"github_token_files": {
  "https://github.com/OWNER/REPO": "/path/to/private/github-token"
}
```

Each file must be a private regular file (0600) containing only the GitHub
token. Keep it outside the repository and grant only the repository permissions
needed for the task. The bridge reads it immediately before session creation
and injects it into an ephemeral request copy as the resource's
`authorization_token`. Neither the token nor its file path enters the run
journal or prompt. Restart/resume reads the current file, allowing rotation.
Missing credentials fail the task without creating a session. No
workspace-wide fallback credential is used.

A project may bind one HTTPS GitHub repository and an optional branch ref.
Without one, the agent's default repository is mounted, and failing that the
workspace's only repository when a credential is configured for it. Other
workspace repositories are listed in the prompt as context only.

The prompt asks the agent to push a branch named `multica/<identifier>-<slug>`
and to prefix the pull request title with the issue identifier, so Multica's
GitHub integration links the pull request back to the issue.

## Pull requests

A mounted repository gives the session git access only: the agent can push
its branch but not open a pull request. To open one, give the Claude agent the
GitHub MCP server and attach a vault holding its credential:

1. Add the server and its toolset to the Claude agent. The toolset defaults to
   `always_ask`, which the bridge cannot answer, so set `always_allow`:

   ```json
   "mcp_servers": [{ "type": "url", "name": "github", "url": "https://api.githubcopilot.com/mcp/" }],
   "tools": [
     { "type": "agent_toolset_20260401", "default_config": { "permission_policy": { "type": "always_allow" } } },
     { "type": "mcp_toolset", "mcp_server_name": "github", "default_config": { "permission_policy": { "type": "always_allow" } } }
   ]
   ```

2. Create a vault and store a GitHub token for that server URL as a
   `static_bearer` credential. A fine-grained token limited to the mounted
   repositories, with **Pull requests: Read and write**, is enough:

   ```json
   { "display_name": "GitHub", "auth": { "type": "static_bearer", "mcp_server_url": "https://api.githubcopilot.com/mcp/", "token": "<GitHub token>" } }
   ```

3. Allow the server's host in the environment's network policy: set
   `allow_mcp_servers: true`, or add `api.githubcopilot.com` to
   `allowed_hosts`. Otherwise Claude rejects session creation.

4. Set the vault ID in `vault_ids` or `CLAUDE_MANAGED_VAULT_IDS`; `--check`
   verifies it.

Vaults attach only when a session is created, so changing `vault_ids` makes
follow-up runs start a new session instead of continuing the previous one.
Without a vault the agent still pushes its branch and reports it in the
result.

## What is rejected

Chat and quick-create with the agent are refused before anything is saved.
A claimed task fails before any session is created when it is:

- a chat task, a squad leader task, or not tied to an issue;
- an issue with attachments;
- for a Multica agent without a valid `claude_agent_id`;
- for a Multica agent with custom skills, skill references, custom environment
  variables or arguments, an MCP configuration, or a model, thinking level or
  service tier override;
- using Multica connected apps, runtime MCP tools or plugin hook tools;
- on a project with a local directory resource or more than one repository;
- on a repository that is not an HTTPS `github.com` URL, or with a tag or
  commit ref instead of a branch.

Built-in Multica CLI skills are omitted because their local tools are
unavailable in the session.

## Execution and recovery

- Up to `max_concurrency` runs (default 1, at most 10) execute at once per
  bridge; further tasks stay queued in Multica. Deploy one instance per
  configuration with its dedicated persistent state volume; this is not an HA
  service.
- A new session is created with the task prompt as its initial event, so
  creation and submission are one request. Sessions carry the Multica task and
  workspace IDs and a digest of their repositories, branches and tokens as
  metadata.
- If a create response is lost, the bridge lists the agent's sessions created
  since the attempt (minus five minutes of clock skew, up to ten pages) and
  reattaches to the one carrying the task ID. If none is found, or the listing
  runs past ten pages, it retries the create once; if that is also unconfirmed
  the run fails with an explicit unconfirmed-creation error, and remote
  sessions should be inspected manually. A transient listing failure is
  retried after a backoff; a definite refusal fails the run. When a run reaches
  its deadline, or its Multica task is cancelled, fails, completes or is
  deleted, without a confirmed session, the same lookup runs first and any
  session it finds is cancelled. A failed lookup never holds the run back; the
  bridge logs that a remote session may exist.
- A follow-up run on the same issue continues the agent's previous session when
  that session is idle, uses the same Claude agent, environment, repositories,
  branches and tokens, and no other run is using it. Otherwise a fresh session
  starts with the current issue and triggering/coalesced comment context. Do
  not send additional messages directly to a bridge-owned session while its run
  is active.
- A follow-up message is sent with persisted intent first. If the response is
  ambiguous, the bridge searches event history for that exact message and never
  sends it twice automatically. Unconfirmed submissions expire after two
  minutes; all runs have a one-hour deadline.
- Progress polls the session's events every three seconds using the opaque
  `page` cursor, sent with the same filters as the request that returned it,
  and deduplicates by event ID. A run in a new session starts when the session
  is created or reattached; a run in a continued session starts at the user
  message carrying its prompt. A run succeeds only after it started and
  `session.status_idle` with `end_turn` follows, without an unrecovered
  `session.error`. Any other stop reason (`requires_action`,
  `retries_exhausted`, and so on) or a terminated session fails the run.
- Shutdown leaves remote execution running. Restart with the same directory to
  resume; do not delete the journal while work is active.
- State is written atomically with private file permissions. It includes task
  content, session ID, event cursor and pending outcome, but no API credentials.
  An OS file lock prevents concurrent use of the same directory.
- Transcript messages carry a task-scoped `idempotency_key` derived from the
  Claude event ID, so replays after a restart do not duplicate messages.

### Cancellation

Claude Managed Agents has no cancel endpoint. Cancelling or deleting a Multica
run, a server-side timeout, a run deadline, or losing runtime access reads the
session first: an `idle` or `terminated` session is left alone. Otherwise the
bridge sends a `user.interrupt` event and waits up to three seconds for the
session to become `idle` or `terminated`. A refused interrupt counts as a stop
when the session then reads as `idle` or `terminated`. A session that no
longer exists counts as stopped. An unconfirmed stop stays in a durable cleanup
queue and is interrupted again on later ticks, independently of the next task;
each tick tries every queued session in order, so one failing session does not
hold back the others. Because the Multica task status is
read before session events, the `end_turn` that follows an interrupt is never
reported as a successful run.

Sessions are never archived or deleted, on cancellation or on completion, so
their history stays available.

## Known limits

- This is an issue-execution bridge. Chat, squad coordination, local
  directories, attachments, arbitrary Git hosts, tag/commit checkout, full
  comment-history hydration, interactive tool approval, custom client-side
  tools, file transfer, session budgets and usage/billing reporting are not
  implemented. Set up a Claude agent that can execute autonomously with its
  configured tools; `requires_action` fails the run and approvals are never
  granted implicitly.
- Events are polled, not streamed.
- Cancellation treats an `idle` session as stopped. An interrupt does not close
  an open workflow run, so linked agents should not rely on dynamic workflows.
- A session that appears in the listing only after the ambiguous-create lookup
  can still be duplicated by the single create retry.
- Detection of a non-workspace-scoped key relies on the wording of Anthropic's
  error message.
- Textual results and PR links are returned in the final summary, without
  parsing them into structured Multica branch fields. End of turn is execution
  completion, not proof that a pull request was created.
- There is no server-managed mode or connection UI yet; this binary is the only
  way to run the runtime.

The bridge uses the existing daemon registration protocol with
`runtime_mode=cloud` and provider `claude_managed`.

## Verification

```sh
go test -race ./internal/claudemanagedruntime ./cmd/claude-managed-runtime
go vet ./internal/claudemanagedruntime ./cmd/claude-managed-runtime
```

Tests use local HTTP fixtures and never access real accounts or execute an
installed coding-agent CLI.

To exercise a running **local** Multica server with a simulated Claude API:

```sh
MULTICA_CLAUDE_MANAGED_INTEGRATION_CONFIG=/private/path/config.json \
  go test -race -tags=integration ./internal/claudemanagedruntime -run '^TestLocalMultica$' -count=1 -v
```

This test requires a loopback Multica URL and a test workspace. Anthropic
credentials in the file are not used; the Claude API is an in-process HTTP
fixture.
