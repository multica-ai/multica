# Local conversation timing diagnostics

Local Codex and Claude Code turns write timing records automatically. Restart the
desktop main process after installing this change; refreshing its page cannot
enable main-process instrumentation.

## Timing log location and privacy

The active Electron profile writes:

```text
<userData>/local-conversations/diagnostics/turn-timing.jsonl
<userData>/local-conversations/diagnostics/turn-timing.jsonl.1
```

On macOS, `userData` is under `~/Library/Application Support/<application name>`.
Canary profiles use their own application name. The file rotates at 5 MiB and
keeps one previous file. Writes are serialized, asynchronous, best effort and
limited to 1,000 pending records. They never delay a turn or fail a send. A full
disk or an overflowing diagnostic queue can therefore leave gaps in the log.
The directory is created with mode 0700 and files with mode 0600.

The daemon owns a separate file, so the two processes never share a writer:

```text
<ProfileDir>/local-conversations/diagnostics/daemon-timing.jsonl
<ProfileDir>/local-conversations/diagnostics/daemon-timing.jsonl.1
```

For Desktop's runtime, `<ProfileDir>` is `~/.multica/profiles/desktop-runtime`,
which is not the directory holding `daemon.log`. The file has the same 5 MiB and
one-previous-file limits. Its writer has a bounded queue of 1,024 records and
never blocks a send; a full queue or failed write becomes a `timing.dropped`
record with a `count`, and an export that reads one reports `coverage.incomplete`
and `coverage.droppedTimingWrites`. Desktop's own log records its gaps the same way
so the standalone MCP can see them. Exports do not coordinate with the daemon's
rotation, so a read that exactly straddles one can miss that generation.

Two more files sit beside it, so link traffic and other devices' reports can never
rotate turn records away:

```text
<ProfileDir>/local-conversations/diagnostics/device-timing.jsonl   (2 MiB + .1)
<ProfileDir>/local-conversations/diagnostics/timing-reports.jsonl  (5 MiB + .1)
```

Only allowlisted stages, opaque conversation/request/turn IDs, provider, source,
timestamps, durations, counts, outcomes, closed-enum dimensions and coarse error
categories are accepted. Unknown dimension values are dropped, not stored.
Message text, tool arguments/results, image data, filenames, credentials, raw
errors, environment variables and unrelated console output are excluded.
Renderer/GUI records pass the same validation before being written, including
in packaged builds. No diagnostics are uploaded.

## Export from Labs or MCP

On desktop, open **Settings → This device → Labs → Developer logs → Export logs**.
The native save dialog writes a JSON snapshot of the selected account/workspace's
retained local conversation timing records from the last 24 hours, this account’s
Web renderer logs in that time range, and the desktop daemon log tail together.
The Web console covers all workspaces for this account. The daemon source covers
the active desktop-owned CLI profile, not the terminal CLI’s default profile. Cancelling writes
nothing. Saving uses a private temporary file and an atomic rename; changing
accounts while the dialog is open cancels the operation. An empty snapshot is a
valid result when no records remain. Version information describes the running
desktop process, rather than whichever checkout happens to be on disk.

The automatically registered `multica_desktop_context` MCP also exposes
`export_developer_logs` to Codex and Claude Code, including without cloud login.
The tool is described in both providers' injected prompt. Its arguments are:

| Argument | Behavior |
| --- | --- |
| `request_id` | Required stable key for one export. Reuse it when retrying. |
| `scope` | `conversation` (default) or `workspace`. |
| `conversation_id` | Defaults to the current conversation. Explicit IDs must belong to this account/workspace. Omit for workspace exports. |
| `since_minutes` | 1–10,080, default 60. This filter cannot recover already rotated records. |

For example, call `export_developer_logs` with
`{"request_id":"latency-check","since_minutes":60}`. It returns the local JSON
path, record count, stage summary, time range and incomplete-read flag. Read the
path with a native file tool; the response does not inject every record into the
model context. No destination path or arbitrary file-read option is accepted.
Duplicate request IDs reuse the same result within an execution.

MCP exports live in `<userData>/local-conversations/diagnostics/exports/`, with
directory mode 0700 and file mode 0600. Each export prunes generated bundles
older than seven days and keeps at most 20. Files saved through the native dialog
are user-managed. Export bundles include allowlisted runtime versions, filters,
coverage, summary, validated timing records, `waterfalls`, `deviceTimeline` and
separate `sources.web` / `sources.daemon` / `sources.daemonTiming` / `sources.linkTiming`
sections (schema version 3). `summary.recordCount` totals timing records, Web records and daemon lines;
separate counts identify each source.
The MCP response includes source availability and truncation without injecting
all log text into the agent’s context.

### Web and daemon sources

Web console output is captured by main in development and packaged builds after
account identity is known. Files are partitioned by a hash of API URL and user ID
at `<userData>/local-conversations/diagnostics/web-<hash>.jsonl` with one `.1`
rotation, 2 MiB per file, private permissions and up to 1,000 pending writes.
Capture is asynchronous and best effort. Timing console records stay in their
existing structured source instead of being duplicated. Logs before this feature
is installed or before account identity is known cannot be recovered.

The daemon snapshot reads at most the last 2 MiB of the current desktop profile’s
`daemon.log`. It never reads config files or selects an arbitrary agent-supplied
path. Daemon timestamps contain only local time of day, so **the conversation and
time filters do not apply to this source**; `timeFilterApplied: false`, scope and
file modification time make that explicit. Missing, empty, unresolved and
unreadable sources are distinguished. Partial tail lines are dropped, and
truncation/read failures are reported without preventing other sources exporting.

Common credential patterns, auth headers, URL query/fragment values, data URLs
and diagnostic payload fields are scrubbed before Web persistence and again on
export; daemon text is scrubbed only in the exported copy. Raw log files are not
modified. Arbitrary diagnostic text can still contain private information, so
review bundles before sharing. They are never uploaded automatically.

### Agents outside desktop conversations

`multica_desktop_context` exists only inside desktop conversations, so agents run
by the daemon (and Codex in a terminal) use a separate stdio MCP,
`multica_developer_logs`. On startup the desktop registers it in the user's Codex
config through `codex mcp add`; the daemon copies that config into each task, so
every Codex agent inherits it. The command is the desktop's own executable with
`ELECTRON_RUN_AS_NODE=1` running `out/main/developer-logs-mcp.js`, so no separate
Node install is needed. Registration is skipped when Codex is missing, when the
entry is already current, or when the user disabled it; the last desktop
started wins when several builds are installed.

The server only reads files the desktop already writes. The desktop publishes
`<userData>/local-conversations/diagnostics/developer-logs-mcp.json` (API URL,
signed-in user ID or null, daemon log and timing paths, runtime versions; no credentials) at
startup and on account changes, and the server reads it on each call. It exposes
one tool, `export_developer_logs(since_minutes)`, writing the same schema version 3
bundle into the same exports folder. Without a desktop conversation list,
timings cover **every local conversation in the time range**
(`filter.timingScope: "device"`, `workspaceId: null`); the Web log is the
published account's, and is omitted while signed out.

## Request flow

```text
GUI submit → renderer bridge → main admission → saved queue
  → queue dispatch/context → persist user turn → resolve attachments
  → prepare Multica credentials/MCP → provider lookup/configuration
  → Codex pool admission/config/thread setup or Claude SDK create/refresh
  → submit turn/input → first provider event
  → first output/text → tool calls/user questions
  → provider result → thread unload/process exit/foreground completion
  → save images, dispose context, recover images, persist final turn
  → renderer reads events → GUI applies the output/terminal projection
```

These phases overlap: the GUI may display output while the provider is still
running. Codex dialogue turns reuse the dual app-server pool and unload their
thread after completion. One-shot CLI calls and persistent Claude SDK queries
have different lifecycle stages; absent per-turn spawn/exit records are expected
for a reused process. Foreground completion does not mean that process exited.

## Send-path marks

`packages/core/local-conversation/timing.ts` is the contract: every mark name, the
closed dimensions (`origin`, `clientKind`, `connection`, `provider`, `prepareKind`,
`turnKind`), `turnIndex`, `attempt`, `prepareId`, the closed `reason`, and the
mapping from the old stage names above onto current marks. Its samples in
`server/internal/localconversation/timing/testdata/contract.json` are decoded by
both the Go and TypeScript sides. Old logs stay readable and read under their
current names in a waterfall (`recordedAs` keeps the name written).

| Where | Marks |
| --- | --- |
| Client | `client.send`, `client.request`, `client.first_event_received`, `client.first_event_applied`, `client.terminal_applied` |
| Daemon entry | `daemon.request_received`, `queue.dispatch`, `turn.persist` |
| Runner | `runner.create`, `runner.limiter`, `prepare.wait` |
| Preheat | `focus.changed` (`count` is the conversation's focused clients), `preheat.requested`, `preheat.cancelled`, `preheat.started`, `preheat.finished`, `preheat.failed`; `reason` says why |
| Spares | `spare.created`, `spare.claim`, `spare.claimed`, `spare.rejected`, `spare.discarded`, `mcp.set`; `reason` says why |
| Reclaim | `runner.evicted` (`durationMs` is how long it sat idle; `reason` is `idle_preheated`, `idle_sent` or `over_limit`) |
| Codex | `provider.spawn`, `provider.handshake`, `provider.config`, `context.bind`, `model.resolve`, `thread.prepare` (`reason` is `start` or `resume`), `provider.turn_start` |
| Claude | `adapter.spawn`, `adapter.ready`, `context.bind`, `sdk.load` and `query.init` (from the adapter's start result; absent from an older adapter), `provider.input_queued`, `provider.first_message` |
| Output | `provider.first_reasoning`, `provider.first_text`, `provider.first_tool`, `batcher.first_output_commit`, `hub.first_output_publish`, `ws.first_output_write` (per subscriber, with `reason` `live` or `catch_up` and that subscriber's identity) |
| End | `turn.finished`, `turn.output_summary` (`count` events, `bytes`, `maxGapMs`, once per turn) |
| Client cost, every 5 s while a conversation changes | `client.projection_summary`, `client.dispatch_summary`, `client.render_summary` (`count` runs, `durationMs` total, `maxGapMs` the longest run); `client.heap_sample` (`count` items loaded, `bytes` JS heap) |
| Provider process | `provider.first_frame` (Codex), `provider.session_ready` (Claude), `provider.exit` (`reason` is `exited` or `stopped`), `thread.prepare` with `bytes` on a Claude resume (its transcript's size) |
| Client | `client.pending_shown`; on mobile `client.first_commit`, which is a React commit because React Native has no paint callback |
| Link service, in `device-timing.jsonl` | `link.start`, `link.ready`, `link.registered`, `link.stop`, `peer.known`, `peer.mdns_found`, `peer.connect`, `relay.reserve`, `relay.connected`, `relay.refused`, `holepunch.start`, `holepunch.success`, `holepunch.failed`, `connection.direct`, `connection.relayed`, `connection.lost` (`connection.direct` and `connection.relayed` carry `transport`: `quic`, `tcp` or `relay`), `network.changed`, and `registry.fetch` only when it fails, recovers or is slow. State changes only; never a tick |
| Tunnel and proxy, in `device-timing.jsonl` | `tunnel.conn` (`reused`), `tunnel.stream_open`, `proxy.request`, `proxy.first_byte` (`role` is `initiator` or `executor`), `migrate.start`, `migrate.stream_closed`, `migrate.reconnected`, `migrate.resubscribed`, and for a client's own commands `link.request_sent`, `link.request_received`, `link.response_sent`, `link.response_received` |
| Traffic per turn and snapshots | `link.event_sent`, `link.event_received` (`count`, `bytes`, `maxGapMs`); `snapshot.server_read`, and on the client `snapshot.request`, `snapshot.first_byte`, `snapshot.received`, `snapshot.parsed`, on mobile `snapshot.rendered` |
| Client socket, another device only | `ws.ticket` (Desktop), `ws.hello`, `ws.open`, `ws.subscribe`, `ws.subscribed`, `ws.catch_up`, `ws.closed` (closed `reason`), `ws.retry` (`durationMs` is the backoff, recorded when it changes), `ws.resubscribed` |

"First" means the turn's first model output. Every turn first commits the user's
message and `turn.started`, which would make a slow turn look fast.

`origin` and `clientKind` come from the daemon's caller, kept in memory by request
ID so a queued turn retains them; after a restart they read as `unknown`. A remote
caller's `clientKind` comes from `X-Multica-Client-Kind`, which Desktop's forwarding
entry and `mobilelink` set; a client that does not send it is `unknown`. A remote
caller's `connection` is read from the stream the request arrived on, not from device
state, which says direct as soon as any connection to that device is direct. `turnKind` is `queued` only
for an item that had to wait: Desktop routes ordinary sends through the queue too.
Background and native turns carry `background: true`, so percentiles leave them out.

## First entry into a long conversation (development)

Development GUI mounts record a bounded `[ui-performance]` sample when a sidebar
conversation is selected. Open previously unopened long conversations, pause on
each for 3–5 seconds, then compare with a short conversation and a return visit.
Use `window.multicaUiPerformance.snapshot()` to read completed samples;
`start()` clears them and enables capture, and `stop()` ends capture. Electron
also writes the summaries to its renderer log.

An uncached conversation initially requests five turns. After the transcript
commits and two animation frames provide a paint opportunity, the active view
requests only the missing older turns up to `min(total turns, 10)`. Existing
viewport prefetch can subsequently load more history. Virtualization uses the
conversation's total turn count for its existing thresholds, including the
minimum of eight turns, rather than treating the five-turn page as the total.
Scheduled supplementation is cancelled when the view hides or unmounts.

Each sample carries `conversationId`, `firstVisit` (since this capture was
installed), `dataCachedAtStart`, total/loaded turn counts, loaded item counts and
mounted row counts. `firstTranscriptFrameMs` is the first animation-frame callback
that observes a transcript row for the selected session, not a browser paint
acknowledgement. `firstQuietFrameMs` observes content followed by at least 250 ms
without root DOM mutations or React commits; it does not promise image, font or
network completion. Selection capture finishes after at least 2 seconds and a
quiet interval, or times out at 10 seconds. A container alone is not content.

`workStages` aggregates count, total and maximum duration for `source_load`
(awaiting the loaded conversation), `source_projection` (committing source
messages), `rows_projection`, `complexity`, `partition`, `locator`,
`participant_headers` and `work_section`. Join the existing `snapshot.request`,
`snapshot.received` and `snapshot.parsed` records by conversation and time for
transport/JSON costs. React durations measure render work; browser long-frame
records identify slow scripts and rendering phases. These measurements can
overlap, so do not add their totals into an end-to-end duration. Work completed
after a capture was stopped is not attributed to a subsequent capture.

Only matching active captures read stage clocks. Capture stops when hidden or
unmounted; no DOM or conversation content is retained in the samples.

## Reports from a phone

A phone keeps its marks in a bounded buffer and reports them to an executor that
offers `timing-report-v1` in `/hello`: `POST /local/v2/timing-reports` with
`{batch_id, records}`, answered `{accepted, rejected, duplicate}`. A batch keeps its
id until acknowledged and is retried when the socket next opens. The executor allows
only a remote phone, about 6 batches a minute per device, conversations in the
request's scope and stages in `timing.Reportable`, and stamps `reporter`, `receivedAt`,
`origin` and `clientKind` itself.

Exports filter and sort reports by `receivedAt`, so a wrong phone clock loses nothing,
and hash `peer` and `reporter`. A record naming a conversation, request or turn joins
that turn's waterfall; one naming none goes on `deviceTimeline`. A turn Desktop started
on another device is not reported: export on both devices and compare by `requestId`.

## Reading one slow turn

`waterfalls.turns` has one entry per turn, newest first and at most 200, merging
every source by turn and request ID. `segments` are ordered by start, with `startMs`
and `endMs` relative to the turn's first record; a segment with `unfinished: true`
started and never ended. Offsets between a client and a daemon on different devices
include clock skew. `totals` stay on one clock each: `clientToFirstEventMs`,
`daemonToFirstOutputMs`, and `transportMs`, which is
`(client.first_event_received − client.send) − (ws.first_output_write − daemon.request_received)`,
pairing the sending client with its own delivery when several clients watch a turn.
A report's own clock does not anchor a turn that has local records, so its offsets can
be negative.
`summary.firstOutput` gives p50/p90 of `daemonToFirstOutputMs` by provider, prepare
kind, origin and client kind.

## Reading a trace

Filter by `requestId` to join GUI submission, queue admission/dispatch and the
main-process execution. `turnId` joins execution with renderer/GUI delivery.
Use `conversationId` to distinguish concurrent conversations and Side sessions.

`at` is a UTC wall-clock timestamp for correlating processes. `elapsedMs` uses a
monotonic clock relative to that span's creation, **not** a shared cross-process
zero. Compare `at` across sources; use `durationMs` for a measured operation.
Start/end records delimit asynchronous operations and preserve failure outcomes.

| Stages | What the measurement establishes |
| --- | --- |
| `gui.submit`, `renderer.request`, `main.request` | Submission/admission latency at each bridge. This ends when accepted, not when the model finishes. |
| `queue.dispatch` | `durationMs` is time since the persisted prompt's creation, including intentional pause or waiting behind another turn. It is wall-clock based and can span an app restart. |
| `queue.context`, `attachments.resolve` | Time spent resolving referenced context and local files. |
| `context.prepare`, `context.credentials`, `context.mcp` | Total preparation, credential verification and local MCP server registration. MCP registration does not prove the provider has completed its own MCP handshake. |
| `provider.lookup`, `provider.config` | CLI discovery and native configuration loading. Codex dialogue reads configuration through the pooled app-server; one-shot CLI calls use a separate probe. |
| `provider.pool_admission`, `provider.pool_wait` | Total admission time (including serialization/configuration), and waiting for an idle slot while generations drain. These spans overlap initialization/configuration and must not be added together. |
| `provider.pool_create`, `provider.pool_reused`, `provider.bridge`, `provider.handshake` | New worker construction, reuse of an existing worker, local MCP bridge startup and app-server handshake. Creation is distinct from OS-confirmed spawn; standby warmup does not imply a model request. |
| `provider.thread_wait`, `provider.thread_ready`, `provider.turn_start`, `provider.thread_unload` | Waiting for prior thread unload, thread start/resume, turn/start acknowledgement and final unload. The acknowledgement is from the local app-server, not the remote model service. |
| `provider.spawn` | Operating system confirmed creation of the execution process. |
| `provider.sdk_ready`, `provider.refresh` | Claude query creation or an existing query's MCP refresh completed. This is not evidence that a network request reached the model. |
| `provider.input_written`, `provider.input_yielded` | Input was written to CLI stdin or yielded to the Claude SDK. Neither means remote acceptance. |
| `provider.first_stdout`, `provider.first_stderr`, `provider.first_event` | First CLI bytes or protocol/SDK event. Startup events are distinct from a reply. |
| `provider.session_ready` | Provider reported a native session ID. |
| `provider.first_output`, `provider.first_text` | First parsed text/thinking/tool activity, and first nonempty assistant text, respectively. Codex exec may emit completed messages rather than individual tokens. |
| `provider.tool`, `user.question` | Tool invocation-to-result duration, or time waiting for a user's answer. Tool payloads and tool names are not logged. |
| `provider.retry`, `provider.failure` | Retry notices or failures with a coarse category: transport, authentication, rate limit, cancelled or other. Categories are hints, not a root-cause conclusion. |
| `provider.result`, `provider.exit`, `turn.result` | Native result/foreground settlement, CLI exit, and the service receiving the runner result. This separates model work from exit delay. |
| `images.save`, `context.dispose`, `images.recover`, `turn.save` | Post-processing and persistence. |
| `renderer.first_output_received` | First event read by this renderer instance; it may be a historical read after reopening. |
| `gui.first_output_applied`, `gui.terminal_applied` | Output/settlement applied to the GUI state for a turn observed live in this frame. This measures state application, not browser paint. History already completed when the frame opens is excluded. |

While dispatching or executing, a record with `event: "wait"` is emitted every
15 seconds. Its stage is the currently measured operation, or the last reached
checkpoint when no measured operation remains open. `idleMs` is time since the
last observed activity; it does not prove the provider has stopped working.
`turn.finished` ends the span and stops its heartbeat on success, failure or
cancellation. Admission spans without a turn ID are distinct from execution.

If input delivery is fast but first output is late, investigate the provider's
native logs/network/service next. If first output is timely but GUI application
is late, inspect event notification, history reads and GUI reconciliation. A
late `provider.exit` after `provider.result` instead indicates process teardown.

## Verification

Tests inject fake executables/SDK transports and never call a real model:

```sh
pnpm --filter @multica/desktop exec vitest run src/main/local-timing-log.test.ts src/local-host/local-provider-runner.test.ts src/local-host/local-conversation-service.test.ts src/local-host/local-conversation-queue.test.ts src/local-host/local-claude-conversation.test.ts src/local-host/local-multica-context.test.ts src/renderer/src/platform/local-conversation-service.test.ts
pnpm --filter @multica/desktop exec vitest run src/main/developer-log-export.test.ts src/main/developer-log-waterfall.test.ts src/main/developer-runtime-logs.test.ts src/local-host/local-context-mcp.test.ts src/local-host/local-codex-pool.test.ts src/renderer/src/components/labs-settings-tab.test.tsx
pnpm --filter @multica/views exec vitest run settings/components/settings-page.test.tsx settings/components/settings-navigation.test.ts locales/parity.test.ts
pnpm --filter @multica/tutti-agent-gui-host exec vitest run src/local-timing.test.ts src/runtime/runtime.test.ts
pnpm --filter @multica/desktop typecheck
pnpm --filter @multica/tutti-agent-gui-host typecheck
```

The export tests cover scoped records, validation after log rotation, native
save cancellation/failure, account changes during the save dialog, restrictive
file permissions, generated-file retention, MCP retry deduplication and actual
MCP registration/calls without cloud credentials. UI tests cover pending,
success, cancellation and retry. Manual desktop verification should save and
inspect a real JSON file, cancel a second export and check both themes. Restore
the original theme afterwards.
