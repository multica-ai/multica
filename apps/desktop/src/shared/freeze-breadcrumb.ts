/**
 * A freeze/crash breadcrumb persisted by the main process and flushed to
 * telemetry by the next renderer boot. Shared across main, preload, and
 * renderer because all three touch it. See main/freeze-breadcrumb.ts for the
 * read/write logic and the rationale.
 */
export interface FreezeBreadcrumb {
  /** "unresponsive" (hang) or "render-process-gone" (crash). */
  kind: string;
  /** Diagnostic context captured at failure time (route, window url, …). */
  context: Record<string, unknown>;
  /** Epoch ms when the failure was recorded. */
  ts: number;
  /** App version at failure time. */
  version: string;
}
