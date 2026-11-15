import { randomUUID } from "crypto";
import { mkdir, readFile, rename, rm, writeFile } from "fs/promises";
import { join } from "path";
import type {
  VoiceHistoryAudio,
  VoiceHistoryEntry,
  VoiceHistoryStatus,
  VoiceHistoryUpdate,
} from "../../shared/voice";

const INDEX = "history.json";
const RETENTION = "retention.json";
const DAY_MS = 24 * 60 * 60 * 1000;
/** Bounds the folder even when history is kept forever. */
export const MAX_HISTORY_ENTRIES = 1000;
/** A recording is capped at two minutes; compressed it stays far below this. */
export const MAX_HISTORY_AUDIO_BYTES = 16 * 1024 * 1024;

const STATUSES = new Set<VoiceHistoryStatus>(["transcribing", "done", "failed", "interrupted"]);

interface StoredEntry extends VoiceHistoryEntry {
  /** Audio file name inside the history folder. */
  file: string;
  mimeType: string;
}

export interface VoiceHistoryStoreOptions {
  /** Folder for the index and one audio file per recording. */
  root: string;
  onChange: () => void;
  now?: () => number;
}

function extensionOf(mimeType: string): string {
  if (mimeType.includes("ogg")) return "ogg";
  if (mimeType.includes("mp4") || mimeType.includes("aac")) return "m4a";
  if (mimeType.includes("wav")) return "wav";
  return "webm";
}

function isStoredEntry(value: unknown): value is StoredEntry {
  if (!value || typeof value !== "object") return false;
  const entry = value as Record<string, unknown>;
  return (
    typeof entry.id === "string" &&
    typeof entry.createdAt === "number" &&
    typeof entry.durationMs === "number" &&
    STATUSES.has(entry.status as VoiceHistoryStatus) &&
    typeof entry.text === "string" &&
    typeof entry.file === "string" &&
    !entry.file.includes("/") &&
    !entry.file.includes("\\") &&
    typeof entry.mimeType === "string"
  );
}

function isKeepDays(value: unknown): value is number | null {
  return value === null || (typeof value === "number" && Number.isFinite(value) && value >= 0);
}

function publicEntry({ id, createdAt, durationMs, status, text }: StoredEntry): VoiceHistoryEntry {
  return { id, createdAt, durationMs, status, text };
}

/**
 * Recordings and their transcripts, kept on this device. The index lives in
 * `<root>/history.json`, newest first; each recording is its own file next
 * to it. Writes are serialized so concurrent saves never lose an entry.
 *
 * The retention last chosen in Settings is kept in `<root>/retention.json`
 * so old recordings expire on launch and on read, not only when recording.
 */
export class VoiceHistoryStore {
  private entries: StoredEntry[] = [];
  /** Undefined until a retention has been chosen; nothing expires by age. */
  private keepDays: number | null | undefined;
  private writes: Promise<void> = Promise.resolve();
  private readonly now: () => number;

  constructor(private readonly options: VoiceHistoryStoreOptions) {
    this.now = options.now ?? Date.now;
  }

  async init(): Promise<void> {
    await mkdir(this.options.root, { recursive: true });
    try {
      const raw: unknown = JSON.parse(await readFile(join(this.options.root, INDEX), "utf8"));
      this.entries = Array.isArray(raw) ? raw.filter(isStoredEntry) : [];
    } catch {
      this.entries = [];
    }
    try {
      const raw: unknown = JSON.parse(await readFile(join(this.options.root, RETENTION), "utf8"));
      const keepDays = (raw as { keepDays?: unknown } | null)?.keepDays;
      if (isKeepDays(keepDays)) this.keepDays = keepDays;
    } catch {
      this.keepDays = undefined;
    }
    // The app quit while these were being transcribed.
    let interrupted = false;
    for (const entry of this.entries) {
      if (entry.status !== "transcribing") continue;
      entry.status = "interrupted";
      interrupted = true;
    }
    const expired = await this.expire(false);
    if (interrupted || expired) await this.persist();
  }

  list(): VoiceHistoryEntry[] {
    return this.entries.map(publicEntry);
  }

  async save(audio: Uint8Array, mimeType: string, durationMs: number, keepDays: number | null): Promise<VoiceHistoryEntry> {
    const id = randomUUID();
    const file = `${id}.${extensionOf(mimeType)}`;
    await writeFile(join(this.options.root, file), audio);
    const entry: StoredEntry = { id, createdAt: this.now(), durationMs, status: "transcribing", text: "", file, mimeType };
    this.entries.unshift(entry);
    await this.rememberRetention(keepDays);
    await this.expire(false);
    await this.persist();
    return publicEntry(entry);
  }

  /** Remember the retention chosen in Settings and apply it now. */
  async setRetention(keepDays: number | null): Promise<void> {
    await this.rememberRetention(keepDays);
    await this.expire();
  }

  /** Drop entries past the remembered retention. Resolves whether any were dropped. */
  async expire(persist = true): Promise<boolean> {
    const keepDays = this.keepDays;
    const cutoff = keepDays === undefined || keepDays === null ? -Infinity : this.now() - keepDays * DAY_MS;
    return this.drop((entry, index) => keepDays === 0 || entry.createdAt < cutoff || index >= MAX_HISTORY_ENTRIES, persist);
  }

  /**
   * Delete every recording, including one still being transcribed; its
   * late result finds no entry and is discarded.
   */
  async clear(): Promise<void> {
    await this.drop(() => true);
  }

  async update(id: string, patch: VoiceHistoryUpdate): Promise<void> {
    const entry = this.entries.find((e) => e.id === id);
    if (!entry || !STATUSES.has(patch.status)) return;
    entry.status = patch.status;
    if (typeof patch.text === "string") entry.text = patch.text;
    await this.persist();
  }

  async audio(id: string): Promise<VoiceHistoryAudio | null> {
    const entry = this.entries.find((e) => e.id === id);
    if (!entry) return null;
    try {
      return { audio: new Uint8Array(await readFile(join(this.options.root, entry.file))), mimeType: entry.mimeType };
    } catch {
      return null;
    }
  }

  async remove(id: string): Promise<void> {
    const entry = this.entries.find((e) => e.id === id);
    if (!entry) return;
    this.entries = this.entries.filter((e) => e !== entry);
    await rm(join(this.options.root, entry.file), { force: true });
    await this.persist();
  }

  private async rememberRetention(keepDays: number | null): Promise<void> {
    if (this.keepDays === keepDays) return;
    this.keepDays = keepDays;
    const path = join(this.options.root, RETENTION);
    await writeFile(`${path}.tmp`, JSON.stringify({ keepDays }));
    await rename(`${path}.tmp`, path);
  }

  private async drop(shouldDrop: (entry: StoredEntry, index: number) => boolean, persist = true): Promise<boolean> {
    const dropped = this.entries.filter(shouldDrop);
    if (dropped.length === 0) return false;
    const gone = new Set(dropped);
    this.entries = this.entries.filter((entry) => !gone.has(entry));
    await Promise.all(dropped.map((entry) => rm(join(this.options.root, entry.file), { force: true })));
    if (persist) await this.persist();
    return true;
  }

  private persist(): Promise<void> {
    const snapshot = JSON.stringify(this.entries);
    const path = join(this.options.root, INDEX);
    this.writes = this.writes
      .catch(() => {})
      .then(async () => {
        await writeFile(`${path}.tmp`, snapshot);
        await rename(`${path}.tmp`, path);
      });
    const written = this.writes;
    void written.then(this.options.onChange, () => {});
    return written;
  }
}
