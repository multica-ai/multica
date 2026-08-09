# Web PTY Agent Cockpit: Phase 0 decision

Status: **Conditional Go**
Date: 2026-08-09
Scope: Codex CLI `0.147.0` on macOS; the compatibility contract is intentionally version-tolerant.

## Decision

Multica will add an opt-in PTY data plane for Codex while preserving the existing
structured `app-server` backend as the default and fallback. A task owns exactly
one provider process: interactive PTY and structured app-server modes are never
started together for the same task.

The launch gate is fail-closed:

- `MULTICA_PTY_ENABLED` defaults to `false`.
- `MULTICA_PTY_RUNTIME_ALLOWLIST` defaults to `codex`.
- `MULTICA_PTY_WORKSPACE_ALLOWLIST` defaults to empty (all workspaces allowed
  after the global and runtime gates pass).
- an old server, a server without `terminal-pty-v1`, or a rejected terminal
  WebSocket leaves the daemon on the existing structured path.

## Phase 0 evidence

The installed Codex CLI was launched under a real pseudo-terminal with a fixed
working directory, inherited account login, `TERM=xterm-256color`, read-only
sandboxing, and no ambient shell interpolation.

Observed:

1. `codex [PROMPT]` produced raw ANSI cursor movement, title changes, bracketed
   paste mode, and normal TUI input/output through the PTY.
2. A positional initial prompt was accepted and the process remained interactive
   after replying.
3. Normal `/exit` returned exit code `0` and printed a resumable Codex session ID.
4. `codex resume <session-id> [PROMPT]` reopened that same session.
5. Writing byte `0x03` interrupted the active model turn but kept the interactive
   session alive. Product **Stop** therefore must terminate the owned process
   group; it must not be implemented as terminal Ctrl+C.
6. The CLI presented a worktree trust prompt before dispatch. A managed task
   cannot wait forever for an unattached browser, so the isolated task
   `CODEX_HOME` must explicitly trust only the exact daemon-prepared workdir.

The rollout created by the smoke test contained `session_meta`, `turn_context`,
`world_state`, `event_msg`, and `response_item` records. Its event vocabulary was
small and version-specific, and the file mode was `0644`. Consequently:

- raw PTY bytes are the only terminal truth source;
- rollout observation is optional and read-only;
- an unavailable, incomplete, delayed, or malformed rollout is surfaced as
  `unavailable` or `stale` and never blocks the terminal;
- rollout bytes are never copied into terminal logs or database rows;
- deployments should protect the daemon user's Codex state directory because
  upstream-created rollout permissions are outside Multica's control.

## Provider adapter contract

PTY support is capability-driven. A provider adapter supplies:

- an absolute executable path;
- arguments as an argv array (never a shell string);
- a fixed workdir and sanitized environment;
- optional provider-session discovery after launch/exit;
- structured-observation capability: `unavailable`, `available`, or `stale`.

Codex v1 builds a fresh launch as:

```text
codex [normalized custom args] [-m MODEL] [-c model_reasoning_effort=LEVEL] PROMPT
```

and a resumed launch as:

```text
codex resume SESSION_ID [normalized custom args] [-m MODEL] [-c model_reasoning_effort=LEVEL] PROMPT
```

Protocol-critical transport flags (`exec`, `app-server`, output-format flags,
working-directory overrides, and another resume command) are rejected by the
adapter. The daemon pins the workdir rather than trusting browser input.

## Transport and lifecycle invariants

- Browser and daemon use dedicated WebSockets; existing control/realtime sockets
  do not carry terminal output.
- Binary input/output frames are bounded to 32 KiB chunks and include protocol
  version, kind, terminal session UUID, and monotonic sequence/client-input ID.
- Each daemon session keeps an approximately 8 MiB bounded replay ring. The
  server keeps a second bounded relay ring for browser reconnects. Eviction is
  explicit via `gap`; sequence numbers never reset within a generation.
- Browser disconnect never kills the task. Refresh attaches to the same live
  session and replays after the acknowledged sequence.
- One lease-holder may write; other clients observe. The lease lasts 30 seconds
  and is renewed every 10 seconds.
- Resize is clamped to 20-400 columns and 5-200 rows.
- Browser input is rate-limited to 64 KiB/s per connection and deduplicated by
  client input ID.
- Stop uses the existing task-cancel API. The daemon cancellation path sends
  TERM to the owned process group, waits briefly, then sends KILL and reaps it.
- Raw terminal input/output is never persisted and never emitted to application
  logs. Durable rows contain metadata, state, sequence counters, dimensions,
  controller lease state, exit status, and timestamps only.

## Compatibility

The existing transcript UI and structured backend remain untouched when the
feature is disabled or capability negotiation fails. New servers ignore old
daemons that never open the terminal data plane; new daemons fall back when the
terminal endpoint is absent; new frontends render the existing Transcript/File
Changes cockpit when metadata reports `available: false`.
