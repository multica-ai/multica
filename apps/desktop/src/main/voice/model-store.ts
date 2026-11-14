import { createHash } from "crypto";
import { createReadStream, createWriteStream } from "fs";
import { mkdir, readFile, rename, rm, stat, writeFile } from "fs/promises";
import { dirname, join } from "path";
import { once } from "events";
import type { VoiceModelInfo } from "../../shared/voice";
import {
  MODEL_MIRRORS,
  VOICE_MODELS,
  modelSize,
  type CatalogFile,
  type CatalogModel,
} from "./catalog";

const MARKER = "model.json";
const PROGRESS_INTERVAL_MS = 200;

type Fetch = (url: string, init: { signal: AbortSignal; headers: Record<string, string> }) => Promise<Response>;

export interface ModelStoreOptions {
  /** Folder that holds one subfolder per installed model. */
  root: string;
  fetch: Fetch;
  onChange: () => void;
  catalog?: readonly CatalogModel[];
  mirrors?: readonly string[];
}

interface Download {
  controller: AbortController;
  /** Bytes on disk per file name. */
  bytes: Map<string, number>;
}

async function sizeOf(path: string): Promise<number> {
  try {
    return (await stat(path)).size;
  } catch {
    return 0;
  }
}

async function sha256Of(path: string): Promise<string> {
  const hash = createHash("sha256");
  for await (const chunk of createReadStream(path)) hash.update(chunk as Buffer);
  return hash.digest("hex");
}

/**
 * Installed speech models and their downloads. A model is installed once all
 * its files are verified and moved into `<root>/<id>` with a marker naming
 * the pinned revision; partial files wait in `<root>/.partial/<id>` so a
 * failed download resumes where it stopped.
 */
export class ModelStore {
  private readonly catalog: readonly CatalogModel[];
  private readonly mirrors: readonly string[];
  private readonly installed = new Set<string>();
  private readonly downloads = new Map<string, Download>();
  private readonly errors = new Map<string, string>();
  private lastProgress = 0;

  constructor(private readonly options: ModelStoreOptions) {
    this.catalog = options.catalog ?? VOICE_MODELS;
    this.mirrors = options.mirrors ?? MODEL_MIRRORS;
  }

  /** Read which models are installed. */
  async init(): Promise<void> {
    await Promise.all(
      this.catalog.map(async (model) => {
        try {
          const marker = JSON.parse(await readFile(join(this.modelDir(model.id), MARKER), "utf8")) as {
            revision?: unknown;
          };
          if (marker.revision === model.revision) this.installed.add(model.id);
        } catch {
          // Not installed, or installed from another revision.
        }
      }),
    );
  }

  modelDir(id: string): string {
    return join(this.options.root, id);
  }

  isReady(id: string): boolean {
    return this.installed.has(id);
  }

  list(): VoiceModelInfo[] {
    return this.catalog.map((model) => {
      const download = this.downloads.get(model.id);
      let received = 0;
      download?.bytes.forEach((bytes) => (received += bytes));
      const error = this.errors.get(model.id);
      return {
        id: model.id,
        name: model.name,
        author: model.author,
        licenseUrl: model.licenseUrl,
        languages: [...model.languages],
        languageHint: model.languageHint,
        ...(model.highlight ? { highlight: model.highlight } : {}),
        sizeBytes: modelSize(model),
        status: download ? "downloading" : this.installed.has(model.id) ? "ready" : "absent",
        receivedBytes: received,
        ...(error ? { error } : {}),
      };
    });
  }

  /** Download and install a model; resolves once it is installed or has failed. */
  async download(id: string): Promise<void> {
    const model = this.catalog.find((entry) => entry.id === id);
    if (!model || this.installed.has(id) || this.downloads.has(id)) return;
    const download: Download = { controller: new AbortController(), bytes: new Map() };
    this.downloads.set(id, download);
    this.errors.delete(id);
    this.options.onChange();
    const partialDir = join(this.options.root, ".partial", id);
    try {
      await mkdir(partialDir, { recursive: true });
      for (const file of model.files) {
        await mkdir(dirname(join(partialDir, file.name)), { recursive: true });
        download.bytes.set(file.name, await sizeOf(join(partialDir, file.name)));
      }
      for (const file of model.files) {
        await this.fetchFile(model, file, join(partialDir, file.name), download);
      }
      download.controller.signal.throwIfAborted();
      const dir = this.modelDir(id);
      await rm(dir, { recursive: true, force: true });
      await rename(partialDir, dir);
      await writeFile(join(dir, MARKER), JSON.stringify({ id, revision: model.revision }));
      this.installed.add(id);
    } catch (err) {
      if (download.controller.signal.aborted) {
        await rm(partialDir, { recursive: true, force: true });
      } else {
        this.errors.set(id, err instanceof Error ? err.message : String(err));
      }
    } finally {
      this.downloads.delete(id);
      this.options.onChange();
    }
  }

  /** Stop a download and throw away what it fetched. */
  cancel(id: string): void {
    this.downloads.get(id)?.controller.abort();
  }

  async remove(id: string): Promise<void> {
    this.cancel(id);
    this.installed.delete(id);
    this.errors.delete(id);
    await rm(this.modelDir(id), { recursive: true, force: true });
    this.options.onChange();
  }

  private async fetchFile(model: CatalogModel, file: CatalogFile, dest: string, download: Download) {
    const { signal } = download.controller;
    let lastError: unknown;
    for (const base of this.mirrors) {
      let have = await sizeOf(dest);
      if (have > file.size) {
        await rm(dest, { force: true });
        have = 0;
      }
      if (have === file.size) break;
      try {
        const url = `${base}/${model.repo}/resolve/${model.revision}/${file.name}`;
        const response = await this.options.fetch(url, {
          signal,
          headers: have > 0 ? { Range: `bytes=${have}-` } : {},
        });
        if (!response.ok || !response.body) throw new Error(`HTTP ${response.status} from ${new URL(url).host}`);
        if (response.status !== 206) have = 0;
        download.bytes.set(file.name, have);
        const out = createWriteStream(dest, { flags: have > 0 ? "a" : "w" });
        try {
          for await (const chunk of response.body as unknown as AsyncIterable<Uint8Array>) {
            signal.throwIfAborted();
            if (!out.write(chunk)) await once(out, "drain");
            have += chunk.byteLength;
            download.bytes.set(file.name, have);
            this.progress();
          }
        } finally {
          out.end();
          await once(out, "close");
        }
        if (have === file.size) break;
        lastError = new Error(`${file.name} ended early`);
      } catch (err) {
        if (signal.aborted) throw err;
        lastError = err;
      }
    }
    if ((await sizeOf(dest)) !== file.size) throw lastError ?? new Error(`${file.name} is incomplete`);
    if ((await sha256Of(dest)) !== file.sha256) {
      await rm(dest, { force: true });
      download.bytes.set(file.name, 0);
      throw new Error(`${file.name} failed its checksum`);
    }
  }

  private progress() {
    const now = Date.now();
    if (now - this.lastProgress < PROGRESS_INTERVAL_MS) return;
    this.lastProgress = now;
    this.options.onChange();
  }
}
