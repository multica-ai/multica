# Runtime Extensions

> **Status:** schema v1 — supports both ACP (`acp-stdio`) and stream-json
> transports as of v0.3.18+.

Runtime extensions let you register **any compatible CLI** as a Multica agent
provider — internal company tools, your own ACP/stream-json server, an
internal fork of Claude Code, anything — without rebuilding the daemon.

The daemon scans `~/.multica/runtimes/<id>/runtime.json` at startup, treats
each manifest as a discoverable provider, and routes tasks to the matching
generic backend (`acp-stdio` or `stream-json`).

## TL;DR — register an internal CLI in 2 minutes

```bash
mkdir -p ~/.multica/runtimes/internal-cli
cat > ~/.multica/runtimes/internal-cli/runtime.json <<'EOF'
{
  "id": "internal-cli",
  "name": "Internal Coding Assistant",
  "version": "1.0.0",
  "provider": "internal-cli",
  "transport": "stream-json",
  "command": {
    "executable": "/usr/local/bin/internal-cli",
    "args": [
      "-p",
      "--output-format", "stream-json",
      "--input-format", "stream-json",
      "--verbose"
    ]
  },
  "config_file": "AGENTS.md",
  "capabilities": {
    "model_selection": true,
    "session_resume": true,
    "max_turns": true,
    "tool_calls": true,
    "attachments": true,
    "custom_args": true
  },
  "models": [
    { "id": "internal-pro",  "label": "Internal Pro",  "default": true },
    { "id": "internal-fast", "label": "Internal Fast" }
  ],
  "min_cli_version": "1.0.0"
}
EOF
multica daemon restart
```

Restart the daemon, refresh the desktop app, and `internal-cli` will appear
as a registered runtime in the workspace settings.

---

## Schema reference (`runtime.json`)

| Field                  | Required | Type                  | Notes                                                                     |
| ---------------------- | -------- | --------------------- | ------------------------------------------------------------------------- |
| `id`                   | ✅        | string                | Unique key under `~/.multica/runtimes/`.                                  |
| `name`                 | ✅        | string                | Display name shown in the UI.                                             |
| `version`              | ❌        | string                | Manifest version (cosmetic).                                              |
| `description`          | ❌        | string                | Short blurb shown alongside the runtime in the UI.                        |
| `provider`             | ✅        | string                | Provider key (must NOT collide with built-ins like `claude`, `codex`).    |
| `transport`            | ✅        | `"acp-stdio"`/`"stream-json"` | Wire protocol the daemon should speak.                            |
| `command.executable`   | ✅        | string                | Path or PATH-resolved name of the CLI binary.                             |
| `command.args`         | ❌        | string[]              | Args appended to every invocation (e.g. `["--acp"]`).                      |
| `command.blocked_args` | ❌        | map<string,string>    | Keys: flag names. Values: `"value"` (flag has value) / `"flag"` (boolean).|
| `capabilities`         | ❌        | object                | See [Capabilities](#capabilities).                                        |
| `models`               | ❌        | object[]              | Static model list (fallback). Each `{id, label?, default?, thinking?[]}`. |
| `models_discovery`     | ❌        | object                | Dynamic model discovery config. See [Model Discovery](#model-discovery).  |
| `pricing`              | ❌        | map<modelID,pricing>  | `{ input, output, cacheRead?, cacheWrite? }` per million tokens.          |
| `config_file`          | ❌        | string                | E.g. `"AGENTS.md"`, `"CLAUDE.md"`, `""` to skip config injection.         |
| `skills_root`          | ❌        | string                | Manifest skills root; exported as `MULTICA_AGENT_SKILLS_ROOT`.            |
| `icon_url`             | ❌        | string                | URL of the provider logo (HTTPS, absolute, SVG/PNG recommended). Frontend renders as `<img>` with `referrerPolicy=no-referrer`. Must be accessible from the daemon host. |
| `env`                  | ❌        | map<string,string>    | Extra env vars merged into the spawn (existing keys win).                 |
| `min_cli_version`      | ❌        | semver                | Daemon warns at startup if the installed CLI is below this.               |
| `launch_header`        | ❌        | string                | One-line "skeleton" of how the CLI is invoked, shown in the UI.           |

### Capabilities

Capabilities are advisory. They tell the wire layer **which optional
parameters the daemon may forward to your CLI**. Setting a capability does
NOT magically teach the daemon a new flag — it just unlocks an existing
forwarding path.

| Capability             | If `true`, the daemon forwards…                                          |
| ---------------------- | ------------------------------------------------------------------------ |
| `thinking`             | ACP: `params.thinkingLevel`. stream-json: `--effort <level>`.            |
| `mcp_config`           | ACP: `params.mcpServers` (translated). stream-json: not auto-injected (manifest may add `--mcp-config` via `command.args`). |
| `inline_system_prompt` | The full runtime brief is passed inline as `--append-system-prompt` (stream-json) or via the user prompt (ACP). |
| `session_resume`       | ACP: `params.sessionId`. stream-json: `--resume <id>`.                   |
| `max_turns`            | ACP: `params.maxTurns`. stream-json: `--max-turns N`.                    |
| `model_selection`      | ACP: `params.model`. stream-json: `--model <id>`.                        |
| `local_skills`         | UI shows the "skills" tab; daemon exports `MULTICA_AGENT_SKILLS_ROOT`.   |
| `slash_commands`       | UI shows the "slash commands" affordance.                                |
| `tool_calls`           | UI surfaces tool-use messages from the CLI.                              |
| `attachments`          | UI exposes the per-task attachment dropzone.                             |
| `image_input`          | UI lets the user attach images directly.                                 |
| `web_search`           | UI flags the runtime as web-search-capable.                              |
| `custom_args`          | UI exposes the per-agent `custom_args` field.                            |
| `extra_args`           | UI exposes the daemon-wide `extra_args` field (default off).             |

> If you flip a capability `true` and the spawn still doesn't carry the
> param, double-check `command.blocked_args` — a manifest can block its own
> daemon-managed flag by mistake.

### Model Discovery

Dynamic model discovery lets the daemon query your CLI for the current
model list at runtime — no manifest update needed when models change.

```jsonc
{
  "models_discovery": {
    "method": "cli",             // "cli" (stream-json) or "acp" (acp-stdio)
    "cli": {
      "args": ["--list-models", "--format", "json"],
      "timeout_seconds": 15      // default 15s
    },
    "cache_ttl_seconds": 120     // default 60s
  },
  "models": [...]                // static fallback when discovery fails
}
```

**CLI discovery** (`method: "cli"`): the daemon runs
`<command.executable> <models_discovery.cli.args>` and parses stdout as:

```json
{
  "models": [
    {
      "id": "model-id",
      "label": "Display Name",
      "default": true,
      "thinking": {
        "supported_levels": [{"value":"low","label":"Low"},{"value":"high","label":"High"}],
        "default_level": "high"
      },
      "pricing": {"input": 3.0, "output": 15.0, "cacheRead": 0.3}
    }
  ]
}
```

Each model's `pricing` field is optional; when present it updates the
pricing table the daemon reports to the frontend.

**ACP discovery** (`method: "acp"`): the daemon does the standard
`initialize` → `session/new` handshake and reads models from the
`result.models.availableModels` array in the session/new response.
Per-model `pricing` is extracted the same way.

**Fallback**: when discovery fails (timeout, parse error, CLI not
found), the static `models` array is used instead. A warning is logged
but the runtime stays functional.

**Auto-inference**: if `method` is omitted, the daemon infers it from
the manifest's `transport`: `stream-json` → `"cli"`, `acp-stdio` →
`"acp"`.

### Agent-level overrides

Each agent can override certain manifest fields without editing the
global `runtime.json`. Set these keys in the agent's metadata (via the
agent settings UI):

| Metadata key | Overrides | Notes |
|---|---|---|
| `skills_root_override` | `skills_root` | Per-agent skills directory |
| `config_file_override` | `config_file` | E.g. use `"CLAUDE.md"` instead of `"AGENTS.md"` |
| `icon_url_override` | `icon_url` | Cosmetic; shows a different logo for this agent |
| `blocked_args_override` | `blocked_args` | Append-only merge; cannot remove manifest-level blocks |

Overrides are applied at task-spawn time before building the backend
command line. The manifest remains the default for all agents that
don't set an override.

---

## Transport choice: ACP vs stream-json

| Question                                                                | Pick `acp-stdio`             | Pick `stream-json`           |
| ----------------------------------------------------------------------- | ---------------------------- | ---------------------------- |
| CLI speaks JSON-RPC 2.0 over stdio with `initialize` / `session/new`?   | ✅                            | ❌                            |
| CLI is a Claude/CodeBuddy clone (`-p --output-format stream-json …`)?    | ❌                            | ✅                            |
| Need bidirectional tool calls or rich session state?                    | ✅                            | ❌ (one-shot per task)        |
| Want the CLI to drive its own session lifecycle?                        | ✅                            | ❌                            |
| Want zero protocol surface area (the CLI just emits text + tool use)?  | ❌                            | ✅                            |

Both transports forward the same set of optional params via capability
flags; the wire shape just differs.

---

## Internal CLI walkthrough (`stream-json`)

Most internal CLIs are forks of Claude Code or implement the same NDJSON
contract. Here's a complete example for one shipping at `/opt/lightbox/bin/coder`:

```json
{
  "id": "lightbox-coder",
  "name": "Lightbox Coder",
  "version": "1.0.0",
  "description": "Internal Lightbox coding assistant",
  "provider": "lightbox-coder",
  "transport": "stream-json",
  "command": {
    "executable": "/opt/lightbox/bin/coder",
    "args": [
      "-p",
      "--output-format", "stream-json",
      "--input-format", "stream-json",
      "--verbose",
      "--permission-mode", "bypassPermissions"
    ],
    "blocked_args": {
      "-p":                 "flag",
      "--output-format":    "value",
      "--input-format":     "value",
      "--permission-mode":  "value"
    }
  },
  "config_file": "AGENTS.md",
  "icon_url": "https://internal.lightbox.com/static/logo.svg",
  "capabilities": {
    "thinking":         true,
    "model_selection":  true,
    "session_resume":   true,
    "max_turns":        true,
    "mcp_config":       true,
    "tool_calls":       true,
    "attachments":      true,
    "custom_args":      true,
    "local_skills":     true
  },
  "models": [
    { "id": "lb-coder-pro",  "label": "Lightbox Pro",  "default": true,  "thinking": ["none","low","medium","high"] },
    { "id": "lb-coder-fast", "label": "Lightbox Fast",                   "thinking": ["none","low"] }
  ],
  "pricing": {
    "lb-coder-pro":  { "input": 3.0, "output": 15.0, "cacheRead": 0.30 },
    "lb-coder-fast": { "input": 0.8, "output": 2.4 }
  },
  "env": {
    "LB_TELEMETRY_OPT_OUT": "1"
  },
  "skills_root": "/var/lib/lightbox/skills",
  "min_cli_version": "1.4.0"
}
```

What you get out of the box:

- The daemon spawns `coder -p --output-format stream-json --input-format stream-json --verbose --permission-mode bypassPermissions`
  with `--model`, `--effort`, `--max-turns`, and `--resume` injected when
  the matching capability is set and the task carries the value.
- `AGENTS.md` is written into each task's working directory with the full
  runtime brief (command catalog, workflow, attachments guidance).
- `MULTICA_AGENT_SKILLS_ROOT=/var/lib/lightbox/skills` is exported into the
  CLI's env.
- Per-agent `custom_args` are filtered against `blocked_args` so a
  workspace user cannot override the protocol-critical flags from the UI.
- The desktop UI shows the Lightbox logo, the static model list, and
  per-model thinking levels.

---

## Internal CLI walkthrough (`acp-stdio`)

For a CLI that speaks ACP (e.g. Hermes, Kimi, Kiro, or your own JSON-RPC
agent server):

```json
{
  "id": "lightbox-acp",
  "name": "Lightbox ACP Agent",
  "version": "0.1.0",
  "provider": "lightbox-acp",
  "transport": "acp-stdio",
  "command": {
    "executable": "/opt/lightbox/bin/agent",
    "args": ["serve", "--acp"]
  },
  "config_file": "AGENTS.md",
  "capabilities": {
    "model_selection":     true,
    "session_resume":      true,
    "max_turns":           true,
    "thinking":            true,
    "mcp_config":          true,
    "inline_system_prompt": false
  },
  "models": [
    { "id": "lb-1", "label": "Lightbox 1", "default": true }
  ],
  "min_cli_version": "0.1.0"
}
```

The ACP backend does the standard handshake:

1. `initialize` → record server capabilities.
2. `session/new` with `cwd` plus capability-gated extras (`model`,
   `sessionId`, `maxTurns`, `thinkingLevel`, `mcpServers`).
3. `session/prompt` with the user prompt.
4. Drains `session/update`, `assistant/message`, `tool/result` events
   until `session/close` or `session/error`.

If your CLI returns a session ID via `session/update`, the daemon stores
it as `prior_session_id` and forwards it on the next task on the same
issue (when `session_resume: true`).

### Translating MCP config

The daemon may receive MCP config in either of two shapes:

```json
// Claude-style (object-of-objects)
{ "linear": { "command": "linear-mcp", "args": ["serve"] } }

// ACP-native (array)
{ "mcpServers": [ { "name": "linear", "command": "linear-mcp" } ] }
```

For ACP runtimes with `mcp_config: true`, the daemon auto-translates the
Claude shape into the ACP `mcpServers` array. The ACP-native shape is
forwarded unchanged. Stream-json runtimes don't get auto-injection — add
your own flag in `command.args` if your CLI accepts one (Claude uses
`--mcp-config`).

---

## Local development & debugging

### Verify the manifest loads

```bash
# Show what the daemon discovered. The agents map will list your runtime
# under its `provider` key.
multica daemon status --output json | jq '.agents | keys'
```

### Spot a transport mismatch

```bash
# Daemon log on startup. Look for:
#   "warning: runtime manifest .../runtime.json declares unsupported transport"
multica daemon logs | grep "runtime manifest"
```

### Check capability gating

The daemon warns when a manifest declares a capability but the corresponding
opt is missing in `command.args`. To dry-run the argv your runtime will see:

```bash
# Build the test binary and run the gated-args unit test verbosely.
cd server && go test ./pkg/agent/ -run TestBuildStreamJSONExternalArgsCapabilityGated -v
```

### Force a min-version warning

Set `min_cli_version` higher than the installed binary. Daemon logs:

```
warning: runtime extension "lightbox-coder": installed CLI 1.3.0 is below required minimum 1.4.0 — upgrade is recommended
```

The runtime still loads — this is a warning, not a hard fail.

### Tail the spawn

```bash
# Daemon-level log (every backend prints command + args at INFO).
multica daemon logs --level info | grep -E "external (command|started|finished)"
```

For deeper debugging, set `MULTICA_LOG_LEVEL=debug` in the daemon's env
before starting it.

---

## Validation rules

A manifest is **rejected at startup** when:

- A required field is missing (`id`, `name`, `provider`, `transport`,
  `command.executable`).
- `transport` is set to anything other than `acp-stdio` or `stream-json`.
- The `provider` collides with a built-in (`claude`, `codex`, `copilot`,
  `opencode`, `openclaw`, `hermes`, `gemini`, `pi`, `cursor`, `kimi`,
  `kiro`, `antigravity`).
- The JSON itself fails to parse.

Rejected manifests print a warning to stderr and are simply skipped — the
daemon keeps starting, so a broken manifest never bricks the host.

---

## Deployment & distribution

### Company-wide runtimes via `MULTICA_RUNTIMES_INCLUDE`

For internal / team-wide deployments, ship a directory tree of runtime
manifests and point the daemon at it with `MULTICA_RUNTIMES_INCLUDE`:

```bash
# Ship manifests to /opt/lightbox/runtimes/codebuddy/runtime.json
# and /opt/lightbox/runtimes/internal-cli/runtime.json

export MULTICA_RUNTIMES_INCLUDE=/opt/lightbox/runtimes
multica daemon restart
```

Multiple paths are supported (delimiter follows `PATH` convention —
`:` on Linux/macOS, `;` on Windows):

```bash
export MULTICA_RUNTIMES_INCLUDE="/opt/lightbox/runtimes:/shared/team-runtimes"
```

The daemon **merges** manifests from all paths. `~/.multica/runtimes/`
still works alongside the include paths, so users can add their own
personal runtimes without touching the company ones. When two manifests
share the same `provider`, the first one loaded wins (the default
`~/.multica/runtimes/` is loaded first).

This makes it trivial to deploy a runtime extension fleet-wide without
touching each user's home directory — just drop the manifests in a
shared directory, set the env var in the daemon's systemd/launchd unit,
and every developer picks them up on the next daemon restart.

### Configuring the daemon for fleet deployment

Example systemd unit snippet (`~/.config/systemd/user/multica-daemon.service`):

```ini
[Service]
Environment="MULTICA_RUNTIMES_INCLUDE=/opt/lightbox/runtimes"
Environment="MULTICA_LOG_LEVEL=info"
ExecStart=/usr/local/bin/multica daemon start
```

Example launchd plist snippet (`~/Library/LaunchAgents/com.multica.daemon.plist`):

```xml
<key>EnvironmentVariables</key>
<dict>
    <key>MULTICA_RUNTIMES_INCLUDE</key>
    <string>/opt/lightbox/runtimes</string>
</dict>
```

---

## FAQ

### Why is my model picker empty in the UI?

Either `models` is missing, or the manifest doesn't declare
`model_selection: true`. The daemon serves the static `models` array
when both are present.

### My CLI hangs forever after returning a result.

For stream-json, your CLI must close stdout (or emit an `is_error: true`
result frame and let the daemon `closeStdin`) so the watchdog can spot
completion. The bundled `streamJSONExternalBackend` already handles the
common case — closing stdin once a `result` frame arrives — but a CLI
that buffers stdout indefinitely will stall.

### Can a manifest override a built-in?

No — to keep the resolution path predictable, conflicts are skipped with
a warning. Pick a non-conflicting `provider` key.

### Where can I find more examples?

`~/.multica/runtimes/codebuddy/runtime.json` and
`~/.multica/runtimes/custom-runtime/runtime.json` ship as reference
samples on developer machines that have run through the manual setup.

---

## Implementation pointers

For contributors hacking on the runtime extension system itself:

| File                                                                | Role                                                       |
| ------------------------------------------------------------------- | ---------------------------------------------------------- |
| `server/internal/daemon/runtime_loader.go`                          | Manifest schema + `LoadRuntimeManifests` scanner.          |
| `server/internal/daemon/runtime_version.go`                         | Best-effort `--version` probe and semver compare.          |
| `server/internal/daemon/types.go`                                   | `AgentEntry` and capability propagation.                   |
| `server/internal/daemon/config.go`                                  | Hooks into `LoadConfig` to register external manifests.    |
| `server/internal/daemon/daemon.go`                                  | Provider→backend wiring, model list, registration payload. |
| `server/internal/daemon/execenv/runtime_config.go`                  | `InjectRuntimeConfigForEntry` writes the brief to the workdir. |
| `server/pkg/agent/agent.go`                                         | `agent.New` factory; routes external entries by transport. |
| `server/pkg/agent/acp_external.go`                                  | Generic ACP backend.                                       |
| `server/pkg/agent/stream_json_external.go`                          | Generic stream-json backend.                               |
