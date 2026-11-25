import { execFile } from "child_process";
import { homedir } from "os";
import { isAbsolute, resolve as resolvePath } from "path";
import { stat } from "fs/promises";
import { ipcMain, shell } from "electron";
import {
  DEFAULT_OPENER_FALLBACK_IDS,
  hostPlatformFromNode,
  isLocalFileOpenerId,
  type ListLocalFileOpenersResult,
  type LocalFileOpener,
  type LocalFileOpenerId,
  type OpenLocalFileRequest,
  type OpenLocalFileResult,
  type ResolveLocalFileResult,
} from "../shared/open-local-file";

const MAX_PATH_LENGTH = 4096;
const MAX_RESOLVE_BATCH = 200;
const LAUNCH_TIMEOUT_MS = 15_000;

interface EditorSpec {
  id: Exclude<LocalFileOpenerId, "file-manager">;
  name: string;
  darwinAppNames: string[];
  linuxBins: string[];
  winBins: string[];
  winLocalRelPaths: string[];
  winProgramFilesRelPaths: string[];
}

const EDITOR_SPECS: readonly EditorSpec[] = [
  {
    id: "cursor",
    name: "Cursor",
    darwinAppNames: ["Cursor"],
    linuxBins: ["cursor"],
    winBins: ["cursor"],
    winLocalRelPaths: ["Programs\\cursor\\Cursor.exe"],
    winProgramFilesRelPaths: [],
  },
  {
    id: "vscode",
    name: "Visual Studio Code",
    darwinAppNames: ["Visual Studio Code"],
    linuxBins: ["code"],
    winBins: ["code"],
    winLocalRelPaths: ["Programs\\Microsoft VS Code\\Code.exe"],
    winProgramFilesRelPaths: ["Microsoft VS Code\\Code.exe"],
  },
  {
    id: "vscodium",
    name: "VSCodium",
    darwinAppNames: ["VSCodium"],
    linuxBins: ["codium"],
    winBins: ["codium"],
    winLocalRelPaths: ["Programs\\VSCodium\\VSCodium.exe"],
    winProgramFilesRelPaths: [],
  },
  {
    id: "zed",
    name: "Zed",
    darwinAppNames: ["Zed"],
    linuxBins: ["zed"],
    winBins: ["zed"],
    winLocalRelPaths: ["Programs\\Zed\\Zed.exe"],
    winProgramFilesRelPaths: [],
  },
  {
    id: "sublime",
    name: "Sublime Text",
    darwinAppNames: ["Sublime Text"],
    linuxBins: ["subl"],
    winBins: ["subl"],
    winLocalRelPaths: [],
    winProgramFilesRelPaths: [
      "Sublime Text\\sublime_text.exe",
      "Sublime Text 3\\sublime_text.exe",
    ],
  },
  {
    id: "jetbrains-idea",
    name: "IntelliJ IDEA",
    darwinAppNames: ["IntelliJ IDEA"],
    linuxBins: ["idea"],
    winBins: ["idea"],
    winLocalRelPaths: [],
    winProgramFilesRelPaths: [],
  },
  {
    id: "jetbrains-idea-ce",
    name: "IntelliJ IDEA CE",
    darwinAppNames: ["IntelliJ IDEA CE"],
    linuxBins: [],
    winBins: [],
    winLocalRelPaths: [],
    winProgramFilesRelPaths: [],
  },
  {
    id: "jetbrains-webstorm",
    name: "WebStorm",
    darwinAppNames: ["WebStorm"],
    linuxBins: ["webstorm"],
    winBins: ["webstorm"],
    winLocalRelPaths: [],
    winProgramFilesRelPaths: [],
  },
  {
    id: "jetbrains-pycharm",
    name: "PyCharm",
    darwinAppNames: ["PyCharm"],
    linuxBins: ["pycharm"],
    winBins: ["pycharm"],
    winLocalRelPaths: [],
    winProgramFilesRelPaths: [],
  },
  {
    id: "jetbrains-pycharm-ce",
    name: "PyCharm CE",
    darwinAppNames: ["PyCharm CE"],
    linuxBins: [],
    winBins: [],
    winLocalRelPaths: [],
    winProgramFilesRelPaths: [],
  },
  {
    id: "jetbrains-goland",
    name: "GoLand",
    darwinAppNames: ["GoLand"],
    linuxBins: ["goland"],
    winBins: ["goland"],
    winLocalRelPaths: [],
    winProgramFilesRelPaths: [],
  },
  {
    id: "jetbrains-phpstorm",
    name: "PhpStorm",
    darwinAppNames: ["PhpStorm"],
    linuxBins: ["phpstorm"],
    winBins: ["phpstorm"],
    winLocalRelPaths: [],
    winProgramFilesRelPaths: [],
  },
  {
    id: "jetbrains-clion",
    name: "CLion",
    darwinAppNames: ["CLion"],
    linuxBins: ["clion"],
    winBins: ["clion"],
    winLocalRelPaths: [],
    winProgramFilesRelPaths: [],
  },
  {
    id: "jetbrains-rider",
    name: "Rider",
    darwinAppNames: ["Rider"],
    linuxBins: ["rider"],
    winBins: ["rider"],
    winLocalRelPaths: [],
    winProgramFilesRelPaths: [],
  },
  {
    id: "jetbrains-rustrover",
    name: "RustRover",
    darwinAppNames: ["RustRover"],
    linuxBins: ["rustrover"],
    winBins: ["rustrover"],
    winLocalRelPaths: [],
    winProgramFilesRelPaths: [],
  },
];

const FILE_MANAGER_OPENER: LocalFileOpener = {
  id: "file-manager",
  kind: "file_manager",
  name: "File manager",
};

export interface DetectedOpener extends LocalFileOpener {
  darwinAppName?: string;
  executable?: string;
}

export interface OpenLocalFileDeps {
  platform: NodeJS.Platform;
  homedir: () => string;
  env: NodeJS.ProcessEnv;
  pathExists: (path: string) => Promise<boolean>;
  which: (command: string) => Promise<string | null>;
  execFile: (
    file: string,
    args: string[],
  ) => Promise<void>;
  showItemInFolder: (path: string) => void;
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/** Reject renderer-supplied strings that are URLs, relative, or otherwise not a
 *  local filesystem path. Windows drive letters (`C:\...`) are allowed;
 *  `vscode://`, `file://`, and `http://` are not. */
export function isSafeAbsoluteLocalPath(
  value: string,
  platform: NodeJS.Platform = process.platform,
): boolean {
  if (typeof value !== "string" || value.length === 0) return false;
  if (value.length > MAX_PATH_LENGTH) return false;
  if (value.includes("\0")) return false;
  const trimmed = value.trim();
  if (trimmed !== value) return false;
  if (/^[a-zA-Z][a-zA-Z+.-]*:/.test(value) && !/^[a-zA-Z]:[\\/]/.test(value)) {
    return false;
  }
  if (platform === "win32") {
    return /^[a-zA-Z]:[\\/]/.test(value) || value.startsWith("\\\\");
  }
  return isAbsolute(value);
}

export async function defaultPathExists(path: string): Promise<boolean> {
  try {
    await stat(path);
    return true;
  } catch {
    return false;
  }
}

export function defaultWhich(command: string): Promise<string | null> {
  const finder = process.platform === "win32" ? "where" : "which";
  return new Promise((resolve) => {
    execFile(finder, [command], { timeout: 5_000, windowsHide: true }, (err, stdout) => {
      if (err) {
        resolve(null);
        return;
      }
      const first = stdout
        .split(/\r?\n/)
        .map((line) => line.trim())
        .find(Boolean);
      resolve(first ?? null);
    });
  });
}

export function defaultExecFile(file: string, args: string[]): Promise<void> {
  return new Promise((resolve, reject) => {
    execFile(
      file,
      args,
      { timeout: LAUNCH_TIMEOUT_MS, windowsHide: true },
      (err) => {
        if (err) reject(err);
        else resolve();
      },
    );
  });
}

function productionDeps(): OpenLocalFileDeps {
  return {
    platform: process.platform,
    homedir,
    env: process.env,
    pathExists: defaultPathExists,
    which: defaultWhich,
    execFile: defaultExecFile,
    showItemInFolder: (path) => {
      shell.showItemInFolder(path);
    },
  };
}

function localAppData(env: NodeJS.ProcessEnv, home: string): string {
  return env.LOCALAPPDATA || `${home}\\AppData\\Local`;
}

function programFiles(env: NodeJS.ProcessEnv): string {
  return env.ProgramFiles || "C:\\Program Files";
}

async function firstExisting(
  paths: string[],
  pathExists: (path: string) => Promise<boolean>,
): Promise<string | undefined> {
  for (const candidate of paths) {
    if (await pathExists(candidate)) return candidate;
  }
  return undefined;
}

export async function detectInstalledOpeners(
  deps: OpenLocalFileDeps,
): Promise<DetectedOpener[]> {
  const home = deps.homedir();
  const found: DetectedOpener[] = [];
  const usedBins = new Set<string>();

  for (const spec of EDITOR_SPECS) {
    if (deps.platform === "darwin") {
      const appPaths = spec.darwinAppNames.flatMap((appName) => [
        `/Applications/${appName}.app`,
        `${home}/Applications/${appName}.app`,
      ]);
      const appPath = await firstExisting(appPaths, deps.pathExists);
      if (appPath) {
        const appName = spec.darwinAppNames[0];
        found.push({
          id: spec.id,
          kind: "editor",
          name: spec.name,
          darwinAppName: appName,
        });
        continue;
      }
    }

    if (deps.platform === "win32") {
      const local = localAppData(deps.env, home);
      const pf = programFiles(deps.env);
      const exeCandidates = [
        ...spec.winLocalRelPaths.map((rel) => `${local}\\${rel}`),
        ...spec.winProgramFilesRelPaths.map((rel) => `${pf}\\${rel}`),
      ];
      const exe = await firstExisting(exeCandidates, deps.pathExists);
      if (exe) {
        found.push({
          id: spec.id,
          kind: "editor",
          name: spec.name,
          executable: exe,
        });
        continue;
      }
    }

    const bins = deps.platform === "win32" ? spec.winBins : spec.linuxBins;
    let resolved: string | null = null;
    for (const bin of bins) {
      if (usedBins.has(bin)) continue;
      resolved = await deps.which(bin);
      if (resolved) {
        usedBins.add(bin);
        break;
      }
    }
    if (resolved) {
      found.push({
        id: spec.id,
        kind: "editor",
        name: spec.name,
        executable: resolved,
      });
    }
  }

  found.push({ ...FILE_MANAGER_OPENER });
  return found;
}

export function pickDefaultOpenerId(openers: readonly LocalFileOpener[]): LocalFileOpenerId {
  for (const id of DEFAULT_OPENER_FALLBACK_IDS) {
    if (openers.some((opener) => opener.id === id)) return id;
  }
  return "file-manager";
}

export async function resolveLocalFilePath(
  raw: unknown,
  deps: Pick<OpenLocalFileDeps, "platform" | "pathExists">,
): Promise<ResolveLocalFileResult> {
  if (typeof raw !== "string") return { ok: false, reason: "unsafe" };
  if (/^[a-zA-Z][a-zA-Z+.-]*:/.test(raw) && !/^[a-zA-Z]:[\\/]/.test(raw)) {
    return { ok: false, reason: "unsafe" };
  }
  if (!isSafeAbsoluteLocalPath(raw, deps.platform)) {
    return { ok: false, reason: "not_absolute" };
  }
  const normalized = resolvePath(raw);
  if (!isSafeAbsoluteLocalPath(normalized, deps.platform)) {
    return { ok: false, reason: "unsafe" };
  }
  try {
    if (!(await deps.pathExists(normalized))) {
      return { ok: false, reason: "not_found" };
    }
  } catch (err) {
    return { ok: false, reason: "error", error: errorMessage(err) };
  }
  return { ok: true, path: normalized };
}

export async function openLocalFile(
  request: OpenLocalFileRequest,
  deps: OpenLocalFileDeps,
  openers?: DetectedOpener[],
): Promise<OpenLocalFileResult> {
  const resolved = await resolveLocalFilePath(request.path, deps);
  if (!resolved.ok) return resolved;

  const detected = openers ?? (await detectInstalledOpeners(deps));
  const requested = isLocalFileOpenerId(request.openerId)
    ? request.openerId
    : pickDefaultOpenerId(detected);
  const opener = detected.find((item) => item.id === requested);
  if (!opener) {
    return { ok: false, reason: "opener_unavailable" };
  }

  try {
    if (opener.id === "file-manager") {
      deps.showItemInFolder(resolved.path);
      return { ok: true };
    }
    if (deps.platform === "darwin" && opener.darwinAppName) {
      await deps.execFile("open", ["-a", opener.darwinAppName, "--", resolved.path]);
      return { ok: true };
    }
    if (opener.executable) {
      await deps.execFile(opener.executable, [resolved.path]);
      return { ok: true };
    }
    return { ok: false, reason: "opener_unavailable" };
  } catch (err) {
    return { ok: false, reason: "launch_failed", error: errorMessage(err) };
  }
}

function toPublicOpener(opener: DetectedOpener): LocalFileOpener {
  return { id: opener.id, kind: opener.kind, name: opener.name };
}

export function setupOpenLocalFile(deps: OpenLocalFileDeps = productionDeps()): void {
  ipcMain.handle(
    "local-file:list-openers",
    async (): Promise<ListLocalFileOpenersResult> => {
      const openers = await detectInstalledOpeners(deps);
      return {
        openers: openers.map(toPublicOpener),
        platform: hostPlatformFromNode(deps.platform),
      };
    },
  );

  ipcMain.handle(
    "local-file:resolve-many",
    async (_event, paths: unknown): Promise<string[]> => {
      if (!Array.isArray(paths)) return [];
      const slice = paths.slice(0, MAX_RESOLVE_BATCH);
      const existing: string[] = [];
      for (const item of slice) {
        const resolved = await resolveLocalFilePath(item, deps);
        if (resolved.ok && typeof item === "string") existing.push(item);
      }
      return existing;
    },
  );

  ipcMain.handle(
    "local-file:open",
    async (_event, request: unknown): Promise<OpenLocalFileResult> => {
      if (
        typeof request !== "object" ||
        request === null ||
        typeof (request as OpenLocalFileRequest).path !== "string" ||
        typeof (request as OpenLocalFileRequest).openerId !== "string"
      ) {
        return { ok: false, reason: "unsafe" };
      }
      return openLocalFile(request as OpenLocalFileRequest, deps);
    },
  );
}
