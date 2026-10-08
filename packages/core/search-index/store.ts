import type { SearchIndexComment, SearchIndexIssue, SearchIndexProject } from "../types";

/**
 * Persistence for one workspace's local search index (MUL-7754): IndexedDB in
 * browsers and Electron, an in-memory implementation for tests.
 *
 * Every write that advances the catch-up cursor carries the records it covers
 * in the same transaction, so a crash or a closed tab can never leave a cursor
 * ahead of the data.
 */

/** Bump to discard every local copy, e.g. after changing what a record holds. */
export const SEARCH_INDEX_SCHEMA_VERSION = 1;

const DB_PREFIX = "multica-search-index";

export interface IndexMeta {
  schemaVersion: number;
  /** Catch-up cursor; set once the copy is complete. */
  cursor: string | null;
  /** Progress of an unfinished bootstrap, so a reload resumes it. */
  bootstrap: { cursor: string; afterNumber: number } | null;
  /** When the workspace last exceeded the memory budget, in epoch ms. */
  tooLargeAt: number | null;
}

export function emptyIndexMeta(): IndexMeta {
  return { schemaVersion: SEARCH_INDEX_SCHEMA_VERSION, cursor: null, bootstrap: null, tooLargeAt: null };
}

export interface IndexBatch {
  issues?: SearchIndexIssue[];
  comments?: SearchIndexComment[];
  projects?: SearchIndexProject[];
  /** Deleting an issue also deletes its comments. */
  deletedIssues?: string[];
  deletedComments?: string[];
  deletedProjects?: string[];
  meta?: IndexMeta;
}

export interface IndexRecords {
  issues: SearchIndexIssue[];
  comments: SearchIndexComment[];
  projects: SearchIndexProject[];
}

export interface IndexStore {
  readMeta(): Promise<IndexMeta | null>;
  loadAll(): Promise<IndexRecords>;
  /** Applies the batch atomically. */
  write(batch: IndexBatch): Promise<void>;
  /** Drops every record and replaces the meta. */
  reset(meta: IndexMeta): Promise<void>;
  getIssues(ids: string[]): Promise<Map<string, SearchIndexIssue>>;
  getComments(ids: string[]): Promise<Map<string, SearchIndexComment>>;
  getProjects(ids: string[]): Promise<Map<string, SearchIndexProject>>;
  /** Deletes the underlying database. The store is unusable afterwards. */
  destroy(): Promise<void>;
}

export function searchIndexDatabaseName(userId: string, workspaceId: string): string {
  return `${DB_PREFIX}:${userId}:${workspaceId}`;
}

const META_KEY = "state";

function requestResult<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}

function transactionDone(tx: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error);
    tx.onabort = () => reject(tx.error ?? new Error("IndexedDB transaction aborted"));
  });
}

export class IdbIndexStore implements IndexStore {
  private db: Promise<IDBDatabase> | null = null;
  private destroyed = false;

  constructor(private readonly name: string) {}

  private open(): Promise<IDBDatabase> {
    // A write racing a logout wipe must not recreate the deleted database.
    if (this.destroyed) return Promise.reject(new Error("search index store was destroyed"));
    if (!this.db) {
      this.db = new Promise((resolve, reject) => {
        const request = indexedDB.open(this.name, 1);
        request.onupgradeneeded = () => {
          const db = request.result;
          db.createObjectStore("meta");
          db.createObjectStore("issues", { keyPath: "id" });
          db.createObjectStore("comments", { keyPath: "id" }).createIndex("issue_id", "issue_id");
          db.createObjectStore("projects", { keyPath: "id" });
        };
        request.onsuccess = () => {
          const db = request.result;
          // Another context deleting the database (logout wipe) must not be
          // blocked by this connection.
          db.onversionchange = () => {
            db.close();
            this.db = null;
          };
          resolve(db);
        };
        request.onerror = () => reject(request.error);
        request.onblocked = () => reject(new Error("IndexedDB open blocked"));
      });
      this.db.catch(() => {
        this.db = null;
      });
    }
    return this.db;
  }

  async readMeta(): Promise<IndexMeta | null> {
    const db = await this.open();
    const meta = await requestResult(db.transaction("meta").objectStore("meta").get(META_KEY));
    return (meta as IndexMeta | undefined) ?? null;
  }

  async loadAll(): Promise<IndexRecords> {
    const db = await this.open();
    const tx = db.transaction(["issues", "comments", "projects"]);
    const [issues, comments, projects] = await Promise.all([
      requestResult(tx.objectStore("issues").getAll()),
      requestResult(tx.objectStore("comments").getAll()),
      requestResult(tx.objectStore("projects").getAll()),
    ]);
    return {
      issues: issues as SearchIndexIssue[],
      comments: comments as SearchIndexComment[],
      projects: projects as SearchIndexProject[],
    };
  }

  async write(batch: IndexBatch): Promise<void> {
    const db = await this.open();
    const tx = db.transaction(["meta", "issues", "comments", "projects"], "readwrite");
    const done = transactionDone(tx);
    const issues = tx.objectStore("issues");
    const comments = tx.objectStore("comments");
    const projects = tx.objectStore("projects");
    batch.issues?.forEach((record) => issues.put(record));
    batch.comments?.forEach((record) => comments.put(record));
    batch.projects?.forEach((record) => projects.put(record));
    batch.deletedComments?.forEach((id) => comments.delete(id));
    batch.deletedProjects?.forEach((id) => projects.delete(id));
    const byIssue = comments.index("issue_id");
    batch.deletedIssues?.forEach((id) => {
      issues.delete(id);
      const cursor = byIssue.openKeyCursor(IDBKeyRange.only(id));
      cursor.onsuccess = () => {
        const current = cursor.result;
        if (!current) return;
        comments.delete(current.primaryKey);
        current.continue();
      };
    });
    if (batch.meta) tx.objectStore("meta").put(batch.meta, META_KEY);
    await done;
  }

  async reset(meta: IndexMeta): Promise<void> {
    const db = await this.open();
    const tx = db.transaction(["meta", "issues", "comments", "projects"], "readwrite");
    const done = transactionDone(tx);
    tx.objectStore("issues").clear();
    tx.objectStore("comments").clear();
    tx.objectStore("projects").clear();
    tx.objectStore("meta").put(meta, META_KEY);
    await done;
  }

  getIssues(ids: string[]): Promise<Map<string, SearchIndexIssue>> {
    return this.getMany<SearchIndexIssue>("issues", ids);
  }

  getComments(ids: string[]): Promise<Map<string, SearchIndexComment>> {
    return this.getMany<SearchIndexComment>("comments", ids);
  }

  getProjects(ids: string[]): Promise<Map<string, SearchIndexProject>> {
    return this.getMany<SearchIndexProject>("projects", ids);
  }

  private async getMany<T extends { id: string }>(store: string, ids: string[]): Promise<Map<string, T>> {
    const out = new Map<string, T>();
    if (ids.length === 0) return out;
    const db = await this.open();
    const objectStore = db.transaction(store).objectStore(store);
    const records = await Promise.all(ids.map((id) => requestResult(objectStore.get(id))));
    for (const record of records) {
      if (record) out.set((record as T).id, record as T);
    }
    return out;
  }

  async destroy(): Promise<void> {
    this.destroyed = true;
    const pending = this.db;
    this.db = null;
    if (pending) {
      try {
        (await pending).close();
      } catch {
        // Never opened; nothing to close.
      }
    }
    await deleteDatabase(this.name);
  }
}

function deleteDatabase(name: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.deleteDatabase(name);
    request.onsuccess = () => resolve();
    request.onerror = () => reject(request.error);
    // A connection in another context will close on versionchange; the
    // deletion completes once it does.
    request.onblocked = () => undefined;
  });
}

/** The (user, workspace) a local search index database belongs to. */
export interface SearchIndexDatabaseOwner {
  userId: string;
  workspaceId: string;
}

function parseSearchIndexDatabaseName(name: string): SearchIndexDatabaseOwner | null {
  const [prefix, userId, workspaceId, ...rest] = name.split(":");
  if (prefix !== DB_PREFIX || !userId || !workspaceId || rest.length > 0) return null;
  return { userId, workspaceId };
}

/**
 * Deletes the local search index databases on this origin that `shouldDelete`
 * selects. A name this version cannot parse is always deleted.
 */
export async function deleteSearchIndexDatabases(
  shouldDelete: (owner: SearchIndexDatabaseOwner) => boolean,
): Promise<void> {
  if (typeof indexedDB === "undefined" || typeof indexedDB.databases !== "function") return;
  const databases = await indexedDB.databases();
  await Promise.all(
    databases
      .map((db) => db.name)
      .filter((name): name is string => !!name && name.startsWith(`${DB_PREFIX}:`))
      .filter((name) => {
        const owner = parseSearchIndexDatabaseName(name);
        return owner === null || shouldDelete(owner);
      })
      .map((name) => deleteDatabase(name).catch(() => undefined)),
  );
}

/** Deletes every local search index database on this origin. */
export function deleteAllSearchIndexDatabases(): Promise<void> {
  return deleteSearchIndexDatabases(() => true);
}

export class MemoryIndexStore implements IndexStore {
  meta: IndexMeta | null = null;
  readonly issues = new Map<string, SearchIndexIssue>();
  readonly comments = new Map<string, SearchIndexComment>();
  readonly projects = new Map<string, SearchIndexProject>();
  destroyed = false;

  async readMeta(): Promise<IndexMeta | null> {
    return this.meta ? { ...this.meta } : null;
  }

  async loadAll(): Promise<IndexRecords> {
    return {
      issues: [...this.issues.values()],
      comments: [...this.comments.values()],
      projects: [...this.projects.values()],
    };
  }

  async write(batch: IndexBatch): Promise<void> {
    batch.issues?.forEach((record) => this.issues.set(record.id, record));
    batch.comments?.forEach((record) => this.comments.set(record.id, record));
    batch.projects?.forEach((record) => this.projects.set(record.id, record));
    batch.deletedComments?.forEach((id) => this.comments.delete(id));
    batch.deletedProjects?.forEach((id) => this.projects.delete(id));
    for (const id of batch.deletedIssues ?? []) {
      this.issues.delete(id);
      for (const [commentId, comment] of this.comments) {
        if (comment.issue_id === id) this.comments.delete(commentId);
      }
    }
    if (batch.meta) this.meta = { ...batch.meta };
  }

  async reset(meta: IndexMeta): Promise<void> {
    this.issues.clear();
    this.comments.clear();
    this.projects.clear();
    this.meta = { ...meta };
  }

  async getIssues(ids: string[]) {
    return pick(this.issues, ids);
  }

  async getComments(ids: string[]) {
    return pick(this.comments, ids);
  }

  async getProjects(ids: string[]) {
    return pick(this.projects, ids);
  }

  async destroy(): Promise<void> {
    this.destroyed = true;
    this.meta = null;
    this.issues.clear();
    this.comments.clear();
    this.projects.clear();
  }
}

function pick<T>(source: Map<string, T>, ids: string[]): Map<string, T> {
  const out = new Map<string, T>();
  for (const id of ids) {
    const record = source.get(id);
    if (record) out.set(id, record);
  }
  return out;
}
