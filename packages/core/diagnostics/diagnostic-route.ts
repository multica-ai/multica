// Last-known app route for freeze diagnostics, shared by web and desktop.
//
// The freeze watchdog runs outside React (a PerformanceObserver callback), so
// it can't read the router. On WEB, `location.pathname` IS the app route, so no
// one needs to set this. On DESKTOP the renderer uses an in-memory router
// (`createMemoryRouter`), so `location.pathname` is the asar file path, not the
// route — the desktop pageview tracker therefore pushes the bucketed route
// template here, and the watchdog reads it to attribute longtask events to the
// real route instead of the asar path (MUL-3738, P0②).
//
// The value is already a bucketed route TEMPLATE (e.g. `/:slug/inbox`), set by
// the same code that reports the renderer route context — never a raw path with
// resource ids.

let currentRouteTemplate: string | undefined;

/** Set the current bucketed app route template (desktop pageview tracker). */
export function setDiagnosticRoute(routeTemplate: string | undefined): void {
  currentRouteTemplate = routeTemplate || undefined;
}

/** Read the current bucketed app route template, or undefined if none set. */
export function getDiagnosticRoute(): string | undefined {
  return currentRouteTemplate;
}
