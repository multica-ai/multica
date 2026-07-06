import { writeFileSync, readFileSync, rmSync } from "node:fs";
import type { FreezeBreadcrumb } from "../shared/freeze-breadcrumb";

// When the renderer truly hangs or its process dies, it can't send telemetry
// itself — the thread is blocked or gone. The main process (always alive) is
// the only watcher that can react, but during the hang it can't reach the
// renderer's posthog-js either. So it writes a breadcrumb to disk; the next
// time a renderer boots, it reads the file and reports the event.
// This survives even a force-quit, which is the whole point.
//
// Read and delete are deliberately SPLIT (MUL-4120): the renderer reads the
// breadcrumb, sends the event with posthog's send_instantly, and only then
// acks the timestamp back so the file is deleted. If the renderer hangs again
// (or is killed) before the event could leave the process, the file survives
// and the next boot retries — the signal is eventually delivered instead of
// being lost on the first attempt. Occasional duplicate reports are the
// accepted cost; events carry `breadcrumb_ts` for query-side dedupe.

export type { FreezeBreadcrumb };

// A breadcrumb the renderer never manages to ack (analytics disabled on
// self-hosted builds, or a persistently crashing boot path) must not be
// re-read on every launch forever. One week comfortably covers "user killed
// the app and reopened it days later" while bounding both the retry loop and
// the duplicate-report window.
export const FREEZE_BREADCRUMB_TTL_MS = 7 * 24 * 60 * 60 * 1000;

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
export function clearFreezeBreadcrumb(filePath: string): void {
  try {
    rmSync(filePath, { force: true });
  } catch {
    // Nothing to clear / permissions — ignore.
  }
}

/**
 * Read the breadcrumb WITHOUT deleting it. Deletion happens via
 * `ackFreezeBreadcrumb` once the renderer confirms the event was handed to
 * posthog-js — see the module comment for why.
 *
 * The file is still deleted eagerly in two cases, so an unreportable
 * breadcrumb can never become permanent boot noise:
 *   - malformed payload (corrupt JSON, missing `kind`, missing/invalid `ts`);
 *   - expired payload (`ts` older than FREEZE_BREADCRUMB_TTL_MS).
 *
 * Returns null when there's no breadcrumb (the normal case) or when the file
 * was dropped for one of the reasons above.
 */
export function readFreezeBreadcrumb(
  filePath: string,
  nowMs: number = Date.now(),
): FreezeBreadcrumb | null {
  let raw: string;
  try {
    raw = readFileSync(filePath, "utf8");
  } catch {
    return null;
  }
  const breadcrumb = parseBreadcrumb(raw);
  if (!breadcrumb || nowMs - breadcrumb.ts > FREEZE_BREADCRUMB_TTL_MS) {
    clearFreezeBreadcrumb(filePath);
    return null;
  }
  return breadcrumb;
}

/**
 * Acknowledge a reported breadcrumb: delete the file only if the on-disk
 * breadcrumb still carries the acked `ts`. The guard makes a late ack from a
 * previous read harmless — if a NEW failure wrote a fresh breadcrumb in the
 * meantime, its `ts` differs and the file is kept for the next boot.
 */
export function ackFreezeBreadcrumb(filePath: string, ts: number): void {
  let raw: string;
  try {
    raw = readFileSync(filePath, "utf8");
  } catch {
    return;
  }
  const breadcrumb = parseBreadcrumb(raw);
  if (breadcrumb && breadcrumb.ts !== ts) return;
  // Matching ts — reported, safe to delete. A malformed file is also deleted
  // here rather than kept: it could never be reported anyway.
  clearFreezeBreadcrumb(filePath);
}

/**
 * The breadcrumb crosses a process boundary and lives across app versions —
 * a future write shape or a corrupt file must never throw into boot. `kind`
 * and a finite `ts` are required: without `kind` the event can't be labeled,
 * and without `ts` neither the TTL nor the ack guard can work.
 */
function parseBreadcrumb(raw: string): FreezeBreadcrumb | null {
  try {
    const parsed: unknown = JSON.parse(raw);
    if (
      parsed &&
      typeof parsed === "object" &&
      typeof (parsed as FreezeBreadcrumb).kind === "string" &&
      typeof (parsed as FreezeBreadcrumb).ts === "number" &&
      Number.isFinite((parsed as FreezeBreadcrumb).ts)
    ) {
      return parsed as FreezeBreadcrumb;
    }
  } catch {
    // Corrupt JSON — caller drops the file.
  }
  return null;
}
