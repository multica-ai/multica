// Desktop-only helpers for opening a run-Diff file in a local editor or
// the OS file manager. Web renderers have no `desktopAPI` and degrade to
// "not supported" so settings can show the desktop-only row and the Diff
// filename row can hide the open button.

export type LocalFileOpenerId =
  | "cursor"
  | "vscode"
  | "vscodium"
  | "zed"
  | "sublime"
  | "jetbrains-idea"
  | "jetbrains-idea-ce"
  | "jetbrains-webstorm"
  | "jetbrains-pycharm"
  | "jetbrains-pycharm-ce"
  | "jetbrains-goland"
  | "jetbrains-phpstorm"
  | "jetbrains-clion"
  | "jetbrains-rider"
  | "jetbrains-rustrover"
  | "file-manager";

export type LocalFileOpenerKind = "editor" | "file_manager";

export type LocalFileHostPlatform = "darwin" | "win32" | "linux";

export interface LocalFileOpener {
  id: LocalFileOpenerId;
  kind: LocalFileOpenerKind;
  name: string;
}

export interface ListLocalFileOpenersResult {
  openers: LocalFileOpener[];
  platform: LocalFileHostPlatform;
}

export type OpenLocalFileResult =
  | { ok: true }
  | {
      ok: false;
      reason:
        | "not_absolute"
        | "not_found"
        | "unsafe"
        | "opener_unavailable"
        | "launch_failed"
        | "error"
        | "unsupported";
      error?: string;
    };

const DEFAULT_OPENER_FALLBACK_IDS: readonly LocalFileOpenerId[] = [
  "cursor",
  "vscode",
  "file-manager",
];

interface DesktopOpenLocalFileAPI {
  listLocalFileOpeners?: () => Promise<ListLocalFileOpenersResult>;
  resolveLocalFiles?: (paths: string[]) => Promise<string[]>;
  openLocalFile?: (
    path: string,
    openerId: string,
  ) => Promise<OpenLocalFileResult>;
}

function readDesktopAPI(): DesktopOpenLocalFileAPI | undefined {
  if (typeof window === "undefined") return undefined;
  return (window as unknown as { desktopAPI?: DesktopOpenLocalFileAPI })
    .desktopAPI;
}

export function isOpenLocalFileSupported(): boolean {
  const api = readDesktopAPI();
  return typeof api?.listLocalFileOpeners === "function";
}

export async function listLocalFileOpeners(): Promise<ListLocalFileOpenersResult | null> {
  const api = readDesktopAPI();
  if (!api?.listLocalFileOpeners) return null;
  return api.listLocalFileOpeners();
}

export async function resolveLocalFiles(paths: string[]): Promise<string[]> {
  const api = readDesktopAPI();
  if (!api?.resolveLocalFiles || paths.length === 0) return [];
  return api.resolveLocalFiles(paths);
}

export async function openLocalFile(
  path: string,
  openerId: string,
): Promise<OpenLocalFileResult> {
  const api = readDesktopAPI();
  if (!api?.openLocalFile) return { ok: false, reason: "unsupported" };
  return api.openLocalFile(path, openerId);
}

export function pickEffectiveOpener(
  openers: readonly LocalFileOpener[],
  preferredId: string | null,
): LocalFileOpener | undefined {
  if (preferredId) {
    const match = openers.find((opener) => opener.id === preferredId);
    if (match) return match;
  }
  for (const id of DEFAULT_OPENER_FALLBACK_IDS) {
    const match = openers.find((opener) => opener.id === id);
    if (match) return match;
  }
  return openers[0];
}

/** True for a native absolute filesystem path. Rejects editor/file URLs so
 *  the renderer never asks main to open `vscode://` or `file://`. */
export function isAbsoluteLocalPath(path: string): boolean {
  if (!path || path.includes("\0")) return false;
  if (/^[a-zA-Z][a-zA-Z+.-]*:/.test(path) && !/^[a-zA-Z]:[\\/]/.test(path)) {
    return false;
  }
  if (path.startsWith("/") && !path.startsWith("//")) return true;
  if (/^[a-zA-Z]:[\\/]/.test(path)) return true;
  if (path.startsWith("\\\\")) return true;
  return false;
}

function joinWorkdir(workdir: string, relative: string): string {
  const trimmedWorkdir = workdir.replace(/[/\\]+$/, "");
  const trimmedRel = relative.replace(/^[/\\]+/, "");
  const win = !workdir.startsWith("/") && (/^[a-zA-Z]:[\\/]/.test(workdir) || workdir.includes("\\"));
  if (win) {
    return `${trimmedWorkdir}\\${trimmedRel.replace(/\//g, "\\")}`;
  }
  return `${trimmedWorkdir}/${trimmedRel.replace(/\\/g, "/")}`;
}

/**
 * Path the open button would hand to the desktop shell.
 * Absolute edit paths are used as-is; relative paths join this run's workdir
 * when that workdir is itself absolute. Anything else (web, remote, URL) is
 * not a local file and must not show the button.
 */
export function resolveLocalFileCandidate(
  editPath: string,
  workdir?: string,
): string | null {
  const path = editPath.trim();
  if (!path) return null;
  if (isAbsoluteLocalPath(path)) return path;
  const root = workdir?.trim();
  if (!root || !isAbsoluteLocalPath(root)) return null;
  if (path.includes("\0") || /[a-zA-Z][a-zA-Z+.-]*:/.test(path)) return null;
  return joinWorkdir(root, path);
}

/** Label the daemon collector gives the run workdir's own root; every other
 *  root label is the root's path relative to the run workdir. */
export const RUN_ROOT_SELF_LABEL = "workdir";

/**
 * Path of a persisted run file change relative to the run workdir, from the
 * collection's root label plus the root-relative file path. Labels that could
 * escape or re-anchor the workdir (`..`, absolute) return null instead of
 * silently pointing somewhere else on this machine.
 */
export function joinRunRootRelativePath(
  rootLabel: string,
  filePath: string,
): string | null {
  const label = rootLabel.trim().replaceAll("\\", "/");
  const rel = filePath.trim().replaceAll("\\", "/");
  if (!rel || rel.includes("\0") || isAbsoluteLocalPath(rel)) return null;
  const parts: string[] = [];
  if (label !== RUN_ROOT_SELF_LABEL) {
    if (!label || label.includes("\0") || isAbsoluteLocalPath(label)) {
      return null;
    }
    const segments = label.split("/").filter((s) => s !== "" && s !== ".");
    if (segments.length === 0 || segments.some((s) => s === "..")) return null;
    parts.push(...segments);
  }
  const fileSegments = rel.split("/").filter((s) => s !== "");
  if (fileSegments.length === 0 || fileSegments.some((s) => s === "..")) {
    return null;
  }
  parts.push(...fileSegments);
  return parts.join("/");
}

/**
 * Absolute local path the open button would use for a persisted run file
 * change: the root-relative path joined onto this run's workdir. Null (web,
 * remote, unknown roots) means the button must not show.
 */
export function resolveRunFileChangeCandidate(
  rootLabel: string,
  filePath: string,
  workdir?: string,
): string | null {
  const rel = joinRunRootRelativePath(rootLabel, filePath);
  if (!rel) return null;
  return resolveLocalFileCandidate(rel, workdir);
}
