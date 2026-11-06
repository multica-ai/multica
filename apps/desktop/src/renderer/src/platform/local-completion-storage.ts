import { completionReadRecordSchema, type CompletionReadRecord } from "@multica/conversation-client/models";
import type { CompletionReadStorage } from "@multica/conversation-client/services";

const DATABASE = "multica-conversation-read-state";
const STORE = "positions";

/** User-owned read positions survive reloads and are never evicted with transcript caches. */
export function createCompletionReadStorage(namespace: () => string, factory: IDBFactory | undefined = globalThis.indexedDB): CompletionReadStorage {
  let database: Promise<IDBDatabase> | undefined;
  const connect = () => {
    if (!factory) return Promise.reject(new Error("Completion read storage is unavailable"));
    return database ??= new Promise<IDBDatabase>((resolve, reject) => {
      const opening = factory.open(DATABASE, 1);
      opening.onupgradeneeded = () => opening.result.createObjectStore(STORE);
      opening.onsuccess = () => {
        const db = opening.result;
        db.onversionchange = () => { db.close(); database = undefined; };
        resolve(db);
      };
      opening.onerror = () => { database = undefined; reject(opening.error); };
      opening.onblocked = () => { database = undefined; reject(new Error("Completion read storage is blocked")); };
    });
  };
  // Services can be constructed before the app has installed its API environment.
  let scope: string | undefined;
  const key = (id: string) => JSON.stringify([scope ??= namespace(), id]);
  const run = async <T>(mode: IDBTransactionMode, operation: (store: IDBObjectStore, resolve: (value: T) => void, reject: (error: unknown) => void) => void): Promise<T> => {
    const db = await connect();
    return new Promise<T>((resolve, reject) => {
      const tx = db.transaction(STORE, mode);
      let result: T;
      tx.oncomplete = () => resolve(result);
      tx.onerror = tx.onabort = () => reject(tx.error ?? new Error("Completion read storage transaction failed"));
      operation(tx.objectStore(STORE), (value) => { result = value; }, reject);
    });
  };
  return {
    read: (id) => run<CompletionReadRecord | null>("readonly", (store, resolve, reject) => {
      const reading = store.get(key(id));
      reading.onsuccess = () => {
        if (reading.result === undefined) { resolve(null); return; }
        const parsed = completionReadRecordSchema.safeParse(reading.result);
        if (parsed.success) resolve(parsed.data);
        else reject(new Error("Invalid completion read record"));
      };
    }),
    write: (id, record) => run<void>("readwrite", (store, resolve) => {
      const parsed = completionReadRecordSchema.parse(record);
      const reading = store.get(key(id));
      reading.onsuccess = () => {
        const old = completionReadRecordSchema.safeParse(reading.result);
        // Concurrent windows may acknowledge different results. Neither read position may regress.
        const previous = old.success && old.data.generation === parsed.generation ? old.data : null;
        const latest = previous && previous.summarySeq > parsed.summarySeq ? previous : parsed;
        store.put({ ...latest, readTurnIndex: Math.max(parsed.readTurnIndex, previous?.readTurnIndex ?? -1),
          ...(previous && previous.completedTurnIndex > latest.completedTurnIndex ? { completedTurnIndex: previous.completedTurnIndex, completedTurnId: previous.completedTurnId } : {}),
        }, key(id));
        resolve();
      };
    }),
  };
}
