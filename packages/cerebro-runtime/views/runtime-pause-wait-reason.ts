import { formatPauseReason } from "./components/pause-controls";

const PREFIX = "runtime_paused|";

export type ParsedRuntimePauseWaitReason = {
  pauseReason: string;
  unpauseAt: string | null;
};

/** Parse wait_reason stamped by PauseRuntime (FIR-2717). */
export function parseRuntimePauseWaitReason(
  waitReason: string | null | undefined,
): ParsedRuntimePauseWaitReason | null {
  if (!waitReason?.startsWith(PREFIX)) return null;
  const rest = waitReason.slice(PREFIX.length);
  const pipe = rest.indexOf("|");
  if (pipe < 0) return null;
  const pauseReason = rest.slice(0, pipe);
  const unpauseAt = rest.slice(pipe + 1);
  return {
    pauseReason,
    unpauseAt: unpauseAt || null,
  };
}

/** Human label for the agent-live / execution-log queued state. */
export function runtimePauseQueuedLabel(parsed: ParsedRuntimePauseWaitReason): string {
  const reason = formatPauseReason(parsed.pauseReason);
  if (!parsed.unpauseAt) {
    return `runtime paused (${reason})`;
  }
  const until = formatUnpauseShort(parsed.unpauseAt);
  return `runtime paused (${reason}), retry ~${until}`;
}

function formatUnpauseShort(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
}
