# Hermes Session Bridge — reverse registration & remote takeover

Multica dispatches work *down* to agents: an issue is assigned, a daemon
runs it, results come back. This example covers the opposite direction,
which Multica does not cover yet: work a user starts *locally* — in the
Hermes desktop app or CLI, with no Multica issue involved — is mirrored
*up* into the workbench as an issue, so the owner can watch it, review
it, and steer it from anywhere (for example from home, or from a phone).

```
Hermes desktop/CLI            this bridge                 Multica
(local session)               (sidecar, stdlib-only)
     │                             │                           │
     │  session store (state.db)   │                           │
     ├──────────── read ──────────►│                           │
     │                             │── issue create ──────────►│  card appears,
     │                             │   status in_progress      │  labelled
     │                             │                           │
     │  (session ends / idles)     │── summary comment ───────►│  card moves
     │                             │   status in_review        │  to in_review
     │                             │                           │
     │                             │◄─ owner comment ──────────┤  owner steers
     │◄── hermes chat --resume ────┤   (when session is idle)  │  from the card
     │  (same session, full        │── receipt + reply ───────►│
     │   context, same cwd)        │                           │
```

## How it relates to existing Multica work

- **Comment steering / chat guidance** (#8760, #10021) steer runs that
  Multica itself dispatched. This bridge steers sessions Multica never
  started and does not own.
- **Resident Hermes gateway mode** (#10118, #10119, #9368) gives
  dispatched tasks a shared, always-on Hermes with a persistent session
  store. It does not surface pre-existing *interactive* sessions — ones
  the user opened in the desktop app — on the board.
- **Plugin Action API** (#7620, #8851) lets external systems list and
  create issues. This bridge deliberately uses only the stock `multica`
  CLI, so it works against an unmodified server today.

The two approaches compose: a fleet can run dispatched work through a
resident gateway *and* mirror local interactive work with this bridge.

## Behaviour

- **Scope.** Only root sessions with `source in (local, gui)` in the
  Hermes default profile's `state.db`. Subagent children, ACP editor
  sessions, one-shots and gateway/channel sessions (e.g. Feishu) are
  ignored.
- **Qualification.** A session becomes an issue once it looks like real
  work: >= 2 user turns, or >= 1 user turn and >= 10 messages.
- **No history flood.** Sessions that already ended before the bridge's
  first run are never registered. Sessions still open at first run are
  adopted exactly once.
- **Lifecycle mirror.** Active -> `in_progress`; ended or idle for
  `BRIDGE_IDLE_SEC` (default 4 h) -> a delta summary comment is posted
  and the issue moves to `in_review`; new activity (local or injected)
  moves it back to `in_progress` and the next summary covers only the
  new delta.
- **Takeover.** New comments on a mirrored issue are queued. When the
  session is idle (not ended sessions resume immediately; live sessions
  must be quiet for `BRIDGE_INJECT_IDLE_SEC`, default 120 s, so the
  bridge never fights the person sitting at the machine), the queued
  comments are combined and injected with
  `hermes chat --resume <session-id> --query-file <file> -Q --yolo
  --accept-hooks`. The resumed turn runs with the session's full history
  and its recorded working directory. Receipts and a scrubbed reply
  excerpt are posted back to the issue. Comments starting with `【` or
  `[bridge]` are treated as bridge/system notes and never injected.
- **Writes go through the CLI only.** The bridge reads `state.db`
  read-only and performs every Multica write with the `multica` CLI
  under a normal user profile — no server changes, no direct DB writes,
  no plugin installation.

## Privacy: summaries are scrubbed deterministically

The bridge's first production summaries were generated from a payroll
session, and the LLM leaked figures *despite* explicit prompt
instructions (an unlabelled base-salary range, a two-decimal amount, a
person's name). Prompting alone is not a privacy control, so the
pipeline is layered and fail-closed:

1. The summarisation prompt forbids names and figures.
2. Every summary (and every injected-reply excerpt) passes through
   deterministic regex scrubbing: currency amounts with units/symbols,
   `6000/8000` style ranges, thousand-separated numbers, two-decimal
   numbers, bare numbers >= 1000, and numbers adjacent to salary-domain
   keywords. Calendar years, version numbers and headcounts survive.
   The keyword list (`SALARY_KEYWORDS`) is bilingual EN/中文 by default —
   extend it for your own sensitive domain.
3. A second LLM pass rewrites the summary to remove personal names. If
   that pass fails, the summary is *withheld* and replaced with a stub
   instead of being posted raw.

## Setup

Prerequisites on the machine that runs the local Hermes sessions:

- Hermes Agent installed and used locally (so `state.db` exists), with
  the `hermes` CLI available (the desktop app's shim is fine).
- The `multica` CLI, logged in (`multica --profile <name> issue list`
  should work). The bridge acts with this profile's identity.
- Python 3.10+, standard library only. No pip installs.

Always dry-run first — it writes nothing and prints what *would* be
registered from the current session store:

```bash
BRIDGE_DRY_RUN=1 python3 bridge.py
```

Then run it:

```bash
BRIDGE_PROFILE=yanqu \
BRIDGE_MACHINE_LABEL=office-pc \
BRIDGE_SUMMARY_URL=https://your-llm-endpoint/v1/chat/completions \
BRIDGE_SUMMARY_KEY=sk-... \
BRIDGE_SUMMARY_MODEL=your-model \
python3 bridge.py
```

Without the `BRIDGE_SUMMARY_*` variables, summaries fall back to a
rule-based stub (message counts, first request, latest reply) — still
scrubbed. To test the full pipeline against one historical session:

```bash
BRIDGE_TEST_SESSION=<session-id> python3 bridge.py   # registers + summarises it once
```

Run it persistently with your supervisor of choice — a Windows
Scheduled Task (`pythonw.exe bridge.py`, trigger: at log on) or a
systemd user service on Linux:

```ini
[Service]
ExecStart=/usr/bin/python3 /path/to/bridge.py
Restart=on-failure
Environment=BRIDGE_PROFILE=yanqu
```

Stop it by ending the process or dropping a `bridge.stop` file into the
state directory.

### Configuration

| Variable | Default | Meaning |
|---|---|---|
| `BRIDGE_HERMES_HOME` | `%LOCALAPPDATA%\hermes` / `~/.hermes` | Hermes profile home |
| `BRIDGE_DB` | `<home>/state.db` | Session store path |
| `BRIDGE_STATE_DIR` | `./bridge-state` | Bridge state, log, stop file |
| `BRIDGE_MULTICA_BIN` | `multica` | multica CLI binary |
| `BRIDGE_PROFILE` | *(none)* | multica CLI profile |
| `BRIDGE_HERMES_BIN` | `hermes` | hermes CLI binary (for injection) |
| `BRIDGE_ORIGIN_LABEL` | `local-session` | Label marking mirrored issues |
| `BRIDGE_MACHINE_LABEL` | hostname | Label naming the source machine |
| `BRIDGE_SUMMARY_URL/KEY/MODEL` | *(unset)* | Optional LLM for summaries |
| `BRIDGE_POLL_SEC` | `60` | Poll interval |
| `BRIDGE_IDLE_SEC` | `14400` | Idle time before summary + in_review |
| `BRIDGE_INJECT_IDLE_SEC` | `120` | Quiet time before injecting into a live session |
| `BRIDGE_INJECT_TIMEOUT` | `1800` | Max seconds for an injected turn |

## Limitations, and what a native version could do better

- **Polling, not events.** A native implementation inside the daemon
  could subscribe to session-store changes instead of polling every
  minute, and could mark mirrored issues with a first-class origin
  instead of labels.
- **Hermes only.** The watcher is written against Hermes' `state.db`
  schema. The pattern transfers to other agents with a local session
  store (Claude Code, Codex), but each needs its own reader and resume
  command.
- **Single machine, single profile** per bridge process. Run one bridge
  per machine/profile.
- **Injection is a CLI resume.** It cannot interrupt a turn that is
  actively running at the machine; it queues until the session is idle
  by design.
- Card attribution uses the CLI profile's identity, so mirrored issues
  and bridge comments appear under that user, as with any CLI client.

## Status

Reference implementation, in production use since 2026-11-10 in a
small self-hosted fleet: one Windows workstation's local Hermes
sessions (including a long-running payroll session) are mirrored to the
workbench, reviewed there, and steered through comment injection.
Shared in the hope that the pattern — not necessarily this sidecar —
finds a native home in Multica.
