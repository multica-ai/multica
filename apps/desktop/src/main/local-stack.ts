import type { LocalStackState } from "../shared/local-stack";
import type { LocalStackConfig } from "./local-stack-config";

export interface CommandResult {
  ok: boolean;
  stdout: string;
  stderr: string;
}

export type CommandRunner = (
  bin: string,
  args: string[],
) => Promise<CommandResult>;

export interface BringUpDeps {
  config: LocalStackConfig;
  run: CommandRunner;
  probeBackend: () => Promise<boolean>;
  onState: (state: LocalStackState) => void;
  sleep: (ms: number) => Promise<void>;
  /** Ceiling for the post-compose backend wait. Default 90s. */
  backendTimeoutMs?: number;
}

const DEFAULT_BACKEND_TIMEOUT_MS = 90_000;
const BACKEND_POLL_INTERVAL_MS = 2_000;

/**
 * Ordered bring-up of the self-hosted stack.
 *
 * The order is not cosmetic: the daemon connects to the backend API as soon as
 * it starts, and login cannot succeed before the backend answers, so every
 * later stage depends on the earlier one actually being up.
 */
export async function bringUpLocalStack(
  deps: BringUpDeps,
): Promise<LocalStackState> {
  const {
    config,
    run,
    probeBackend,
    onState,
    sleep,
    backendTimeoutMs = DEFAULT_BACKEND_TIMEOUT_MS,
  } = deps;

  const emit = (state: LocalStackState): LocalStackState => {
    onState(state);
    return state;
  };

  // Fast path. With the stack left running between app launches this is the
  // common case, and it costs exactly one request.
  emit({ phase: "running", step: "probe" });
  if (await probeBackend()) return emit({ phase: "ready" });

  // Docker engine.
  emit({ phase: "running", step: "engine" });
  const status = await run("colima", ["status"]);
  if (!colimaRunning(status)) {
    const started = await run("colima", [
      "start",
      "--cpu",
      "2",
      "--memory",
      "4",
    ]);
    if (!started.ok) {
      return emit({
        phase: "failed",
        step: "engine",
        message: lastLine(started.stderr || started.stdout) || "colima start failed",
      });
    }
  }

  // Containers. `up -d` is a no-op for services already running, so this is
  // safe to re-run. Never `pull` — the backend image name is a local shadow tag.
  emit({ phase: "running", step: "containers" });
  const composed = await run("docker", [
    "compose",
    "-f",
    config.composeFile,
    "up",
    "-d",
  ]);
  if (!composed.ok) {
    return emit({
      phase: "failed",
      step: "containers",
      message: lastLine(composed.stderr || composed.stdout) || "compose up failed",
    });
  }

  // Backend readiness. Containers being "up" says nothing about migrations
  // having finished, so poll the API rather than trusting compose.
  emit({ phase: "running", step: "backend" });
  const deadline = backendTimeoutMs;
  let waited = 0;
  for (;;) {
    if (await probeBackend()) return emit({ phase: "ready" });
    if (waited >= deadline) {
      return emit({
        phase: "failed",
        step: "backend",
        message: `backend did not respond within ${Math.round(deadline / 1000)}s`,
      });
    }
    await sleep(BACKEND_POLL_INTERVAL_MS);
    waited += BACKEND_POLL_INTERVAL_MS;
  }
}

/**
 * `colima status` exits non-zero when the VM is stopped, and prints its state
 * to stderr when running. Treat any non-ok exit as "needs starting" — starting
 * an already-running colima is harmless, while skipping a needed start is not.
 */
function colimaRunning(result: CommandResult): boolean {
  if (!result.ok) return false;
  return /running/i.test(`${result.stdout}${result.stderr}`);
}

function lastLine(text: string): string {
  const lines = text.trim().split("\n").filter(Boolean);
  return lines.at(-1)?.trim() ?? "";
}

// --- Electron wiring ------------------------------------------------------
// Kept in this file so the state machine above and its only real caller stay
// together; everything electron-coupled lives below this line.

import { execFile } from "child_process";
import { BrowserWindow, ipcMain } from "electron";
import {
  isLocalApiUrl,
  loadLocalStackConfig,
  localStackConfigPath,
} from "./local-stack-config";
// LocalStackState and LocalStackConfig are already imported at the top of
// this file for the state machine above; no second import is needed here.

const COMMAND_TIMEOUT_MS = 180_000;

/**
 * Runs a command inside the checkout with BACKEND_PORT exported, mirroring what
 * multica-start.sh does. process.env is already PATH-corrected by the fixPath()
 * block in index.ts, which is how colima and docker (both in /opt/homebrew/bin)
 * are found from a GUI launch.
 */
function createCommandRunner(config: LocalStackConfig): CommandRunner {
  return (bin, args) =>
    new Promise((resolve) => {
      execFile(
        bin,
        args,
        {
          cwd: config.repoDir,
          timeout: COMMAND_TIMEOUT_MS,
          env: {
            ...process.env,
            BACKEND_PORT: String(config.backendPort),
          },
          maxBuffer: 4 * 1024 * 1024,
        },
        (err, stdout, stderr) => {
          resolve({
            ok: !err,
            stdout: stdout?.toString() ?? "",
            stderr: stderr?.toString() ?? (err ? err.message : ""),
          });
        },
      );
    });
}

function createBackendProbe(apiUrl: string): () => Promise<boolean> {
  return async () => {
    try {
      const res = await fetch(`${apiUrl}/api/config`, {
        signal: AbortSignal.timeout(3_000),
      });
      return res.ok;
    } catch {
      return false;
    }
  };
}

let currentState: LocalStackState = { phase: "idle" };

function broadcast(
  windowGetter: () => BrowserWindow | null,
  state: LocalStackState,
): void {
  currentState = state;
  const win = windowGetter();
  if (win && !win.isDestroyed()) {
    win.webContents.send("local-stack:state", state);
  }
}

/**
 * Brings the local stack up when this build points at a backend on this machine
 * and the supervisor is configured. Any other configuration resolves to `ready`
 * immediately, so the renderer's gate is a no-op for SaaS builds.
 */
export function setupLocalStack(
  windowGetter: () => BrowserWindow | null,
  apiUrl: string,
): void {
  ipcMain.handle("local-stack:get-state", () => currentState);
  ipcMain.handle("local-stack:skip", () => {
    broadcast(windowGetter, { phase: "ready" });
  });

  const start = async (): Promise<LocalStackState> => {
    if (!isLocalApiUrl(apiUrl)) {
      broadcast(windowGetter, { phase: "ready" });
      return currentState;
    }

    let config: LocalStackConfig | null;
    try {
      config = await loadLocalStackConfig(localStackConfigPath());
    } catch (err) {
      broadcast(windowGetter, {
        phase: "failed",
        step: "probe",
        message: err instanceof Error ? err.message : String(err),
      });
      return currentState;
    }

    // Not configured — behave exactly as the app does without this feature.
    if (!config) {
      broadcast(windowGetter, { phase: "ready" });
      return currentState;
    }

    return bringUpLocalStack({
      config,
      run: createCommandRunner(config),
      probeBackend: createBackendProbe(apiUrl),
      onState: (state) => broadcast(windowGetter, state),
      sleep: (ms) => new Promise((r) => setTimeout(r, ms)),
    });
  };

  ipcMain.handle("local-stack:retry", () => start());
  void start();
}
