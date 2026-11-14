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

/**
 * Recordings and their transcripts, kept on this device. The index lives in
 * `<root>/history.json`, newest first; each recording is its own file next
 * to it. Writes are serialized so concurrent saves never lose an entry.
 */
export class VoiceHistoryStore {
  private entries: StoredEntry[] = [];
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
    // The app quit while these were being transcribed.
    let interrupted = false;
    for (const entry of this.entries) {
      if (entry.status !== "transcribing") continue;
      entry.status = "interrupted";
      interrupted = true;
    }
    if (interrupted) await this.persist();
  }

  list(): VoiceHistoryEntry[] {
    return this.entries.map(({ id, createdAt, durationMs, status, text }) => ({ id, createdAt, durationMs, status, text }));
  }

  async save(audio: Uint8Array, mimeType: string, durationMs: number, keepDays: number | null): Promise<VoiceHistoryEntry> {
    const id = randomUUID();
    const file = `${id}.${extensionOf(mimeType)}`;
    await writeFile(join(this.options.root, file), audio);
    const entry: StoredEntry = { id, createdAt: this.now(), durationMs, status: "transcribing", text: "", file, mimeType };
    this.entries.unshift(entry);
    await this.prune(keepDays, false);
    await this.persist();
    return this.list()[0]!;
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

  /** Drop entries older than `keepDays` (0 drops all); null keeps them, up to the cap. */
  async prune(keepDays: number | null, persist = true): Promise<void> {
    const cutoff = keepDays === null ? -Infinity : this.now() - keepDays * DAY_MS;
    const keep: StoredEntry[] = [];
    const drop: StoredEntry[] = [];
    for (const entry of this.entries) {
      // A recording still being transcribed is never pruned from under it.
      const expired = entry.status !== "transcribing" && (keepDays === 0 || entry.createdAt < cutoff);
      (expired || keep.length >= MAX_HISTORY_ENTRIES ? drop : keep).push(entry);
    }
    if (drop.length === 0) return;
    this.entries = keep;
    await Promise.all(drop.map((entry) => rm(join(this.options.root, entry.file), { force: true })));
    if (persist) await this.persist();
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
