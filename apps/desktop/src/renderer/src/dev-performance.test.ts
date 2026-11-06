// @vitest-environment node
import { performance, PerformanceObserver } from "node:perf_hooks";
import { afterEach, expect, it, vi } from "vitest";
import { installReactTimingCleanup } from "./dev-performance";

const component = "\u200bTimingCleanupFixture";
const detail = { devtools: { track: "Components \u269b", properties: [["props", "fixture"]] } };
let stop = () => {};

afterEach(() => {
  stop();
  performance.clearMeasures(component);
  performance.clearMeasures("app:fixture");
  performance.clearMeasures("\u200bNonReactFixture");
  vi.unstubAllGlobals();
});

it("releases React component timings after delivery while retaining application timings", async () => {
  vi.stubGlobal("PerformanceObserver", PerformanceObserver);
  vi.stubGlobal("performance", performance);
  stop = installReactTimingCleanup();
  performance.measure("app:fixture", { start: 0, end: 1 });
  performance.measure("\u200bNonReactFixture", { start: 0, end: 1 });
  for (let i = 0; i < 1000; i++) {
    performance.measure(component, { start: 0, end: 1, detail });
  }
  await vi.waitFor(() => expect(performance.getEntriesByName(component)).toHaveLength(0));
  expect(performance.getEntriesByName("app:fixture")).toHaveLength(1);
  expect(performance.getEntriesByName("\u200bNonReactFixture")).toHaveLength(1);
});

it("disconnects when the development entry is disposed", async () => {
  vi.stubGlobal("PerformanceObserver", PerformanceObserver);
  vi.stubGlobal("performance", performance);
  stop = installReactTimingCleanup();
  stop();
  performance.measure(component, { start: 0, end: 1, detail });
  await new Promise<void>((resolve) => setImmediate(resolve));
  expect(performance.getEntriesByName(component)).toHaveLength(1);
});

it("also releases buffered React timings recorded before installation", async () => {
  vi.stubGlobal("PerformanceObserver", PerformanceObserver);
  vi.stubGlobal("performance", performance);
  performance.measure(component, { start: 0, end: 1, detail });
  stop = installReactTimingCleanup();
  await vi.waitFor(() => expect(performance.getEntriesByName(component)).toHaveLength(0));
});
