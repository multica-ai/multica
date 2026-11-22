import { app, BrowserWindow, ipcMain } from "electron";
import { spawn, type ChildProcess } from "child_process";
import { constants, createWriteStream } from "fs";
import { access, chmod, copyFile, mkdir, mkdtemp, readdir, rm } from "fs/promises";
import { homedir, tmpdir } from "os";
import { delimiter, join } from "path";
import { Readable } from "stream";
import { pipeline } from "stream/promises";
import type { ReadableStream as WebReadableStream } from "stream/web";
import { randomUUID } from "crypto";
import { daemonSpawnEnv } from "./daemon-spawn-env";
import {
  CODING_TOOL_IDS,
  CODING_TOOLS_CANCEL_CHANNEL,
  CODING_TOOLS_EVENT_CHANNEL,
  CODING_TOOLS_PROBE_CHANNEL,
  CODING_TOOLS_START_CHANNEL,
  CODING_TOOLS_SUBMIT_CODE_CHANNEL,
  isCodingToolId,
  type CodingToolError,
  type CodingToolErrorCode,
  type CodingToolId,
  type CodingToolPhase,
  type CodingToolProbe,
  type CodingToolRunEvent,
  type CodingToolRunKind,
  type CodingToolStartResult,
} from "../shared/coding-tools";

/** The binary each tool installs; the daemon looks for the same names. */
const BINARIES: Record<CodingToolId, string> = {
  claude: "claude",
  codex: "codex",
  cursor: "cursor-agent",
};

const INSTALL_SCRIPTS: Partial<Record<CodingToolId, string>> = {
  claude: "https://claude.ai/install.sh",
  cursor: "https://cursor.com/install",
};

const LOGIN_ARGS: Record<CodingToolId, string[]> = {
  claude: ["auth", "login"],
  codex: ["login"],
  cursor: ["login"],
};

const STATUS_ARGS: Record<CodingToolId, string[]> = {
  claude: ["auth", "status"],
  codex: ["login", "status"],
  cursor: ["status"],
};

const VERIFY_PROMPT = "Reply with the single word OK.";

const VERIFY_ARGS: Record<CodingToolId, string[]> = {
  claude: ["-p", VERIFY_PROMPT, "--output-format", "text"],
  codex: ["exec", "--skip-git-repo-check", VERIFY_PROMPT],
  cursor: ["-p", VERIFY_PROMPT, "--output-format", "text", "--trust"],
};

const PROBE_TIMEOUT_MS = 10_000;
const DOWNLOAD_TIMEOUT_MS = 10 * 60_000;
const INSTALL_TIMEOUT_MS = 10 * 60_000;
const LOGIN_TIMEOUT_MS = 10 * 60_000;
const VERIFY_TIMEOUT_MS = 2 * 60_000;
const LOG_LIMIT = 16 * 1024;
const LOG_TAIL = 4 * 1024;
const MAX_CODE_LENGTH = 1024;

// --- Pure helpers (exported for tests) -------------------------------------

// OSC sequences (hyperlinks print the URL twice) and CSI colour codes.
// eslint-disable-next-line no-control-regex -- matching terminal escape bytes is the point
const OSC = /\u001b\][^\u0007\u001b]*(?:\u0007|\u001b\\)/g;
// eslint-disable-next-line no-control-regex -- matching terminal escape bytes is the point
const CSI = /\u001b\[[0-9;?]*[ -/]*[@-~]/g;

export function stripAnsi(text: string): string {
  return text.replace(OSC, "").replace(CSI, "");
}

/** The first https URL a login command printed. */
export function extractLoginUrl(output: string): string | null {
  const match = stripAnsi(output).match(/https:\/\/[^\s"'<>]+/);
  return match ? match[0] : null;
}

/** Claude Code prints this when the browser page hands back a code to paste. */
export function loginNeedsCode(output: string): boolean {
  return /paste (the )?code/i.test(stripAnsi(output));
}

export function parseVersion(output: string): string | null {
  const match = stripAnsi(output).match(/\d+\.\d+(?:\.\d+)?(?:[-.][0-9A-Za-z]+)*/);
  return match ? match[0] : null;
}

export function parseLoggedIn(
  toolId: CodingToolId,
  result: { code: number | null; stdout: string; stderr: string },
): boolean | null {
  const text = stripAnsi(`${result.stdout}\n${result.stderr}`);
  if (toolId === "claude") {
    const json = text.match(/\{[\s\S]*\}/);
    if (!json) return null;
    try {
      const parsed = JSON.parse(json[0]) as { loggedIn?: unknown };
      return typeof parsed.loggedIn === "boolean" ? parsed.loggedIn : null;
    } catch {
      return null;
    }
  }
  if (/not logged in|logged out|unauthenticated/i.test(text)) return false;
  if (/logged in/i.test(text)) return true;
  if (toolId === "codex" && result.code !== null) return result.code === 0;
  return null;
}

/** Codex's native release asset for this machine, or null when there is none. */
export function codexAssetName(platform: NodeJS.Platform, arch: string): string | null {
  const cpu = arch === "arm64" ? "aarch64" : arch === "x64" ? "x86_64" : null;
  if (!cpu) return null;
  if (platform === "darwin") return `codex-${cpu}-apple-darwin`;
  if (platform === "linux") return `codex-${cpu}-unknown-linux-musl`;
  return null;
}

export function userBinDir(): string {
  return join(homedir(), ".local", "bin");
}

/** Every directory a freshly installed tool may sit in, PATH first. */
export function searchDirs(env: NodeJS.ProcessEnv): string[] {
  const dirs = (env.PATH ?? "").split(delimiter).filter(Boolean);
  const local = userBinDir();
  return dirs.includes(local) ? dirs : [...dirs, local];
}

// --- Processes --------------------------------------------------------------

interface ExecResult {
  code: number | null;
  stdout: string;
  stderr: string;
  timedOut: boolean;
}

function spawnEnv(): NodeJS.ProcessEnv {
  const env = daemonSpawnEnv(process.env);
  const local = userBinDir();
  const path = env.PATH ?? "";
  if (!path.split(delimiter).includes(local)) env.PATH = path ? `${path}${delimiter}${local}` : local;
  return env;
}

async function findBinary(toolId: CodingToolId): Promise<string | null> {
  const names = process.platform === "win32"
    ? [`${BINARIES[toolId]}.exe`, `${BINARIES[toolId]}.cmd`]
    : [BINARIES[toolId]];
  for (const dir of searchDirs(spawnEnv())) {
    for (const name of names) {
      const path = join(dir, name);
      try {
        await access(path, constants.X_OK);
        return path;
      } catch {
        // keep looking
      }
    }
  }
  return null;
}

function killTree(child: ChildProcess): void {
  if (child.exitCode !== null || child.signalCode !== null || !child.pid) return;
  try {
    if (process.platform === "win32") child.kill();
    else process.kill(-child.pid, "SIGTERM");
  } catch {
    child.kill();
  }
}

interface RunOptions {
  timeoutMs: number;
  cwd?: string;
  stdin?: "pipe" | "ignore";
  onOutput?: (chunk: string) => void;
  onSpawn?: (child: ChildProcess) => void;
}

function run(file: string, args: string[], options: RunOptions): Promise<ExecResult> {
  return new Promise((resolve) => {
    let stdout = "";
    let stderr = "";
    let timedOut = false;
    let child: ChildProcess;
    try {
      child = spawn(file, args, {
        cwd: options.cwd,
        env: spawnEnv(),
        stdio: [options.stdin ?? "ignore", "pipe", "pipe"],
        // Own process group, so cancel stops the installer and what it started.
        detached: process.platform !== "win32",
      });
    } catch (err) {
      resolve({ code: null, stdout, stderr: err instanceof Error ? err.message : String(err), timedOut });
      return;
    }
    options.onSpawn?.(child);
    const timer = setTimeout(() => {
      timedOut = true;
      killTree(child);
    }, options.timeoutMs);
    child.stdout?.setEncoding("utf8");
    child.stderr?.setEncoding("utf8");
    child.stdout?.on("data", (chunk: string) => {
      stdout += chunk;
      options.onOutput?.(chunk);
    });
    child.stderr?.on("data", (chunk: string) => {
      stderr += chunk;
      options.onOutput?.(chunk);
    });
    child.on("error", (err) => {
      stderr += err.message;
    });
    child.on("close", (code) => {
      clearTimeout(timer);
      resolve({ code, stdout, stderr, timedOut });
    });
  });
}

// --- Probe ------------------------------------------------------------------

async function probeTool(toolId: CodingToolId): Promise<CodingToolProbe> {
  const binary = await findBinary(toolId);
  if (!binary) return { id: toolId, installed: false, version: null, loggedIn: null };
  const [version, status] = await Promise.all([
    run(binary, ["--version"], { timeoutMs: PROBE_TIMEOUT_MS }),
    run(binary, STATUS_ARGS[toolId], { timeoutMs: PROBE_TIMEOUT_MS }),
  ]);
  return {
    id: toolId,
    installed: true,
    version: parseVersion(version.stdout || version.stderr),
    loggedIn: status.timedOut ? null : parseLoggedIn(toolId, status),
  };
}

let pendingProbe: Promise<CodingToolProbe[]> | null = null;

// A hung CLI holds a probe for PROBE_TIMEOUT_MS, longer than the renderer's
// poll interval, so concurrent callers share the probe in flight.
export function probeCodingTools(): Promise<CodingToolProbe[]> {
  pendingProbe ??= Promise.all(CODING_TOOL_IDS.map(probeTool)).finally(() => {
    pendingProbe = null;
  });
  return pendingProbe;
}

// --- Runs -------------------------------------------------------------------

class RunFailed extends Error {
  constructor(readonly code: CodingToolErrorCode, message: string) {
    super(message);
  }
}

interface ActiveRun {
  id: string;
  toolId: CodingToolId;
  kind: CodingToolRunKind;
  phase: CodingToolPhase;
  log: string;
  child: ChildProcess | null;
  abort: AbortController;
  cancelled: boolean;
}

const runs = new Map<string, ActiveRun>();

function emit(active: ActiveRun, event: Omit<CodingToolRunEvent, "runId" | "toolId" | "kind" | "phase">): void {
  const payload: CodingToolRunEvent = {
    runId: active.id,
    toolId: active.toolId,
    kind: active.kind,
    phase: active.phase,
    ...event,
  };
  for (const win of BrowserWindow.getAllWindows()) {
    if (!win.isDestroyed()) win.webContents.send(CODING_TOOLS_EVENT_CHANNEL, payload);
  }
}

function appendLog(active: ActiveRun, chunk: string): void {
  active.log = (active.log + stripAnsi(chunk)).slice(-LOG_LIMIT);
}

function enterPhase(active: ActiveRun, phase: CodingToolPhase): void {
  if (active.cancelled) throw new RunFailed("cancelled", "Cancelled");
  active.phase = phase;
  emit(active, { status: "running" });
}

async function step(active: ActiveRun, file: string, args: string[], options: Omit<RunOptions, "onSpawn">) {
  const result = await run(file, args, {
    ...options,
    onSpawn: (child) => {
      active.child = child;
    },
    onOutput: (chunk) => {
      appendLog(active, chunk);
      options.onOutput?.(chunk);
    },
  });
  active.child = null;
  if (active.cancelled) throw new RunFailed("cancelled", "Cancelled");
  return result;
}

function failureCode(log: string, fallback: CodingToolErrorCode): CodingToolErrorCode {
  if (/permission denied|EACCES|operation not permitted/i.test(log)) return "permission";
  if (/could not resolve host|network|timed out|ENOTFOUND|ECONNRESET|curl: \(\d+\)/i.test(log)) return "network";
  return fallback;
}

async function download(active: ActiveRun, url: string, target: string, reportProgress: boolean): Promise<void> {
  const timer = setTimeout(() => active.abort.abort(), DOWNLOAD_TIMEOUT_MS);
  try {
    const response = await fetch(url, { signal: active.abort.signal, redirect: "follow" });
    if (!response.ok || !response.body) {
      throw new RunFailed("network", `Download failed: HTTP ${response.status}`);
    }
    const total = Number(response.headers.get("content-length")) || 0;
    let received = 0;
    let lastSent = 0;
    const body = Readable.fromWeb(response.body as WebReadableStream<Uint8Array>);
    if (reportProgress && total > 0) {
      body.on("data", (chunk: Buffer) => {
        received += chunk.length;
        const progress = received / total;
        if (progress - lastSent >= 0.02 || progress === 1) {
          lastSent = progress;
          emit(active, { status: "running", progress });
        }
      });
    }
    await pipeline(body, createWriteStream(target, { mode: 0o644 }));
  } catch (err) {
    if (active.cancelled) throw new RunFailed("cancelled", "Cancelled");
    if (err instanceof RunFailed) throw err;
    throw new RunFailed("network", err instanceof Error ? err.message : String(err));
  } finally {
    clearTimeout(timer);
  }
}

async function install(active: ActiveRun, workDir: string): Promise<void> {
  const { toolId } = active;
  const script = INSTALL_SCRIPTS[toolId];
  if (script) {
    enterPhase(active, "download");
    const path = join(workDir, "install.sh");
    await download(active, script, path, false);
    enterPhase(active, "install");
    const result = await step(active, "/bin/bash", [path], { timeoutMs: INSTALL_TIMEOUT_MS, cwd: workDir });
    if (result.code !== 0) {
      throw new RunFailed(failureCode(active.log, result.timedOut ? "network" : "unknown"), lastLine(active.log));
    }
    return;
  }

  const asset = codexAssetName(process.platform, process.arch);
  if (!asset) throw new RunFailed("unsupported_platform", `No Codex build for ${process.platform}/${process.arch}`);
  enterPhase(active, "download");
  const archive = join(workDir, `${asset}.tar.gz`);
  await download(active, `https://github.com/openai/codex/releases/latest/download/${asset}.tar.gz`, archive, true);
  enterPhase(active, "install");
  const untar = await step(active, "tar", ["-xzf", archive, "-C", workDir], { timeoutMs: INSTALL_TIMEOUT_MS });
  if (untar.code !== 0) throw new RunFailed("unknown", lastLine(active.log) || "Could not unpack Codex");
  const extracted = (await readdir(workDir)).find((name) => name === asset);
  if (!extracted) throw new RunFailed("unknown", "The Codex download did not contain a binary");
  const binDir = userBinDir();
  try {
    await mkdir(binDir, { recursive: true });
    const target = join(binDir, BINARIES.codex);
    await copyFile(join(workDir, extracted), target);
    await chmod(target, 0o755);
    appendLog(active, `Installed Codex to ${target}\n`);
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    throw new RunFailed(failureCode(message, "unknown"), message);
  }
}

async function login(active: ActiveRun, binary: string): Promise<void> {
  enterPhase(active, "login");
  let output = "";
  let sentUrl: string | null = null;
  let sentNeedsCode = false;
  const result = await step(active, binary, LOGIN_ARGS[active.toolId], {
    timeoutMs: LOGIN_TIMEOUT_MS,
    stdin: "pipe",
    onOutput: (chunk) => {
      output += chunk;
      const url = extractLoginUrl(output);
      const needsCode = loginNeedsCode(output);
      if ((url && url !== sentUrl) || needsCode !== sentNeedsCode) {
        sentUrl = url;
        sentNeedsCode = needsCode;
        emit(active, { status: "running", loginUrl: url ?? undefined, needsCode });
      }
    },
  });
  if (result.code !== 0) {
    throw new RunFailed("login_failed", result.timedOut ? "Sign-in timed out" : lastLine(active.log) || "Sign-in failed");
  }
}

async function verify(active: ActiveRun, binary: string): Promise<void> {
  enterPhase(active, "verify");
  const cwd = await mkdtemp(join(tmpdir(), "multica-verify-"));
  try {
    const result = await step(active, binary, VERIFY_ARGS[active.toolId], { timeoutMs: VERIFY_TIMEOUT_MS, cwd });
    if (result.code !== 0) {
      throw new RunFailed("verify_failed", result.timedOut ? "The test run timed out" : lastLine(active.log) || "The test run failed");
    }
  } finally {
    await rm(cwd, { recursive: true, force: true }).catch(() => undefined);
  }
}

function lastLine(log: string): string {
  const lines = log.split("\n").map((line) => line.trim()).filter(Boolean);
  return lines[lines.length - 1]?.slice(0, 300) ?? "";
}

async function execute(active: ActiveRun): Promise<void> {
  const workDir = await mkdtemp(join(tmpdir(), "multica-tool-"));
  try {
    if (active.kind === "install") await install(active, workDir);
    const binary = await findBinary(active.toolId);
    if (!binary) throw new RunFailed("unknown", `${BINARIES[active.toolId]} was not found after installing`);
    const status = await probeTool(active.toolId);
    if (status.loggedIn !== true) await login(active, binary);
    await verify(active, binary);
    emit(active, { status: "done", finished: true, logTail: active.log.slice(-LOG_TAIL) });
  } catch (err) {
    const error: CodingToolError = err instanceof RunFailed
      ? { code: active.cancelled ? "cancelled" : err.code, message: err.message }
      : { code: "unknown", message: err instanceof Error ? err.message : String(err) };
    emit(active, { status: "failed", finished: true, error, logTail: active.log.slice(-LOG_TAIL) });
  } finally {
    runs.delete(active.id);
    await rm(workDir, { recursive: true, force: true }).catch(() => undefined);
  }
}

export function startCodingToolRun(toolId: unknown, kind: unknown): CodingToolStartResult {
  if (!isCodingToolId(toolId) || (kind !== "install" && kind !== "login")) {
    return { error: { code: "unknown", message: "Invalid request" } };
  }
  if (kind === "install" && process.platform === "win32") {
    return { error: { code: "unsupported_platform", message: "Installing from Multica is not supported on Windows yet" } };
  }
  for (const active of runs.values()) {
    if (active.toolId === toolId) return { error: { code: "busy", message: "Already running" } };
  }
  const active: ActiveRun = {
    id: randomUUID(),
    toolId,
    kind,
    phase: kind === "install" ? "download" : "login",
    log: "",
    child: null,
    abort: new AbortController(),
    cancelled: false,
  };
  runs.set(active.id, active);
  void execute(active);
  return { runId: active.id };
}

export function submitCodingToolCode(runId: unknown, code: unknown): boolean {
  if (typeof runId !== "string" || typeof code !== "string") return false;
  const trimmed = code.trim();
  if (!trimmed || trimmed.length > MAX_CODE_LENGTH || /[\r\n]/.test(trimmed)) return false;
  const stdin = runs.get(runId)?.child?.stdin;
  if (!stdin || stdin.destroyed) return false;
  stdin.write(`${trimmed}\n`);
  return true;
}

export function cancelCodingToolRun(runId: unknown): void {
  if (typeof runId !== "string") return;
  const active = runs.get(runId);
  if (!active) return;
  active.cancelled = true;
  active.abort.abort();
  if (active.child) killTree(active.child);
}

export function setupCodingTools(): void {
  ipcMain.handle(CODING_TOOLS_PROBE_CHANNEL, () => probeCodingTools());
  ipcMain.handle(CODING_TOOLS_START_CHANNEL, (_event, toolId: unknown, kind: unknown) =>
    startCodingToolRun(toolId, kind),
  );
  ipcMain.handle(CODING_TOOLS_SUBMIT_CODE_CHANNEL, (_event, runId: unknown, code: unknown) =>
    submitCodingToolCode(runId, code),
  );
  ipcMain.handle(CODING_TOOLS_CANCEL_CHANNEL, (_event, runId: unknown) => cancelCodingToolRun(runId));
  // Runs sit in their own process group, so they would outlive the app.
  app.on("will-quit", () => {
    for (const id of runs.keys()) cancelCodingToolRun(id);
  });
}
