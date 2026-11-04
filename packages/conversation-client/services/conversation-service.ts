import type { z } from "zod";
import { parseWithFallback } from "@multica/core/api/schema";
import {
  LOCAL_V2_VERSION, localV2HistorySchema, localV2QueueSchema, localV2ServerMessageSchema, localV2SessionListSchema, localV2SnapshotSchema, localV2UploadSchema,
  type LocalV2Device, type LocalV2Event, type LocalV2Scope, type LocalV2Snapshot, type LocalV2Summary, type LocalV2Transport,
} from "@multica/core/local-conversation/v2";
import { LOCAL_V2_FOCUS_CAPABILITY, LocalV2FocusTracker } from "@multica/core/local-conversation/focus";
import { ConversationModel } from "../models";

/** Conversations of other devices as last read, shown while the current state travels. */
export interface ConversationSnapshotStore {
  read(key: string): Promise<LocalV2Snapshot | null>;
  write(key: string, snapshot: LocalV2Snapshot): Promise<void>;
}

/** A failed v2 request with the daemon's stable error kind. */
export class LocalV2RequestError extends Error {
  constructor(readonly kind: string, message: string) { super(message); }
}

/** IPC carries the daemon's error kind as a "kind: message" prefix. */
export function localV2Error(error: unknown): LocalV2RequestError {
  const text = error instanceof Error ? error.message.replace(/^Error invoking remote method '[^']+': (?:Error: )?/, "") : String(error);
  const match = /^([a-z][a-z0-9_]*): ([\s\S]*)$/.exec(text);
  return match ? new LocalV2RequestError(match[1]!, match[2]!) : new LocalV2RequestError("unknown", text);
}

const SUMMARY_PAGES = 100;
/** Turns read at once: a first screen and each page while the view fills its prefetch. */
const PAGE_TURNS = 10;
const LIST_RETRY_MIN_MS = 1_000;
const LIST_RETRY_MAX_MS = 30_000;
interface ContentEntry {
  /** Replaced when the entry is dropped, so a late answer cannot land in a new one. */
  epoch: number;
  conversation?: ConversationModel;
  /** Views holding the conversation live. None left means it is being left or was left. */
  users: number;
  observers: Set<(events: readonly LocalV2Event[]) => void>;
  /** Changes whenever the conversation does: the model updates in place. */
  version: number;
  error: Error | null;
  loading?: Promise<ConversationModel>;
  older?: Promise<void>;
  /** A subscription exists on the daemon. */
  subscribed: boolean;
  /** The current subscription has answered; earlier events belong to a cancelled one. */
  confirmed: boolean;
  /** What the subscription was told to catch up to; leaving finishes this much. */
  highWater: number;
  queueRead?: Promise<void>;
  retries: number;
}

export interface ConversationServiceOptions {
  /** Told each store generation the daemon reports. */
  onGeneration(generation: string): void;
  /** Told a conversation's content is in use, to retain it. */
  onUsed(service: ConversationService, id: string): void;
  snapshots: ConversationSnapshotStore | null;
  deviceId: string;
}

/**
 * One workspace's conversations on one device. Nothing here depends on a view
 * existing: views ask for work and watch for changes, and work they started
 * finishes after they leave.
 *
 * The list is complete, stays current through the scope subscription, and is
 * never evicted. Content is kept per conversation. A view holds a conversation
 * live with open(); returning to one kept here costs only the events since,
 * not a snapshot.
 */
export class ConversationService {
  private summaryMap = new Map<string, LocalV2Summary>();
  /** The list as last read; replaced on each change so React sees a new value. */
  private summaryList: LocalV2Summary[] = [];
  private summaryLoad?: Promise<void>;
  private summariesRead = false;
  private scopeSeq = 0;
  private listRetry = LIST_RETRY_MIN_MS;
  private listTimer?: ReturnType<typeof setTimeout>;
  private summaryListeners = new Set<() => void>();
  private entries = new Map<string, ContentEntry>();
  private nextEpoch = 1;
  private disposed = false;
  private presenceMap = new Map<string, LocalV2Device[]>();
  private presenceListeners = new Set<() => void>();
  private focused = new LocalV2FocusTracker(async (id, focused) => {
    try {
      const capabilities = await this.transport.capabilities?.();
      // A daemon that cannot use it has nothing to catch up on either.
      if (capabilities?.includes(LOCAL_V2_FOCUS_CAPABILITY)) await this.transport.send(this.scope, { version: LOCAL_V2_VERSION, type: "conversation.focus", conversation_id: id, focused });
      return true;
    } catch { return false; }
  });

  constructor(private transport: LocalV2Transport, readonly scope: LocalV2Scope, private options: ConversationServiceOptions) {}

  /** Someone is looking at a conversation until the returned function is called. Only views call this: it keeps the provider's process warm. */
  focus(id: string): () => void { return this.focused.hold(id); }

  async request(method: "GET" | "POST" | "DELETE", path: string, body?: unknown): Promise<unknown> {
    try { return await this.transport.request(this.scope, method, path, body); }
    catch (error) { throw localV2Error(error); }
  }

  /** A conversation command: send, interrupt, update, queue_update... */
  command(id: string, body: Record<string, unknown>): Promise<unknown> {
    return this.request("POST", `/sessions/${encodeURIComponent(id)}/commands`, body);
  }

  /** Reads a snapshot without touching what the store keeps; callers needing live content use ensure. */
  async snapshot(id: string, turns?: number): Promise<LocalV2Snapshot> {
    const raw = await this.request("GET", `/sessions/${encodeURIComponent(id)}/snapshot${turns ? `?turn_limit=${turns}` : ""}`);
    const snapshot = parseWithFallback<LocalV2Snapshot | null>(raw, localV2SnapshotSchema, null, { endpoint: "local-v2.snapshot" });
    if (!snapshot) throw new LocalV2RequestError("invalid_response", "Invalid local conversation response. Reload and retry.");
    this.noteGeneration(snapshot.store_generation);
    this.receiveSummary(snapshot.conversation);
    return snapshot;
  }

  private noteGeneration(generation: string) {
    if (generation) this.options.onGeneration(generation);
  }

  fileUrl(path: string): string { return this.transport.fileUrl(this.scope, path); }
  async upload(file: File, options?: { id?: string; signal?: AbortSignal }) {
    try { return localV2UploadSchema.parse(await this.transport.upload(this.scope, file, options)); }
    catch (error) { throw localV2Error(error); }
  }
  /** Attaches content the daemon already stores; kind not_found means upload it. */
  async linkUpload(body: { id: string; sha256: string; name: string; mime_type: string }) {
    return localV2UploadSchema.parse(await this.request("POST", "/attachments/link", body));
  }
  async discardUpload(id: string) { await this.request("DELETE", `/attachments/${encodeURIComponent(id)}`); }

  // ---- List ----

  /** Whether the list has been read; after that it stays current on its own. */
  hasSummaries(): boolean { return this.summariesRead; }

  /** Every conversation of the scope, loaded once and kept current. */
  summaries(): LocalV2Summary[] { return this.summaryList; }
  summary(id: string): LocalV2Summary | undefined { return this.summaryMap.get(id); }

  loadSummaries(): Promise<void> {
    return this.summaryLoad ??= (async () => {
      // A reload after a reconnect or a fallen-behind stream asks only for what changed.
      let after = this.scopeSeq;
      for (let page = 0; page < SUMMARY_PAGES; page++) {
        const raw = await this.request("GET", `/sessions?after_scope_seq=${after}`);
        const list = parseWithFallback<z.infer<typeof localV2SessionListSchema>>(raw, localV2SessionListSchema, { scope_seq: after, summaries: [], has_more: false }, { endpoint: "local-v2.sessions" });
        for (const summary of list.summaries) this.store(summary);
        after = Math.max(after, list.scope_seq, ...list.summaries.map((summary) => summary.summary_seq));
        if (!list.has_more) break;
      }
      this.scopeSeq = after;
      this.summariesRead = true;
      this.listRetry = LIST_RETRY_MIN_MS;
      this.notifySummaries();
      // The list is usable now; only its updates need the socket.
      void this.transport.send(this.scope, { version: LOCAL_V2_VERSION, type: "subscribe_scope", after_scope_seq: after }).catch(() => { this.summaryLoad = undefined; });
    })().catch((error: unknown) => { this.summaryLoad = undefined; throw error; });
  }

  onSummaries(listener: () => void): () => void {
    this.summaryListeners.add(listener);
    return () => { this.summaryListeners.delete(listener); };
  }

  /** Records a conversation the caller just created, changed or read. */
  receiveSummary(summary: LocalV2Summary): void {
    if (this.store(summary)) this.notifySummaries();
  }

  private store(summary: LocalV2Summary): boolean {
    const current = this.summaryMap.get(summary.conversation_id);
    if (current && current.summary_seq >= summary.summary_seq) return false;
    this.summaryMap.set(summary.conversation_id, summary);
    this.summaryList = [...this.summaryMap.values()];
    const entry = this.entries.get(summary.conversation_id);
    entry?.conversation?.setSummary(summary);
    return true;
  }

  private notifySummaries() { for (const listener of this.summaryListeners) listener(); }

  /** Reads the list again until it works; nothing else would, with no view open. */
  private reloadSummaries() {
    if (this.disposed) return;
    this.summaryLoad = undefined;
    clearTimeout(this.listTimer);
    this.loadSummaries().catch(() => {
      if (this.disposed) return;
      this.listTimer = setTimeout(() => this.reloadSummaries(), this.listRetry);
      this.listRetry = Math.min(this.listRetry * 2, LIST_RETRY_MAX_MS);
    });
  }

  // ---- Content ----

  private entry(id: string): ContentEntry {
    let entry = this.entries.get(id);
    if (!entry) {
      entry = { epoch: this.nextEpoch++, users: 0, observers: new Set(), version: 0, error: null, subscribed: false, confirmed: false, highWater: 0, retries: 0 };
      this.entries.set(id, entry);
    }
    return entry;
  }

  conversation(id: string): ConversationModel | undefined { return this.entries.get(id)?.conversation; }
  version(id: string): number { return this.entries.get(id)?.version ?? 0; }
  status(id: string): { error: Error | null; loading: boolean } {
    const entry = this.entries.get(id);
    return { error: entry?.error ?? null, loading: !!entry?.loading };
  }

  /** Whether views hold this conversation or work on it is unfinished, so it cannot be dropped. */
  inUse(id: string): boolean {
    const entry = this.entries.get(id);
    return !!entry && (entry.users > 0 || entry.subscribed || !!entry.loading || !!entry.older || entry.observers.size > 0);
  }

  /** Forgets a conversation's content; the list keeps it. */
  evict(id: string): void { this.entries.delete(id); }

  observe(id: string, listener: (events: readonly LocalV2Event[]) => void): () => void {
    const entry = this.entry(id);
    entry.observers.add(listener);
    return () => { entry.observers.delete(listener); };
  }

  private changed(id: string, entry: ContentEntry, events: readonly LocalV2Event[] = []) {
    entry.version++;
    this.options.onUsed(this, id);
    for (const observer of [...entry.observers]) observer(events);
  }

  /**
   * Holds a conversation live: the first turns if it has none, then every event.
   * The returned function is the leave event: the work under way finishes and is
   * kept, and nothing new starts.
   */
  open(id: string): () => void {
    const entry = this.entry(id);
    entry.users++;
    entry.error = null;
    if (entry.users === 1) this.sync(id, entry);
    let open = true;
    return () => {
      if (!open) return;
      open = false;
      if (--entry.users === 0) this.leave(id, entry);
    };
  }

  private sync(id: string, entry: ContentEntry) {
    if (entry.conversation) { this.subscribe(id, entry); return; }
    void this.ensure(id).catch(() => {});
  }

  private leave(id: string, entry: ContentEntry) {
    // Still catching up: the rest of this batch arrives, and receive() ends it.
    if (entry.subscribed && entry.conversation && entry.confirmed && entry.conversation.seq < entry.highWater) return;
    if (entry.subscribed && !entry.confirmed) return;
    this.unsubscribe(id, entry);
  }

  private unsubscribe(id: string, entry: ContentEntry) {
    if (!entry.subscribed) return;
    entry.subscribed = false; entry.confirmed = false;
    void this.transport.send(this.scope, { version: LOCAL_V2_VERSION, type: "unsubscribe", conversation_id: id }).catch(() => {});
  }

  private subscribe(id: string, entry: ContentEntry) {
    const conversation = entry.conversation;
    if (!conversation || this.disposed) return;
    entry.subscribed = true; entry.confirmed = false; entry.highWater = conversation.seq;
    this.transport.send(this.scope, { version: LOCAL_V2_VERSION, type: "subscribe", conversation_id: id, after_seq: conversation.seq, store_generation: conversation.storeGeneration })
      // Offline: the reconnect subscribes again. Retrying here would only spin.
      .catch(() => { entry.subscribed = false; entry.confirmed = false; });
  }

  /** The conversation with at least its first turns; reads them only if missing. */
  ensure(id: string): Promise<ConversationModel> {
    const entry = this.entry(id);
    if (entry.conversation) return Promise.resolve(entry.conversation);
    return this.read(id, entry);
  }

  /** Reads the conversation's current state from the daemon, for a caller that must see what it just caused. */
  refresh(id: string): Promise<ConversationModel> { return this.read(id, this.entry(id)); }

  retry(id: string): void {
    const entry = this.entry(id);
    entry.error = null;
    entry.retries = 0;
    void this.read(id, entry).catch(() => {});
  }

  private read(id: string, entry: ContentEntry): Promise<ConversationModel> {
    return entry.loading ??= (async () => {
      const epoch = entry.epoch;
      // Another device's conversation shows as last read while the current one travels.
      if (!entry.conversation && this.options.snapshots) {
        const stored = await this.options.snapshots.read(this.snapshotKey(id)).catch(() => null);
        if (stored && !entry.conversation && this.entries.get(id)?.epoch === epoch) {
          entry.conversation = ConversationModel.fromSnapshot(stored);
          this.changed(id, entry);
        }
      }
      const snapshot = await this.snapshot(id, PAGE_TURNS);
      if (this.entries.get(id)?.epoch !== epoch && !this.entries.has(id)) return ConversationModel.fromSnapshot(snapshot);
      // The same instance: views hold on to it.
      const conversation = entry.conversation;
      if (conversation) conversation.replaceSnapshot(snapshot);
      else entry.conversation = ConversationModel.fromSnapshot(snapshot);
      entry.error = null;
      entry.retries = 0;
      void this.options.snapshots?.write(this.snapshotKey(id), snapshot);
      this.changed(id, entry);
      // Subscribed already and the new cursor is ahead: duplicates are skipped by sequence.
      if (entry.users > 0 && !entry.subscribed) this.subscribe(id, entry);
      return entry.conversation!;
    })().catch((error: unknown) => {
      const failure = error instanceof Error ? error : new Error(String(error));
      entry.error = failure;
      entry.version++;
      for (const observer of [...entry.observers]) observer([]);
      throw failure;
    }).finally(() => { entry.loading = undefined; });
  }

  private snapshotKey(id: string) { return JSON.stringify([this.options.deviceId, this.scope.userId, this.scope.workspaceId, id]); }

  /** Reads the page before the oldest loaded turn; one at a time, kept even if the view has left. */
  loadOlder(id: string): Promise<void> {
    const entry = this.entry(id);
    const conversation = entry.conversation;
    if (!conversation?.hasOlderTurns) return Promise.resolve();
    const before = conversation.oldestTurnIndex();
    if (before === null || before <= 0) return Promise.resolve();
    return entry.older ??= (async () => {
      const epoch = entry.epoch;
      const raw = await this.request("GET", `/sessions/${encodeURIComponent(id)}/history?before_index=${before}&turn_limit=${PAGE_TURNS}`);
      const history = localV2HistorySchema.parse(raw);
      // Dropped, replaced or from another store: this page belongs to none of what is here.
      if (this.entries.get(id)?.epoch !== epoch || entry.conversation?.storeGeneration !== history.store_generation) return;
      const current = entry.conversation;
      if (!current) return;
      const oldest = current.oldestTurnIndex();
      if (oldest !== null && history.turns.length && history.turns.at(-1)!.turn_index !== oldest - 1) return;
      current.mergeHistory(history);
      this.changed(id, entry);
    })().finally(() => { entry.older = undefined; });
  }

  private readQueue(id: string, entry: ContentEntry) {
    entry.queueRead ??= (async () => {
      const conversation = entry.conversation;
      if (!conversation) return;
      const asked = conversation.queue.revision;
      const queue = localV2QueueSchema.parse(await this.request("GET", `/sessions/${encodeURIComponent(id)}/queue`));
      if (entry.conversation !== conversation) return;
      conversation.receiveQueue(queue);
      this.changed(id, entry);
      // Another change arrived during the read; one that failed waits for the next change or a reconnect.
      entry.queueRead = undefined;
      if (conversation.queueItemsStale() && conversation.queue.revision > asked && !this.disposed) this.readQueue(id, entry);
    })().catch(() => {}).finally(() => { entry.queueRead = undefined; });
  }

  /** Routes one server message of this scope. */
  receive(raw: unknown): void {
    if (this.disposed) return;
    const parsed = localV2ServerMessageSchema.safeParse(raw);
    if (!parsed.success) return;
    const message = parsed.data;
    if (message.type === "scope.summaries") {
      for (const summary of message.summaries) this.store(summary);
      this.scopeSeq = Math.max(this.scopeSeq, message.scope_seq);
      this.notifySummaries();
      return;
    }
    if (message.type === "host.request") return; // answered by the transport
    if (message.type === "scope.presence") {
      if (message.devices.length) this.presenceMap.set(message.conversation_id, message.devices);
      else this.presenceMap.delete(message.conversation_id);
      for (const listener of this.presenceListeners) listener();
      return;
    }
    if (message.type === "subscription.status" && !message.conversation_id) {
      // The scope stream fell behind: read the list again.
      if (message.status === "resubscribe" || message.status === "snapshot_required") this.reloadSummaries();
      return;
    }
    const id = message.conversation_id!;
    const entry = this.entries.get(id);
    if (!entry) return;
    if (message.type === "conversation.events") this.receiveEvents(id, entry, message.events);
    else this.receiveStatus(id, entry, message.status, message.high_water, message.error);
  }

  private receiveEvents(id: string, entry: ContentEntry, events: readonly LocalV2Event[]) {
    const conversation = entry.conversation;
    // Before the answer, these belong to a subscription already cancelled.
    if (!conversation || !entry.confirmed) return;
    if (conversation.applyEvents(events) === "gap") { this.recover(id, entry); return; }
    entry.retries = 0;
    if (conversation.queueItemsStale()) this.readQueue(id, entry);
    this.changed(id, entry, events);
    if (entry.users === 0 && conversation.seq >= entry.highWater) this.unsubscribe(id, entry);
  }

  private receiveStatus(id: string, entry: ContentEntry, status: string, highWater: number | undefined, error: { kind: string; message: string } | undefined) {
    switch (status) {
      case "subscribed": {
        entry.confirmed = true;
        entry.highWater = highWater ?? entry.conversation?.seq ?? 0;
        // Reopened with changes missed while away, or a retained queue behind the snapshot it came from.
        if (entry.conversation?.queueItemsStale()) this.readQueue(id, entry);
        if (entry.users === 0 && (entry.conversation?.seq ?? 0) >= entry.highWater) this.unsubscribe(id, entry);
        return;
      }
      case "live":
        if (entry.users === 0) this.unsubscribe(id, entry);
        return;
      case "resubscribe":
        entry.subscribed = false; entry.confirmed = false;
        this.recover(id, entry);
        return;
      case "snapshot_required":
        entry.subscribed = false; entry.confirmed = false;
        if (entry.users > 0) void this.refresh(id).catch(() => {});
        return;
      case "rejected":
        // A limit or an error another read will not fix: wait for a retry or reconnect.
        entry.subscribed = false; entry.confirmed = false;
        entry.error = new LocalV2RequestError(error?.kind ?? "rejected", error?.message ?? "The daemon rejected this conversation's subscription");
        entry.version++;
        for (const observer of [...entry.observers]) observer([]);
        return;
    }
  }

  /** Continues from what is applied; only repeated failure costs a snapshot. */
  private recover(id: string, entry: ContentEntry) {
    if (entry.users === 0) { this.unsubscribe(id, entry); return; }
    if (entry.retries++ < 2) { this.subscribe(id, entry); return; }
    entry.retries = 0;
    entry.subscribed = false; entry.confirmed = false;
    void this.refresh(id).catch(() => {});
  }

  /** Other devices using a conversation now. */
  presence(id: string): LocalV2Device[] { return this.presenceMap.get(id) ?? []; }

  onPresence(listener: () => void): () => void {
    this.presenceListeners.add(listener);
    return () => { this.presenceListeners.delete(listener); };
  }

  /** A disconnect loses every subscription; only what views still hold comes back. */
  connection(connected: boolean): void {
    if (this.disposed) return;
    if (!connected) {
      // The daemon sends presence again with the next scope subscription.
      if (this.presenceMap.size) { this.presenceMap.clear(); for (const listener of this.presenceListeners) listener(); }
      for (const entry of this.entries.values()) { entry.subscribed = false; entry.confirmed = false; }
      clearTimeout(this.listTimer);
      return;
    }
    this.focused.reconnected();
    void this.transport.storeGeneration?.().then((generation) => this.noteGeneration(generation)).catch(() => {});
    if (this.summaryLoad || this.summariesRead) this.reloadSummaries();
    for (const [id, entry] of this.entries) {
      if (entry.users === 0) continue;
      entry.retries = 0;
      if (entry.conversation) {
        this.subscribe(id, entry);
        if (entry.conversation.queueItemsStale()) this.readQueue(id, entry);
      } else void this.read(id, entry).catch(() => {});
    }
  }

  /** The daemon's database was replaced: nothing here describes it any more. */
  invalidate(): void {
    this.scopeSeq = 0;
    this.summariesRead = false;
    this.summaryMap.clear();
    this.summaryList = [];
    this.notifySummaries();
    this.reloadSummaries();
    for (const [id, entry] of this.entries) {
      entry.epoch = this.nextEpoch++;
      entry.conversation = undefined;
      entry.subscribed = false; entry.confirmed = false;
      entry.loading = undefined;
      if (entry.users > 0) void this.read(id, entry).catch(() => {});
      else if (!entry.observers.size) this.entries.delete(id);
    }
  }

  /** Stops all work for good; a late answer changes nothing and nothing reconnects. */
  dispose(): void {
    this.disposed = true;
    clearTimeout(this.listTimer);
    this.entries.clear();
    this.summaryListeners.clear();
    this.presenceListeners.clear();
    this.transport.release?.(this.scope);
  }
}
