import type { FreezeBreadcrumb } from "../../shared/freeze-breadcrumb";

// Turning a breadcrumb the previous session left behind into event properties.
//
// The breadcrumb context is assembled in the main process and can grow a field
// at any time, so props are built by explicit whitelist here rather than by
// spreading it: an unknown key is dropped, not forwarded. That matters because
// this context is the one place a raw identifier could reach telemetry.

/**
 * Build the event properties for a pending freeze/crash breadcrumb, field by
 * field. Anything not named here does not ship.
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
