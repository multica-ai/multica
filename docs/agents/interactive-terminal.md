# Interactive Terminal — Architecture

Cerebro feature that streams a running agent's output live to a browser-based terminal panel.

## Feature flag

`cerebro_interactive_terminal` — defaults to `false`. Must be enabled per workspace before the toggle renders.

## How it works end-to-end

```
User enables interactive mode on a runtime
        │
        ▼
  DB: agent_runtime.presentation_mode = 'interactive'
        │
  Task is assigned and claimed by daemon
        │
  daemon.go:runTask → task.PresentationMode == "interactive"
        │
        ▼
  cerebroAttachTerminal(ctx, task)          [daemon/cerebro_terminal.go]
  ├── sends cerebro:term_attach frame over daemonws WS
  └── installs cerebroTermSink on daemon (buffers + sends term_stdout frames)
        │
        ▼  (over daemon ↔ server WebSocket)
  DaemonBridge.handleAttach()               [server/internal/cerebro/terminal/daemon_bridge.go]
  ├── verifies runtime is in identity scope
  ├── queries DB: presentation_mode == "interactive"?
  └── broker.Adopt() → Session created, indexed by runtimeId
        │
  Agent runs → stdout → cerebroTermSink → pump.flush()
        │
        ▼  (cerebro:term_stdout frames over daemonws WS)
  DaemonBridge.handleStdout() → session.PushOutput()
        │
        ▼  (broker fan-out)
  Browser WebSocket subscriber ← session stdout chunks
        │
  Browser renders in xterm.js terminal panel
```

## Session lifecycle

| Event | What happens |
|---|---|
| `term_attach` received | `broker.Adopt()` registers session in `runtimeIndex` |
| `term_stdout` received | `session.PushOutput()` fans out to all browser subscribers |
| `term_exit` received | `session.Exit()` → `closed = true`; subscriber channels closed |
| Browser opens terminal | Polls `GET /api/cerebro/terminal/runtimes/{id}/session` every 1500 ms |
| 200 response | Frontend opens WebSocket at returned `attach_path` |
| 204 response | No active session — stays in "Waiting for agent…" state |
| WS closes | Frontend returns to polling ("Waiting for agent…") |

## UI entry points

- **`PresentationModeToggle`** (`packages/cerebro-terminal/views/components/presentation-mode-toggle.tsx`): Switch on runtime detail page. Calls `PUT /api/cerebro/terminal/runtimes/{id}/presentation-mode`.
- **Terminal popout** (`apps/web/app/[workspaceSlug]/terminal/[runtimeId]/page.tsx`): Full-window xterm.js view, opened via `window.open()`. Uses `TerminalPanel` which calls `useTerminalSession`.
- **`useIssueTerminalLink`** (`packages/cerebro-terminal/views/use-issue-terminal-link.ts`): Hook that shows "Open terminal" link on issue page when runtime is interactive.

## Key files

| File | Role |
|---|---|
| `packages/cerebro-terminal/views/use-terminal-session.ts` | Frontend polling + WebSocket lifecycle hook |
| `packages/cerebro-terminal/views/components/terminal-panel.tsx` | xterm.js view component |
| `server/internal/cerebro/terminal/broker.go` | In-memory session registry |
| `server/internal/cerebro/terminal/daemon_bridge.go` | Translates daemon WS frames → broker calls |
| `server/internal/cerebro/terminal/handler.go` | HTTP + WebSocket handlers for browser |
| `server/internal/daemon/cerebro_terminal.go` | Daemon-side stdout tap + frame sender |
| `server/internal/daemon/daemon.go` | Calls `cerebroAttachTerminal` when `presentation_mode == "interactive"` |
| `server/migrations/9024_cerebro_runtime_presentation.up.sql` | `presentation_mode` column on `agent_runtime` |

## Constraints

- **Daemon-only**: `SetPresentationMode` rejects cloud runtime providers (`firtal-gateway`). Only runtimes backed by a local daemon can stream.
- **Single sink**: The daemon holds one `cerebroTermSink` at a time. If multiple tasks run concurrently on the same runtime, only the last one's output is mirrored.
- **In-memory broker**: Sessions live in RAM. A server restart clears all sessions. The daemon re-announces on reconnect via a fresh `term_attach`.
- **v1 read-only**: `sendInput` on the frontend is a no-op. stdin from the browser is wired through the protocol but intentionally dropped at the daemon (the channel is drained so it never blocks).

## Known bugs fixed (2026-06-08)

### Bug 1 — `term_attach` dropped at task start (race condition)

**File:** `server/internal/daemon/wakeup.go`

`signalTaskWakeup` was called at line 102, BEFORE `d.wsWrites.Store(&writes)` at line 119. Because `runTask` goroutines can start immediately on the signal, `cerebroAttachTerminal` would call `enqueueCerebroFrame` while `d.wsWrites` was still nil — silently dropping `term_attach`. No session was ever created, leaving the browser in "Waiting for agent…" permanently.

**Fix:** Move `wsWrites.Store` to before `signalTaskWakeup`.

Patch: `CEREBRO-PATCH(daemon-ws-write-accessor-signal-order)`

### Bug 2 — daemonws read limit too small for `term_stdout` frames

**File:** `server/internal/daemonws/hub.go`

`c.conn.SetReadLimit(4096)` was set for heartbeat frames only. `term_stdout` frames are base64-encoded and flushed at `cerebroTermFlushBytes = 8 KB`, producing frames of ~11 KB after encoding + JSON wrapping. gorilla/websocket would close the connection with a 1009 "message too big" error on the first large flush, preventing terminal output from ever reaching the browser.

**Fix:** Increase `SetReadLimit` from 4096 to `128 * 1024`.

Patch: `CEREBRO-PATCH(daemonws-term-read-limit)`

## Operational dependency — the whole feature rides on ONE WebSocket (TECH-3388)

Everything the daemon normally does (claim tasks, heartbeat, wakeup) has an
**HTTP fallback**: if the daemon ↔ server task-wakeup WebSocket (`/api/daemon/ws`)
is unavailable, the daemon degrades to HTTP polling and agents keep running
"glimrende".

The interactive terminal does **not** have that fallback. `term_attach`,
`term_stdout`, and `term_exit` are sent **only** over that WebSocket
(`server/internal/daemon/cerebro_terminal.go` → `enqueueCerebroFrame` →
`d.wsWrites`). If the WS does not connect (reverse proxy doesn't upgrade
`/api/daemon/ws`, TLS/origin issue, HTTP/2-only proxy that won't do an
RFC-6455 upgrade), then:

- the agent still runs and posts comments normally (HTTP path), **but**
- no `term_attach` ever reaches the server → broker never `Adopt`s a session →
  `GET /api/cerebro/terminal/runtimes/{id}/session` returns `204` forever →
  the browser sits on **"Waiting for agent…"** indefinitely.

This is the first thing to check when "the agent works but the live terminal
doesn't": confirm the daemon log shows `task wakeup websocket connected`, not
`task wakeup websocket unavailable; polling fallback remains active`.

**A second thing to check: the daemon binary version.** The terminal needs the
daemon-side fixes from 2026-06-08 (`CEREBRO-PATCH(daemon-ws-write-accessor-signal-order)`).
A daemon built before that drops `term_attach` at task start and the terminal
never connects even though the agent runs fine. Check `multica --version` /
`multica update` on the machine hosting the runtime.

### Surviving WS flaps (TECH-3388)

The task-wakeup WS flaps regularly in the field (`close 1006 unexpected EOF`,
reconnect gaps up to ~55s — likely a tunnel/proxy connection max-age). Before
the fix, the server closed the adopted session 15s after any disconnect, so the
browser terminal died on every flap. Now:

- `daemonDisconnectGrace` is 90s (comfortably exceeds an observed reconnect).
- On reconnect the daemon re-announces each active task; the duplicate
  `term_attach` bumps `Session.reattachGen`, and the disconnect reaper skips any
  session whose generation changed during the grace window. A flap is invisible
  to the browser apart from a brief pause in output.
- Output produced *during* the reconnect gap is **buffered on the daemon and
  replayed in order on reconnect** (`CEREBRO-PATCH(daemon-cerebro-term-buffer)`,
  TECH-3388). When `enqueueCerebroFrame` can't send (WS down or write channel
  backed up), `cerebroTermPump.flush` routes the frame to a per-task ordered
  backlog (`cerebroTermBuffers`) instead of dropping it. The WS reconnect path in
  `wakeup.go` re-emits the attach frames, then calls `drainAllCerebroTermBuffers`
  to flush each task's backlog in order — so a Cloudflare WS restart now costs a
  brief pause, not lost output. The backlog is bounded at
  `cerebroTermBufferMaxBytes` (512 KB) per task; during a prolonged outage the
  oldest frames are dropped first, keeping the most recent output. This is what
  "100% fremover" needs: the live terminal survives the ~30-90 min Cloudflare WS
  flaps (`close 1001 "CloudFlare WebSocket proxy restarting"`) without going
  blank.

  **Still single-transport.** The backlog rides the same WS on reconnect; it does
  not add an HTTP fallback. If the WS never connects at all (proxy won't upgrade
  `/api/daemon/ws`), there is still nothing to flush onto — see "Operational
  dependency" above. The backlog fixes *flaps*, not a permanently-down WS.

## Fixed — many interactive terminals at once, per daemon process (TECH-3388)

`cerebroTermSink` and `cerebroActiveAttach` are **single process-wide fields**
on the `Daemon` struct (`daemon.go` lines 173-176), but the daemon runs up to
`MaxConcurrentTasks` tasks at once (`newTaskSlotSemaphore`), possibly across
several interactive runtimes hosted by the same daemon.

Consequences when two or more interactive tasks overlap:

1. **Last-attach-wins.** `cerebroAttachTerminal` calls `setCerebroTermSink(...)`
   which overwrites the single field. `executeAndDrain` reads that one field and
   calls it with its own `taskID`; the installed closure filters
   `if taskID != task.ID { return }`, so the **earlier** task's output is
   silently dropped — its browser shows a frozen/blank terminal even though the
   agent is producing output.
2. **Teardown kills the others.** When *any* interactive task ends, its teardown
   runs `setCerebroTermSink(nil)` + `cerebroActiveAttach.Store(nil)`,
   disabling mirroring (and reconnect re-emit) for every other still-running
   interactive task.

For a busy dogfood runtime (Sara, Mia) that runs many tasks in parallel, this
broke the live terminal most of the time, not just in a rare race.

**Fix (shipped):** the daemon sink is now a per-task registry keyed by `taskID`
(`cerebroTermSinks map[string]func(string)` under `cerebroTermSinkMu`), and the
in-flight attach frames are a `cerebroActiveAttaches map[string][]byte` so a WS
reconnect re-emits every active task's attach (not just the latest). Each task's
teardown removes only its own `taskID`. `executeAndDrain` routes through
`dispatchCerebroTerm(taskID, text)`, which looks up that task's sink. Concurrent
interactive tasks — across slots or across runtimes hosted by one daemon — now
each mirror to their own broker session.

Note: the browser popout is keyed by `runtimeId`, and `GetByRuntime` returns the
most-recent open session for a runtime. So "many terminals at once" means one
live terminal per runtime across many runtimes. If a single runtime runs several
concurrent tasks, its popout shows the newest task's session.

## Diagnostic runbook — where is it breaking?

Walk the chain in order; the first failing hop is the cause.

| Check | How | If it fails |
|---|---|---|
| Feature flag on for the workspace | `cerebro_interactive_terminal` (defaults **false**) | Toggle + "Open terminal" never render |
| Runtime is `interactive` | `GET /api/cerebro/terminal/runtimes/{id}/presentation-mode` | Toggle was never flipped, or it's a `firtal-gateway` cloud runtime (rejected) |
| Daemon WS connected | daemon log: `task wakeup websocket connected` | term frames have no transport — see "Operational dependency" above |
| Server adopted a session | server log: `term_attach: session adopted` | claim didn't carry `presentation_mode`, or attach frame dropped |
| Active session visible to UI | `GET /api/cerebro/terminal/runtimes/{id}/session` returns `200` (not `204`) | broker has no open session for the runtime |
| Browser WS attaches | network tab: WS to `/api/cerebro/terminal/sessions/{id}/ws` upgrades (101) + `auth_ack`/cookie | cross-origin cookie not sent, or auth handshake mismatch |
| Only one task running | if multiple interactive tasks overlap → single-sink bug above | output frozen/blank for all but the most recent task |
