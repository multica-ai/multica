export interface CommandFailure { errorCode: string | null; errorReason: string | null; errorMessage: string }
export type CommandOutcome<T> =
  | { outcome: "succeeded"; value: T }
  | ({ outcome: "failed" } & CommandFailure)
  | { outcome: "timedOut" };

export function commandFailure(error: unknown): CommandFailure {
  const record = error && typeof error === "object" ? error as Record<string, unknown> : null;
  const code = typeof record?.code === "string" ? record.code.trim() : "";
  const reason = typeof record?.reason === "string" ? record.reason.trim() : "";
  return {
    errorCode: code || (error instanceof Error && error.name === "AbortError" ? "aborted" : null),
    errorReason: reason || null,
    errorMessage: error instanceof Error ? error.message : typeof record?.message === "string" ? record.message : String(error),
  };
}

/** Runs commands under a deadline. Once disposed, nothing settles: there is no one left to tell. */
export class PromptCommands {
  private readonly running = new Map<AbortController, ReturnType<typeof setTimeout>>();
  private disposed = false;

  run<T>(timeoutMs: number, execute: (signal: AbortSignal) => Promise<T>): Promise<CommandOutcome<T>> {
    return new Promise((resolve) => {
      const controller = new AbortController();
      let settled = false;
      const finish = (outcome: CommandOutcome<T>) => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        this.running.delete(controller);
        if (!this.disposed) resolve(outcome);
      };
      const timer = setTimeout(() => {
        controller.abort(new Error("command timed out"));
        finish({ outcome: "timedOut" });
      }, timeoutMs);
      this.running.set(controller, timer);
      execute(controller.signal).then(
        (value) => finish({ outcome: "succeeded", value }),
        (error: unknown) => finish({ outcome: "failed", ...commandFailure(error) }),
      );
    });
  }

  dispose(): void {
    this.disposed = true;
    for (const [controller, timer] of this.running) { clearTimeout(timer); controller.abort(new Error("service disposed")); }
    this.running.clear();
  }
}
