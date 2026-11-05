import {
  storedSubmissionSchema,
  type StoredSubmission,
  type SubmissionStorage,
} from "@multica/core/local-conversation/submission-storage";
import type { LocalSubmission } from "@multica/core/local-conversation/submission";

const DATABASE = "multica-local-submissions";
const ENTRIES = "entries";
const META = "meta";
const request = <T>(value: IDBRequest<T>) =>
  new Promise<T>((resolve, reject) => {
    value.onsuccess = () => resolve(value.result);
    value.onerror = () => reject(value.error);
  });
const completed = (tx: IDBTransaction) =>
  new Promise<void>((resolve, reject) => {
    tx.oncomplete = () => resolve();
    tx.onabort = () =>
      reject(tx.error ?? new Error("Submission storage transaction aborted"));
    tx.onerror = () =>
      reject(tx.error ?? new Error("Submission storage failed"));
  });

/** Reliable records are never subject to the conversation snapshot LRU. */
export function createSubmissionStorage(
  namespace: string,
  factory: IDBFactory | undefined = globalThis.indexedDB,
): SubmissionStorage {
  let database: Promise<IDBDatabase> | undefined;
  const connect = () => {
    if (!factory)
      return Promise.reject(
        new Error("Reliable submission storage is unavailable"),
      );
    return (database ??= new Promise<IDBDatabase>((resolve, reject) => {
      const opening = factory.open(DATABASE, 1);
      opening.onupgradeneeded = () => {
        opening.result.createObjectStore(ENTRIES);
        opening.result.createObjectStore(META);
      };
      opening.onsuccess = () => {
        opening.result.onversionchange = () => {
          opening.result.close();
          database = undefined;
        };
        resolve(opening.result);
      };
      opening.onerror = () => {
        database = undefined;
        reject(opening.error);
      };
    }));
  };
  const key = (id: string) => JSON.stringify([namespace, id]);
  const mutate = async (
    id: string,
    change: (value: unknown, sequence: number) => StoredSubmission | undefined,
  ) => {
    const db = await connect();
    const tx = db.transaction([ENTRIES, META], "readwrite",{durability:"strict"});
    const commit = completed(tx);
    try {
      const entries = tx.objectStore(ENTRIES),
        meta = tx.objectStore(META);
      const [old, counter] = await Promise.all([
        request(entries.get(key(id))),
        request(meta.get(namespace)),
      ]);
      const seq = typeof counter === "number" ? counter : 0;
      const value = change(old, seq);
      if (value) {
        const parsed = storedSubmissionSchema.parse(value);
        entries.put(parsed, key(id));
        if (parsed.sequence > seq) meta.put(parsed.sequence, namespace);
      } else entries.delete(key(id));
      await commit;
      return value;
    } catch (error) {
      try {
        tx.abort();
      } catch {
        // The transaction may have already aborted.
      }
      await commit.catch(() => {});
      throw error;
    }
  };
  return {
    async read() {
      const db = await connect();
      const tx = db.transaction(ENTRIES);
      const commit = completed(tx);
      const values = await request(
        tx
          .objectStore(ENTRIES)
          .getAll(IDBKeyRange.bound(key(""), key("\uffff"))),
      );
      await commit;
      return values
        .map((value) => storedSubmissionSchema.parse(value))
        .sort((a, b) => a.sequence - b.sequence);
    },
    async put(record) {
      let saved: StoredSubmission | undefined;
      await mutate(record.requestId, (old, seq) => {
        const previous = storedSubmissionSchema.safeParse(old);
        saved = storedSubmissionSchema.parse({
          ...record,
          sequence: previous.success ? previous.data.sequence : seq + 1,
          ...(previous.success && previous.data.stage === "confirmed"
            ? { stage: "confirmed" }
            : {}),
          ...(previous.success &&
          previous.data.authority &&
          (!record.authority ||
            previous.data.authority.revision >= record.authority.revision)
            ? { authority: previous.data.authority }
            : {}),
        });
        return saved;
      });
      return saved!;
    },
    async confirm(id: string, authority?: LocalSubmission) {
      await mutate(id, (old) => {
        const previous = storedSubmissionSchema.safeParse(old);
        if (!previous.success) {
          if (old !== undefined)
            throw new Error("Stored submission is invalid");
          return undefined;
        }
        if (
          previous.data.authority &&
          authority &&
          previous.data.authority.revision > authority.revision
        )
          return previous.data;
        return {
          ...previous.data,
          stage: "confirmed",
          ...(authority ? { authority } : {}),
        };
      });
    },
    async remove(id) {
      await mutate(id, () => undefined);
    },
  };
}

export function createSubmissionAssets(
  namespace: string,
  factory: IDBFactory | undefined = globalThis.indexedDB,
) {
  let database: Promise<IDBDatabase> | undefined;
  const connect = () =>
    (database ??= new Promise<IDBDatabase>((resolve, reject) => {
      if (!factory) {
        reject(new Error("Reliable attachment storage is unavailable"));
        return;
      }
      const opening = factory.open(`${DATABASE}-assets`, 1);
      opening.onupgradeneeded = () =>
        opening.result.createObjectStore("assets");
      opening.onsuccess = () => resolve(opening.result);
      opening.onerror = () => {
        database = undefined;
        reject(opening.error);
      };
    }));
  const key = (id: string) => JSON.stringify([namespace, id]);
  return {
    async put(id: string, file: File) {
      const db = await connect();
      const tx = db.transaction("assets", "readwrite",{durability:"strict"});
      const done = completed(tx);
      tx.objectStore("assets").put(
        {
          blob: file,
          name: file.name,
          type: file.type,
          lastModified: file.lastModified,
        },
        key(id),
      );
      await done;
    },
    async get(id: string): Promise<File | undefined> {
      const db = await connect();
      const tx = db.transaction("assets");
      const done = completed(tx);
      const value: unknown = await request(
        tx.objectStore("assets").get(key(id)),
      );
      await done;
      if (value === undefined) return;
      if (
        !value ||
        typeof value !== "object" ||
        !("blob" in value) ||
        !(value.blob instanceof Blob) ||
        !("name" in value) ||
        typeof value.name !== "string"
      )
        throw new Error("Stored attachment is invalid");
      return new File([value.blob], value.name, { type: value.blob.type });
    },
    async remove(id: string) {
      const db = await connect();
      const tx = db.transaction("assets", "readwrite",{durability:"strict"});
      const done = completed(tx);
      tx.objectStore("assets").delete(key(id));
      await done;
    },
  };
}
