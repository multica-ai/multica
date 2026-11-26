import { afterEach, describe, expect, it, vi } from "vitest";
import { revealTimelineList, scrollToTimelineComment } from "./scroll-to-timeline-comment";

const frame = () => new Promise((resolve) => requestAnimationFrame(resolve));

function anchor(container: HTMLElement, top: number) {
  const el = document.createElement("div");
  el.id = "comment-target";
  // Positioned like a real row: its viewport top moves with the scroll.
  el.getBoundingClientRect = () => el.isConnected
    ? new DOMRect(0, top - container.scrollTop, 400, 100)
    : new DOMRect(0, 0, 0, 0);
  return el;
}

describe("scrollToTimelineComment", () => {
  afterEach(() => { document.body.innerHTML = ""; });

  it("follows an anchor that a commit replaces between frames instead of scrolling to the top", async () => {
    const container = document.createElement("div");
    container.getBoundingClientRect = () => new DOMRect(0, 0, 400, 600);
    Object.defineProperty(container, "clientHeight", { value: 600 });
    document.body.append(container);
    container.append(anchor(container, 5000));
    const onComplete = vi.fn();
    const onMounted = vi.fn(() => {
      // The same commit that mounted the row re-renders it before the
      // alignment frame: the node the landing found is gone.
      container.replaceChildren(anchor(container, 5000));
    });

    scrollToTimelineComment({ container, virtualizer: null, index: 0, commentId: "target", onMounted, onComplete });
    for (let i = 0; i < 10 && !onComplete.mock.calls.length; i++) await frame();

    expect(onMounted).toHaveBeenCalledTimes(1);
    expect(onComplete).toHaveBeenCalled();
    // 5000 - (600 - 100) / 2: the replacement row, centred.
    expect(container.scrollTop).toBe(4750);
  });

  it("yields immediately when the user scrolls during alignment", async () => {
    const container = document.createElement("div");
    container.getBoundingClientRect = () => new DOMRect(0, 0, 400, 600);
    Object.defineProperty(container, "clientHeight", { value: 600 });
    document.body.append(container);
    let driftingTop = 5000;
    const el = document.createElement("div");
    el.id = "comment-target";
    el.getBoundingClientRect = () => new DOMRect(0, (driftingTop += 50) - container.scrollTop, 400, 100);
    container.append(el);

    const onComplete = vi.fn();
    const onUserCancel = vi.fn();
    scrollToTimelineComment({ container, virtualizer: null, index: 0, commentId: "target", onComplete, onUserCancel });
    await frame();
    await frame();

    // User scrolls elsewhere mid-alignment.
    container.scrollTop = 120;
    container.dispatchEvent(new Event("wheel"));
    await frame();
    await frame();

    expect(onUserCancel).toHaveBeenCalledTimes(1);
    expect(onComplete).toHaveBeenCalledTimes(1);
    expect(container.scrollTop).toBe(120);
  });
});

describe("revealTimelineList", () => {
  afterEach(() => { vi.unstubAllGlobals(); });

  it("stops revealing as soon as the user scrolls", () => {
    const pending = new Map<number, FrameRequestCallback>();
    let nextId = 0;
    vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => { pending.set(++nextId, cb); return nextId; });
    vi.stubGlobal("cancelAnimationFrame", (id: number) => { pending.delete(id); });
    const container = document.createElement("div");
    container.getBoundingClientRect = () => new DOMRect(0, 0, 400, 600);
    const list = document.createElement("div");
    list.getBoundingClientRect = () => new DOMRect(0, 2000, 400, 100);
    const onUserCancel = vi.fn();

    revealTimelineList(container, list, onUserCancel);
    expect(pending.size).toBe(1);

    container.dispatchEvent(new Event("wheel"));
    expect(onUserCancel).toHaveBeenCalledTimes(1);
    expect(pending.size).toBe(0);
  });

  it("keeps one frame budget per list across the effect re-runs that restart it", () => {
    const pending = new Map<number, FrameRequestCallback>();
    let nextId = 0;
    vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => { pending.set(++nextId, cb); return nextId; });
    vi.stubGlobal("cancelAnimationFrame", (id: number) => { pending.delete(id); });
    const runFrames = (count: number) => {
      for (let i = 0; i < count && pending.size; i++) {
        const [id, cb] = pending.entries().next().value!;
        pending.delete(id);
        cb(0);
      }
    };
    let writes = 0;
    const container = { getBoundingClientRect: () => new DOMRect(0, 0, 400, 600) } as HTMLElement;
    Object.defineProperty(container, "scrollTop", { get: () => 0, set: () => { writes++; } });
    // Content above keeps the list below the viewport, so the reveal never ends on its own.
    const list = { getBoundingClientRect: () => new DOMRect(0, 2000, 400, 100) } as HTMLElement;

    let cancel = revealTimelineList(container, list);
    runFrames(50);
    for (let restart = 0; restart < 20; restart++) {
      cancel();
      cancel = revealTimelineList(container, list);
      runFrames(10);
    }
    runFrames(500);
    cancel();

    expect(writes).toBe(120);
    expect(pending.size).toBe(0);
  });
});
