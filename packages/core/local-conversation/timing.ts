/**
 * Timing record contract shared by the daemon, Desktop and mobile. It only
 * carries identifiers, timings, counts and closed-enum classifications: never
 * prompts, payloads, paths, addresses or raw error text. The Go side
 * (server/internal/localconversation/timing) decodes the same samples in
 * server/internal/localconversation/timing/testdata/contract.json.
 */

/** Stage names of the Electron-era pipeline; old logs stay readable. */
export const LOCAL_TIMING_LEGACY_STAGES = [
  "gui.submit", "gui.first_output_applied", "gui.terminal_applied", "renderer.request", "renderer.first_output_received", "main.request",
  "queue.context", "turn.execute", "turn.result", "attachments.resolve",
  "context.prepare", "context.credentials", "context.mcp", "provider.lookup",
  "provider.sdk_ready", "provider.refresh", "provider.input_written", "provider.input_yielded",
  "provider.pool_admission", "provider.pool_wait", "provider.pool_create", "provider.pool_reused", "provider.bridge",
  "provider.thread_wait", "provider.thread_ready", "provider.thread_unload",
  "provider.first_stdout", "provider.first_stderr", "provider.first_event",
  "provider.first_output", "provider.tool", "provider.result", "provider.failure", "provider.retry",
  "title.generate", "title.persist", "user.question", "images.save", "context.dispose", "images.recover", "turn.save",
] as const;

/** Every mark of the plan's list, so an older reader does not reject what a newer writer emits. */
export const LOCAL_TIMING_MARKS = [
  "client.send", "client.pending_shown", "client.request",
  "daemon.request_received", "queue.dispatch", "turn.persist",
  "runner.create", "runner.limiter", "prepare.wait",
  "batcher.first_output_commit", "hub.first_output_publish", "ws.first_output_write",
  "client.first_event_received", "client.first_event_applied", "client.first_commit",
  "turn.finished", "client.terminal_applied",
  "provider.spawn", "provider.handshake", "provider.config", "context.bind", "model.resolve", "thread.prepare",
  "provider.turn_start", "provider.first_frame", "provider.first_reasoning", "provider.first_text", "provider.first_tool", "provider.exit",
  "adapter.spawn", "adapter.ready", "sdk.load", "query.init", "spare.claim", "mcp.set",
  "provider.input_queued", "provider.first_message", "provider.session_ready", "provider.first_thinking",
  "focus.changed", "preheat.requested", "preheat.cancelled", "preheat.started", "preheat.finished", "preheat.failed",
  "spare.created", "spare.claimed", "spare.rejected", "spare.discarded", "runner.evicted",
  "link.start", "link.ready", "link.registered", "link.stop",
  "registry.fetch", "peer.known", "peer.mdns_found",
  "peer.connect", "relay.reserve", "relay.connected", "relay.refused",
  "holepunch.start", "holepunch.success", "holepunch.failed",
  "connection.direct", "connection.relayed", "connection.lost", "network.changed",
  "tunnel.conn", "tunnel.stream_open", "proxy.request", "proxy.first_byte",
  "ws.ticket", "ws.hello", "ws.open", "ws.subscribe", "ws.subscribed", "ws.catch_up",
  "migrate.start", "migrate.stream_closed", "migrate.reconnected", "migrate.resubscribed",
  "ws.closed", "ws.retry", "ws.resubscribed",
  "link.request_sent", "link.request_received", "link.response_sent", "link.response_received", "link.event_sent", "link.event_received",
  "snapshot.request", "snapshot.server_read", "snapshot.first_byte", "snapshot.received", "snapshot.parsed", "snapshot.rendered",
  "turn.output_summary",
  "timing.dropped",
] as const;

export const LOCAL_TIMING_STAGES = [...LOCAL_TIMING_LEGACY_STAGES, ...LOCAL_TIMING_MARKS] as const;
export type LocalTimingStage = typeof LOCAL_TIMING_STAGES[number];
export type LocalTimingMark = typeof LOCAL_TIMING_MARKS[number];

/** What an old stage measured under the current mark list. Other old stages have no successor. */
export const LOCAL_TIMING_LEGACY_MAPPING: Partial<Record<LocalTimingStage, LocalTimingMark>> = {
  "gui.submit": "client.request",
  "gui.first_output_applied": "client.first_event_applied",
  "gui.terminal_applied": "client.terminal_applied",
  "renderer.request": "client.request",
  "renderer.first_output_received": "client.first_event_received",
  "main.request": "daemon.request_received",
  "context.prepare": "context.bind",
  "provider.sdk_ready": "query.init",
  "provider.input_written": "provider.input_queued",
  "provider.input_yielded": "provider.input_queued",
  "provider.pool_admission": "prepare.wait",
  "provider.thread_ready": "thread.prepare",
  "provider.first_event": "provider.first_message",
};

export function canonicalLocalTimingStage(stage: LocalTimingStage): LocalTimingStage {
  return LOCAL_TIMING_LEGACY_MAPPING[stage] ?? stage;
}

export const LOCAL_TIMING_DIMENSIONS = {
  origin: ["local", "p2p", "unknown"],
  clientKind: ["desktop", "mobile", "unknown"],
  connection: ["loopback", "direct", "relayed", "unknown"],
  provider: ["codex", "claude"],
  prepareKind: ["hot", "preheated", "spare", "cold"],
  turnKind: ["user", "queued", "steer", "background", "native", "agent"],
} as const;
export type LocalTimingOrigin = typeof LOCAL_TIMING_DIMENSIONS.origin[number];
export type LocalTimingClientKind = typeof LOCAL_TIMING_DIMENSIONS.clientKind[number];
export type LocalTimingConnection = typeof LOCAL_TIMING_DIMENSIONS.connection[number];
export type LocalTimingProvider = typeof LOCAL_TIMING_DIMENSIONS.provider[number];
export type LocalTimingPrepareKind = typeof LOCAL_TIMING_DIMENSIONS.prepareKind[number];
export type LocalTimingTurnKind = typeof LOCAL_TIMING_DIMENSIONS.turnKind[number];

export const LOCAL_TIMING_REASONS = [
  "start", "resume", "skipped", "live", "catch_up",
  "concurrency_limit", "provider_unavailable", "provider_error", "provider_exited", "outcome_unknown",
  "storage_limit", "compaction_failed", "interrupt_unconfirmed", "remote_failed",
  "focused", "unfocused", "connection_lost",
  "not_focused", "draining", "disabled", "no_session", "archived",
  "claim_refused", "option_not_applied", "settings_not_applied", "mcp_failed", "bind_failed", "unsupported",
  "max_age", "config_changed", "cli_changed", "environment_changed", "no_clients", "exited", "stopped",
  "idle_preheated", "idle_sent", "over_limit",
  "explicit", "config", "catalog", "catalog_cached", "native_default",
  "background", "slow", "late_direct", "reservation_refused", "resource_limit", "permission_denied", "connection_failed", "ws_normal", "ws_going_away", "ws_abnormal", "ws_protocol", "ws_handshake_timeout",
] as const;
export type LocalTimingReason = typeof LOCAL_TIMING_REASONS[number];

/** A daemon that takes a phone's marks says so in /hello. */
export const LOCAL_TIMING_REPORT_CAPABILITY = "timing-report-v1";

export const LOCAL_TIMING_SOURCES = ["main", "renderer", "gui", "daemon", "mobile"] as const;
export type LocalTimingSource = typeof LOCAL_TIMING_SOURCES[number];
export type LocalTimingEvent = "start" | "end" | "mark" | "wait";
export type LocalTimingOutcome = "completed" | "failed" | "cancelled";
export type LocalTimingFailure = "rate_limit" | "authentication" | "transport" | "cancelled" | "other";

export const LOCAL_TIMING_ROLES = ["initiator", "receiver", "executor"] as const;
export type LocalTimingRole = typeof LOCAL_TIMING_ROLES[number];
export const LOCAL_TIMING_ADDR_TYPES = ["loopback", "lan", "public", "relay", "unknown"] as const;
export type LocalTimingAddrType = typeof LOCAL_TIMING_ADDR_TYPES[number];

export interface LocalTimingDimensions {
  provider?: LocalTimingProvider;
  origin?: LocalTimingOrigin;
  clientKind?: LocalTimingClientKind;
  connection?: LocalTimingConnection;
  prepareKind?: LocalTimingPrepareKind;
  turnKind?: LocalTimingTurnKind;
  turnIndex?: number;
  attempt?: number;
  prepareId?: string;
}
export interface LocalTimingIdentity extends LocalTimingDimensions {
  source: LocalTimingSource;
  background?: boolean;
  /** Absent on device-level records, which belong to no conversation. */
  conversationId?: string;
  requestId?: string;
  turnId?: string;
}
export interface LocalTimingRecord extends LocalTimingIdentity {
  version: 1 | 2;
  at: string;
  stage: LocalTimingStage;
  event: LocalTimingEvent;
  elapsedMs: number;
  durationMs?: number;
  idleMs?: number;
  outcome?: LocalTimingOutcome;
  failure?: LocalTimingFailure;
  reason?: LocalTimingReason;
  /** A count, such as the writes a `timing.dropped` record stands for. */
  count?: number;
  /** Bytes and the largest gap of a per-turn or per-snapshot summary. */
  bytes?: number;
  maxGapMs?: number;
  /** The other device; exports replace it with a hash. */
  peer?: string;
  role?: LocalTimingRole;
  addrType?: LocalTimingAddrType;
  reused?: boolean;
  /** Stamped by the executor on a report, on its own clock: the sender's clock can be wrong. */
  reporter?: string;
  receivedAt?: string;
}

const stages = new Set<string>(LOCAL_TIMING_STAGES);
const sources = new Set<string>(LOCAL_TIMING_SOURCES);
const reasons = new Set<string>(LOCAL_TIMING_REASONS);
const IDENTIFIER = /^[\w:.-]{1,160}$/;
const AT = /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{3}Z$/;
const isIdentifier = (value: unknown): value is string => typeof value === "string" && IDENTIFIER.test(value);
const isMs = (value: unknown): value is number => typeof value === "number" && Number.isFinite(value) && value >= 0;
const isCount = (value: unknown): value is number => typeof value === "number" && Number.isSafeInteger(value) && value >= 0;
const member = <T extends string>(values: readonly T[], value: unknown): T | undefined => (values as readonly unknown[]).includes(value) ? value as T : undefined;

/**
 * Rebuilds the allowlisted shape. A record without a known stage or with an
 * identifier that could carry anything but an opaque id is rejected; unknown
 * values of a closed dimension are dropped so one bad field keeps its timing.
 */
export function parseLocalTiming(value: unknown): LocalTimingRecord | null {
  if (!value || typeof value !== "object") return null;
  const raw = value as Record<string, unknown>;
  if ((raw.version !== 1 && raw.version !== 2) || !sources.has(String(raw.source))
    || !stages.has(String(raw.stage)) || !["start", "end", "mark", "wait"].includes(String(raw.event))
    || typeof raw.at !== "string" || !AT.test(raw.at) || !Number.isFinite(Date.parse(raw.at)) || !isMs(raw.elapsedMs)) return null;
  if (raw.version === 1 ? !isIdentifier(raw.conversationId) : raw.conversationId !== undefined && !isIdentifier(raw.conversationId)) return null;
  if ((raw.requestId !== undefined && !isIdentifier(raw.requestId)) || (raw.turnId !== undefined && !isIdentifier(raw.turnId))) return null;
  const result: LocalTimingRecord = {
    version: raw.version, source: raw.source as LocalTimingSource,
    at: raw.at, stage: raw.stage as LocalTimingStage, event: raw.event as LocalTimingEvent, elapsedMs: raw.elapsedMs,
  };
  if (raw.conversationId !== undefined) result.conversationId = raw.conversationId as string;
  if (raw.background === true) result.background = true;
  if (raw.requestId !== undefined) result.requestId = raw.requestId as string;
  if (raw.turnId !== undefined) result.turnId = raw.turnId as string;
  if (isMs(raw.durationMs)) result.durationMs = raw.durationMs;
  if (isMs(raw.idleMs)) result.idleMs = raw.idleMs;
  const outcome = member(["completed", "failed", "cancelled"] as const, raw.outcome);
  if (outcome) result.outcome = outcome;
  const failure = member(["rate_limit", "authentication", "transport", "cancelled", "other"] as const, raw.failure);
  if (failure) result.failure = failure;
  if (reasons.has(String(raw.reason))) result.reason = raw.reason as LocalTimingReason;
  const provider = member(LOCAL_TIMING_DIMENSIONS.provider, raw.provider);
  if (provider) result.provider = provider;
  const origin = member(LOCAL_TIMING_DIMENSIONS.origin, raw.origin);
  if (origin) result.origin = origin;
  const clientKind = member(LOCAL_TIMING_DIMENSIONS.clientKind, raw.clientKind);
  if (clientKind) result.clientKind = clientKind;
  const connection = member(LOCAL_TIMING_DIMENSIONS.connection, raw.connection);
  if (connection) result.connection = connection;
  const prepareKind = member(LOCAL_TIMING_DIMENSIONS.prepareKind, raw.prepareKind);
  if (prepareKind) result.prepareKind = prepareKind;
  const turnKind = member(LOCAL_TIMING_DIMENSIONS.turnKind, raw.turnKind);
  if (turnKind) result.turnKind = turnKind;
  if (isCount(raw.turnIndex)) result.turnIndex = raw.turnIndex;
  if (isCount(raw.attempt)) result.attempt = raw.attempt;
  if (isIdentifier(raw.prepareId)) result.prepareId = raw.prepareId;
  if (isCount(raw.count)) result.count = raw.count;
  if (isCount(raw.bytes)) result.bytes = raw.bytes;
  if (isMs(raw.maxGapMs)) result.maxGapMs = raw.maxGapMs;
  if (isIdentifier(raw.peer)) result.peer = raw.peer;
  const role = member(LOCAL_TIMING_ROLES, raw.role);
  if (role) result.role = role;
  const addrType = member(LOCAL_TIMING_ADDR_TYPES, raw.addrType);
  if (addrType) result.addrType = addrType;
  if (raw.reused === true) result.reused = true;
  if (isIdentifier(raw.reporter)) result.reporter = raw.reporter;
  if (typeof raw.receivedAt === "string" && AT.test(raw.receivedAt) && Number.isFinite(Date.parse(raw.receivedAt))) result.receivedAt = raw.receivedAt;
  return result;
}

/** A failure's closed category; the error text itself never goes on a record. */
export function localTimingFailure(error: unknown): LocalTimingFailure {
  const message = error instanceof Error ? error.message : typeof error === "string" ? error : "";
  if (/429|rate.?limit|insufficient|quota|余额不足/i.test(message)) return "rate_limit";
  if (/401|403|unauthorized|authentication|not signed in/i.test(message)) return "authentication";
  if (/abort|cancel/i.test(message)) return "cancelled";
  if (/websocket|disconnect|ECONN|ETIMEDOUT|fetch failed|timeout|timed out/i.test(message)) return "transport";
  return "other";
}

/** A WebSocket close code as a closed reason; codes can come from the other side's text-bearing frame. */
export function localTimingCloseReason(code: number | undefined): LocalTimingReason {
  switch (code) {
    case 1000: return "ws_normal";
    case 1001: return "ws_going_away";
    case 1006: case undefined: return "ws_abnormal";
    default: return "ws_protocol";
  }
}

/** The request a command belongs to, so its link marks can join that turn's waterfall. */
export function localTimingRequestId(body: unknown): string | undefined {
  const value = body as { request_id?: unknown; item?: { requestId?: unknown } } | null | undefined;
  const id = value?.request_id ?? value?.item?.requestId;
  return isIdentifier(id) ? id : undefined;
}

/** Ties a proxied command to its turn. Older daemons ignore it. */
export const LOCAL_TIMING_REQUEST_HEADER = "X-Multica-Request-ID";

/** When a record arrived, for filtering and ordering: a report's own clock can be wrong. */
export function localTimingTime(record: LocalTimingRecord): string { return record.receivedAt ?? record.at; }

/** Whether a committed event carries model output, not the user message or lifecycle events every turn commits first. */
export function isLocalTimingModelOutput(eventType: string, kind: unknown): boolean {
  return eventType.startsWith("item.") && typeof kind === "string" && kind !== "" && kind !== "error" && kind !== "background_task";
}
