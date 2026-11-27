import { writeFileSync, readFileSync, rmSync } from "node:fs";
import type { FreezeBreadcrumb } from "../shared/freeze-breadcrumb";

// When the renderer truly hangs or its process dies, it can't send telemetry
// itself — the thread is blocked or gone. The main process (always alive) is
// the only watcher that can react, but during the hang it can't reach the
// renderer's posthog-js either. So it writes a breadcrumb to disk; the next
// time a renderer boots, it reads the file and reports the event.
//
// Read and delete are deliberately SEPARATE steps. Reading used to delete in
// the same call, which loses the report in exactly the case we care about
// most: a deterministic freeze, where the user reopens the app and hits the
// same content again within seconds. The event is still sitting in a JS batch
// timer that the second hang kills, the process is force-quit, and the file is
// already gone (MUL-4115: three hangs, zero events). Now the renderer reports
// first and acknowledges after, so a failed attempt is simply retried on the
// next boot.

export type { FreezeBreadcrumb };

/**
 * How long an unacknowledged breadcrumb keeps being retried. Without a bound,
 * a client that can never deliver (analytics disabled, permanently offline)
 * would re-report the same ancient failure on every launch forever.
 */
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
 * in-thread long-frame watchdog already reports it — keeping the breadcrumb
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
 * Read the pending breadcrumb WITHOUT deleting it. Returns null when there is
 * none (the normal case).
 *
 * Payloads that can never become a useful report are deleted here rather than
 * retried: corrupt JSON, and anything older than the TTL. Everything else
 * survives until `ackFreezeBreadcrumb`.
 */
export function readFreezeBreadcrumb(
  filePath: string,
  now = Date.now(),
): FreezeBreadcrumb | null {
  let raw: string;
  try {
    raw = readFileSync(filePath, "utf8");
  } catch {
    return null;
  }

  let breadcrumb: FreezeBreadcrumb | null = null;
  try {
    const parsed: unknown = JSON.parse(raw);
    if (
      parsed &&
      typeof parsed === "object" &&
      typeof (parsed as FreezeBreadcrumb).kind === "string"
    ) {
      breadcrumb = parsed as FreezeBreadcrumb;
    }
  } catch {
    // Corrupt JSON — fall through to the discard below.
  }

  if (!breadcrumb) {
    discard(filePath);
    return null;
  }
  if (isExpired(breadcrumb, now)) {
    discard(filePath);
    return null;
  }
  return breadcrumb;
}

/**
 * Delete the breadcrumb the renderer just reported. The timestamp must match
 * the stored one exactly: a hang that happened between the read and the ack
 * has already overwritten the slot, and that newer failure must survive to be
 * reported on the next boot.
 */
export function ackFreezeBreadcrumb(filePath: string, ts: number): void {
  try {
    const parsed = JSON.parse(readFileSync(filePath, "utf8")) as unknown;
    if (
      !parsed ||
      typeof parsed !== "object" ||
      (parsed as FreezeBreadcrumb).ts !== ts
    ) {
      return;
    }
    rmSync(filePath, { force: true });
  } catch {
    // Already gone / unreadable — nothing to acknowledge.
  }
}

function isExpired(breadcrumb: FreezeBreadcrumb, now: number): boolean {
  if (typeof breadcrumb.ts !== "number" || !Number.isFinite(breadcrumb.ts)) {
    return true;
  }
  return now - breadcrumb.ts > FREEZE_BREADCRUMB_TTL_MS;
}

function discard(filePath: string): void {
  try {
    rmSync(filePath, { force: true });
  } catch {
    // If we can't delete it we'd re-read it next launch; acceptable over throwing.
  }
}
