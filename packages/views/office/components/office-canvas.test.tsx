/**
 * @vitest-environment jsdom
 *
 * The office fills its page edge to edge. When the page changes width, as it
 * does when the nav panel collapses or opens, the floor is laid out again for the
 * new shape instead of being left at the old one with the page's background
 * showing at its sides.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import { unitsFor, type ToWorker } from "../lib/pixtuoid/office-protocol";
import { OfficeCanvas } from "./office-canvas";

const posted: ToWorker[] = [];
const observers: Array<() => void> = [];

const workers: FakeWorker[] = [];

class FakeWorker {
  onmessage: ((event: { data: unknown }) => void) | null = null;
  constructor() {
    workers.push(this);
  }
  postMessage(message: ToWorker) {
    posted.push(message);
  }
  terminate() {}
}

class FakeResizeObserver {
  constructor(private readonly callback: () => void) {}
  observe() {
    observers.push(this.callback);
  }
  unobserve() {}
  disconnect() {}
}

function sizes() {
  return posted.filter((message): message is Extract<ToWorker, { type: "size" }> => message.type === "size");
}

describe("OfficeCanvas", () => {
  let width = 1180;
  let height = 780;

  beforeEach(() => {
    posted.length = 0;
    workers.length = 0;
    observers.length = 0;
    width = 1180;
    height = 780;
    vi.stubGlobal("Worker", FakeWorker);
    vi.stubGlobal("ResizeObserver", FakeResizeObserver);
    HTMLCanvasElement.prototype.transferControlToOffscreen = () => ({}) as OffscreenCanvas;
    Object.defineProperty(HTMLElement.prototype, "clientWidth", { configurable: true, get: () => width });
    Object.defineProperty(HTMLElement.prototype, "clientHeight", { configurable: true, get: () => height });
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    Reflect.deleteProperty(HTMLCanvasElement.prototype, "transferControlToOffscreen");
    Reflect.deleteProperty(HTMLElement.prototype, "clientWidth");
    Reflect.deleteProperty(HTMLElement.prototype, "clientHeight");
  });

  const renderCanvas = () =>
    render(<OfficeCanvas sessions={[]} tagText={() => ""} onPick={() => {}} onMove={() => {}} />);

  it("lays the floor out for the page's own shape", () => {
    renderCanvas();

    expect(sizes().at(-1)?.units).toEqual(unitsFor(1180, 780));
  });

  it("lays the floor out again when the page widens, as it does once the nav panel collapses", () => {
    renderCanvas();
    width = 1460;

    act(() => observers.forEach((notify) => notify()));

    expect(sizes().at(-1)?.units).toEqual(unitsFor(1460, 780));
    expect(sizes().at(-1)?.units.w).toBeGreaterThan(unitsFor(1180, 780).w);
  });

  it("lays the floor out again when the page narrows, as it does once the nav panel opens", () => {
    renderCanvas();
    width = 900;

    act(() => observers.forEach((notify) => notify()));

    expect(sizes().at(-1)?.units).toEqual(unitsFor(900, 780));
  });

  it("stretches the canvas over the whole page, so no background is left at its sides", () => {
    const { container } = renderCanvas();

    const canvas = container.querySelector("canvas")!;
    expect(canvas.className).toContain("absolute inset-0 size-full");
  });

  it("names the top rooms above their workers' tags and the lower rooms below the room", () => {
    render(
      <OfficeCanvas sessions={[]} tagText={() => ""} onPick={() => {}} onMove={() => {}} roomNames={["North", "East", "South", "West"]} />,
    );
    act(() =>
      workers[0]!.onmessage?.({
        data: {
          type: "rooms",
          rooms: [
            { x: 104, y: 60, w: 66, h: 44 },
            { x: 184, y: 60, w: 66, h: 44 },
            { x: 104, y: 112, w: 66, h: 44 },
            { x: 184, y: 112, w: 66, h: 44 },
          ],
          tank: null,
          boards: [],
        },
      }),
    );

    expect(screen.getByText("North").dataset.roomName).toBe("above");
    expect(screen.getByText("East").dataset.roomName).toBe("above");
    expect(screen.getByText("South").dataset.roomName).toBe("below");
    expect(screen.getByText("West").dataset.roomName).toBe("below");
  });
});
