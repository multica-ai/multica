import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { keepThreadRowAnchored } from "./keep-thread-rows-anchored";

// Geometry lives in content coordinates; the viewport is [scrollTop, scrollTop + 500].
let observers: { callback: ResizeObserverCallback; disconnected: boolean }[] = [];
beforeEach(() => {
  observers = [];
  vi.stubGlobal("ResizeObserver", class {
    entry: { callback: ResizeObserverCallback; disconnected: boolean };
    constructor(callback: ResizeObserverCallback) { this.entry = { callback, disconnected: false }; observers.push(this.entry); }
    observe() {}
    disconnect() { this.entry.disconnected = true; }
  });
});
afterEach(() => { vi.unstubAllGlobals(); document.body.innerHTML = ""; });

function thread(heights: number[], scrollTop: number) {
  const container = document.createElement("div");
  const rows = heights.map(() => {
    const row = document.createElement("div");
    row.setAttribute("data-virtual-thread-row", "");
    container.append(row);
    return row;
  });
  document.body.append(container);
  let top = scrollTop;
  Object.defineProperty(container, "scrollTop", { get: () => top, set: (v: number) => { top = v; } });
  container.getBoundingClientRect = () => new DOMRect(0, 0, 400, 500);
  rows.forEach((row, i) => {
    row.getBoundingClientRect = () => {
      const offset = heights.slice(0, i).reduce((a, b) => a + b, 0);
      return new DOMRect(0, offset - top, 400, heights[i]);
    };
  });
  return { container, rows, heights, scroll: () => top };
}

async function frame() {
  await Promise.resolve();
  for (const o of observers.filter((o) => !o.disconnected)) o.callback([], {} as ResizeObserver);
}

describe("keepThreadRowAnchored", () => {
  it("follows a reply above the viewport that mounts shorter than its estimate", async () => {
    const t = thread([84, 320, 84, 84], 600);
    t.heights[1] = 84; // mounted: 320 → 84, wholly above the viewport (offset 84..404)
    keepThreadRowAnchored(t.container, t.rows[1]!, 320);
    await frame();
    expect(t.scroll()).toBe(364);
  });

  it("leaves the position alone when the list already compensated", async () => {
    const t = thread([84, 320, 84, 84], 600);
    t.heights[1] = 84;
    keepThreadRowAnchored(t.container, t.rows[1]!, 320);
    await Promise.resolve();
    t.container.scrollTop = 364; // Virtuoso's own compensation in its earlier observer
    for (const o of observers) o.callback([], {} as ResizeObserver);
    expect(t.scroll()).toBe(364);
  });

  it("undoes a list compensation for a reply that mounted in or below the viewport", async () => {
    const t = thread([400, 84, 320, 84], 300);
    t.heights[2] = 84; // offset 484, inside the viewport
    keepThreadRowAnchored(t.container, t.rows[2]!, 320);
    await Promise.resolve();
    t.container.scrollTop = 64; // a whole-item compensation the reader must not see
    for (const o of observers) o.callback([], {} as ResizeObserver);
    expect(t.scroll()).toBe(300);
  });

  it("batches rows mounted in one commit", async () => {
    const t = thread([320, 320, 84, 84], 700);
    t.heights[0] = 84;
    t.heights[1] = 84;
    keepThreadRowAnchored(t.container, t.rows[0]!, 320);
    keepThreadRowAnchored(t.container, t.rows[1]!, 320);
    await frame();
    expect(t.scroll()).toBe(228);
    expect(observers).toHaveLength(1);
  });
});
