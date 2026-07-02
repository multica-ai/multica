import type { RendererRouteContext } from "./renderer-route-context";
import type { CpuProfilePayload } from "./cpu-profile";

/**
 * Diagnostic context captured at failure time and carried in a freeze/crash
 * breadcrumb. This is an EXPLICIT whitelist (MUL-3738): every field below is a
 * known, low-sensitivity diagnostic, and nothing else is allowed in. It
 * replaces the previous `Record<string, unknown>` free bag, which let any
 * caller-supplied key flow to telemetry unchecked. To add a field, add it here
 * and confirm it carries no user content — code locations and bucketed routes
 * only, never bodies, parameters, variable values, resource ids, or heap data.
 */
export interface FreezeDiagnosticContext {
  /** Renderer window URL at failure time — an asar/file path, not an app route. */
  windowUrl?: string;
  /** Sanitized renderer route context (bucketed app route + surface). */
  desktopRoute?: RendererRouteContext;
  /** `render-process-gone` crash details (reason / exit code). */
  details?: { reason?: string; exitCode?: number };
  /** `preload-error` script path (our own preload, an asar/file path). */
  preloadPath?: string;
  /** `preload-error` formatted error (stack — code locations). */
  error?: string;
  /** CPU sampling profile of the hung renderer (P0① / MUL-3738). */
  cpuProfile?: CpuProfilePayload;
}

/**
 * A freeze/crash breadcrumb persisted by the main process and flushed to
 * telemetry by the next renderer boot. Shared across main, preload, and
 * renderer because all three touch it. See main/freeze-breadcrumb.ts for the
 * read/write logic and the rationale.
 */
export interface FreezeBreadcrumb {
  /** "unresponsive" (hang) or "render-process-gone" (crash). */
  kind: string;
  /** Diagnostic context captured at failure time — explicit whitelist above. */
  context: FreezeDiagnosticContext;
  /** Epoch ms when the failure was recorded. */
  ts: number;
  /** App version at failure time. */
  version: string;
}
