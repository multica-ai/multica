// Cap on the User Timing buffer for development windows — shared by web and
// desktop, which each install it from their entry point only in development.
//
// React's development build records a `performance.measure` entry, carrying a
// changed-props `detail`, for component renders and never clears them. The
// browser keeps User Timing entries for the life of the page, so a dev window
// that stays open while runs stream grows without bound: about 1.4KB per entry
// at 50–200 entries a second, mostly in native memory that V8's heap limit
// does not cap. A Desktop Canary left open for two days reached 30GB+
// (MUL-8781). Production React emits no measures, so production never needs
// this.
//
// DevTools Performance recordings capture measures as they are emitted, so
// emptying the buffer loses nothing a recording would show.

const CLEAR_INTERVAL_MS = 60_000;

let installed = false;

/**
 * Periodically empty the User Timing measure buffer. Safe to call multiple
 * times (idempotent) and safe on the server. The caller decides whether this
 * is a development build.
 */
export function installUserTimingCap(): void {
  if (installed) return;
  if (typeof window === "undefined") return;
  if (typeof performance === "undefined" || typeof performance.clearMeasures !== "function") return;
  installed = true;

  setInterval(() => performance.clearMeasures(), CLEAR_INTERVAL_MS);
}
