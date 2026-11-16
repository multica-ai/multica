export type DaemonInstallPrep =
  | { kind: "ready" }
  | { kind: "busy"; activeTasks: number }
  | { kind: "timeout" };

export interface DaemonInstallProbe {
  alive: boolean;
  pid?: number;
  activeTasks: number;
  externallyManaged: boolean;
}

export interface DaemonInstallGateDeps {
  probe: () => Promise<DaemonInstallProbe | null>;
  // Check this signal before issuing a stop that was queued behind other work.
  stop: (signal: AbortSignal) => Promise<void>;
  processExists: (pid: number) => boolean;
  sleep: (ms: number) => Promise<void>;
}

export interface DaemonInstallGateOptions {
  interruptActiveTasks: boolean;
  timeoutMs?: number;
}

// Bound the whole preparation, including health probes and lifecycle queuing.
export async function prepareDaemonForInstall(
  deps: DaemonInstallGateDeps,
  options: DaemonInstallGateOptions,
): Promise<DaemonInstallPrep> {
  const controller = new AbortController();
  const { signal } = controller;
  const prepare = async (): Promise<DaemonInstallPrep> => {
    const initial = await deps.probe();
    if (signal.aborted) return { kind: "timeout" };
    if (!initial?.alive || initial.externallyManaged) return { kind: "ready" };
    if (!options.interruptActiveTasks && initial.activeTasks > 0) {
      return { kind: "busy", activeTasks: initial.activeTasks };
    }
    try {
      await deps.stop(signal);
    } catch (err) {
      console.warn("[updater] daemon stop failed:", err);
    }
    while (!signal.aborted) {
      const current = await deps.probe();
      if (signal.aborted) break;
      // The CLI can report success and close /health while the process still
      // drains. Windows releases the executable lock only when that PID exits.
      if (!current?.alive && (initial.pid === undefined || !deps.processExists(initial.pid))) {
        return { kind: "ready" };
      }
      await deps.sleep(250);
    }
    return { kind: "timeout" };
  };
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    return await Promise.race([
      prepare(),
      new Promise<DaemonInstallPrep>((resolve) => {
        timer = setTimeout(() => resolve({ kind: "timeout" }), options.timeoutMs ?? 15_000);
      }),
    ]);
  } finally {
    clearTimeout(timer);
    controller.abort();
  }
}
