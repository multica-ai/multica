// Development-only cap on the User Timing buffer — shared by web and desktop.
//
// React's development build records a `performance.measure` entry, carrying a
// changed-props `detail`, for component renders and never clears them. The
// browser keeps User Timing entries for the life of the page, so a dev window
// that stays open while runs stream grows without bound: about 1.4KB per entry
// at 50–200 entries a second, mostly in native memory that V8's heap limit
// does not cap. A Desktop Canary left open for two days reached 30GB+
// (MUL-8781). Production React emits no measures, so this is dev-only.
//
// DevTools Performance recordings capture measures as they are emitted, so
// emptying the buffer loses nothing a recording would show.

const CLEAR_INTERVAL_MS = 60_000;

let installed = false;

/**
 * Periodically empty the User Timing measure buffer in development. Safe to
 * call multiple times (idempotent) and safe on the server. Call once from a
 * client-only effect.
 */
export function installUserTimingCap(): void {
  if (installed) return;
  if (process.env.NODE_ENV === "production") return;
  if (typeof window === "undefined") return;
  if (typeof performance === "undefined" || typeof performance.clearMeasures !== "function") return;
  installed = true;

  setInterval(() => performance.clearMeasures(), CLEAR_INTERVAL_MS);
}
