import type { CaptureEventOptions } from "@multica/core/analytics";
import type { FreezeBreadcrumb } from "../../shared/freeze-breadcrumb";
import { sanitizeHangStackFrames } from "../../shared/hang-stack";

// Reporting a failure the previous session couldn't report itself.
//
// Two things here are easy to get wrong, so both are pinned by tests:
//
// 1. ACK TIMING. `onCaptured` fires when posthog.capture() returns, which is a
//    hand-off to the SDK, NOT a delivery confirmation — posthog-js exposes no
//    delivery callback (`CaptureOptions` has `send_instantly` and `transport`,
//    and nothing else). Acking there would delete the breadcrumb while the
//    request is still in flight, so an app that freezes again or is killed a
//    moment later loses the report anyway — the exact MUL-4115 failure this
//    was meant to fix. We therefore ack after a grace window: if the process
//    dies inside it, the timer never fires, the file survives, and the next
//    boot retries. A duplicate report is the acceptable trade (they carry
//    `breadcrumb_ts`, so query-side dedupe is trivial); a lost one is not.
//
// 2. FIELD SELECTION. The breadcrumb context is assembled in the main process
//    and could grow a field at any time. Spreading it into telemetry props
//    means any such field ships automatically. Props are therefore built by
//    explicit whitelist below — unknown keys are dropped, not forwarded.

/**
 * How long to leave the breadcrumb on disk after hand-off. Long enough for an
 * instant send to complete on a slow link; short enough that the duplicate
 * window stays small.
 */
export const FREEZE_ACK_GRACE_MS = 10_000;

type CaptureFn = (
  name: string,
  props: Record<string, unknown>,
  options?: CaptureEventOptions,
) => void;

export interface FlushFreezeBreadcrumbDeps {
  getLastFreeze: () => FreezeBreadcrumb | null;
  ackFreeze: (ts: number) => void;
  capture: CaptureFn;
  graceMs?: number;
}

/**
 * Report a pending freeze/crash breadcrumb, then retire it once the report has
 * had time to leave. Returns a cleanup that cancels a pending ack — a
 * cancelled ack leaves the breadcrumb for the next boot, which is the safe
 * direction.
 */
export function flushFreezeBreadcrumb({
  getLastFreeze,
  ackFreeze,
  capture,
  graceMs = FREEZE_ACK_GRACE_MS,
}: FlushFreezeBreadcrumbDeps): () => void {
  const last = getLastFreeze();
  if (!last) return () => undefined;

  let ackTimer: ReturnType<typeof setTimeout> | null = null;
  const crashed = last.kind === "render-process-gone";

  capture(crashed ? "client_crash" : "client_unresponsive", buildFreezeEventProps(last), {
    // The batch timer lives in the thread that already froze once and may be
    // about to freeze again.
    sendInstantly: true,
    onCaptured: () => {
      ackTimer = setTimeout(() => ackFreeze(last.ts), graceMs);
    },
  });

  return () => {
    if (ackTimer) clearTimeout(ackTimer);
  };
}

/**
 * Build the event properties from a breadcrumb, field by field. Anything not
 * named here does not ship — including a future key added to the context in
 * the main process.
 */
export function buildFreezeEventProps(
  breadcrumb: FreezeBreadcrumb,
): Record<string, unknown> {
  const crashed = breadcrumb.kind === "render-process-gone";
  const context = (breadcrumb.context ?? {}) as Record<string, unknown>;

  return {
    source: crashed ? "render-process-gone" : "main-unresponsive",
    recovered: false,
    breadcrumb_ts: breadcrumb.ts,
    crashed_version: breadcrumb.version,
    ...routeProps(context.desktopRoute),
    ...stackProps(context.stack),
    ...crashProps(context.details),
  };
}

/**
 * `path` is flattened to the top level under the same name the in-thread
 * watchdog uses, so both hang sources group in one query. The value arrives
 * already bucketed to a template and carries no slug or resource id — see
 * renderer-route-context.
 */
function routeProps(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object") return {};
  const route = value as Record<string, unknown>;
  return {
    ...(typeof route.path === "string" ? { path: route.path } : {}),
    ...(typeof route.surface === "string" ? { surface: route.surface } : {}),
  };
}

/**
 * Frames are rebuilt here rather than forwarded.
 *
 * The capture side already whitelists, but its output travels through an
 * on-disk breadcrumb that `readFreezeBreadcrumb` barely validates — it only
 * has to survive version skew. So the array reaching this function could come
 * from an older build, a corrupt file, or a future writer, and shipping it
 * as-is would put whatever it contains (a `scopeChain` handle, an absolute
 * install path) straight into telemetry. Re-sanitizing is cheap; trusting the
 * file is not.
 *
 * The top frame is also flattened onto the event so a hang groups by the
 * function that blocked the thread without unpacking the array.
 */
function stackProps(value: unknown): Record<string, unknown> {
  const frames = sanitizeHangStackFrames(value);
  if (!frames) return {};
  const top = frames[0]!;
  return {
    stack: frames,
    stack_depth: frames.length,
    stack_function: top.functionName,
    ...(top.url ? { stack_url: top.url } : {}),
    stack_line: top.lineNumber,
  };
}

/** Electron's render-process-gone details: two scalars, nothing else. */
function crashProps(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object") return {};
  const details = value as Record<string, unknown>;
  return {
    ...(typeof details.reason === "string" ? { crash_reason: details.reason } : {}),
    ...(typeof details.exitCode === "number"
      ? { crash_exit_code: details.exitCode }
      : {}),
  };
}
