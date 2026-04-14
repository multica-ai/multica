import { app, ipcMain, BrowserWindow } from "electron";
import { spawn, execFile, type ChildProcess } from "child_process";
import { readFile, writeFile, mkdir } from "fs/promises";
import { existsSync } from "fs";
import { join } from "path";
import { homedir } from "os";

const HEALTH_PORT = 19514;
const HEALTH_URL = `http://127.0.0.1:${HEALTH_PORT}/health`;
const POLL_INTERVAL_MS = 5_000;
const CONFIG_PATH = join(homedir(), ".multica", "config.json");
const LOG_PATH = join(homedir(), ".multica", "daemon.log");
const PREFS_PATH = join(homedir(), ".multica", "desktop_prefs.json");

interface DaemonPrefs {
  autoStart: boolean;
  autoStop: boolean;
}

const DEFAULT_PREFS: DaemonPrefs = { autoStart: true, autoStop: false };

export interface DaemonStatus {
  state: "running" | "stopped" | "starting" | "stopping" | "cli_not_found";
  pid?: number;
  uptime?: string;
  daemonId?: string;
  deviceName?: string;
  agents?: string[];
  workspaceCount?: number;
}

let statusPollTimer: ReturnType<typeof setInterval> | null = null;
let logTailProcess: ChildProcess | null = null;
let currentState: DaemonStatus["state"] = "stopped";
let getMainWindow: () => BrowserWindow | null = () => null;

function sendStatus(status: DaemonStatus): void {
  const win = getMainWindow();
  win?.webContents.send("daemon:status", status);
}

async function fetchHealth(): Promise<DaemonStatus> {
  try {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 2_000);
    const res = await fetch(HEALTH_URL, { signal: controller.signal });
    clearTimeout(timeout);

    if (!res.ok) return { state: "stopped" };
    const data = await res.json();
    if (data.status === "running") {
      return {
        state: "running",
        pid: data.pid,
        uptime: data.uptime,
        daemonId: data.daemon_id,
        deviceName: data.device_name,
        agents: data.agents ?? [],
        workspaceCount: Array.isArray(data.workspaces)
          ? data.workspaces.length
          : 0,
      };
    }
    return { state: "stopped" };
  } catch {
    return { state: currentState === "starting" ? "starting" : "stopped" };
  }
}

function findCliBinary(): string | null {
  const candidates =
    process.platform === "win32"
      ? ["multica.exe"]
      : ["multica"];

  for (const name of candidates) {
    const paths = (process.env["PATH"] ?? "").split(
      process.platform === "win32" ? ";" : ":",
    );
    // Also check common Homebrew locations
    if (process.platform === "darwin") {
      paths.push("/opt/homebrew/bin", "/usr/local/bin");
    }
    for (const dir of paths) {
      const full = join(dir, name);
      if (existsSync(full)) return full;
    }
  }
  return null;
}

async function syncToken(token: string): Promise<void> {
  const dir = join(homedir(), ".multica");
  await mkdir(dir, { recursive: true });

  let config: Record<string, unknown> = {};
  try {
    const raw = await readFile(CONFIG_PATH, "utf-8");
    config = JSON.parse(raw);
  } catch {
    // File doesn't exist or invalid JSON — start fresh
  }
  config.token = token;
  await writeFile(CONFIG_PATH, JSON.stringify(config, null, 2), "utf-8");
}

async function loadPrefs(): Promise<DaemonPrefs> {
  try {
    const raw = await readFile(PREFS_PATH, "utf-8");
    const parsed = JSON.parse(raw);
    return { ...DEFAULT_PREFS, ...parsed };
  } catch {
    return { ...DEFAULT_PREFS };
  }
}

async function savePrefs(prefs: DaemonPrefs): Promise<void> {
  const dir = join(homedir(), ".multica");
  await mkdir(dir, { recursive: true });
  await writeFile(PREFS_PATH, JSON.stringify(prefs, null, 2), "utf-8");
}

async function clearToken(): Promise<void> {
  try {
    const raw = await readFile(CONFIG_PATH, "utf-8");
    const config = JSON.parse(raw);
    delete config.token;
    await writeFile(CONFIG_PATH, JSON.stringify(config, null, 2), "utf-8");
  } catch {
    // Ignore — file doesn't exist
  }
}

async function startDaemon(): Promise<{ success: boolean; error?: string }> {
  const bin = findCliBinary();
  if (!bin) return { success: false, error: "multica CLI not found in PATH" };

  const status = await fetchHealth();
  if (status.state === "running") return { success: true };

  currentState = "starting";
  sendStatus({ state: "starting" });

  return new Promise((resolve) => {
    execFile(bin, ["daemon", "start"], { timeout: 20_000 }, (err) => {
      if (err) {
        currentState = "stopped";
        sendStatus({ state: "stopped" });
        resolve({ success: false, error: err.message });
        return;
      }
      currentState = "running";
      pollOnce();
      resolve({ success: true });
    });
  });
}

async function stopDaemon(): Promise<{ success: boolean; error?: string }> {
  const bin = findCliBinary();
  if (!bin) return { success: false, error: "multica CLI not found in PATH" };

  currentState = "stopping";
  sendStatus({ state: "stopping" });

  return new Promise((resolve) => {
    execFile(bin, ["daemon", "stop"], { timeout: 15_000 }, (err) => {
      if (err) {
        resolve({ success: false, error: err.message });
      } else {
        resolve({ success: true });
      }
      currentState = "stopped";
      sendStatus({ state: "stopped" });
    });
  });
}

async function restartDaemon(): Promise<{ success: boolean; error?: string }> {
  const stopResult = await stopDaemon();
  if (!stopResult.success) return stopResult;
  return startDaemon();
}

async function pollOnce(): Promise<void> {
  const status = await fetchHealth();
  currentState = status.state;
  sendStatus(status);
}

function startPolling(): void {
  if (statusPollTimer) return;
  pollOnce();
  statusPollTimer = setInterval(pollOnce, POLL_INTERVAL_MS);
}

function stopPolling(): void {
  if (statusPollTimer) {
    clearInterval(statusPollTimer);
    statusPollTimer = null;
  }
}

function startLogTail(win: BrowserWindow): void {
  stopLogTail();
  if (!existsSync(LOG_PATH)) return;

  logTailProcess = spawn("tail", ["-n", "200", "-f", LOG_PATH]);
  logTailProcess.stdout?.on("data", (chunk: Buffer) => {
    const lines = chunk.toString("utf-8").split("\n").filter(Boolean);
    for (const line of lines) {
      win.webContents.send("daemon:log-line", line);
    }
  });
  logTailProcess.on("error", () => {
    // Ignore tail errors
  });
}

function stopLogTail(): void {
  if (logTailProcess) {
    logTailProcess.kill();
    logTailProcess = null;
  }
}

export function setupDaemonManager(
  windowGetter: () => BrowserWindow | null,
): void {
  getMainWindow = windowGetter;

  ipcMain.handle("daemon:start", () => startDaemon());
  ipcMain.handle("daemon:stop", () => stopDaemon());
  ipcMain.handle("daemon:restart", () => restartDaemon());
  ipcMain.handle("daemon:get-status", () => fetchHealth());
  ipcMain.handle("daemon:sync-token", (_event, token: string) =>
    syncToken(token),
  );
  ipcMain.handle("daemon:clear-token", () => clearToken());
  ipcMain.handle("daemon:is-cli-installed", () => findCliBinary() !== null);
  ipcMain.handle("daemon:get-prefs", () => loadPrefs());
  ipcMain.handle(
    "daemon:set-prefs",
    (_event, prefs: Partial<DaemonPrefs>) =>
      loadPrefs().then((cur) => {
        const merged = { ...cur, ...prefs };
        return savePrefs(merged).then(() => merged);
      }),
  );
  ipcMain.handle("daemon:auto-start", async () => {
    const prefs = await loadPrefs();
    if (!prefs.autoStart) return;
    const bin = findCliBinary();
    if (!bin) return;
    const health = await fetchHealth();
    if (health.state === "running") return;
    await startDaemon();
  });

  ipcMain.on("daemon:start-log-stream", () => {
    const win = getMainWindow();
    if (win) startLogTail(win);
  });

  ipcMain.on("daemon:stop-log-stream", () => {
    stopLogTail();
  });

  startPolling();

  app.on("before-quit", async () => {
    stopPolling();
    stopLogTail();
    const prefs = await loadPrefs();
    if (prefs.autoStop) {
      const bin = findCliBinary();
      if (bin) {
        try {
          await stopDaemon();
        } catch {
          // Best-effort stop on quit
        }
      }
    }
  });
}
