// IndexedDB-backed draft storage for the PWA Web Share Target flow.
//
// The browser dispatches the OS share-sheet POST to a service worker, which
// stashes the form data here under a one-time-use token, then redirects the
// user to a GET page that reads it back. IDB is the right fit:
//
//   * Survives cold start of the PWA (the share intent often launches it).
//   * Survives a login redirect (token in the URL keys back to the entry).
//   * Holds Blobs natively, so file attachments don't need re-encoding.
//
// The same module is imported by the SW (WebWorker scope) and by the page
// (DOM scope) — every API used here lives on WindowOrWorkerGlobalScope.

const DB_NAME = "multica-share-drafts";
const DB_VERSION = 1;
const STORE = "drafts";

// One hour is the TTL the issue body settled on. Drafts older than this are
// pruned opportunistically (on read/write) — there is no background cleanup
// because the SW only wakes on share or push events.
const TTL_MS = 60 * 60 * 1000;

export interface ShareDraftFile {
  // The original filename as the source app sent it. Browsers vary in how
  // they populate this — on iOS shared images frequently arrive as
  // `image.jpg`; on Android with Gboard a screenshot may be `screenshot.png`.
  name: string;
  type: string;
  blob: Blob;
}

export interface ShareDraft {
  token: string;
  title: string;
  text: string;
  url: string;
  files: ShareDraftFile[];
  createdAt: number;
}

function openDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION);
    req.onupgradeneeded = () => {
      const db = req.result;
      if (!db.objectStoreNames.contains(STORE)) {
        db.createObjectStore(STORE, { keyPath: "token" });
      }
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error ?? new Error("indexedDB open failed"));
  });
}

function tx<T>(
  db: IDBDatabase,
  mode: IDBTransactionMode,
  fn: (store: IDBObjectStore) => IDBRequest<T> | Promise<T>,
): Promise<T> {
  return new Promise((resolve, reject) => {
    const transaction = db.transaction(STORE, mode);
    const store = transaction.objectStore(STORE);
    const out = fn(store);
    if (out instanceof IDBRequest) {
      out.onsuccess = () => resolve(out.result);
      out.onerror = () => reject(out.error ?? new Error("idb request failed"));
      return;
    }
    out.then(resolve, reject);
  });
}

// Cryptographically random tokens — these end up in the URL and are the only
// authorization a user has to read back the (potentially private) draft they
// just shared. crypto.randomUUID is available in both the worker and window
// scopes on every browser that supports Web Share Target at all.
export function newDraftToken(): string {
  return crypto.randomUUID();
}

export async function saveShareDraft(draft: ShareDraft): Promise<void> {
  const db = await openDB();
  try {
    await tx(db, "readwrite", (store) => store.put(draft));
    await pruneExpiredInTransaction(db);
  } finally {
    db.close();
  }
}

export async function loadShareDraft(token: string): Promise<ShareDraft | null> {
  const db = await openDB();
  try {
    const result = await tx<ShareDraft | undefined>(db, "readonly", (store) =>
      store.get(token) as IDBRequest<ShareDraft | undefined>,
    );
    if (!result) return null;
    if (Date.now() - result.createdAt > TTL_MS) {
      await deleteShareDraft(token);
      return null;
    }
    return result;
  } finally {
    db.close();
  }
}

export async function deleteShareDraft(token: string): Promise<void> {
  const db = await openDB();
  try {
    await tx(db, "readwrite", (store) => store.delete(token));
  } finally {
    db.close();
  }
}

async function pruneExpiredInTransaction(db: IDBDatabase): Promise<void> {
  const cutoff = Date.now() - TTL_MS;
  return new Promise((resolve, reject) => {
    const transaction = db.transaction(STORE, "readwrite");
    const store = transaction.objectStore(STORE);
    const cursorReq = store.openCursor();
    cursorReq.onerror = () => reject(cursorReq.error ?? new Error("cursor failed"));
    cursorReq.onsuccess = () => {
      const cursor = cursorReq.result;
      if (!cursor) {
        resolve();
        return;
      }
      const value = cursor.value as ShareDraft;
      if (value.createdAt < cutoff) {
        cursor.delete();
      }
      cursor.continue();
    };
  });
}
