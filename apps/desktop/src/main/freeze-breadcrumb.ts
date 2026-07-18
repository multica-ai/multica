import { writeFileSync, readFileSync, rmSync } from "node:fs";
import type { FreezeBreadcrumb } from "../shared/freeze-breadcrumb";

// When the renderer truly hangs or its process dies, it can't send telemetry
// itself — the thread is blocked or gone. The main process (always alive) is
// the only watcher that can react, but during the hang it can't reach the
// renderer's posthog-js either. So it writes a breadcrumb to disk; the next
// time a renderer boots, it reads + clears the file and reports the event.
// This survives even a force-quit, which is the whole point.

export type { FreezeBreadcrumb };

/**
 * Best-effort write. A breadcrumb we can't persist is lost, never fatal.
 *
 * Known limitation: this is a single slot — last write wins. Multiple failures
 * within one session collapse to the last one, so per-session failure counts
 * are undercounted. Acceptable for now: telemetry aggregates presence and
 * frequency across users, not exhaustive per-session sequences. Upgrade to an
 * append/ring buffer if per-session failure chains become a question.
 */
export function writeFreezeBreadcrumb(filePath: string, breadcrumb: FreezeBreadcrumb): void {
  try {
    writeFileSync(filePath, JSON.stringify(breadcrumb), "utf8");
  } catch {
    // Disk full / permissions — drop silently.
  }
}

/**
 * Delete a persisted breadcrumb. Called when the renderer recovers from a hang
 * (a `responsive` event after `unresponsive`): the breadcrumb was written
 * pre-emptively while the thread was stuck, but since it came back, the
 * in-thread long-task watchdog already reports it — keeping the breadcrumb
 * would double-count it AND mislabel a recovered window as `recovered: false`.
 * Best-effort; a stale breadcrumb only costs one duplicate report.
 */
export function clearFreezeBreadcrumb(filePath: string, ownerId?: string): void {
  try {
    if (ownerId !== undefined) {
      const parsed = JSON.parse(readFileSync(filePath, "utf8")) as unknown;
      if (
        !parsed ||
        typeof parsed !== "object" ||
        (parsed as FreezeBreadcrumb).ownerId !== ownerId
      ) {
        return;
      }
    }
    rmSync(filePath, { force: true });
  } catch {
    // Nothing to clear / permissions — ignore.
  }
}

/**
 * Read the breadcrumb and delete it in the same call, so a failure is reported
 * exactly once. Returns null when there's no breadcrumb (the normal case) or
 * when the file is unreadable / corrupt.
 */
export function readAndClearFreezeBreadcrumb(filePath: string): FreezeBreadcrumb | null {
  let raw: string;
  try {
    raw = readFileSync(filePath, "utf8");
  } catch {
    return null;
  }
  try {
    rmSync(filePath, { force: true });
  } catch {
    // If we can't delete it we'd re-report next launch; acceptable over throwing.
  }
  try {
    const parsed: unknown = JSON.parse(raw);
    if (
      parsed &&
      typeof parsed === "object" &&
      typeof (parsed as FreezeBreadcrumb).kind === "string"
    ) {
      return parsed as FreezeBreadcrumb;
    }
  } catch {
    // Corrupt JSON — drop.
  }
  return null;
}
