import type { LocalStackState, LocalStackStep } from "../shared/local-stack";
import { isLocalApiUrl, type LocalStackConfig } from "./local-stack-config";

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

/**
 * What `setupLocalStack`'s `start()` should do next, given the app's
 * configured apiUrl and a way to load the supervisor config. Kept pure and
 * electron-free (no fs, no ipcMain) so the branch that enforces "a
 * SaaS-pointed build must never touch docker" is directly testable without
 * mocking electron.
 */
export type StartDecision =
  | { kind: "inert"; reason: "non-local-api" | "not-configured" }
  | { kind: "config-error"; message: string }
  | { kind: "bring-up"; config: LocalStackConfig };

export async function resolveStartDecision(deps: {
  apiUrl: string;
  loadConfig: () => Promise<LocalStackConfig | null>;
}): Promise<StartDecision> {
  if (!isLocalApiUrl(deps.apiUrl)) {
    return { kind: "inert", reason: "non-local-api" };
  }

  let config: LocalStackConfig | null;
  try {
    config = await deps.loadConfig();
  } catch (err) {
    return {
      kind: "config-error",
      message: err instanceof Error ? err.message : String(err),
    };
  }

  // Not configured — behave exactly as the app does without this feature.
  if (!config) {
    return { kind: "inert", reason: "not-configured" };
  }

  return { kind: "bring-up", config };
}

// --- Electron wiring ------------------------------------------------------
// Kept in this file so the state machine above and its only real caller stay
// together; everything electron-coupled lives below this line.

import { execFile } from "child_process";
import { ipcMain } from "electron";
import type { BrowserWindow } from "electron";
import { loadLocalStackConfig, localStackConfigPath } from "./local-stack-config";
// LocalStackState, LocalStackConfig, and isLocalApiUrl are already imported
// at the top of this file for the pure logic above; no second import here.

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

/**
 * Same guard shape as `sendToLiveRenderer` in updater.ts. A window can be torn
 * down between the `isDestroyed()` check and the send, and accessing
 * `webContents` on a destroyed window throws rather than returning null.
 */
function isDestroyedObjectError(err: unknown): boolean {
  return err instanceof Error && err.message.includes("Object has been destroyed");
}

/**
 * Brings the local stack up when this build points at a backend on this machine
 * and the supervisor is configured. Any other configuration resolves to `ready`
 * immediately, so the renderer's gate is a no-op for SaaS builds.
 *
 * `apiUrl` is null when the runtime config failed to parse. There is then no
 * backend URL to gate on, so the supervisor resolves to `ready` and lets the
 * renderer render its blocking configuration-error screen. Registering the IPC
 * surface is NOT conditional on that: the renderer reads the initial state
 * synchronously and would otherwise hang on a channel with no handler.
 */
export function setupLocalStack(
  windowGetter: () => BrowserWindow | null,
  apiUrl: string | null,
): void {
  let currentState: LocalStackState = { phase: "idle" };
  // Bring-up generation. Both `skip` and `retry` bump it, and a broadcast
  // carrying a stale generation is dropped. Without this, the bring-up already
  // in flight when the user pressed "Continue anyway" — most likely the 90s
  // backend poll — would broadcast its failure a minute later, re-block the
  // window, and unmount CoreProvider (React Query cache, WebSocket, editor
  // state) mid-session with no user action.
  let generation = 0;
  // Set by `skip`, cleared by `retry`. Redundant with the generation check for
  // the runs we know about, and deliberately so: "the user chose to continue"
  // outranks anything a background run has to say.
  let dismissed = false;
  let inFlight: Promise<LocalStackState> | null = null;
  // Last step the current run reached, so a thrown bring-up can be attributed
  // to where it actually died rather than to a generic bucket.
  let lastStep: LocalStackStep = "config";

  const broadcast = (state: LocalStackState, gen: number): void => {
    if (gen !== generation) return;
    if (dismissed && state.phase !== "ready") return;
    if (state.phase === "running") lastStep = state.step;
    currentState = state;

    const win = windowGetter();
    if (!win || win.isDestroyed()) return;
    try {
      const { webContents } = win;
      if (webContents.isDestroyed()) return;
      webContents.send("local-stack:state", state);
    } catch (err) {
      if (isDestroyedObjectError(err)) return;
      throw err;
    }
  };

  const runStart = async (gen: number): Promise<LocalStackState> => {
    if (apiUrl === null) {
      broadcast({ phase: "ready" }, gen);
      return currentState;
    }

    const decision = await resolveStartDecision({
      apiUrl,
      loadConfig: () => loadLocalStackConfig(localStackConfigPath()),
    });

    switch (decision.kind) {
      case "inert":
        broadcast({ phase: "ready" }, gen);
        return currentState;
      case "config-error":
        broadcast(
          { phase: "failed", step: "config", message: decision.message },
          gen,
        );
        return currentState;
      case "bring-up":
        return bringUpLocalStack({
          config: decision.config,
          run: createCommandRunner(decision.config),
          probeBackend: createBackendProbe(apiUrl),
          onState: (state) => broadcast(state, gen),
          sleep: (ms) => new Promise((r) => setTimeout(r, ms)),
        });
    }
  };

  /**
   * Single-flight bring-up, mirroring `checkForUpdatesOnce` in updater.ts. The
   * retry button is an IPC round-trip away from its first `running` broadcast,
   * so a double-click would otherwise launch two concurrent `compose up` runs
   * on the same project (container-name conflicts) that then race to set the
   * state.
   */
  const start = (): Promise<LocalStackState> => {
    if (inFlight) return inFlight;
    generation += 1;
    dismissed = false;
    lastStep = "config";
    const gen = generation;
    const p = runStart(gen)
      .catch((err): LocalStackState => {
        // Anything thrown here (including a send into a renderer that died
        // mid-broadcast) would otherwise be an unhandled rejection off
        // `void start()`, freezing the overlay on the last running step with
        // no Retry and no Skip until the app is relaunched.
        const failure: LocalStackState = {
          phase: "failed",
          step: lastStep,
          message: err instanceof Error ? err.message : String(err),
        };
        try {
          broadcast(failure, gen);
        } catch {
          // Renderer torn down mid-send; currentState already carries it.
        }
        return failure;
      })
      .finally(() => {
        if (inFlight === p) inFlight = null;
      });
    inFlight = p;
    return p;
  };

  // Synchronous read, mirroring the other boot-critical preload reads
  // (app:get-info, runtime-config:get, freeze:get-last). The renderer seeds its
  // initial state from this, so the overlay is never the first painted frame on
  // a build that has nothing to bring up, and a missing async handler can never
  // leave the window stuck on a button-less overlay.
  ipcMain.on("local-stack:get-initial-state", (event) => {
    event.returnValue = currentState;
  });
  ipcMain.handle("local-stack:get-state", () => currentState);
  ipcMain.handle("local-stack:skip", () => {
    generation += 1;
    dismissed = true;
    broadcast({ phase: "ready" }, generation);
  });
  ipcMain.handle("local-stack:retry", () => start());

  void start();
}
