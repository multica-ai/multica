import { z } from "zod";
import { parseLocalTiming, type LocalTimingRecord } from "@multica/core/local-conversation/timing";
import { createRequestId } from "@/lib/request-id";
import { readResponse } from "./schemas";

const BUFFER_SIZE = 2000;
const BATCH_SIZE = 200;
const DRAIN_PAGES = 3;

/** native is the position in Go's ring just after a record that came from there. */
interface Entry { record: LocalTimingRecord; workspaceId?: string; native?: number }
export interface TimingScope { deviceId: string; workspaceId: string }
export interface TimingBatch { batch_id: string; records: LocalTimingRecord[] }

/** What mobilelink's loopback route returns. Records are read one by one so a bad one costs only itself. */
export const diagnosticsSchema = z.object({ records: z.array(z.unknown()).default([]), next: z.number().int().nonnegative(), dropped: z.number().int().nonnegative().default(0) });
/** The executor's answer. Nothing but a success clears a batch, so missing fields read as not stored. */
export const reportAckSchema = z.object({ accepted: z.number().int().nonnegative(), rejected: z.number().int().nonnegative().default(0), duplicate: z.boolean().default(false) });

/**
 * The phone's bounded record of its own marks until an executor acknowledges them.
 * It lives in JS memory, which iOS keeps while the app sleeps, so it survives
 * backgrounding and link restarts; a process kill loses what was not yet reported.
 * Go's marks survive that too, through mobilelink's own saved ring, which holds them
 * until settle says the executor has them: this cursor starts over with the process.
 */
export class MobileTimingBuffer {
  private entries: Entry[] = [];
  private dropped = 0;
  private reportedDropped = 0;
  private cursor = 0;
  private nativeDropped = 0;
  private settled = 0;
  private userId?: string;
  private pending?: { batch: TimingBatch; entries: Set<Entry>; deviceId: string; workspaceId: string; dropped: number };
  /** Keeps a copy of the marks made here, since this buffer forgets each once an executor has it. Go keeps its own. */
  retain?: (record: LocalTimingRecord) => void;

  constructor(private size = BUFFER_SIZE) {}

  /** A buffer holds one account's marks; another account must not report them. */
  forUser(userId: string) {
    if (this.userId !== undefined && this.userId !== userId) {
      this.entries = []; this.dropped = 0; this.reportedDropped = 0; this.cursor = 0; this.nativeDropped = 0; this.settled = 0; this.pending = undefined;
    }
    this.userId = userId;
  }

  write(value: Partial<LocalTimingRecord> & Pick<LocalTimingRecord, "stage">, workspaceId?: string, native?: number) {
    try {
      const record = parseLocalTiming({ version: 2, source: "mobile", clientKind: "mobile", origin: "p2p", at: new Date().toISOString(), event: "mark", elapsedMs: 0, ...value });
      if (!record) return;
      if (native === undefined) this.retain?.(record);
      this.entries.push({ record, ...(record.conversationId && workspaceId ? { workspaceId } : {}), ...(native === undefined ? {} : { native }) });
      if (this.entries.length > this.size) { this.entries.shift(); this.dropped++; }
    } catch { /* Diagnostics must not change delivery. */ }
  }

  /** Reads what the Go side recorded since the last read. */
  async drain(endpoint: { url: string; token: string }, fetcher: typeof fetch) {
    for (let page = 0; page < DRAIN_PAGES; page++) {
      const response = await fetcher(`${endpoint.url}/diagnostics?after=${this.cursor}`, { headers: { Authorization: `Bearer ${endpoint.token}` } });
      // An older embedded link has no such route.
      if (!response.ok) return;
      const body = readResponse(await response.json(), diagnosticsSchema, "mobile.diagnostics");
      const first = body.next - body.records.length;
      body.records.forEach((raw, index) => {
        const record = parseLocalTiming(raw);
        if (record) this.write(record, undefined, first + index + 1); else this.dropped++;
      });
      this.dropped += Math.max(0, body.dropped - this.nativeDropped);
      this.nativeDropped = body.dropped;
      const finished = body.records.length === 0 || body.next === this.cursor;
      this.cursor = body.next;
      if (finished) return;
    }
  }

  /** Tells Go how far its marks have reached an executor, stopping at the oldest still waiting here. An older link has no such route. */
  async settle(endpoint: { url: string; token: string }, fetcher: typeof fetch) {
    const next = this.entries.reduce((lowest, entry) => entry.native === undefined ? lowest : Math.min(lowest, entry.native - 1), this.cursor);
    const dropped = this.reportedDropped >= this.dropped ? this.nativeDropped : 0;
    if (next <= this.settled && !dropped) return;
    const response = await fetcher(`${endpoint.url}/diagnostics/ack`, { method: "POST", headers: { Authorization: `Bearer ${endpoint.token}`, "Content-Type": "application/json" }, body: JSON.stringify({ next, dropped }) });
    if (!response.ok) return;
    this.settled = Math.max(this.settled, next);
    this.nativeDropped -= dropped;
  }

  /** The next batch for this executor. A batch not yet acknowledged keeps its id, so a retry after a lost answer stores nothing twice. */
  batch(scope: TimingScope): TimingBatch | null {
    if (this.pending?.deviceId === scope.deviceId && this.pending.workspaceId === scope.workspaceId) return this.pending.batch;
    const entries = this.entries.filter(({ record, workspaceId }) => (!record.peer || record.peer === scope.deviceId) && (!workspaceId || workspaceId === scope.workspaceId)).slice(0, BATCH_SIZE);
    const dropped = this.dropped - this.reportedDropped;
    const records = entries.map((entry) => entry.record);
    if (dropped > 0) {
      const gap = parseLocalTiming({ version: 2, source: "mobile", at: new Date().toISOString(), stage: "timing.dropped", event: "mark", elapsedMs: 0, count: dropped });
      if (gap) records.push(gap);
    }
    if (!records.length) return null;
    const batch = { batch_id: createRequestId(), records };
    this.pending = { batch, entries: new Set(entries), deviceId: scope.deviceId, workspaceId: scope.workspaceId, dropped };
    return batch;
  }

  acknowledge(batchId: string) {
    if (this.pending?.batch.batch_id !== batchId) return;
    const { entries, dropped } = this.pending;
    this.entries = this.entries.filter((entry) => !entries.has(entry));
    this.reportedDropped += dropped;
    this.pending = undefined;
  }

  get count() { return this.entries.length; }
}

export const mobileTiming = new MobileTimingBuffer();
