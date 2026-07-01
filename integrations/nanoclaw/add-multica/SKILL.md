---
name: add-multica
description: Add Multica issue creation and status lookup tools to a NanoClaw agent group without putting a Multica PAT in the agent container.
---

# Add Multica to NanoClaw

This capability has two parts:

1. `multica nanoclaw serve` runs on the host and uses the host's authenticated
   Multica CLI configuration.
2. A small stdio MCP server runs in the NanoClaw container and calls that
   bridge with a separate, revocable bridge token.

The MCP tools are:

- `multica_create_issue`: create an issue for an agent or squad by name;
- `multica_get_issue`: fetch an issue and its current `status`.

## Phase 1: Host bridge

Verify the Multica CLI is authenticated and has a workspace selected:

```bash
multica auth status
multica config show
```

Use a dedicated random bridge token. This is not a Multica PAT; it only grants
the two bridge operations above.

```bash
mkdir -p ~/.config/multica-nanoclaw
openssl rand -hex 32 > ~/.config/multica-nanoclaw/bridge-token
chmod 600 ~/.config/multica-nanoclaw/bridge-token
```

Start the bridge so NanoClaw's containers can reach it through the Docker host
gateway:

```bash
MULTICA_NANOCLAW_BRIDGE_TOKEN="$(cat ~/.config/multica-nanoclaw/bridge-token)" \
  multica nanoclaw serve --listen 0.0.0.0:8099
```

Run the bridge under the same user as the authenticated Multica CLI. For a
persistent install, put that command in the host service manager and load the
token from the mode-0600 file; do not paste the Multica PAT into NanoClaw.

## Phase 2: Install the MCP client

Run from the NanoClaw project root. Re-running this copy is safe.

```bash
cp "${CLAUDE_SKILL_DIR}/multica-mcp-stdio.ts" \
  container/agent-runner/src/multica-mcp-stdio.ts
```

The file is pure-add and `/app/src` is mounted read-only from the host runner
tree, so no NanoClaw core source edit is required.

## Phase 3: Register for an agent group

List groups and choose the group that should receive the tools:

```bash
ncl groups list
```

Build the MCP environment without putting the token literal in shell history:

```bash
BRIDGE_TOKEN="$(cat ~/.config/multica-nanoclaw/bridge-token)"
MCP_ENV="$(jq -cn \
  --arg url 'http://host.docker.internal:8099' \
  --arg token "$BRIDGE_TOKEN" \
  '{MULTICA_BRIDGE_URL:$url,MULTICA_BRIDGE_TOKEN:$token}')"

ncl groups config add-mcp-server \
  --id <group-id> \
  --name multica \
  --command bun \
  --args '["run","/app/src/multica-mcp-stdio.ts"]' \
  --env "$MCP_ENV"
unset BRIDGE_TOKEN MCP_ENV

ncl groups restart --id <group-id>
```

The bridge token is intentionally present in the selected container. It cannot
act as a general Multica credential: the bridge only implements create/get.
Rotate it by replacing the host token file and re-registering the MCP env.

## Phase 4: Verify

Check the bridge from the host:

```bash
curl --fail-with-body \
  -H "Authorization: Bearer $(cat ~/.config/multica-nanoclaw/bridge-token)" \
  http://127.0.0.1:8099/health
```

Then tell NanoClaw:

> Create a Multica task called "Check subscription retries" for the
> "AWG Service" team, but leave it in backlog.

The response must contain the created issue ID and `status: backlog`. Then ask:

> What is the current status of that Multica task?

If an agent or squad name is missing or ambiguous, the tool returns the
matching error and NanoClaw must ask the user to clarify instead of guessing.

## Removal

See [`REMOVE.md`](REMOVE.md).
