import { createHmac, randomBytes, randomUUID } from "node:crypto";
import { mkdir, readdir, rename, rm, stat, writeFile } from "node:fs/promises";
import { basename, dirname, join } from "node:path";
import { z } from "zod";
import type { LocalConversationScope } from "@multica/core/local-conversation/protocol";
import { localTimingTime, type LocalTimingRecord } from "@multica/tutti-agent-gui-host/local-timing";
import type { DeveloperLogQuery, DeveloperLogSaveResult } from "../shared/developer-logs";
import type { WorkspaceMcpTool } from "./mcp-tool";
import { developerLogTool } from "./developer-log-tool";
import { buildTurnWaterfalls, summarizeFirstOutput } from "./developer-log-waterfall";
import { snapshotDaemonTiming, snapshotLinkTiming, type DaemonTimingSource, type LinkTimingSource, type LocalTimingLog, type LocalTimingSnapshot } from "./local-timing-log";
import { hashPeerIds, type DaemonLogSource, type RuntimeLogSource } from "./developer-runtime-logs";

const querySchema = z.object({
  sinceMinutes: z.number().int().min(1).max(10080),
  conversationId: z.string().regex(/^[\w:.-]{1,160}$/).optional(),
}).strict();
const exportFilename = /^multica-developer-logs-[\dTZ-]+-[\da-f-]+\.json$/;
const retentionMs = 7 * 24 * 60 * 60 * 1000;

export interface RuntimeInfo {
  appVersion: string;
  platform: string;
  arch: string;
  electronVersion: string;
  chromeVersion: string;
  nodeVersion: string;
  packaged: boolean;
}
interface Options {
  log: LocalTimingLog;
  exportsRoot: string;
  runtime: RuntimeInfo;
  assertScope(scope: LocalConversationScope): void;
  conversations(scope: LocalConversationScope): Promise<string[]>;
  runtimeLogs(scope: LocalConversationScope, since: number, until: number): Promise<{ web: RuntimeLogSource; daemon: DaemonLogSource }>;
  /** The daemon's timing file on this device, null while no daemon profile is known. */
  daemonTimingPath?(): string | null;
  chooseSavePath(filename: string): Promise<string | null>;
}

function summarize(records: LocalTimingRecord[]) {
  const stages = new Map<string, { stage: string; background: boolean; records: number; waits: number; durations: number[] }>();
  for (const record of records) {
    const background = record.background === true;
    const key = `${record.stage}:${background}`;
    const stage = stages.get(key) ?? { stage: record.stage, background, records: 0, waits: 0, durations: [] };
    stage.records++;
    if (record.event === "wait") stage.waits++;
    if (record.event === "end" && record.durationMs !== undefined) stage.durations.push(record.durationMs);
    stages.set(key, stage);
  }
  return {
    recordCount: records.length,
    conversationCount: new Set(records.flatMap((record) => record.conversationId ? [record.conversationId] : [])).size,
    turnCount: new Set(records.flatMap((record) => record.turnId ? [record.turnId] : [])).size,
    failureCount: records.filter((record) => record.failure !== undefined).length,
    stages: [...stages.values()].map(({ durations, ...stage }) => {
      durations.sort((a, b) => a - b);
      return { ...stage, ...(durations.length ? {
        samples: durations.length,
        p50DurationMs: durations[Math.ceil(durations.length * 0.5) - 1]!,
        p90DurationMs: durations[Math.ceil(durations.length * 0.9) - 1]!,
        p95DurationMs: durations[Math.ceil(durations.length * 0.95) - 1]!,
        maxDurationMs: durations.at(-1)!,
      } : {}) };
    }),
  };
}

const DEVICE_TIMELINE_LIMIT = 2000;

/** Exports can be shared, so a device is only recognizable within one: each gets its own key, kept nowhere. */
function peerHasher() {
  const key = randomBytes(32);
  return (id: string) => createHmac("sha256", key).update(id).digest("hex").slice(0, 16);
}

function hashPeers(record: LocalTimingRecord, hash: (id: string) => string): LocalTimingRecord {
  if (!record.peer && !record.reporter) return record;
  const hashed = { ...record };
  if (record.peer) hashed.peer = hash(record.peer);
  if (record.reporter) hashed.reporter = hash(record.reporter);
  return hashed;
}

/**
 * Joins link records to the turns in this export. One that names a conversation,
 * request or turn here reads inside that turn's waterfall; one that names none is
 * the device's connection state and goes on its own timeline.
 */
function mergeLinkTiming(primary: LocalTimingRecord[], link: LinkTimingSource, selected: ReadonlySet<string> | null) {
  const requests = new Set(primary.flatMap((record) => record.requestId ? [record.requestId] : []));
  const turns = new Set(primary.flatMap((record) => record.turnId ? [record.turnId] : []));
  const belongs = (record: LocalTimingRecord) => record.conversationId !== undefined
    ? !selected || selected.has(record.conversationId)
    : !selected || (record.requestId !== undefined && requests.has(record.requestId)) || (record.turnId !== undefined && turns.has(record.turnId));
  const named = (record: LocalTimingRecord) => record.conversationId !== undefined || record.requestId !== undefined || record.turnId !== undefined;
  return {
    correlated: [...link.records, ...link.reports].filter((record) => named(record) && belongs(record)),
    timeline: [...link.records, ...link.reports].filter((record) => !named(record)),
  };
}

/** Shared by the desktop exporter and the standalone stdio MCP so both files have one format. */
export function buildDeveloperLogBundle(input: {
  runtime: RuntimeInfo; workspaceId: string | null; since: number; until: number; conversationId: string | null;
  timing: LocalTimingSnapshot; daemonTiming?: DaemonTimingSource; linkTiming?: LinkTimingSource; sources: { web: RuntimeLogSource; daemon: DaemonLogSource };
  timingScope?: "device";
  /** The conversations this export covers; absent for a device-wide one. */
  conversationIds?: ReadonlySet<string> | null;
}) {
  const daemonTiming = input.daemonTiming ?? { status: "unavailable" as const, records: [], incomplete: false, droppedWrites: 0 };
  const linkTiming = input.linkTiming ?? { status: "unavailable" as const, records: [], reports: [], incomplete: false, droppedWrites: 0 };
  const byTime = (a: LocalTimingRecord, b: LocalTimingRecord) => localTimingTime(a).localeCompare(localTimingTime(b));
  const primary = [...input.timing.records, ...daemonTiming.records];
  const { correlated, timeline } = mergeLinkTiming(primary, linkTiming, input.conversationIds ?? null);
  const hash = peerHasher();
  const records = [...primary, ...correlated].map((record) => hashPeers(record, hash)).sort(byTime);
  const deviceTimeline = [...timeline, ...(input.timing.deviceRecords ?? []), ...(daemonTiming.deviceRecords ?? [])].map((record) => hashPeers(record, hash)).sort(byTime);
  const incomplete = input.timing.incomplete || daemonTiming.incomplete || linkTiming.incomplete;
  const timingSummary = summarize(records);
  const waterfalls = buildTurnWaterfalls(records);
  const web = { ...input.sources.web, records: input.sources.web.records.map((record) => ({ ...record, message: hashPeerIds(record.message, hash), ...(record.source ? { source: hashPeerIds(record.source, hash) } : {}) })) };
  const daemon = { ...input.sources.daemon, text: hashPeerIds(input.sources.daemon.text, hash) };
  const webRecordCount = web.records.length;
  const daemonLineCount = daemon.text.split("\n").filter(Boolean).length;
  return {
    schemaVersion: 3,
    exportedAt: new Date(input.until).toISOString(),
    runtime: input.runtime,
    workspaceId: input.workspaceId,
    filter: { since: new Date(input.since).toISOString(), until: new Date(input.until).toISOString(), conversationId: input.conversationId, ...(input.timingScope ? { timingScope: input.timingScope } : {}) },
    coverage: { incomplete: incomplete || input.sources.web.incomplete || input.sources.daemon.incomplete, droppedTimingWrites: input.timing.droppedWrites + daemonTiming.droppedWrites + linkTiming.droppedWrites, retainedFiles: 2, maxFileBytes: 5 * 1024 * 1024, historicalLogsMayHaveRotated: true },
    summary: { ...timingSummary, timingRecordCount: timingSummary.recordCount, webRecordCount, daemonLineCount, daemonTimingRecordCount: daemonTiming.records.length, reportRecordCount: linkTiming.reports.length, deviceTimelineCount: deviceTimeline.length, recordCount: timingSummary.recordCount + webRecordCount + daemonLineCount, waterfallCount: waterfalls.turns.length, firstOutput: summarizeFirstOutput(waterfalls.turns) },
    records,
    waterfalls,
    deviceTimeline: deviceTimeline.slice(-DEVICE_TIMELINE_LIMIT),
    sources: { web: { ...web, scope: "account", timeFilterApplied: true }, daemon, daemonTiming: { status: daemonTiming.status, incomplete: daemonTiming.incomplete, droppedWrites: daemonTiming.droppedWrites }, linkTiming: { status: linkTiming.status, incomplete: linkTiming.incomplete, droppedWrites: linkTiming.droppedWrites } },
  };
}
type DeveloperLogBundle = ReturnType<typeof buildDeveloperLogBundle>;

function exportFileName() { return `multica-developer-logs-${new Date().toISOString().replaceAll(/[:.]/g, "-")}-${randomUUID()}.json`; }

/** Writes a bundle into the private exports folder and keeps it bounded. */
export async function writeDeveloperLogExport(exportsRoot: string, bundle: DeveloperLogBundle, check: () => void = () => undefined) {
  const path = join(exportsRoot, exportFileName());
  try {
    await mkdir(exportsRoot, { recursive: true, mode: 0o700 });
    check();
    await writeFile(path, JSON.stringify(bundle, null, 2) + "\n", { mode: 0o600, flag: "wx" });
    check();
  } catch (error) {
    await rm(path, { force: true }).catch(() => undefined);
    throw error;
  }
  await pruneDeveloperLogExports(exportsRoot).catch(() => undefined);
  return { path, format: "json", ...bundle.summary, incomplete: bundle.coverage.incomplete, filter: bundle.filter, sources: { web: { status: bundle.sources.web.status, truncated: bundle.sources.web.truncated }, daemon: { status: bundle.sources.daemon.status, truncated: bundle.sources.daemon.truncated, timeFilterApplied: false }, daemonTiming: { status: bundle.sources.daemonTiming.status }, linkTiming: { status: bundle.sources.linkTiming.status } } };
}

async function pruneDeveloperLogExports(exportsRoot: string) {
  const files = await Promise.all((await readdir(exportsRoot)).filter((name) => exportFilename.test(name)).map(async (name) => ({ path: join(exportsRoot, name), modified: (await stat(join(exportsRoot, name))).mtimeMs })));
  files.sort((a, b) => b.modified - a.modified);
  await Promise.all(files.filter((file, index) => index >= 20 || Date.now() - file.modified > retentionMs).map((file) => rm(file.path, { force: true })));
}

/** Both export entry points use the same bounded, account/workspace-scoped snapshot. */
export class DeveloperLogExporter {
  constructor(private options: Options) {}

  private async bundle(scope: LocalConversationScope, input: DeveloperLogQuery, signal: AbortSignal) {
    const query = querySchema.parse(input);
    signal.throwIfAborted(); this.options.assertScope(scope);
    const ids = await this.options.conversations(scope);
    this.options.assertScope(scope); signal.throwIfAborted();
    if (query.conversationId && !ids.includes(query.conversationId)) throw new Error("Conversation is unavailable in the current workspace");
    const until = Date.now();
    const since = until - query.sinceMinutes * 60_000;
    const selected = new Set(query.conversationId ? [query.conversationId] : ids);
    const daemonTimingPath = this.options.daemonTimingPath?.() ?? null;
    const [timing, daemonTiming, linkTiming] = await Promise.all([
      this.options.log.snapshot(selected, since, until),
      snapshotDaemonTiming(daemonTimingPath, selected, since, until),
      snapshotLinkTiming(daemonTimingPath, since, until),
    ]);
    this.options.assertScope(scope); signal.throwIfAborted();
    const sources = await this.options.runtimeLogs(scope, since, until);
    this.options.assertScope(scope); signal.throwIfAborted();
    return buildDeveloperLogBundle({ runtime: this.options.runtime, workspaceId: scope.workspaceId, since, until, conversationId: query.conversationId ?? null, timing, daemonTiming, linkTiming, conversationIds: selected, sources });
  }

  async save(scope: LocalConversationScope): Promise<DeveloperLogSaveResult> {
    const signal = new AbortController().signal;
    const bundle = await this.bundle(scope, { sinceMinutes: 1440 }, signal);
    const path = await this.options.chooseSavePath(exportFileName());
    this.options.assertScope(scope);
    if (!path) return { status: "cancelled" };
    const temporary = join(dirname(path), `.multica-logs-${randomUUID()}.tmp`);
    try {
      await writeFile(temporary, JSON.stringify(bundle, null, 2) + "\n", { mode: 0o600, flag: "wx" });
      this.options.assertScope(scope);
      await rename(temporary, path);
    } catch { throw new Error("Could not save developer logs"); }
    finally { await rm(temporary, { force: true }).catch(() => undefined); }
    return { status: "saved", filename: basename(path), recordCount: bundle.summary.recordCount };
  }

  async export(scope: LocalConversationScope, query: DeveloperLogQuery, signal: AbortSignal) {
    const bundle = await this.bundle(scope, query, signal);
    try {
      return await writeDeveloperLogExport(this.options.exportsRoot, bundle, () => { this.options.assertScope(scope); signal.throwIfAborted(); });
    } catch { throw new Error(signal.aborted ? "Developer log export cancelled" : "Could not export developer logs"); }
  }
}

export function createDeveloperLogTool(exporter: DeveloperLogExporter, scope: LocalConversationScope, currentConversationId?: string): WorkspaceMcpTool {
  return developerLogTool((query, signal) => exporter.export(scope, query, signal), currentConversationId);
}
