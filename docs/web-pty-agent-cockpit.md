# Web PTY Agent Cockpit

This document describes the opt-in Codex PTY implementation. The Phase 0
evidence and Conditional Go decision are recorded in
[`web-pty-architecture-decision.md`](./web-pty-architecture-decision.md).

## Runtime path

```text
one agent_task_queue row
  -> daemon capability gate
  -> one Codex process in one OS PTY
  -> daemon reverse WebSocket /api/daemon/terminal/ws
  -> bounded server relay
  -> browser WebSocket /api/tasks/{task_id}/terminal/ws
  -> xterm.js in Agent Cockpit
```

The daemon chooses either the PTY adapter or the existing structured backend
before starting the provider process. It never starts both for a task. The
browser supplies no executable, argv, workdir, environment, runtime, task, or
generation metadata.

## Enablement and rollback

PTY is off by default. Both server and daemon must receive the same setting:

```dotenv
MULTICA_PTY_ENABLED=true
MULTICA_PTY_RUNTIME_ALLOWLIST=codex
MULTICA_PTY_WORKSPACE_ALLOWLIST=
```

The workspace allowlist is a comma-separated list of workspace UUIDs. Empty
means all workspaces after the global and runtime gates pass. Restart the
server and daemon after changing the settings.

The immediate kill switch is `MULTICA_PTY_ENABLED=false`. New work then uses
the existing structured backend and the existing Cockpit UI. Running PTYs are
also stopped when their daemon shuts down. A code rollback may leave the
additive metadata tables in place; older binaries do not read them. If a full
database rollback is required, apply down migrations from 271 through 267 only
after all PTY sessions have stopped.

## Protocol v1

Capability: `terminal-pty-v1`.

- Browser endpoint: `/api/tasks/{task_id}/terminal/ws`.
- Daemon endpoint: `/api/daemon/terminal/ws`.
- Raw input/output is binary. The 28-byte header contains `MT`, version, kind,
  a terminal session UUID, and a big-endian unsigned 64-bit sequence or input
  ID. Payloads are limited to 32 KiB.
- Lifecycle, resize, replay, heartbeat, and lease operations are JSON text
  frames.
- Browser operations are `attach`, `input`, `resize`, `ctrl_c`,
  `claim_control`, `renew_control`, `release_control`, `ack`, and `ping`.
- Server events are `attached`, `output`, `replay_complete`, `gap`, `state`,
  `control`, `exit`, and `error`.

Browser bearer credentials are sent only in the first WebSocket frame. Cookie
authentication is also supported. Tokens never appear in the URL. The gateway
checks Origin, workspace membership, task access, runtime ownership, daemon
identity, task identity, and generation before attaching a peer.

## Limits and lifecycle

- 8 MiB daemon replay ring per Session.
- 8 MiB server replay ring per Session.
- 256-message bounded outbound queue per peer; overflow disconnects only that
  slow peer.
- 16 browser connections per terminal Session.
- 64 KiB/s browser input limit and 64 KiB WebSocket read limit.
- 20-400 columns and 5-200 rows.
- One 30-second controller lease, renewed every 10 seconds.
- Refresh attaches to the same Session and requests bytes after the last
  observed output sequence. Ring eviction emits `gap`.
- Browser disconnect releases control but does not stop Codex.
- `Ctrl+C` writes byte `0x03`; it does not invoke task cancellation.
- **Stop Agent** uses the existing task cancel flow. The daemon sends TERM to
  the PTY process group, escalates to KILL after the grace period, closes the
  PTY, and reaps the process.

## Persistence and observation

Migrations 267-271 add Session metadata, the unique task-generation guard, a
hashed controller lease, a task lookup index, and control audit events. There
are no new foreign keys or cascades. Raw terminal input/output, prompts,
credentials, tokens, environment values, and terminal text are never stored.

Codex rollout files are not treated as a stable protocol. In v1,
`structured_observation` is normally `unavailable`; the Terminal is the truth
source. The Transcript and Files tabs remain historical or explicitly marked
as stale/unavailable. Historical tasks with no in-memory replay show the
existing structured Cockpit rather than pretending to provide live terminal
replay.

## Local verification

```bash
cd server
IS_SANDBOX=1 go test ./...

cd ..
pnpm -C packages/core typecheck
pnpm -C packages/core test
pnpm -C packages/core lint
pnpm -C packages/views typecheck
pnpm -C packages/views test
pnpm -C packages/views lint
git diff --check
make selfhost-build
```

The deterministic `server/cmd/fake-tui` fixture exercises ANSI color,
alternate screen, cursor positioning, burst output, echo and Chinese input,
SIGWINCH resize, Ctrl+C survival, and zero/non-zero exit without starting an
agent or consuming model quota. It is infrastructure evidence, not evidence
that the real Codex TUI works.

## Real browser acceptance

On 2026-08-09, the self-hosted stack was exercised end to end with Codex CLI
`0.147.0`, a real daemon-owned PTY, and two browser tabs against local issue
`COC-2`:

- the xterm view rendered the real Codex alternate-screen TUI and accepted
  exact web input (`WEB_PTY_ACCEPTED`);
- fitting a 900x650 browser viewport propagated a `105x29` PTY resize;
- a second tab attached to the same terminal session as a read-only observer;
- refreshing replayed the same session without starting a second Codex
  process;
- **Ctrl+C** interrupted `sleep 30` while leaving the Codex session usable for
  the next exact response (`AFTER_INTERRUPT`);
- Transcript and Files showed explicit unavailable/empty states rather than
  synthesized provider events;
- **Stop Agent** cancelled the task, terminated the PTY process group, and left
  no Codex or child process behind;
- after the in-memory relay was restarted, the historical task showed the
  structured fallback and a replay-unavailable explanation; ended PTY runs
  offered **Restart** only, never **Continue**.

The acceptance task's Codex sandbox could not reach the host-only local URL;
that network isolation affected the prompt's application request, not the PTY
transport, browser control, replay, resize, interrupt, or stop checks above.

## Security and fault-injection evidence

Automated tests exercise invalid protocol frames, cross-runtime daemon session
registration, duplicate and stale generations, non-controller input, competing
controller claims, lease expiry, browser disconnect, replay gaps after ring
eviction, more than 256 tiny replay writes, a full slow-peer queue, and the
16-browser Session limit. The fake TUI integration also forces burst output,
SIGWINCH, Ctrl+C, and non-zero exit paths. Browser WebSockets reuse the existing
Origin and workspace-membership authentication path; task lookup is scoped to
that authenticated workspace before a Session can attach.

The real acceptance run additionally injected a server relay restart, a browser
refresh, a second observer, a 30-second child command interrupted with Ctrl+C,
and task cancellation. Observed results were bounded replay or an explicit gap,
single-controller behavior, the same PTY Session after refresh, an explicit
historical fallback after relay loss, and no residual provider or child process
after Stop.

## Known v1 limits

- Codex is the only PTY provider.
- Windows returns unsupported and remains on structured execution.
- Raw PTY output is memory-only; server and daemon restarts can make historical
  terminal replay unavailable.
- Structured Codex observation remains unavailable until a stable, documented
  side channel can be validated independently of PTY bytes.
- Mobile is read-only.
