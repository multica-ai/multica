import type {
  SearchIndexChanges,
  SearchIndexComment,
  SearchIndexIssue,
  SearchIndexManifest,
  SearchIndexProject,
  SearchIndexSnapshotPage,
  SearchIssuesResponse,
  SearchProjectsResponse,
} from "../types";
import {
  SearchIndexEngine,
  toSearchIssueResult,
  toSearchProjectResult,
  type IssueSearchParams,
  type ProjectSearchParams,
} from "./engine";
import { SEARCH_INDEX_SCHEMA_VERSION, emptyIndexMeta, type IndexMeta, type IndexStore } from "./store";

/**
 * Keeps one workspace's local search index in step with the server
 * (MUL-7754): a first full copy through the manifest and snapshot pages, then
 * catch-up through the change log. Answers searches only while the copy is
 * complete and recently confirmed current; otherwise callers use server search.
 */

/**
 * Largest workspace (UTF-8 bytes of titles, descriptions, and comments) kept in
 * memory. Local search holds roughly one copy of this text; a 150 MB workspace
 * measured about 185 MB of worker heap and 10–25 ms per query.
 */
export const DEFAULT_MAX_TEXT_BYTES = 500 * 1024 * 1024;

const SNAPSHOT_PAGE_SIZE = 200;
const CHANGES_PAGE_SIZE = 500;

export interface IndexFetcher {
  manifest(): Promise<SearchIndexManifest>;
  snapshot(afterNumber: number, limit: number): Promise<SearchIndexSnapshotPage>;
  changes(cursor: string, limit: number): Promise<SearchIndexChanges>;
}

/** Thrown by fetchers for an HTTP error response. */
export class IndexFetchError extends Error {
  constructor(
    message: string,
    readonly status?: number,
  ) {
    super(message);
    this.name = "IndexFetchError";
  }
}

/** The copy outgrew the memory budget; it is dropped and server search takes over. */
class OverBudgetError extends Error {}

export type IndexState =
  | "idle"
  | "loading"
  | "bootstrapping"
  | "ready"
  | "too_large"
  | "unavailable"
  | "disposed";

export interface WorkspaceIndexOptions {
  maxTextBytes: number;
  /** A workspace over budget is measured again after this long. */
  tooLargeRecheckMs: number;
  /** After losing access (or the kill switch), wait this long before trying again. */
  unavailableRetryMs: number;
  /** Catch up at least this often while attached. */
  pollIntervalMs: number;
  /** Local results stop being served when the last good catch-up is this old and later ones failed. */
  staleAfterMs: number;
  /** Coalesces bursts of change notifications. */
  syncDebounceMs: number;
  retryBaseMs: number;
  retryMaxMs: number;
  now: () => number;
  /** Returns [0, 1); spreads retries so clients rejected together do not return together. */
  random: () => number;
  /**
   * Serializes syncs of the same workspace across contexts (dedicated workers
   * in several tabs share one database).
   */
  withLock: <T>(name: string, fn: () => Promise<T>) => Promise<T>;
  /** Called whenever whether the index can serve searches may have changed. */
  onChange?: () => void;
}

export const defaultWorkspaceIndexOptions = (): WorkspaceIndexOptions => ({
  maxTextBytes: DEFAULT_MAX_TEXT_BYTES,
  tooLargeRecheckMs: 24 * 60 * 60 * 1000,
  unavailableRetryMs: 60 * 60 * 1000,
  pollIntervalMs: 5 * 60 * 1000,
  staleAfterMs: 10 * 60 * 1000,
  syncDebounceMs: 2_000,
  retryBaseMs: 5_000,
  retryMaxMs: 5 * 60 * 1000,
  now: () => Date.now(),
  random: () => Math.random(),
  withLock: (_name, fn) => fn(),
});

export class WorkspaceIndex {
  private readonly engine = new SearchIndexEngine();
  private readonly options: WorkspaceIndexOptions;
  private state: IndexState = "idle";
  /** Cursor the in-memory engine reflects; differs from the store's when another context advanced it. */
  private loadedCursor: string | null = null;
  private lastSyncOkAt: number | null = null;
  private lastErrorAt: number | null = null;
  private failures = 0;
  private running: Promise<void> | null = null;
  private rerun = false;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private pollTimer: ReturnType<typeof setInterval> | null = null;

  constructor(
    readonly key: string,
    private readonly store: IndexStore,
    private readonly fetcher: IndexFetcher,
    options: Partial<WorkspaceIndexOptions> = {},
  ) {
    this.options = { ...defaultWorkspaceIndexOptions(), ...options };
  }

  get currentState(): IndexState {
    return this.state;
  }

  /** Loads the stored copy and starts syncing and polling; resolves after the first sync. */
  start(): Promise<void> {
    if (this.state !== "idle") return Promise.resolve();
    this.setState("loading");
    this.pollTimer = setInterval(() => this.requestSync(), this.options.pollIntervalMs);
    return this.sync();
  }

  /** Asks for a catch-up soon; bursts collapse into one run. */
  requestSync(): void {
    if (this.state === "disposed" || this.timer) return;
    this.timer = setTimeout(() => {
      this.timer = null;
      void this.sync();
    }, this.options.syncDebounceMs);
  }

  /** Runs one sync now, or once more after the one in flight. */
  sync(): Promise<void> {
    if (this.state === "disposed") return Promise.resolve();
    if (this.running) {
      this.rerun = true;
      return this.running;
    }
    this.running = (async () => {
      try {
        do {
          this.rerun = false;
          await this.options.withLock(`multica-search-index:${this.key}`, () => this.syncOnce());
        } while (this.rerun && this.state !== "disposed");
      } finally {
        this.running = null;
      }
    })();
    return this.running;
  }

  /** True when a search can be answered locally with current data. */
  isServing(): boolean {
    if (this.state !== "ready" || this.lastSyncOkAt === null) return false;
    if (this.lastErrorAt === null || this.lastErrorAt <= this.lastSyncOkAt) return true;
    return this.options.now() - this.lastSyncOkAt < this.options.staleAfterMs;
  }

  async searchIssues(params: IssueSearchParams): Promise<SearchIssuesResponse | null> {
    if (!this.isServing()) return null;
    const hits = this.engine.searchIssues(params);
    const snippetIds = hits.map((hit) => hit.snippetCommentId).filter((id): id is string => id !== null);
    const [issues, comments] = await Promise.all([
      this.store.getIssues(hits.map((hit) => hit.id)),
      this.store.getComments(snippetIds),
    ]);
    const results = [];
    for (const hit of hits) {
      const issue = issues.get(hit.id);
      // Deleted between the scan and the read; the next search drops it too.
      if (!issue) continue;
      const comment = hit.snippetCommentId ? (comments.get(hit.snippetCommentId) ?? null) : null;
      results.push(toSearchIssueResult(issue, hit, params.q, comment));
    }
    return { issues: results };
  }

  async searchProjects(params: ProjectSearchParams): Promise<SearchProjectsResponse | null> {
    if (!this.isServing()) return null;
    const hits = this.engine.searchProjects(params);
    const projects = await this.store.getProjects(hits.map((hit) => hit.id));
    const results = [];
    for (const hit of hits) {
      const project = projects.get(hit.id);
      if (project) results.push(toSearchProjectResult(project, hit, params.q));
    }
    return { projects: results };
  }

  /** Stops syncing and releases memory. The stored copy stays unless `destroy`. */
  async dispose(destroy = false): Promise<void> {
    this.setState("disposed");
    if (this.timer) clearTimeout(this.timer);
    if (this.pollTimer) clearInterval(this.pollTimer);
    this.timer = null;
    this.pollTimer = null;
    this.engine.clear();
    if (destroy) await this.store.destroy();
  }

  private setState(state: IndexState): void {
    if (this.state === state || this.state === "disposed") return;
    this.state = state;
    this.options.onChange?.();
  }

  private async syncOnce(): Promise<void> {
    if (this.state === "disposed") return;
    try {
      let meta = await this.store.readMeta();
      if (!meta || meta.schemaVersion !== SEARCH_INDEX_SCHEMA_VERSION) {
        meta = emptyIndexMeta();
        await this.store.reset(meta);
        this.forgetLoaded();
      }
      if (this.state === "unavailable" && this.lastErrorAt !== null) {
        if (this.options.now() - this.lastErrorAt < this.options.unavailableRetryMs) return;
      }
      if (!meta.cursor) {
        if (meta.tooLargeAt !== null && this.options.now() - meta.tooLargeAt < this.options.tooLargeRecheckMs) {
          this.setState("too_large");
          return;
        }
        meta = await this.bootstrap(meta);
        if (!meta.cursor) return;
      } else if (this.loadedCursor !== meta.cursor) {
        await this.reloadFromStore();
        this.checkBudget();
        this.loadedCursor = meta.cursor;
      }
      this.setState("ready");
      await this.catchUp(meta);
      this.lastSyncOkAt = this.options.now();
      this.failures = 0;
      this.options.onChange?.();
    } catch (err) {
      if (err instanceof OverBudgetError) await this.declineTooLarge();
      else await this.handleFailure(err);
      this.options.onChange?.();
    }
  }

  private async bootstrap(meta: IndexMeta): Promise<IndexMeta> {
    let progress = meta.bootstrap;
    if (progress) {
      // Resume: pages written before the interruption are already stored.
      await this.reloadFromStore();
      this.checkBudget();
    } else {
      const manifest = await this.fetcher.manifest();
      if (manifest.text_bytes > this.options.maxTextBytes) throw new OverBudgetError();
      progress = { cursor: manifest.cursor, afterNumber: 0 };
      await this.store.reset({ ...emptyIndexMeta(), bootstrap: progress });
      this.forgetLoaded();
    }
    this.setState("bootstrapping");

    for (;;) {
      if (this.state === "disposed") return meta;
      const page = await this.fetcher.snapshot(progress.afterNumber, SNAPSHOT_PAGE_SIZE);
      const next: IndexMeta = page.done
        ? { ...emptyIndexMeta(), cursor: progress.cursor }
        : { ...emptyIndexMeta(), bootstrap: { cursor: progress.cursor, afterNumber: page.next_after_number } };
      await this.store.write({ issues: page.issues, comments: page.comments, projects: page.projects, meta: next });
      this.applyUpserts(page.issues, page.comments, page.projects);
      this.checkBudget();
      if (page.done) {
        this.loadedCursor = progress.cursor;
        return next;
      }
      progress = next.bootstrap!;
    }
  }

  private async catchUp(meta: IndexMeta): Promise<void> {
    let cursor = meta.cursor!;
    for (;;) {
      if (this.state === "disposed") return;
      const changes = await this.fetcher.changes(cursor, CHANGES_PAGE_SIZE);
      await this.store.write({
        issues: changes.issues,
        comments: changes.comments,
        projects: changes.projects,
        deletedIssues: changes.deleted.issues,
        deletedComments: changes.deleted.comments,
        deletedProjects: changes.deleted.projects,
        meta: { ...emptyIndexMeta(), cursor: changes.cursor },
      });
      this.engine.deleteIssues(changes.deleted.issues);
      changes.deleted.comments.forEach((id) => this.engine.deleteComment(id));
      changes.deleted.projects.forEach((id) => this.engine.deleteProject(id));
      this.applyUpserts(changes.issues, changes.comments, changes.projects);
      this.checkBudget();
      cursor = changes.cursor;
      this.loadedCursor = cursor;
      if (!changes.has_more) return;
    }
  }

  private applyUpserts(issues: SearchIndexIssue[], comments: SearchIndexComment[], projects: SearchIndexProject[]): void {
    issues.forEach((issue) => this.engine.upsertIssue(issue));
    comments.forEach((comment) => this.engine.upsertComment(comment));
    projects.forEach((project) => this.engine.upsertProject(project));
  }

  private async reloadFromStore(): Promise<void> {
    const records = await this.store.loadAll();
    this.engine.clear();
    this.applyUpserts(records.issues, records.comments, records.projects);
  }

  /**
   * The manifest only sizes the first copy; a workspace keeps growing after
   * that, so every load and every applied batch is measured again.
   */
  private checkBudget(): void {
    if (this.engine.textBytes > this.options.maxTextBytes) throw new OverBudgetError();
  }

  /** Drops the copy and serves nothing until the workspace is measured again. */
  private async declineTooLarge(): Promise<void> {
    await this.store.reset({ ...emptyIndexMeta(), tooLargeAt: this.options.now() }).catch(() => undefined);
    this.forgetLoaded();
    this.setState("too_large");
  }

  private forgetLoaded(): void {
    this.engine.clear();
    this.loadedCursor = null;
  }

  private async handleFailure(err: unknown): Promise<void> {
    if (this.state === "disposed") return;
    this.lastErrorAt = this.options.now();
    const status = err instanceof IndexFetchError ? err.status : undefined;
    if (status === 410) {
      // Retention pruning passed our snapshot: rebuild from a fresh manifest.
      await this.store.reset(emptyIndexMeta()).catch(() => undefined);
      this.forgetLoaded();
      this.setState("loading");
      this.scheduleRetry(0);
      return;
    }
    if (status === 403 || status === 404) {
      // Membership was lost, the workspace is gone, or the server turned the
      // index off. Keep nothing on disk; try again much later.
      await this.store.reset(emptyIndexMeta()).catch(() => undefined);
      this.forgetLoaded();
      this.setState("unavailable");
      return;
    }
    // Transient: network, a busy server (503 while it sheds bootstrap load),
    // or no tab to run the request. Back off exponentially with jitter.
    this.failures += 1;
    const backoff = Math.min(this.options.retryBaseMs * 2 ** (this.failures - 1), this.options.retryMaxMs);
    this.scheduleRetry(backoff * (0.5 + this.options.random()));
  }

  private scheduleRetry(delay: number): void {
    if (this.state === "disposed") return;
    if (this.timer) clearTimeout(this.timer);
    this.timer = setTimeout(() => {
      this.timer = null;
      void this.sync();
    }, delay);
  }
}
