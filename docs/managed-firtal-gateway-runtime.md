# Managed Firtal Gateway Runtime

## System Overview

The `firtal-gateway` provider lets the Multica server register a managed HTTPS runtime backed by the Firtal Data Registry AI Gateway. It is intended for centrally operated cloud chat runtimes where the central team controls gateway credentials and workspace admins can grant agents access to the shared runtime without running a local daemon.

## Affected Files And Components

- `server/internal/cerebro/runtime/firtal_gateway_*.go`: server-side runtime registration, task claim loop, OpenAI-compatible chat completion client, usage rollup.
- `server/internal/cerebro/queries/firtal_gateway_runtime.sql`: Cerebro sqlc queries for workspace runtime sync and chat-task claims.
- `server/cmd/server/main.go`: background worker startup.
- `server/pkg/agent/firtal_gateway.go`: daemon-side HTTP backend for deployments that still expose the gateway through a central daemon.
- `server/pkg/agent/agent.go`: provider launch label.
- `server/pkg/agent/models.go`: provider model discovery.
- `server/internal/daemon/config.go`: daemon-side runtime registration from environment variables.
- `server/internal/daemon/prompt.go`: explicit chat transcript prompt for the stateless HTTP runtime.
- `server/internal/handler/daemon.go`: capped chat history included in claim payloads.
- `packages/views/runtimes/components/provider-logo.tsx`: runtime provider icon.

## Dataflow

1. The Multica server starts with `FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL` and `FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY`.
2. The server registers an online cloud runtime with provider `firtal-gateway` in each workspace, or only the workspaces listed in `MULTICA_SERVER_FIRTAL_GATEWAY_WORKSPACE_IDS`.
3. The server worker claims queued chat tasks for that runtime. It does not claim issue, autopilot, quick-create, shell, or repository tasks.
4. The worker sends agent instructions and a capped chat transcript to `POST /api/ai/proxy/v1/chat/completions`.
5. The gateway returns assistant text plus Firtal cost metadata. The server writes the assistant reply through the existing chat/task completion flow and records token usage/budget spend.

## State Ownership

Multica owns chat sessions, task lifecycle, runtime registration, and agent access control. Firtal Data Registry owns model availability, routing policy, budgets, PII routing, and provider credentials behind the gateway. The server worker only owns transient execution and does not persist gateway conversation state.

## API Contracts

Model listing:

```http
GET /api/ai/proxy/v1/models
Authorization: Bearer <registry-key>
```

Chat completion:

```json
{
  "model": "claude-sonnet-4-6",
  "max_tokens": 4096,
  "stream": false,
  "messages": [
    { "role": "system", "content": "..." },
    { "role": "user", "content": "..." }
  ]
}
```

The backend reads `choices[0].message.content` and token usage from the gateway `firtal` extension first, falling back to standard OpenAI `usage`.

## Setup

Configure the Multica server:

```bash
MULTICA_SERVER_FIRTAL_GATEWAY_ENABLED=true
FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL=https://<data-registry-host>
FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY=rk_<key>
FIRTAL_DATA_REGISTRY_AI_MODEL=claude-sonnet-4-6
```

Optional controls:

```bash
FIRTAL_DATA_REGISTRY_AI_MAX_TOKENS=4096
FIRTAL_DATA_REGISTRY_AI_TEMPERATURE=
MULTICA_SERVER_FIRTAL_GATEWAY_WORKSPACE_IDS= # optional comma-separated UUID allowlist
MULTICA_SERVER_FIRTAL_GATEWAY_MAX_CONCURRENCY=4
MULTICA_SERVER_FIRTAL_GATEWAY_POLL_INTERVAL=2s
MULTICA_SERVER_FIRTAL_GATEWAY_SYNC_INTERVAL=30s
```

Restart the server. It will register a runtime with provider `firtal-gateway`. Create or update a Multica agent to use that runtime. Existing daemon-side mode still exists for deployments that want a central daemon; use `MULTICA_FIRTAL_GATEWAY_ENABLED=true` on that daemon host instead.

## Known Risks

- Missing or invalid gateway credentials fail server startup when `MULTICA_SERVER_FIRTAL_GATEWAY_ENABLED=true`.
- The HTTPS runtime is intentionally stateless. Cerebro sends a capped transcript for chat continuity, so very long chats only include recent context.
- Tool use, repository checkout, and local shell access are not available through this runtime. It is scoped for managed chat.
- Gateway model access is controlled by the registry key permissions. A model visible in Multica can still fail if the Data Registry key later loses access.
