import { ApiError } from "../api/client";
import type {
  SearchIndexChanges,
  SearchIndexManifest,
  SearchIndexSnapshotPage,
  SearchIssuesResponse,
  SearchProjectsResponse,
} from "../types";
import type { IssueSearchParams } from "./engine";
import { indexTargetKey, type IndexTarget, type TabMessage, type WorkerMessage } from "./protocol";
import { deleteAllSearchIndexDatabases, deleteSearchIndexDatabases } from "./store";

/**
 * Tab-side handle on the local search index worker (MUL-7754). Everything here
 * degrades to "not available": a platform without workers, a worker that
 * failed to load, a workspace that is still syncing, or a search that takes
 * too long all make `searchIssues` resolve to null, and callers use server
 * search instead.
 */

/** The API calls the worker delegates to this tab. */
export interface SearchIndexApi {
  getSearchIndexManifest(params: { workspaceSlug: string }): Promise<SearchIndexManifest>;
  getSearchIndexSnapshot(params: { workspaceSlug: string; afterNumber: number; limit?: number }): Promise<SearchIndexSnapshotPage>;
  getSearchIndexChanges(params: { workspaceSlug: string; cursor: string; limit?: number }): Promise<SearchIndexChanges>;
}

/** A started worker, reduced to what the client needs. */
export interface WorkerChannel {
  post(message: TabMessage): void;
  onMessage(handler: (message: WorkerMessage) => void): void;
  onError(handler: () => void): void;
}

const SEARCH_TIMEOUT_MS = 1500;
const PING_INTERVAL_MS = 20_000;

export class LocalSearchIndexClient {
  private channel: WorkerChannel | null = null;
  private failed = false;
  private helloReceived = false;
  private target: IndexTarget | null = null;
  private serving = false;
  private nextId = 1;
  private readonly searches = new Map<number, (result: SearchIssuesResponse | SearchProjectsResponse | null) => void>();
  private readonly wipes = new Map<number, () => void>();

  constructor(
    private readonly getApi: () => SearchIndexApi,
    private readonly startWorker: () => WorkerChannel | null,
  ) {}

  /** Starts (or retargets) syncing for the workspace this tab shows. */
  attach(target: IndexTarget): void {
    const current = this.target;
    if (
      current &&
      indexTargetKey(current) === indexTargetKey(target) &&
      current.workspaceSlug === target.workspaceSlug
    ) {
      return;
    }
    if (!current || indexTargetKey(current) !== indexTargetKey(target)) this.serving = false;
    this.target = target;
    const channel = this.ensureChannel();
    channel?.post({ type: "attach", target });
    requestPersistentStorage();
  }

  detach(): void {
    if (!this.target) return;
    this.target = null;
    this.serving = false;
    this.channel?.post({ type: "detach" });
  }

  /** Something changed on the server; catch up soon. */
  poke(): void {
    if (this.target) this.channel?.post({ type: "poke" });
  }

  /** Whether the attached workspace can currently be searched locally. */
  isServing(): boolean {
    return this.serving && this.helloReceived && !this.failed;
  }

  searchIssues(params: IssueSearchParams): Promise<SearchIssuesResponse | null> {
    return this.search("issues", params) as Promise<SearchIssuesResponse | null>;
  }

  searchProjects(params: IssueSearchParams): Promise<SearchProjectsResponse | null> {
    return this.search("projects", params) as Promise<SearchProjectsResponse | null>;
  }

  /**
   * Deletes every local index on this device (logout, session end). Falls
   * back to deleting the databases from this tab when no worker answers.
   */
  async wipe(): Promise<void> {
    this.target = null;
    this.serving = false;
    const channel = this.usableChannel();
    if (channel) {
      const id = this.nextId++;
      const done = new Promise<void>((resolve) => {
        this.wipes.set(id, resolve);
        setTimeout(resolve, 5_000);
      });
      channel.post({ type: "wipe", id });
      await done;
      this.wipes.delete(id);
    }
    await deleteAllSearchIndexDatabases().catch(() => undefined);
  }

  /**
   * Access to a workspace ended (deleted, removed, left): destroy every local
   * copy of it now, rather than waiting for a sync that may never run again
   * because no tab shows that workspace any more.
   */
  forget(workspaceId: string): Promise<void> {
    if (this.target?.workspaceId === workspaceId) {
      this.target = null;
      this.serving = false;
    }
    // The worker drops the copy from memory and stops syncing it. The tab
    // deletes the database itself as well: this usually runs just before a
    // full-page navigation, which may end the worker before its delete does.
    this.usableChannel()?.post({ type: "forget", workspaceId });
    return deleteSearchIndexDatabases((owner) => owner.workspaceId === workspaceId).catch(() => undefined);
  }

  /** Destroys local copies of any workspace (or user) outside this list. */
  prune(userId: string, workspaceIds: string[]): Promise<void> {
    const channel = this.usableChannel();
    if (channel) {
      channel.post({ type: "prune", userId, workspaceIds });
      return Promise.resolve();
    }
    const keep = new Set(workspaceIds);
    const shown = this.target;
    return deleteSearchIndexDatabases(
      (owner) =>
        !(owner.userId === userId && keep.has(owner.workspaceId)) &&
        !(shown && owner.userId === shown.userId && owner.workspaceId === shown.workspaceId),
    ).catch(() => undefined);
  }

  private usableChannel(): WorkerChannel | null {
    return this.helloReceived && !this.failed ? this.channel : null;
  }

  private search(kind: "issues" | "projects", params: IssueSearchParams) {
    const channel = this.channel;
    if (!channel || !this.target || !this.isServing()) return Promise.resolve(null);
    const id = this.nextId++;
    return new Promise<SearchIssuesResponse | SearchProjectsResponse | null>((resolve) => {
      const timer = setTimeout(() => {
        this.searches.delete(id);
        resolve(null);
      }, SEARCH_TIMEOUT_MS);
      this.searches.set(id, (result) => {
        clearTimeout(timer);
        // The worker could not answer (it lost this tab's attachment, or the
        // copy went stale). Stop treating searches as local, which also
        // restores the server-search debounce, until it reports serving again.
        if (result === null) this.serving = false;
        resolve(result);
      });
      const { q, limit, offset, include_closed } = params;
      channel.post({ type: "search", id, kind, params: { q, limit, offset, include_closed } });
    });
  }

  private ensureChannel(): WorkerChannel | null {
    if (this.channel || this.failed) return this.channel;
    let channel: WorkerChannel | null = null;
    try {
      channel = this.startWorker();
    } catch {
      channel = null;
    }
    if (!channel) {
      this.failed = true;
      return null;
    }
    this.channel = channel;
    channel.onMessage((message) => this.handle(message));
    channel.onError(() => {
      this.failed = true;
      this.serving = false;
      for (const resolve of this.searches.values()) resolve(null);
      this.searches.clear();
    });
    // Lets the worker tell a live tab from one that closed without pagehide.
    setInterval(() => this.channel?.post({ type: "ping" }), PING_INTERVAL_MS);
    if (typeof window !== "undefined") {
      window.addEventListener("pagehide", () => this.channel?.post({ type: "close" }));
      // A page restored from the back/forward cache said "close" on the way
      // out, so the worker forgot it; attach again.
      window.addEventListener("pageshow", (event) => {
        if (event.persisted && this.target) this.channel?.post({ type: "attach", target: this.target });
      });
    }
    return channel;
  }

  private handle(message: WorkerMessage): void {
    switch (message.type) {
      case "hello":
        this.helloReceived = true;
        return;
      case "serving":
        if (this.target && indexTargetKey(this.target) === message.key) this.serving = message.serving;
        return;
      case "search-result":
        this.searches.get(message.id)?.(message.result);
        this.searches.delete(message.id);
        return;
      case "wiped":
        this.wipes.get(message.id)?.();
        return;
      case "fetch":
        void this.runFetch(message);
        return;
    }
  }

  private async runFetch(message: Extract<WorkerMessage, { type: "fetch" }>): Promise<void> {
    const api = this.getApi();
    const { workspaceSlug, params } = message;
    try {
      let data: unknown;
      switch (message.op) {
        case "manifest":
          data = await api.getSearchIndexManifest({ workspaceSlug });
          break;
        case "snapshot":
          data = await api.getSearchIndexSnapshot({ workspaceSlug, afterNumber: params.afterNumber ?? 0, limit: params.limit });
          break;
        case "changes":
          data = await api.getSearchIndexChanges({ workspaceSlug, cursor: params.cursor ?? "", limit: params.limit });
          break;
      }
      this.channel?.post({ type: "fetch-result", id: message.id, ok: true, data });
    } catch (err) {
      this.channel?.post({
        type: "fetch-result",
        id: message.id,
        ok: false,
        status: err instanceof ApiError ? err.status : undefined,
        message: err instanceof Error ? err.message : String(err),
      });
    }
  }
}

let persistRequested = false;

/** Asks the browser not to evict the index under storage pressure (best effort). */
function requestPersistentStorage(): void {
  if (persistRequested) return;
  persistRequested = true;
  const storage = typeof navigator !== "undefined" ? navigator.storage : undefined;
  void storage?.persist?.().catch(() => false);
}

/**
 * Starts the worker: one SharedWorker for every tab of the origin where
 * available, otherwise a dedicated worker for this tab. The `new URL(...)`
 * expressions must stay literal so webpack and Vite bundle the worker.
 */
export function startSearchIndexWorker(): WorkerChannel | null {
  if (typeof window === "undefined") return null;
  if (typeof SharedWorker !== "undefined") {
    try {
      const worker = new SharedWorker(new URL("./worker.ts", import.meta.url), {
        type: "module",
        name: "multica-search-index",
      });
      const port = worker.port;
      const channel: WorkerChannel = {
        post: (message) => port.postMessage(message),
        onMessage: (handler) => {
          port.onmessage = (event: MessageEvent<WorkerMessage>) => handler(event.data);
        },
        onError: (handler) => {
          worker.onerror = handler;
        },
      };
      port.start();
      return channel;
    } catch {
      // Fall through to a dedicated worker.
    }
  }
  if (typeof Worker !== "undefined") {
    try {
      const worker = new Worker(new URL("./worker.ts", import.meta.url), {
        type: "module",
        name: "multica-search-index",
      });
      return {
        post: (message) => worker.postMessage(message),
        onMessage: (handler) => {
          worker.onmessage = (event: MessageEvent<WorkerMessage>) => handler(event.data);
        },
        onError: (handler) => {
          worker.onerror = handler;
        },
      };
    } catch {
      return null;
    }
  }
  return null;
}
