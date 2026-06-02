import type { PomodoroPhase, PomodoroSession } from "@/shared/types";

/**
 * Calculates the remaining seconds for the current pomodoro phase.
 */
export function getPomodoroRemainingSeconds(
  session: PomodoroSession,
  nowMs: number = Date.now(),
): number {
  const runningForSeconds = session.status === "running" && session.started_at
    ? (nowMs - new Date(session.started_at).getTime()) / 1000
    : 0;

  return Math.max(
    0,
    Math.round(session.phase_duration_seconds - session.elapsed_seconds - runningForSeconds),
  );
}

/**
 * Maps a pomodoro phase to the compact shell label.
 */
export function getPomodoroHeaderLabel(phase: PomodoroPhase): "Focus" | "Break" {
  switch (phase) {
    case "work":
      return "Focus";
    case "break":
    case "long_break":
      return "Break";
    default: {
      const _exhaustive: never = phase;
      return _exhaustive;
    }
  }
}

/**
 * Formats a duration as mm:ss for the shell pill.
 */
export function formatPomodoroTimer(totalSeconds: number): string {
  const minutes = Math.floor(totalSeconds / 60).toString().padStart(2, "0");
  const seconds = (totalSeconds % 60).toString().padStart(2, "0");
  return `${minutes}:${seconds}`;
}
