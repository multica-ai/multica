import { app, BrowserWindow, ipcMain, shell } from "electron";
import { watch, type FSWatcher } from "fs";
import { mkdir, readdir, readFile, realpath, stat, writeFile } from "fs/promises";
import { dirname, join, relative, sep } from "path";
import {
  THEME_FOLDER_CHANGED_CHANNEL,
  THEME_FOLDER_NAME,
  THEME_FOLDER_OPEN_CHANNEL,
  THEME_FOLDER_READ_CHANNEL,
  THEME_FOLDER_WRITE_CHANNEL,
  type ThemeFolderEntrySnapshot,
  type ThemeFolderFile,
  type ThemeFolderSnapshot,
  type ThemeFolderWriteResult,
} from "../shared/theme-folder";

/** Matches THEME_PACKAGE_MAX_BYTES in @multica/core/appearance. */
const MAX_THEME_BYTES = 1024 * 1024;
const MAX_ENTRIES = 20;
const MAX_FILES = 64;
const MAX_DEPTH = 4;
const DEBOUNCE_MS = 150;
/** Only what a theme can use; anything else in the folder is never read. */
const THEME_FILE = /(^|\/)(manifest\.json|theme\.css)$|\.(woff2?|ttf|otf|png|jpe?g|webp|gif|avif|svg)$/i;
const SKIPPED = new Set(["node_modules", "__MACOSX"]);

class TooLarge extends Error {}

export function themeFolderPath(): string {
  return join(app.getPath("userData"), "themes");
}

async function readFolder(root: string): Promise<ThemeFolderFile[]> {
  const files: ThemeFolderFile[] = [];
  let total = 0;
  const walk = async (dir: string, depth: number) => {
    for (const entry of await readdir(dir, { withFileTypes: true })) {
      if (entry.name.startsWith(".") || SKIPPED.has(entry.name)) continue;
      const path = join(dir, entry.name);
      const isDir = entry.isDirectory() || (entry.isSymbolicLink() && (await stat(path)).isDirectory());
      if (isDir) {
        if (depth < MAX_DEPTH) await walk(path, depth + 1);
        continue;
      }
      const rel = relative(root, path).split(sep).join("/");
      if (!THEME_FILE.test(rel)) continue;
      total += (await stat(path)).size;
      if (total > MAX_THEME_BYTES || files.length >= MAX_FILES) throw new TooLarge();
      files.push({ path: rel, data: new Uint8Array(await readFile(path)) });
    }
  };
  await walk(root, 0);
  return files;
}

async function readEntry(root: string, name: string): Promise<ThemeFolderEntrySnapshot | null> {
  const path = join(root, name);
  try {
    const info = await stat(path);
    if (info.isDirectory()) return { folder: name, files: await readFolder(path) };
    if (!/\.zip$/i.test(name)) return null;
    if (info.size > MAX_THEME_BYTES) throw new TooLarge();
    return { folder: name, files: [{ path: name, data: new Uint8Array(await readFile(path)) }] };
  } catch (err) {
    if (err instanceof TooLarge) {
      return { folder: name, error: `A theme can be at most ${MAX_THEME_BYTES / 1024} KB and ${MAX_FILES} files` };
    }
    return { folder: name, error: err instanceof Error ? err.message : String(err) };
  }
}

/** Every theme in the folder, as raw files for the renderer to check. */
export async function readThemeFolder(root: string): Promise<ThemeFolderSnapshot> {
  await mkdir(root, { recursive: true });
  const names = (await readdir(root))
    .filter((name) => !name.startsWith(".") && !SKIPPED.has(name))
    .sort()
    .slice(0, MAX_ENTRIES);
  const entries = await Promise.all(names.map((name) => readEntry(root, name)));
  return { path: root, entries: entries.filter((entry) => entry !== null) };
}

function isThemeFile(value: unknown): value is ThemeFolderFile {
  if (!value || typeof value !== "object") return false;
  const { path, data } = value as Partial<ThemeFolderFile>;
  return (
    typeof path === "string" &&
    data instanceof Uint8Array &&
    THEME_FILE.test(path) &&
    !path.includes("\\") &&
    !path.includes(":") &&
    path.split("/").every((part) => part !== "" && part !== "." && part !== "..")
  );
}

/** Writes a new theme folder; never replaces an existing one. */
export async function writeThemeFolder(
  root: string,
  name: unknown,
  files: unknown,
): Promise<ThemeFolderWriteResult> {
  if (
    typeof name !== "string" ||
    !THEME_FOLDER_NAME.test(name) ||
    !Array.isArray(files) ||
    files.length === 0 ||
    files.length > MAX_FILES ||
    !files.every(isThemeFile) ||
    files.reduce((sum, file) => sum + file.data.length, 0) > MAX_THEME_BYTES
  ) {
    return { ok: false, reason: "invalid" };
  }
  const target = join(root, name);
  try {
    await mkdir(root, { recursive: true });
    await mkdir(target);
  } catch (err) {
    if ((err as NodeJS.ErrnoException).code === "EEXIST") return { ok: false, reason: "exists" };
    return { ok: false, reason: "error", error: err instanceof Error ? err.message : String(err) };
  }
  try {
    for (const file of files) {
      const path = join(target, ...file.path.split("/"));
      await mkdir(dirname(path), { recursive: true });
      await writeFile(path, file.data);
    }
    return { ok: true };
  } catch (err) {
    return { ok: false, reason: "error", error: err instanceof Error ? err.message : String(err) };
  }
}

/**
 * Watches the folder and each theme in it, following symlinks, so a theme
 * kept in its own repository and linked in reloads too. Calls `onChange`
 * once a burst of writes settles.
 */
export function watchThemeFolder(root: string, onChange: () => void): () => void {
  let watchers: FSWatcher[] = [];
  let timer: ReturnType<typeof setTimeout> | undefined;
  let closed = false;

  const add = (path: string, recursive: boolean) => {
    try {
      const watcher = watch(path, { recursive }, schedule);
      watcher.on("error", () => watcher.close());
      watchers.push(watcher);
    } catch {
      // Unwatchable (removed meanwhile, or out of watch handles): no live reload for it.
    }
  };

  const rewatch = async () => {
    const names = await readdir(root).catch(() => [] as string[]);
    const targets = await Promise.all(
      names
        .filter((name) => !name.startsWith("."))
        .slice(0, MAX_ENTRIES)
        .map(async (name) => {
          const path = join(root, name);
          const info = await stat(path).catch(() => null);
          return info?.isDirectory() ? realpath(path).catch(() => null) : null;
        }),
    );
    if (closed) return;
    for (const watcher of watchers) watcher.close();
    watchers = [];
    add(root, false);
    for (const target of targets) if (target) add(target, true);
  };

  function schedule() {
    clearTimeout(timer);
    timer = setTimeout(() => {
      void rewatch().then(() => {
        if (!closed) onChange();
      });
    }, DEBOUNCE_MS);
  }

  void rewatch();
  return () => {
    closed = true;
    clearTimeout(timer);
    for (const watcher of watchers) watcher.close();
    watchers = [];
  };
}

export function setupThemeFolder(): void {
  const root = themeFolderPath();
  let stopWatching: (() => void) | undefined;

  ipcMain.handle(THEME_FOLDER_READ_CHANNEL, async () => {
    const snapshot = await readThemeFolder(root);
    stopWatching ??= watchThemeFolder(root, () => {
      for (const win of BrowserWindow.getAllWindows()) {
        if (!win.isDestroyed()) win.webContents.send(THEME_FOLDER_CHANGED_CHANNEL);
      }
    });
    return snapshot;
  });

  ipcMain.handle(THEME_FOLDER_OPEN_CHANNEL, async (_event, name?: unknown) => {
    await mkdir(root, { recursive: true });
    const sub = typeof name === "string" && THEME_FOLDER_NAME.test(name) ? join(root, name) : root;
    const error = await shell.openPath(sub);
    return error ? { ok: false, error } : { ok: true };
  });

  ipcMain.handle(THEME_FOLDER_WRITE_CHANNEL, (_event, name: unknown, files: unknown) =>
    writeThemeFolder(root, name, files),
  );

  app.on("will-quit", () => stopWatching?.());
}
