import type { ConversationSnapshotStore } from "@multica/conversation-client/services";
import { localV2SnapshotSchema } from "@multica/core/local-conversation/v2";

const DATABASE = "multica-local-v2-snapshots";
const STORE = "snapshots";
const MAX_SNAPSHOTS = 40;


const done = <T>(request: IDBRequest<T>) => new Promise<T>((resolve, reject) => {
  request.onsuccess = () => resolve(request.result);
  request.onerror = () => reject(request.error);
});

function open(factory: IDBFactory): Promise<IDBDatabase> {
  const request = factory.open(DATABASE, 1);
  request.onupgradeneeded = () => {
    request.result.createObjectStore(STORE, { keyPath: "key" }).createIndex("savedAt", "savedAt");
  };
  return done(request);
}

/** Storage is an optimization: every failure reads as "nothing stored". */
export function createLocalV2SnapshotStore(factory: IDBFactory | undefined = globalThis.indexedDB): ConversationSnapshotStore {
  let database: Promise<IDBDatabase> | undefined;
  const connect = () => {
    if (!factory) return Promise.reject(new Error("IndexedDB is unavailable"));
    return database ??= open(factory).catch((error: unknown) => { database = undefined; throw error; });
  };
  return {
    async read(key) {
      try {
        const db = await connect();
        const row = await done(db.transaction(STORE).objectStore(STORE).get(key)) as { snapshot?: unknown } | undefined;
        const parsed = localV2SnapshotSchema.safeParse(row?.snapshot);
        return parsed.success ? parsed.data : null;
      } catch { return null; }
    },
    async write(key, snapshot) {
      try {
        const db = await connect();
        const store = db.transaction(STORE, "readwrite").objectStore(STORE);
        await done(store.put({ key, snapshot, savedAt: Date.now() }));
        const excess = await done(store.count()) - MAX_SNAPSHOTS;
        if (excess <= 0) return;
        const oldest = await done(db.transaction(STORE).objectStore(STORE).index("savedAt").getAllKeys(null, excess));
        const removing = db.transaction(STORE, "readwrite").objectStore(STORE);
        await Promise.all(oldest.map((old) => done(removing.delete(old))));
      } catch { /* Next read fetches the conversation, as it always can. */ }
    },
  };
}

/** Removes stored conversations when the account signs out. */
export function deleteLocalV2Snapshots(factory: IDBFactory | undefined = globalThis.indexedDB): Promise<void> {
  if (!factory) return Promise.resolve();
  return done(factory.deleteDatabase(DATABASE)).then(() => undefined, () => undefined);
}
