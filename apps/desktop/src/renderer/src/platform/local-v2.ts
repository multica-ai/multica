import type { z } from "zod";
import { parseWithFallback } from "@multica/core/api/schema";
import {
  LOCAL_V2_VERSION, localV2ServerMessageSchema, localV2SessionListSchema, localV2SnapshotSchema, localV2UploadSchema,
  type LocalV2Device, type LocalV2Scope, type LocalV2Snapshot, type LocalV2Summary, type LocalV2Transport,
} from "@multica/core/local-conversation/v2";
import { LOCAL_V2_FOCUS_CAPABILITY, LocalV2FocusTracker } from "@multica/core/local-conversation/focus";

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

export type ConversationListener = (message: unknown) => void;

interface ConversationSubscription {
  listeners: Set<ConversationListener>;
  cursor: number;
}

const SUMMARY_PAGES = 100;

/**
 * One account and workspace of the local v2 API in this renderer. It keeps
 * the complete conversation list current through the scope subscription,
 * and shares one WebSocket subscription per conversation among its
 * listeners. Subscriptions do not survive a reconnect: once connected
 * again, listeners are told to reload and resubscribe from a new snapshot.
 */
export class LocalV2ScopeClient {
  private summaryMap = new Map<string, LocalV2Summary>();
  /** The list as last read; replaced on each change so React sees a new value. */
  private summaryList: LocalV2Summary[] = [];
  private summaryLoad?: Promise<void>;
  private scopeSeq = 0;
  private summaryListeners = new Set<() => void>();
  private conversations = new Map<string, ConversationSubscription>();
  /** Listeners whose subscriptions a disconnect dropped, reloaded on reconnect. */
  private lost = new Map<string, Set<ConversationListener>>();
  private generation = "";
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

  constructor(private transport: LocalV2Transport, readonly scope: LocalV2Scope) {}

  /** Someone is looking at a conversation until the returned function is called. */
  focus(id: string): () => void { return this.focused.hold(id); }

  async request(method: "GET" | "POST" | "DELETE", path: string, body?: unknown): Promise<unknown> {
    try { return await this.transport.request(this.scope, method, path, body); }
    catch (error) { throw localV2Error(error); }
  }

  /** A conversation command: send, interrupt, update, queue_update... */
  command(id: string, body: Record<string, unknown>): Promise<unknown> {
    return this.request("POST", `/sessions/${encodeURIComponent(id)}/commands`, body);
  }

  async snapshot(id: string): Promise<LocalV2Snapshot> {
    const raw = await this.request("GET", `/sessions/${encodeURIComponent(id)}/snapshot`);
    const snapshot = parseWithFallback<LocalV2Snapshot | null>(raw, localV2SnapshotSchema, null, { endpoint: "local-v2.snapshot" });
    if (!snapshot) throw new LocalV2RequestError("invalid_response", "Invalid local conversation response. Reload and retry.");
    this.generation = snapshot.store_generation;
    this.receiveSummary(snapshot.conversation);
    return snapshot;
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

  /** Every conversation of the scope, loaded once and kept current. */
  summaries(): LocalV2Summary[] { return this.summaryList; }
  summary(id: string): LocalV2Summary | undefined { return this.summaryMap.get(id); }

  loadSummaries(): Promise<void> {
    return this.summaryLoad ??= (async () => {
      let after = 0;
      for (let page = 0; page < SUMMARY_PAGES; page++) {
        const raw = await this.request("GET", `/sessions?after_scope_seq=${after}`);
        const list = parseWithFallback<z.infer<typeof localV2SessionListSchema>>(raw, localV2SessionListSchema, { scope_seq: after, summaries: [], has_more: false }, { endpoint: "local-v2.sessions" });
        for (const summary of list.summaries) this.store(summary);
        after = Math.max(after, list.scope_seq, ...list.summaries.map((summary) => summary.summary_seq));
        if (!list.has_more) break;
      }
      this.scopeSeq = after;
      await this.transport.send(this.scope, { version: LOCAL_V2_VERSION, type: "subscribe_scope", after_scope_seq: after });
      this.notifySummaries();
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
    return true;
  }

  private notifySummaries() { for (const listener of this.summaryListeners) listener(); }

  /**
   * Receives a conversation's server messages after afterSeq. A listener
   * that starts behind the shared subscription is told to reload instead.
   */
  subscribe(id: string, afterSeq: number, generation: string, listener: ConversationListener): () => void {
    let subscription = this.conversations.get(id);
    if (!subscription) {
      subscription = { listeners: new Set(), cursor: afterSeq };
      this.conversations.set(id, subscription);
      void this.transport.send(this.scope, { version: LOCAL_V2_VERSION, type: "subscribe", conversation_id: id, after_seq: afterSeq, store_generation: generation || this.generation })
        .catch(() => listener({ version: LOCAL_V2_VERSION, type: "subscription.status", conversation_id: id, status: "resubscribe" }));
    } else if (afterSeq < subscription.cursor) {
      queueMicrotask(() => listener({ version: LOCAL_V2_VERSION, type: "subscription.status", conversation_id: id, status: "resubscribe" }));
    }
    subscription.listeners.add(listener);
    return () => {
      this.lost.get(id)?.delete(listener);
      const current = this.conversations.get(id);
      if (!current?.listeners.delete(listener) || current.listeners.size) return;
      this.conversations.delete(id);
      void this.transport.send(this.scope, { version: LOCAL_V2_VERSION, type: "unsubscribe", conversation_id: id }).catch(() => {});
    };
  }

  /** Routes one server message of this scope. */
  receive(raw: unknown): void {
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
    const subscription = this.conversations.get(id);
    if (!subscription) return;
    if (message.type === "conversation.events") subscription.cursor = Math.max(subscription.cursor, message.to_seq);
    else if (message.status !== "subscribed" && message.status !== "live") this.conversations.delete(id);
    for (const listener of [...subscription.listeners]) listener(raw);
  }

  /** Other devices using a conversation now. */
  presence(id: string): LocalV2Device[] { return this.presenceMap.get(id) ?? []; }

  onPresence(listener: () => void): () => void {
    this.presenceListeners.add(listener);
    return () => { this.presenceListeners.delete(listener); };
  }

  /**
   * A disconnect loses every subscription. Listeners reload once the daemon
   * is back: a reload while it is down would fail and not be retried.
   */
  connection(connected: boolean): void {
    if (!connected) {
      // The daemon sends presence again with the next scope subscription.
      if (this.presenceMap.size) { this.presenceMap.clear(); for (const listener of this.presenceListeners) listener(); }
      for (const [id, subscription] of this.conversations) {
        const lost = this.lost.get(id) ?? new Set();
        for (const listener of subscription.listeners) lost.add(listener);
        this.lost.set(id, lost);
      }
      this.conversations.clear();
      return;
    }
    this.focused.reconnected();
    if (this.summaryLoad) this.reloadSummaries();
    const lost = [...this.lost.entries()];
    this.lost.clear();
    for (const [id, listeners] of lost) {
      for (const listener of listeners) listener({ version: LOCAL_V2_VERSION, type: "subscription.status", conversation_id: id, status: "resubscribe" });
    }
  }

  private reloadSummaries() {
    this.summaryLoad = undefined;
    void this.loadSummaries().catch(() => {});
  }
}

const routed = new WeakMap<LocalV2Transport, Map<string, LocalV2ScopeClient>>();

/** The renderer's client of a scope on a transport; one per account and workspace. */
export function localV2(transport: LocalV2Transport, scope: LocalV2Scope): LocalV2ScopeClient {
  let clients = routed.get(transport);
  if (!clients) {
    // Each device has its own transport; tests create their own too.
    const own = new Map<string, LocalV2ScopeClient>();
    clients = own;
    routed.set(transport, own);
    const find = (value: LocalV2Scope) => own.get(JSON.stringify([value.userId, value.workspaceId]));
    transport.subscribe((value, message) => find(value)?.receive(message));
    transport.onConnection((value, connected) => find(value)?.connection(connected));
  }
  const key = JSON.stringify([scope.userId, scope.workspaceId]);
  let client = clients.get(key);
  if (!client) { client = new LocalV2ScopeClient(transport, scope); clients.set(key, client); }
  return client;
}
