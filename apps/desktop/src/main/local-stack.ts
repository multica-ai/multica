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
