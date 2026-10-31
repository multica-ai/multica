import type { AgentRuntime } from "../types";

/** A daemon registers one runtime per CLI. Without a daemon identity, each
 * runtime counts separately: matching names or host labels cannot prove that
 * two registrations share a computer. */
export function countRuntimeComputers(
  runtimes: readonly Pick<AgentRuntime, "id" | "daemon_id">[],
): number {
  return new Set(
    runtimes.map((runtime) =>
      runtime.daemon_id ? `daemon:${runtime.daemon_id}` : `runtime:${runtime.id}`,
    ),
  ).size;
}
