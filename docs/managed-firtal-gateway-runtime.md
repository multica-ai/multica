# Managed Firtal Gateway Runtime

## System Overview

The `firtal-gateway` provider lets a Multica daemon register a managed HTTP runtime backed by the Firtal Data Registry AI Gateway. It is intended for centrally operated cloud runtimes where the central team controls gateway credentials and workspace admins can grant agents access to the shared runtime.

## Affected Files And Components

- `server/pkg/agent/firtal_gateway.go`: HTTP backend for OpenAI-compatible chat completions and model listing.
- `server/pkg/agent/agent.go`: provider registration and runtime launch label.
- `server/pkg/agent/models.go`: provider model discovery.
- `server/internal/daemon/config.go`: daemon-side runtime registration from environment variables.
- `server/internal/daemon/prompt.go`: explicit chat transcript prompt for the stateless HTTP runtime.
- `server/internal/handler/daemon.go`: capped chat history included in claim payloads.
- `packages/views/runtimes/components/provider-logo.tsx`: runtime provider icon.

## Dataflow

1. A central daemon starts with `FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL` and `FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY`.
2. The daemon registers provider `firtal-gateway` with the Multica server.
3. A chat task is claimed by the daemon. The claim payload includes agent instructions, the latest pending user messages, and a capped chat transcript.
4. The backend calls `POST /api/ai/proxy/v1/chat/completions` on the Data Registry gateway.
5. The gateway returns assistant text plus Firtal cost metadata. The daemon reports the result and token usage back to Multica.

## State Ownership

Multica owns chat sessions, task lifecycle, runtime registration, and agent access control. Firtal Data Registry owns model availability, routing policy, budgets, PII routing, and provider credentials behind the gateway. The daemon only owns transient execution and does not persist gateway conversation state.

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

Configure the central daemon host:

```bash
MULTICA_FIRTAL_GATEWAY_ENABLED=true
FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL=https://<data-registry-host>
FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY=rk_<key>
FIRTAL_DATA_REGISTRY_AI_MODEL=claude-sonnet-4-6
```

Optional controls:

```bash
FIRTAL_DATA_REGISTRY_AI_MAX_TOKENS=4096
FIRTAL_DATA_REGISTRY_AI_TEMPERATURE=
```

Restart the daemon. It will register a runtime with provider `firtal-gateway`. Create or update a Multica agent to use that runtime, then manage who can create additional runtimes through the existing runtime/admin permissions path.

## Known Risks

- Missing or invalid gateway credentials fail runtime startup when `MULTICA_FIRTAL_GATEWAY_ENABLED=true`.
- The HTTP runtime is intentionally stateless. Cerebro sends a capped transcript for chat continuity, so very long chats only include recent context.
- Tool use, repository checkout, and local shell access are not available through this runtime. It is scoped for managed chat.
- Gateway model access is controlled by the registry key permissions. A model visible in Multica can still fail if the Data Registry key later loses access.
