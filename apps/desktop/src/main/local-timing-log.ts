import { mkdir, appendFile, rename, stat, open } from "node:fs/promises";
import { dirname, join } from "node:path";
import { LOCAL_TIMING_PREFIX, localTimingTime, parseLocalTiming, type LocalTimingRecord } from "@multica/tutti-agent-gui-host/local-timing";

const DEFAULT_MAX_BYTES = 5 * 1024 * 1024;

export interface LocalTimingSnapshot {
  records: LocalTimingRecord[];
  incomplete: boolean;
  /** Writes known to be lost in this range; they make the snapshot incomplete. */
  droppedWrites: number;
  /** A scoped read's records of no conversation, request or turn: the device's connection state. */
  deviceRecords?: LocalTimingRecord[];
}

/** A read of one rotated log; `present` tells a missing log from an empty one. */
async function readTimingFiles(path: string, maxBytes: number, conversationIds: ReadonlySet<string> | null, since: number, until: number): Promise<LocalTimingSnapshot & { present: boolean }> {
  const records: LocalTimingRecord[] = [];
  const deviceRecords: LocalTimingRecord[] = [];
  let incomplete = false;
  let droppedWrites = 0;
  let present = false;
  for (const candidate of [path + ".1", path]) {
    try {
      const file = await open(candidate, "r");
      try {
        present = true;
        if ((await file.stat()).size > Math.max(maxBytes, 2048)) { incomplete = true; continue; }
        const contents = await file.readFile("utf8");
        for (const line of contents.split("\n")) {
          if (!line.trim()) continue;
          try {
            const record = parseLocalTiming(JSON.parse(line));
            if (!record) { incomplete = true; continue; }
            // A report is filtered on when it arrived: the sender's clock can be wrong.
            const at = Date.parse(localTimingTime(record));
            if (at < since || at > until) continue;
            if (record.stage === "timing.dropped") { droppedWrites += record.count ?? 0; incomplete = true; continue; }
            // Device-level records belong to no conversation, so only a device-wide read includes them.
            if (!conversationIds || (record.conversationId !== undefined && conversationIds.has(record.conversationId))) records.push(record);
            else if (record.conversationId === undefined && record.requestId === undefined && record.turnId === undefined) deviceRecords.push(record);
          } catch { incomplete = true; }
        }
      } finally { await file.close(); }
    } catch (error) {
      if (!(error && typeof error === "object" && "code" in error && error.code === "ENOENT")) incomplete = true;
    }
  }
  const byTime = (a: LocalTimingRecord, b: LocalTimingRecord) => localTimingTime(a).localeCompare(localTimingTime(b));
  records.sort(byTime);
  return { records, incomplete, droppedWrites, present, ...(deviceRecords.length ? { deviceRecords: deviceRecords.sort(byTime) } : {}) };
}

export interface DaemonTimingSource extends LocalTimingSnapshot {
  status: "available" | "empty" | "missing" | "unavailable" | "unreadable";
}

/** Reads the daemon's own timing file; its rotation is not coordinated with this reader. */
export async function snapshotDaemonTiming(path: string | null, conversationIds: ReadonlySet<string> | null, since: number, until: number): Promise<DaemonTimingSource> {
  if (!path) return { status: "unavailable", records: [], incomplete: false, droppedWrites: 0 };
  const { present, ...snapshot } = await readTimingFiles(path, DEFAULT_MAX_BYTES, conversationIds, since, until);
  const status = snapshot.records.length ? "available" : present ? (snapshot.incomplete ? "unreadable" : "empty") : (snapshot.incomplete ? "unreadable" : "missing");
  return { status, ...snapshot };
}

export interface LinkTimingSource extends DaemonTimingSource {
  /** What other devices reported about their side of this device's conversations. */
  reports: LocalTimingRecord[];
}

/** Reads the files the daemon keeps next to its turn records. A later step ties their records to a turn. */
export async function snapshotLinkTiming(daemonTimingPath: string | null, since: number, until: number): Promise<LinkTimingSource> {
  if (!daemonTimingPath) return { status: "unavailable", records: [], reports: [], incomplete: false, droppedWrites: 0 };
  const directory = dirname(daemonTimingPath);
  const [device, reports] = await Promise.all([
    readTimingFiles(join(directory, "device-timing.jsonl"), DEFAULT_MAX_BYTES, null, since, until),
    readTimingFiles(join(directory, "timing-reports.jsonl"), DEFAULT_MAX_BYTES, null, since, until),
  ]);
  const incomplete = device.incomplete || reports.incomplete;
  const present = device.present || reports.present;
  const found = device.records.length + reports.records.length > 0;
  return { status: found ? "available" : present ? (incomplete ? "unreadable" : "empty") : (incomplete ? "unreadable" : "missing"), records: device.records, reports: reports.records, incomplete, droppedWrites: device.droppedWrites + reports.droppedWrites };
}

/** A bounded, best-effort diagnostic log, separate from saved conversation content. */
export class LocalTimingLog {
  private pending = Promise.resolve();
  private queued = 0;
  private size?: number;
  private dropped = 0;
  readonly path: string;
  constructor(private directory: string, private maxBytes = DEFAULT_MAX_BYTES) {
    this.path = join(directory, "turn-timing.jsonl");
  }
  private async append(line: string): Promise<void> {
    if (this.size === undefined) {
      await mkdir(this.directory, { recursive: true, mode: 0o700 });
      this.size = await stat(this.path).then((value) => value.size, () => 0);
    }
    if (this.size > 0 && this.size + Buffer.byteLength(line) > this.maxBytes) {
      await rename(this.path, this.path + ".1");
      this.size = 0;
    }
    await appendFile(this.path, line, { mode: 0o600 });
    this.size += Buffer.byteLength(line);
  }
  /** Persists the gap, because the standalone MCP reads only this file and not this process. */
  private async reportDropped(): Promise<void> {
    const count = this.dropped;
    if (!count) return;
    this.dropped = 0;
    const record = parseLocalTiming({ version: 2, source: "main", at: new Date().toISOString(), stage: "timing.dropped", event: "mark", elapsedMs: 0, count });
    try { await this.append(JSON.stringify(record) + "\n"); }
    catch { this.dropped += count; this.size = undefined; }
  }
  write = (value: unknown): void => {
    const record = parseLocalTiming(value);
    if (!record) return;
    if (this.queued >= 1000) { this.dropped++; return; }
    const line = JSON.stringify(record) + "\n";
    this.queued++;
    this.pending = this.pending.then(async () => {
      await this.append(line);
      await this.reportDropped();
    }).catch(() => { this.dropped++; this.size = undefined; }).finally(() => { this.queued--; });
  };
  console(message: string): void {
    if (!message.startsWith(LOCAL_TIMING_PREFIX) || message.length > 2048) return;
    try {
      const record = parseLocalTiming(JSON.parse(message.slice(LOCAL_TIMING_PREFIX.length)));
      if (record && record.source !== "main") this.write(record);
    } catch { /* Other console output is never persisted here. */ }
  }
  /** `null` reads every conversation, for the device-wide standalone MCP. */
  snapshot(conversationIds: ReadonlySet<string> | null, since: number, until: number): Promise<LocalTimingSnapshot> {
    // Join the write queue so rotation cannot split a snapshot across two generations.
    const snapshot = this.pending.then(async () => {
      const { present: _present, ...read } = await readTimingFiles(this.path, this.maxBytes, conversationIds, since, until);
      return { ...read, incomplete: read.incomplete || this.dropped > 0, droppedWrites: read.droppedWrites + this.dropped };
    });
    this.pending = snapshot.then(() => undefined, () => undefined);
    return snapshot;
  }
  flush(): Promise<void> { return this.pending; }
}
