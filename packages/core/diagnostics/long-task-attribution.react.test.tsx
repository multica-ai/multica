// @vitest-environment jsdom
import { afterAll, afterEach, expect, it, vi } from "vitest";
import { startTransition, useEffect } from "react";
import { flushSync } from "react-dom";
import { createRoot } from "react-dom/client";
import { attributeLongTask, recordMeasureEntry, resetLongTaskAttribution } from "./long-task-attribution";

// React emits its component measures only where console.timeStamp exists,
// which it checks once, when react-dom loads.
const originalTimeStamp = vi.hoisted(() => {
  const original = console.timeStamp;
  console.timeStamp = () => {};
  return original;
});

afterEach(() => resetLongTaskAttribution());
afterAll(() => {
  console.timeStamp = originalTimeStamp;
});

function busy(ms: number) {
  const start = performance.now();
  while (performance.now() - start < ms) {
    // Hold the thread, as a slow render or handler would.
  }
}

it("charges no render time to a task that ran between slices of a transition render", async () => {
  const renders: { start: number; end: number }[] = [];
  let otherTask: { start: number; duration: number } | null = null;
  let scheduled = false;
  let committed!: () => void;
  const done = new Promise<void>((resolve) => (committed = resolve));

  function Row({ index, version }: { index: number; version: number }) {
    const start = performance.now();
    if (version) busy(3);
    if (version) renders.push({ start, end: performance.now() });
    if (version && index === 5 && !scheduled) {
      scheduled = true;
      setTimeout(() => {
        const taskStart = performance.now();
        busy(90);
        otherTask = { start: taskStart, duration: performance.now() - taskStart };
      }, 0);
    }
    return <div>{`${index}:${version}`}</div>;
  }
  function App({ version }: { version: number }) {
    useEffect(() => {
      if (version) committed();
    }, [version]);
    return Array.from({ length: 30 }, (_, index) => <Row key={index} index={index} version={version} />);
  }

  const container = document.createElement("div");
  const root = createRoot(container);
  flushSync(() => root.render(<App version={0} />));
  performance.clearMeasures();
  startTransition(() => root.render(<App version={1} />));
  await done;

  const measures = performance.getEntriesByType("measure") as PerformanceMeasure[];
  for (const measure of measures) recordMeasureEntry(measure);
  root.unmount();

  const task = otherTask as { start: number; duration: number } | null;
  expect(task).not.toBeNull();
  expect(measures.some((m) => m.name === "\u200bRow")).toBe(true);
  // The timer ran between slices: no Row rendered during it.
  expect(renders.filter((r) => r.start < task!.start + task!.duration && r.end > task!.start)).toEqual([]);

  const attribution = attributeLongTask(task!.start, task!.duration);
  expect(attribution.components.find((c) => c.name === "Row")).toBeUndefined();
  expect(attribution.spanningComponents.map((c) => c.name)).toContain("Row");
});
