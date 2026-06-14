# Runtime Extensions Architecture RFC

## Summary

Runtime extensions v1 solve one problem well: a local daemon can discover a
compatible CLI from `runtime.json`, register it as a provider, and route tasks
through a generic transport backend. The next set of runtime issues asks for a
broader system: multiple accounts for the same CLI, runtime pools, fallback,
workspace-wide MCP configuration, container isolation, network policy, and
new protocols.

Those concerns should not all become fields on `runtime.json`. The design
should split runtime extensibility into five layers:

1. Provider Manifest: how to launch and speak to a provider.
2. Runtime Profile: how a workspace/user configures one provider instance.
3. Tooling Profile: which MCP servers and skill sources are attached.
4. Binding Policy: how an agent selects one or more runtime instances.
5. Execution Policy: how a claimed task is isolated, retried, and observed.

Keeping these layers separate lets Multica support new CLIs quickly without
turning provider manifests into platform policy files.

## Current v1 Boundary

`runtime.json` is a provider declaration. It belongs on the daemon host and
describes local facts:

- provider key and display metadata
- transport (`acp-stdio` or `stream-json`)
- executable and daemon-managed args
- capability flags
- static or dynamic model discovery
- manifest-level pricing
- config file and skills root

It should remain safe to share and deploy as a fleet artifact. It should not
own workspace-specific secrets, agent fallback chains, runtime pools, or
container firewall policy.

## Issue Coverage

| Requirement | Examples | Current coverage | Required design owner |
|---|---|---|---|
| Add a compatible CLI runtime | Devin, Kilo, Factory Droid, Claude-compatible wrappers | Mostly covered by manifest transports | Provider Manifest |
| Add a new wire protocol | A2A, WebSocket/gateway mode | Not covered; transport enum is closed | Provider Manifest + transport adapter registry |
| Multiple accounts/profiles for one provider | custom Codex config, Hermes profile, alternate base URL | Partially covered by agent env/config, awkward to reuse | Runtime Profile |
| Workspace/runtime MCP configuration | shared MCP servers across agents | Agent-level MCP exists, shared scope missing | Runtime Profile + Tooling Profile |
| Runtime local skills sync | import, overwrite, refresh runtime skills | Partially covered by local skills import | Tooling Profile / Skill Source |
| Choose which local runtimes register | local runtime selection, workspace filters | Partially covered by daemon config/env | Runtime Profile registration policy |
| Multiple runtimes per agent | priority, fallback, pools, per-user local runtime | Not covered; agent has one runtime_id | Binding Policy |
| Horizontal runtime scaling | runtime pools, auto-select online runtime | Not covered | Binding Policy + scheduler |
| Containerized execution | Docker/container, egress allowlist | Not covered | Execution Policy |
| Runtime liveness and no-op guards | no context guard, stuck task detection, 429 retry | Partially covered by failure reasons and sweeps | Execution Policy + scheduler |
| Remove or revoke runtimes | offline/foreign runtime deletion | Covered in current code path | Runtime lifecycle API |

## Target Model

### 1. Provider Manifest

The manifest remains daemon-local and declarative:

```jsonc
{
  "schema_version": 2,
  "id": "devin",
  "name": "Devin CLI",
  "provider": "devin",
  "transport": "acp-stdio",
  "command": {
    "executable": "devin",
    "args": ["acp"]
  },
  "capabilities": {
    "model_selection": true,
    "session_resume": true,
    "mcp_config": true,
    "tool_calls": true
  },
  "models_discovery": {
    "method": "acp"
  }
}
```

Allowed responsibilities:

- launch contract
- transport contract
- supported capability declaration
- local discovery defaults
- provider-safe defaults

Explicit non-responsibilities:

- workspace secrets
- account selection
- runtime priority/fallback
- container or network policy
- MCP server definitions with secret values

### 2. Runtime Profile

A runtime profile is the workspace/user-owned configuration of a provider
instance. Profiles solve the "same provider, different account or endpoint"
problem without cloning manifests.

Conceptual shape:

```jsonc
{
  "id": "profile_codex_selfhosted",
  "workspace_id": "ws_...",
  "owner_id": "user_...",
  "provider": "codex",
  "display_name": "Codex Self-hosted",
  "base_url": "https://gateway.example/v1",
  "model_defaults": {
    "default": "custom:lfm2.5:8b"
  },
  "env_secret_refs": {
    "OPENAI_API_KEY": "secret_runtime_profile_..."
  },
  "config": {
    "home_dir": ".multica/profiles/codex-selfhosted"
  },
  "tooling_profile_ids": ["tooling_workspace_default"]
}
```

Profiles can be backed by a local daemon, cloud runtime node, or future remote
gateway. The daemon receives a resolved, redacted execution request at claim
time rather than owning the workspace source of truth.

This layer covers:

- alternate base URLs
- separate accounts for one provider
- named provider instances
- profile-specific model defaults
- profile-specific MCP/tooling attachments
- profile-specific config roots

### 3. Tooling Profile

MCP servers and runtime-local skills are shared tooling, not provider identity.
They should be reusable across agents and profiles.

Conceptual shape:

```jsonc
{
  "id": "tooling_workspace_default",
  "scope": "workspace",
  "mcp_servers": [
    {
      "name": "playwright",
      "transport": "stdio",
      "command": "npx",
      "args": ["@playwright/mcp"],
      "env_secret_refs": {}
    }
  ],
  "skill_sources": [
    {
      "kind": "runtime_local",
      "runtime_profile_id": "profile_codex_selfhosted",
      "sync": "manual"
    }
  ]
}
```

Merge order at task claim should be deterministic:

1. workspace tooling profile
2. runtime profile tooling profile
3. agent-level `mcp_config` and attached skills
4. task-specific attachments or temporary tools

Later entries override named conflicts only where the caller has permission to
see and mutate that scope.

### 4. Binding Policy

Agents should no longer be limited to one `runtime_id` as their only execution
target. A binding policy resolves the agent to a runtime instance at task
dispatch or claim time.

Conceptual shape:

```jsonc
{
  "mode": "priority_list",
  "entries": [
    {
      "runtime_profile_id": "profile_primary_workstation",
      "when": { "status": "online" }
    },
    {
      "runtime_profile_id": "profile_always_on_vm",
      "when": { "status": "online" }
    }
  ],
  "fallback": {
    "on": ["runtime_offline", "rate_limit", "quota_exceeded"],
    "max_handoffs": 2
  }
}
```

Supported modes should start small:

- `single`: current behavior.
- `priority_list`: first healthy candidate wins.
- `pool`: choose any healthy candidate by capacity.
- `per_user_local`: resolve to the initiating user's compatible local runtime.

This layer owns:

- runtime priority
- automatic failover
- runtime pools
- "use my own local runtime" team-shared agents
- future cost/performance routing

The scheduler must record the resolved runtime on `agent_task_queue.runtime_id`
once a task is assigned so usage, cancellation, and audit remain concrete.

### 5. Execution Policy

Execution policy controls the environment and failure behavior of a claimed
task. It is separate from provider selection because the same provider can run
on the host, inside a container, or in a remote gateway.

Conceptual shape:

```jsonc
{
  "isolation": {
    "mode": "container",
    "image": "ghcr.io/multica/runtime-codex:latest",
    "mounts": ["workdir", "repo_cache"],
    "egress_allowlist": [
      "api.multica.ai",
      "api.openai.com",
      "gateway.example"
    ]
  },
  "liveness": {
    "no_output_timeout_seconds": 900,
    "no_state_change_timeout_seconds": 1200
  },
  "retry": {
    "rate_limit": {
      "max_attempts": 3,
      "backoff_seconds": [60, 180, 600]
    }
  }
}
```

This layer owns:

- container execution
- outbound network allowlists
- task liveness checks
- retry classification and backoff
- "no project/no context" admission guards
- post-claim runtime health checks

## Transport Adapter Registry

`transport` should evolve from a closed string switch into a registry:

| Transport | Status | Role |
|---|---|---|
| `stream-json` | current | Claude-compatible one-shot task execution |
| `acp-stdio` | current | ACP JSON-RPC over stdio |
| `a2a` | future | Agent-to-Agent protocol discovery and dispatch |
| `websocket` | future | warm gateway/session mode |
| `http` | future | remote runtime gateway protocol |

The manifest should declare transport and optional transport-specific config.
The daemon should validate that a registered adapter owns that transport.

## Migration Plan

### Phase 1: Document and stabilize v1 boundaries

- Keep `runtime.json` provider-focused.
- Add schema/version language that marks profile, binding, and execution
  policy as out of scope for v1 manifests.
- Ensure dynamic model discovery and pricing remain data-driven.

### Phase 2: Add runtime profiles

- Add server-side `runtime_profile` storage with provider, display name,
  config JSON, secret refs, and ownership.
- Let agents reference a profile while still resolving to a concrete
  `agent_runtime` for each task.
- Move account/base_url/config-root customization out of manifest cloning.

### Phase 3: Add binding policies

- Introduce `agent.runtime_binding_policy` alongside existing `runtime_id`.
- Treat missing policy as `single(runtime_id)` for compatibility.
- Resolve policy to a concrete runtime before claim.
- Record both policy ID and resolved runtime ID for audit.

### Phase 4: Add execution policies

- Add execution policy storage and claim-time resolution.
- Implement host mode first as the current behavior.
- Add container mode behind a capability flag and explicit admin opt-in.
- Add egress allowlist enforcement only after the container boundary exists.

### Phase 5: Expand transport adapters

- Extract transport routing into an adapter registry.
- Add A2A or websocket/gateway mode without creating provider-specific
  built-ins for every compatible CLI.

## Design Rules

- Do not put secrets in `runtime.json`.
- Do not put fallback chains in `runtime.json`.
- Do not put container firewall policy in `runtime.json`.
- Do not require one manifest per account.
- Always resolve abstract policies to concrete runtime IDs before task
  execution.
- Keep usage, audit, cancellation, and realtime events tied to the concrete
  runtime that actually ran the task.
- Keep provider capabilities advisory; platform policy must be enforced by
  the scheduler, handler, or execution layer.

## Open Questions

- Should runtime profiles be user-owned, workspace-owned, or both?
- Should `per_user_local` agents fall back to a shared runtime when the
  initiating user has no compatible local runtime?
- Should pool selection be capacity-based, least-recently-used, or queue-depth
  based in the first version?
- Should rate-limit fallback preserve the same session when switching
  providers, or force a summarized handoff?
- Which execution policy fields are admin-only in self-hosted workspaces?
- How should cloud runtimes expose container/network guarantees to the UI?
