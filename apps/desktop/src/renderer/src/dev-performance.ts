/** Release React 19 development measures, whose cloned props otherwise remain
 * in the page's User Timing buffer for the entire lifetime of the dev window. */
export function installReactTimingCleanup(): () => void {
  const observer = new PerformanceObserver((list) => {
    const names = new Set<string>();
    for (const entry of list.getEntries()) {
      if (
        entry.entryType === "measure" &&
        entry.name.startsWith("\u200b") &&
        (entry as PerformanceMeasure).detail?.devtools?.track === "Components \u269b"
      ) {
        names.add(entry.name);
      }
    }
    for (const name of names) performance.clearMeasures(name);
  });
  observer.observe({ type: "measure", buffered: true });
  return () => observer.disconnect();
}
