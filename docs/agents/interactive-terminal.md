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
