import { canonicalLocalTimingStage, type LocalTimingRecord, type LocalTimingStage } from "@multica/tutti-agent-gui-host/local-timing";

const MAX_TURNS = 200;
const FIRST_OUTPUT: readonly LocalTimingStage[] = ["provider.first_reasoning", "provider.first_thinking", "provider.first_text", "provider.first_tool"];

export interface WaterfallSegment {
  stage: LocalTimingStage;
  /** The name written, when an old log used a stage that has since been renamed. */
  recordedAs?: LocalTimingStage;
  source: LocalTimingRecord["source"];
  at: string;
  /** Where the stage began and ended relative to the turn's first record. Offsets across devices include clock skew. */
  startMs: number;
  endMs: number;
  durationMs?: number;
  unfinished?: true;
  outcome?: LocalTimingRecord["outcome"];
  failure?: LocalTimingRecord["failure"];
  reason?: LocalTimingRecord["reason"];
  clientKind?: LocalTimingRecord["clientKind"];
}

export interface TurnWaterfall {
  turnId?: string;
  requestId?: string;
  conversationId?: string;
  provider?: LocalTimingRecord["provider"];
  origin?: LocalTimingRecord["origin"];
  clientKind?: LocalTimingRecord["clientKind"];
  connection?: LocalTimingRecord["connection"];
  prepareKind?: LocalTimingRecord["prepareKind"];
  turnKind?: LocalTimingRecord["turnKind"];
  turnIndex?: number;
  attempt?: number;
  background: boolean;
  outcome?: LocalTimingRecord["outcome"];
  startedAt: string;
  /** Each total uses one device's clock, so it holds across devices. */
  totals: { clientToFirstEventMs?: number; daemonToFirstOutputMs?: number; transportMs?: number };
  segments: WaterfallSegment[];
}

function groupKeys(records: LocalTimingRecord[]) {
  const turnOfRequest = new Map<string, string>();
  for (const record of records) if (record.requestId && record.turnId && !turnOfRequest.has(record.requestId)) turnOfRequest.set(record.requestId, record.turnId);
  return (record: LocalTimingRecord) => record.turnId ?? (record.requestId ? turnOfRequest.get(record.requestId) ?? `request:${record.requestId}` : undefined);
}

function earliest(records: LocalTimingRecord[], stages: readonly LocalTimingStage[], matches: (record: LocalTimingRecord) => boolean = () => true) {
  let found: number | undefined;
  for (const record of records) {
    if (!stages.includes(canonicalLocalTimingStage(record.stage)) || !matches(record)) continue;
    const at = Date.parse(record.at);
    if (found === undefined || at < found) found = at;
  }
  return found;
}

function totals(records: LocalTimingRecord[]): TurnWaterfall["totals"] {
  // Several clients can watch one turn; the total is about the one that sent it.
  const sender = records.find((record) => canonicalLocalTimingStage(record.stage) === "client.send");
  const send = earliest(records, ["client.send"]);
  const received = earliest(records, ["client.first_event_received"], (record) => !sender || record.source === sender.source);
  const requested = earliest(records, ["daemon.request_received"]);
  const senderKind = sender?.clientKind;
  const written = earliest(records, ["ws.first_output_write"], (record) => !senderKind || record.clientKind === senderKind) ?? earliest(records, ["ws.first_output_write"]);
  const output = earliest(records, FIRST_OUTPUT);
  const result: TurnWaterfall["totals"] = {};
  if (send !== undefined && received !== undefined) result.clientToFirstEventMs = received - send;
  if (requested !== undefined && output !== undefined) result.daemonToFirstOutputMs = output - requested;
  // Each difference stays on one clock, which is why no clock sync is needed.
  if (send !== undefined && received !== undefined && requested !== undefined && written !== undefined) {
    const transport = (received - send) - (written - requested);
    if (transport >= 0) result.transportMs = transport;
  }
  return result;
}

function segments(records: LocalTimingRecord[], first: number): WaterfallSegment[] {
  const ended = new Set(records.filter((record) => record.event === "end").map((record) => `${record.source}:${record.stage}`));
  const out: WaterfallSegment[] = [];
  for (const record of records) {
    if (record.event === "wait" || (record.event === "start" && ended.has(`${record.source}:${record.stage}`))) continue;
    const stage = canonicalLocalTimingStage(record.stage);
    const end = Date.parse(record.at) - first;
    // A report on a wrong clock can land before the turn's first local record.
    const start = end - (record.durationMs ?? 0);
    const segment: WaterfallSegment = { stage, source: record.source, at: record.at, startMs: record.receivedAt ? start : Math.max(0, start), endMs: end };
    if (stage !== record.stage) segment.recordedAs = record.stage;
    if (record.durationMs !== undefined) segment.durationMs = record.durationMs;
    if (record.event === "start") segment.unfinished = true;
    if (record.outcome) segment.outcome = record.outcome;
    if (record.failure) segment.failure = record.failure;
    if (record.reason) segment.reason = record.reason;
    if (stage === "ws.first_output_write" && record.clientKind) segment.clientKind = record.clientKind;
    out.push(segment);
  }
  return out.sort((a, b) => a.startMs - b.startMs || a.endMs - b.endMs);
}

function waterfall(records: LocalTimingRecord[]): TurnWaterfall {
  // A report's clock is not this device's, so it only anchors a turn with nothing else.
  const local = records.filter((record) => !record.receivedAt);
  const first = Math.min(...(local.length ? local : records).map((record) => Date.parse(record.at)));
  const result: TurnWaterfall = { background: records.some((record) => record.background === true), startedAt: new Date(first).toISOString(), totals: totals(records), segments: segments(records, first) };
  // A delivery record names whoever it reached, not who started the turn.
  const identity = records.filter((record) => canonicalLocalTimingStage(record.stage) !== "ws.first_output_write");
  for (const record of identity) {
    result.turnId ??= record.turnId;
    result.requestId ??= record.requestId;
    result.conversationId ??= record.conversationId;
    result.provider ??= record.provider;
    result.origin ??= record.origin;
    result.clientKind ??= record.clientKind;
    result.connection ??= record.connection;
    result.prepareKind ??= record.prepareKind;
    result.turnKind ??= record.turnKind;
    result.turnIndex ??= record.turnIndex;
    result.attempt ??= record.attempt;
    if (canonicalLocalTimingStage(record.stage) === "turn.finished") result.outcome ??= record.outcome;
  }
  for (const key of Object.keys(result) as (keyof TurnWaterfall)[]) if (result[key] === undefined) delete result[key];
  return result;
}

/** One entry per turn, so a single slow message reads end to end across the initiating and executing sides. */
export function buildTurnWaterfalls(records: LocalTimingRecord[]): { turns: TurnWaterfall[]; truncated: boolean } {
  const keyOf = groupKeys(records);
  const groups = new Map<string, LocalTimingRecord[]>();
  for (const record of records) {
    const key = keyOf(record);
    if (key === undefined) continue;
    const group = groups.get(key);
    if (group) group.push(record); else groups.set(key, [record]);
  }
  const turns = [...groups.values()].map(waterfall).sort((a, b) => b.startedAt.localeCompare(a.startedAt));
  return { turns: turns.slice(0, MAX_TURNS), truncated: turns.length > MAX_TURNS };
}

const percentile = (sorted: number[], ratio: number) => sorted[Math.ceil(sorted.length * ratio) - 1]!;

/** First-output latency by path. Background and native turns are not the user waiting, so they stay out. */
export function summarizeFirstOutput(turns: TurnWaterfall[]) {
  const groups = new Map<string, { provider?: string; prepareKind?: string; origin?: string; clientKind?: string; samples: number[] }>();
  for (const turn of turns) {
    const sample = turn.totals.daemonToFirstOutputMs;
    if (sample === undefined || turn.background || turn.turnKind === "background" || turn.turnKind === "native") continue;
    const key = JSON.stringify([turn.provider, turn.prepareKind, turn.origin, turn.clientKind]);
    const group = groups.get(key) ?? { provider: turn.provider, prepareKind: turn.prepareKind, origin: turn.origin, clientKind: turn.clientKind, samples: [] };
    group.samples.push(sample);
    groups.set(key, group);
  }
  return [...groups.values()].map(({ samples, ...group }) => {
    samples.sort((a, b) => a - b);
    return { ...group, samples: samples.length, p50Ms: percentile(samples, 0.5), p90Ms: percentile(samples, 0.9), maxMs: samples.at(-1)! };
  });
}
