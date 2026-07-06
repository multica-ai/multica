import type { RendererRouteContextInput } from "../../../shared/renderer-route-context";

// Renderer-side mirror of the last route context reported to the main
// process (PageviewTracker → setRendererRouteContext IPC). Keeping it here
// gives the shared freeze watchdog a lazy path provider from the SAME source
// the main process uses for freeze breadcrumbs, so a longtask event and a
// main-unresponsive breadcrumb for the same freeze attribute to the same
// route (MUL-4120). Module-level state matches the watchdog's page-lifetime
// singleton scope.

let current: RendererRouteContextInput | null = null;

/** Record the route context that was just reported to the main process. */
export function setDiagnosticRoute(context: RendererRouteContextInput): void {
  current = context;
}

/**
 * Current surface path for freeze-watchdog attribution: the active tab's
 * memory-router pathname, or the `/login` / overlay synthetic path. Undefined
 * until the first report (the watchdog then falls back to
 * `location.pathname`).
 */
export function getDiagnosticRoutePath(): string | undefined {
  return current?.path;
}
