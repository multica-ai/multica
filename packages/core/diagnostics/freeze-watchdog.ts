// Client freeze watchdog — shared by web and desktop.
//
// Installs a long-task observer in the main thread. A "long task" is any
// stretch where the thread didn't return to the event loop; the browser
// already tracks them and delivers each entry once the thread unblocks, so
// even a multi-second freeze reports its duration after the fact. We only
// emit for blocks at or above FREEZE_THRESHOLD_MS to keep this to genuine
// "almost froze" events, not the normal 50–600ms render cost.
//
// This is the in-thread, recoverable tier: it catches freezes the thread
// survives. A true non-recoverable hang (the thread never unblocks) can only
// be caught from outside — on desktop that is the main process `unresponsive`
// handler (see apps/desktop renderer-recovery). Web has no free external
// watcher, so this observer is its only freeze signal for now.
//
// The emitted `client_unresponsive` event carries `client_type` automatically
// (an analytics super-property), so desktop vs web is queryable without any
// platform branch here.

import { captureEvent } from "../analytics";

// 2s is well above the normal switch/render cost (measured 50–600ms) and just
// under Electron's renderer-hang threshold, so an event here means "the user
// felt a real stall" without flooding on routine heavy renders.
const FREEZE_THRESHOLD_MS = 2000;

// A single sustained freeze is delivered by the browser as several separate
// long-task entries, so emitting per entry makes client_unresponsive volume
// grow without bound with the freeze length (MUL-3331). A global cooldown caps
// it to at most one event per window. Module-level (page-lifetime) state is the
// right scope here — it matches the `installed` singleton and resets on a full
// reload, which is rare and itself a distinct signal. No route bucketing: a
// global window is the most direct cap on volume.
const COOLDOWN_MS = 60_000;
let lastEmitMs = 0;

let installed = false;

/**
 * Install the long-task observer. Safe to call multiple times (idempotent) and
 * safe on the server (no-op when `window` / `PerformanceObserver` is absent).
 * Call once from a client-only effect.
 */
export function installFreezeWatchdog(): void {
  if (installed) return;
  if (typeof window === "undefined") return;
  if (typeof PerformanceObserver === "undefined") return;
  installed = true;

  try {
    const observer = new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) {
        if (entry.duration < FREEZE_THRESHOLD_MS) continue;
        // Cooldown is checked only against qualifying freezes, so sub-threshold
        // long tasks neither emit nor reset the window.
        const now = Date.now();
        if (now - lastEmitMs < COOLDOWN_MS) continue;
        lastEmitMs = now;
        captureEvent("client_unresponsive", {
          source: "longtask",
          duration_ms: Math.round(entry.duration),
          path: typeof location !== "undefined" ? location.pathname : undefined,
        });
      }
    });
    // No `buffered: true` — we only want freezes from now on. Replaying tasks
    // buffered before install would mislabel slow startup as a runtime freeze.
    observer.observe({ type: "longtask" });
  } catch {
    // longtask entry type unsupported on this engine — nothing else to do.
  }
}
