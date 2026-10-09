import type { SearchIndexChanges, SearchIndexManifest, SearchIndexSnapshotPage } from "../types";
import { indexTargetKey, type FetchOp, type IndexTarget, type TabMessage, type WorkerMessage } from "./protocol";
import type { IndexStore, SearchIndexDatabaseOwner } from "./store";
import { IndexFetchError, WorkspaceIndex, type IndexFetcher, type WorkspaceIndexOptions } from "./sync";

/**
 * Runs inside the search index worker (MUL-7754). A SharedWorker hosts every
 * tab of the origin; a dedicated worker hosts one. Tabs attach to the
 * workspace they show, and the host keeps one WorkspaceIndex per attached
 * (user, workspace), released a minute after its last tab leaves.
 *
 * The worker has no credentials: it asks an attached tab to run each request
 * through the tab's API client, so authentication, session renewal, and
 * workspace headers stay where they already work.
 */

export interface PortLike {
  postMessage(message: WorkerMessage): void;
}

export interface SearchIndexHostDeps {
  createStore(target: IndexTarget): IndexStore;
  /** Deletes the stored indexes on this origin that `shouldDelete` selects. */
  deleteDatabases(shouldDelete: (owner: SearchIndexDatabaseOwner) => boolean): Promise<void>;
  indexOptions?: Partial<WorkspaceIndexOptions>;
  /** Wait before freeing an index no tab is attached to. */
  releaseDelayMs?: number;
  /** How long a tab has to answer a request before another tab is asked. */
  fetchTimeoutMs?: number;
  /** A tab silent for this long is treated as closed. */
  portTimeoutMs?: number;
  now?: () => number;
}

interface PortEntry {
  port: PortLike;
  target: IndexTarget | null;
  lastSeen: number;
  /**
   * What the port was attached to when a sweep dropped it. A tab the browser
   * froze in the background stays silent past the timeout while another tab
   * keeps the worker alive; its next message restores the attachment.
   */
  suspended: IndexTarget | null;
}

interface IndexEntry {
  index: WorkspaceIndex;
  target: IndexTarget;
  slug: string;
  ports: Set<PortEntry>;
  releaseTimer: ReturnType<typeof setTimeout> | null;
}

interface PendingFetch {
  entry: PortEntry;
  resolve: (data: unknown) => void;
  reject: (err: Error) => void;
  timer: ReturnType<typeof setTimeout>;
}

class PortGoneError extends Error {}

export class SearchIndexHost {
  private readonly ports = new Map<PortLike, PortEntry>();
  private readonly indexes = new Map<string, IndexEntry>();
  private readonly pending = new Map<number, PendingFetch>();
  private nextFetchId = 1;
  private readonly releaseDelayMs: number;
  private readonly fetchTimeoutMs: number;
  private readonly portTimeoutMs: number;
  private readonly now: () => number;
  private sweepTimer: ReturnType<typeof setInterval> | null = null;

  constructor(private readonly deps: SearchIndexHostDeps) {
    this.releaseDelayMs = deps.releaseDelayMs ?? 60_000;
    this.fetchTimeoutMs = deps.fetchTimeoutMs ?? 60_000;
    this.portTimeoutMs = deps.portTimeoutMs ?? 10 * 60_000;
    this.now = deps.now ?? (() => Date.now());
  }

  /** Registers a tab's port and returns the handler for its messages. */
  connect(port: PortLike): (message: TabMessage) => void {
    const entry: PortEntry = { port, target: null, lastSeen: this.now(), suspended: null };
    this.ports.set(port, entry);
    this.sweepTimer ??= setInterval(() => this.sweep(), Math.min(this.portTimeoutMs, 60_000));
    port.postMessage({ type: "hello" });
    return (message) => this.handle(entry, message);
  }

  disconnect(port: PortLike): void {
    const entry = this.ports.get(port);
    if (!entry) return;
    this.ports.delete(port);
    this.detach(entry);
    for (const [id, pending] of this.pending) {
      if (pending.entry === entry) {
        clearTimeout(pending.timer);
        this.pending.delete(id);
        pending.reject(new PortGoneError("tab closed"));
      }
    }
  }

  private handle(entry: PortEntry, message: TabMessage): void {
    entry.lastSeen = this.now();
    if (!this.ports.has(entry.port)) {
      if (message.type === "close") return;
      this.ports.set(entry.port, entry);
      const suspended = entry.suspended;
      entry.suspended = null;
      if (suspended && message.type !== "attach" && message.type !== "detach") this.attach(entry, suspended);
    }
    switch (message.type) {
      case "attach":
        this.attach(entry, message.target);
        return;
      case "detach":
        this.detach(entry);
        return;
      case "close":
        this.disconnect(entry.port);
        return;
      case "poke":
        if (entry.target) this.indexes.get(indexTargetKey(entry.target))?.index.requestSync();
        return;
      case "ping":
        return;
      case "search":
        void this.search(entry, message.id, message.kind, message.params);
        return;
      case "fetch-result": {
        const pending = this.pending.get(message.id);
        if (!pending) return;
        clearTimeout(pending.timer);
        this.pending.delete(message.id);
        if (message.ok) pending.resolve(message.data);
        else pending.reject(new IndexFetchError(message.message, message.status));
        return;
      }
      case "wipe":
        void this.wipe(entry, message.id);
        return;
      case "forget":
        void this.forget(message.workspaceId);
        return;
      case "prune":
        void this.prune(message.userId, message.workspaceIds);
        return;
    }
  }

  private attach(entry: PortEntry, target: IndexTarget): void {
    const key = indexTargetKey(target);
    if (entry.target && indexTargetKey(entry.target) !== key) this.detach(entry);
    entry.target = target;
    let indexEntry = this.indexes.get(key);
    if (!indexEntry) {
      const created: IndexEntry = {
        index: null as unknown as WorkspaceIndex,
        target,
        slug: target.workspaceSlug,
        ports: new Set(),
        releaseTimer: null,
      };
      created.index = new WorkspaceIndex(key, this.deps.createStore(target), this.fetcherFor(key), {
        ...this.deps.indexOptions,
        onChange: () => this.broadcastServing(key),
      });
      indexEntry = created;
      this.indexes.set(key, indexEntry);
      void indexEntry.index.start();
    }
    // A renamed workspace keeps its id; later requests use the new slug.
    indexEntry.slug = target.workspaceSlug;
    if (indexEntry.releaseTimer) {
      clearTimeout(indexEntry.releaseTimer);
      indexEntry.releaseTimer = null;
    }
    indexEntry.ports.add(entry);
    entry.port.postMessage({ type: "serving", key, serving: indexEntry.index.isServing() });
    indexEntry.index.requestSync();
  }

  private detach(entry: PortEntry): void {
    if (!entry.target) return;
    const key = indexTargetKey(entry.target);
    entry.target = null;
    const indexEntry = this.indexes.get(key);
    if (!indexEntry) return;
    indexEntry.ports.delete(entry);
    if (indexEntry.ports.size === 0 && !indexEntry.releaseTimer) {
      indexEntry.releaseTimer = setTimeout(() => {
        if (indexEntry.ports.size > 0) return;
        this.indexes.delete(key);
        void indexEntry.index.dispose();
      }, this.releaseDelayMs);
    }
  }

  private async search(
    entry: PortEntry,
    id: number,
    kind: "issues" | "projects",
    params: Parameters<WorkspaceIndex["searchIssues"]>[0],
  ): Promise<void> {
    const indexEntry = entry.target ? this.indexes.get(indexTargetKey(entry.target)) : undefined;
    let result = null;
    try {
      if (indexEntry) {
        result =
          kind === "issues" ? await indexEntry.index.searchIssues(params) : await indexEntry.index.searchProjects(params);
      }
    } catch {
      // The tab falls back to server search.
      result = null;
    }
    entry.port.postMessage({ type: "search-result", id, result });
  }

  private async wipe(entry: PortEntry, id: number): Promise<void> {
    await this.destroyIndexes(() => true);
    await this.deps.deleteDatabases(() => true).catch(() => undefined);
    entry.port.postMessage({ type: "wiped", id });
  }

  /** Destroys every copy of a workspace, in memory and on disk. */
  private async forget(workspaceId: string): Promise<void> {
    await this.destroyIndexes((indexEntry) => indexEntry.target.workspaceId === workspaceId);
    await this.deps.deleteDatabases((owner) => owner.workspaceId === workspaceId).catch(() => undefined);
  }

  /**
   * Keeps only this user's copies of the listed workspaces. A copy a tab is
   * still showing survives even if the list lacks it: the list can be a
   * cached one from before the user joined that workspace.
   */
  private async prune(userId: string, workspaceIds: string[]): Promise<void> {
    const keep = new Set(workspaceIds.map((workspaceId) => indexTargetKey({ userId, workspaceId })));
    for (const [key, indexEntry] of this.indexes) {
      if (indexEntry.ports.size > 0) keep.add(key);
    }
    await this.destroyIndexes((indexEntry) => !keep.has(indexTargetKey(indexEntry.target)));
    await this.deps.deleteDatabases((owner) => !keep.has(indexTargetKey(owner))).catch(() => undefined);
  }

  private async destroyIndexes(select: (indexEntry: IndexEntry) => boolean): Promise<void> {
    const doomed: IndexEntry[] = [];
    for (const [key, indexEntry] of this.indexes) {
      if (!select(indexEntry)) continue;
      this.indexes.delete(key);
      if (indexEntry.releaseTimer) clearTimeout(indexEntry.releaseTimer);
      for (const port of indexEntry.ports) {
        port.target = null;
        port.port.postMessage({ type: "serving", key, serving: false });
      }
      doomed.push(indexEntry);
    }
    await Promise.all(doomed.map((indexEntry) => indexEntry.index.dispose(true).catch(() => undefined)));
  }

  private broadcastServing(key: string): void {
    const indexEntry = this.indexes.get(key);
    if (!indexEntry) return;
    const serving = indexEntry.index.isServing();
    for (const port of indexEntry.ports) port.port.postMessage({ type: "serving", key, serving });
  }

  private fetcherFor(key: string): IndexFetcher {
    const request = async <T>(op: FetchOp, params: { afterNumber?: number; cursor?: string; limit?: number }) =>
      (await this.requestFromTab(key, op, params)) as T;
    return {
      manifest: () => request<SearchIndexManifest>("manifest", {}),
      snapshot: (afterNumber, limit) => request<SearchIndexSnapshotPage>("snapshot", { afterNumber, limit }),
      changes: (cursor, limit) => request<SearchIndexChanges>("changes", { cursor, limit }),
    };
  }

  /** Asks the most recently active attached tab; moves on if it has gone away. */
  private async requestFromTab(
    key: string,
    op: FetchOp,
    params: { afterNumber?: number; cursor?: string; limit?: number },
  ): Promise<unknown> {
    const indexEntry = this.indexes.get(key);
    const candidates = indexEntry ? [...indexEntry.ports].sort((a, b) => b.lastSeen - a.lastSeen) : [];
    for (const entry of candidates) {
      if (!this.ports.has(entry.port) || !entry.target || indexTargetKey(entry.target) !== key) continue;
      try {
        return await this.send(entry, op, indexEntry!.slug, params);
      } catch (err) {
        if (err instanceof PortGoneError) continue;
        throw err;
      }
    }
    throw new Error("no tab is attached to this workspace");
  }

  private send(
    entry: PortEntry,
    op: FetchOp,
    workspaceSlug: string,
    params: { afterNumber?: number; cursor?: string; limit?: number },
  ): Promise<unknown> {
    const id = this.nextFetchId++;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new PortGoneError("tab did not answer"));
      }, this.fetchTimeoutMs);
      this.pending.set(id, { entry, resolve, reject, timer });
      entry.port.postMessage({ type: "fetch", id, op, workspaceSlug, params });
    });
  }

  /**
   * Frees ports whose tab went silent: closed without pagehide, or frozen in
   * the background. A frozen tab that speaks again is restored in `handle`.
   */
  private sweep(): void {
    const cutoff = this.now() - this.portTimeoutMs;
    for (const entry of [...this.ports.values()]) {
      if (entry.lastSeen >= cutoff) continue;
      const target = entry.target;
      this.disconnect(entry.port);
      entry.suspended = target;
    }
  }
}
