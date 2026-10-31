# Custom Runtime Profiles

Custom runtime profiles (MUL-3284) let a workspace register a site-specific
agent executable — a wrapper, a pinned binary, an internal CLI — while reusing
one of Multica's **supported protocol families** for task routing and result
parsing.

This guide is the verification runbook for wiring such a CLI into a Multica
deployment.

## What a profile is — and is not

- A profile lives **server-side, scoped to a workspace**. Members of the
  workspace can bind agents to it.
- A profile declares:
  - `protocol_family` — one of the whitelisted agent types below. This is the
    *routing backend*: Multica drives the task with the family's CLI contract
    (argv + output protocol). **The family is immutable after creation.**
  - `command_name` — the executable the daemon resolves on each host's `PATH`
    at registration time. This can be any binary you control (e.g. a wrapper
    that injects credentials).
  - `display_name` / `description` — human-readable metadata.
- A profile does **not** introduce a new protocol family. If your CLI speaks a
  protocol Multica does not yet route, you need a code contribution (see
  [Contributing a new protocol family](#contributing-a-new-protocol-family)).
- `fixed_args` (command + args parsed in the product UI) are stored with the
  profile; the CLI surface currently exposes only `command_name`.

## Supported protocol families

The canonical whitelist (single source of truth: `server/pkg/agent/agent.go`
`SupportedTypes`, mirrored by the `runtime_profile.protocol_family` CHECK
constraint):

```
claude, codebuddy, codex, copilot, opencode, deveco, openclaw, hermes, pi,
cursor, kimi, reasonix, dsh, kiro, antigravity, qoder, qoderclicn, traecli,
grok, qwen, qwenpaw
```

Representative CLI contracts you must match when choosing a family:

| family | invocation shape (daemon side) |
|---|---|
| `claude` | `claude --output-format stream-json --input-format stream-json ...` — prompt via stdin, events on stdout |
| `qwen` | `qwen -p <prompt> --output-format stream-json ...` |
| `grok` | `grok agent --always-approve stdio` (ACP backend) |
| `qoder` / `qoderclicn` | shared ACP backend |
| `dsh` | detection via `dsh --profile multica --probe`; launch args + env from the backend |
| `traecli` | dedicated backend with its own launch header |

Pick the family whose argv contract your wrapper can satisfy — the daemon
will call your `command_name` with that family's arguments.

## Prerequisites

- A running Multica daemon on the host (it resolves `command_name` at
  registration time, so the wrapper must exist **before** you create the
  profile).
- `multica` CLI signed into the workspace.
- Your wrapper on `PATH`, or pinned per-machine with `set-path` (below).

## Verification runbook

### 1. Prepare the wrapper

Make an executable that:

1. Accepts the chosen family's argv. Self-check by hand: run it with the
   exact arguments from the table above and confirm it parses them.
2. Emits output the family's parser understands. For `stream-json` families
   the daemon consumes line-delimited JSON events; the minimal contract is a
   start event, assistant turns, and a final `result` event carrying the task
   text. Verify with a manual run:

   ```sh
   your-wrapper -p "hello" --output-format stream-json | head -5
   ```

   — confirm the first line is parseable JSON.

### 2. Create the profile

```sh
multica runtime profile create \
  --protocol-family qwen \
  --command-name your-wrapper \
  --display-name "Internal CLI (wrapped)" \
  --description "Routes to our internal agent via the qwen stream-json contract"
```

Constraints:

- `--protocol-family` must be in the whitelist; the CLI validates it
  client-side with a helpful error listing valid families.
- `command_name` is resolved per host; there is no server-side validation of
  the executable. Different daemon hosts may resolve different binaries under
  the same name — this is the supported way to keep credentials out of the
  server.

### 3. Confirm registration

- `multica runtime profile list` — profile present.
- On the daemon host, watch the daemon log for the profile probe/registration
  round; the daemon discovers the wrapper from `PATH` on its normal refresh
  cycle (restarting the daemon picks it up immediately).
- In the web UI: Workspace → Runtimes — the profile should appear as a
  runtime backed by the wrapper.

### 4. Smoke-test a task

Create an agent bound to the profile runtime and assign a trivial task
(e.g. "reply with OK"). Check that:

- the daemon invoked `command_name` with the family's arguments;
- the session stream rendered in the UI;
- the result text was recorded server-side.

### 5. Manage the profile

```sh
multica runtime profile list
multica runtime profile update <profile-id> --display-name "..." --description "..."
multica runtime profile delete <profile-id>   # 409 while agents are still bound
```

`update` cannot change `protocol_family` (immutable). `delete` refuses with a
409 (machine-readable `message`) while agents are bound — unbind them first.

### 6. Pin an executable per machine (optional)

`set-path` records a `profile_id -> absolute path` mapping in the **local**
CLI config. It never leaves the machine — use it when the wrapper is not on
`PATH`, or when different hosts should use different installs.

```sh
multica runtime profile set-path <profile-id> --path /opt/internal/your-wrapper
multica runtime profile unset-path <profile-id>
```

## Compatibility checklist (custom CLI, e.g. an internal agent)

- [ ] CLI runs headless (non-interactive) with the family's flags
- [ ] Emits line-delimited JSON events on stdout for `stream-json` families
- [ ] A `result`-type event terminates the stream with the final text
- [ ] Exit code reflects task success/failure
- [ ] Works when stdin is a pipe (families that feed the prompt via stdin)
- [ ] Discoverable on `PATH` (or pinned with `set-path`) before profile
      creation

## Contributing a new protocol family

If your CLI cannot fit any existing family (different argv contract, binary
protocol, etc.), a native backend is a code contribution. The dsh family is
the reference integration:

- `server/pkg/agent/dsh.go` — backend (launch args, env)
- `server/internal/daemon/agents_probe.go` — `probeDshMulticaProfile`
  (detection via `dsh --profile multica --probe`)
- `server/pkg/agent/agent.go` `SupportedTypes` plus the lockstep test
  `agent_supported_types_test.go`
- a `runtime_profile.protocol_family` migration (history: dsh = migration 313)

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Profile never registers on a host | `command_name` not on `PATH`; pin with `set-path` or restart the daemon |
| Tasks fail immediately | Wrapper doesn't accept the family's argv — verify with a manual run |
| Stream empty / no result | Wrapper isn't emitting parseable events — check the first line is JSON |
| `delete` returns 409 | Agents still bound to the profile — unbind first |
| Profile disappears after restart | `enabled=false` or drift reconciliation — re-enable or recreate |
