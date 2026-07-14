import type { Capabilities } from "./types";

function supportsEntryType(type: string): boolean {
  if (typeof PerformanceObserver === "undefined") return false;
  const supported = (PerformanceObserver as unknown as { supportedEntryTypes?: string[] })
    .supportedEntryTypes;
  return Array.isArray(supported) ? supported.includes(type) : false;
}

/**
 * Probe browser support once at start. Unsupported signals are recorded as
 * `false` and simply skipped — the recorder never polyfills or fabricates
 * performance data for a missing API (MUL-4466 §13).
 */
export function detectCapabilities(reactCommit: boolean): Capabilities {
  return {
    longTask: supportsEntryType("longtask"),
    longAnimationFrame: supportsEntryType("long-animation-frame"),
    eventTiming: supportsEntryType("event"),
    reactCommit,
    resourceTiming: supportsEntryType("resource"),
    mutationObserver: typeof MutationObserver !== "undefined",
  };
}
