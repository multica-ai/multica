import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { VirtuosoHandle } from "react-virtuoso";
import { useCommentAnchorCalibration } from "./use-comment-anchor-calibration";

// ---------------------------------------------------------------------------
// jsdom has no layout, so the geometry is stubbed: the scroll container sits at
// viewport top with a 600px height, and the target row's top is whatever the
// test says it is. That is enough to drive every branch, because the hook only
// ever reads two rects and compares them.
// ---------------------------------------------------------------------------

const TOP_GAP = 16;
const CONTAINER_HEIGHT = 600;

let observers: Array<{ callback: () => void; targets: Element[] }>;
let rafQueue: FrameRequestCallback[];

function flushFrame() {
  const queued = rafQueue;
  rafQueue = [];
  for (const cb of queued) cb(0);
}

/** Fire the ResizeObserver that is watching `el`, and nothing else. */
function fireResizeFor(el: Element) {
  for (const observer of observers) {
    if (observer.targets.includes(el)) observer.callback();
  }
}

describe("useCommentAnchorCalibration", () => {
  let container: HTMLElement;
  let wrapper: HTMLElement;
  let row: HTMLElement;
  let virtuosoRef: { current: VirtuosoHandle | null };
  let scrollToIndex: ReturnType<typeof vi.fn>;
  let targetTop: number;

  function setup(overrides: Partial<Parameters<typeof useCommentAnchorCalibration>[0]> = {}) {
    return renderHook(() =>
      useCommentAnchorCalibration({
        contentWrapperEl: wrapper,
        enabled: true,
        scrollContainerEl: container,
        targetCommentId: "c1",
        targetIndex: 3,
        topGap: TOP_GAP,
        virtuosoRef: virtuosoRef as never,
        ...overrides,
      }),
    );
  }

  beforeEach(() => {
    vi.useFakeTimers();
    observers = [];
    rafQueue = [];

    vi.stubGlobal(
      "ResizeObserver",
      class {
        targets: Element[] = [];
        constructor(private cb: () => void) {
          observers.push({ callback: () => this.cb(), targets: this.targets });
        }
        observe(el: Element) {
          this.targets.push(el);
        }
        unobserve(el: Element) {
          const i = this.targets.indexOf(el);
          if (i >= 0) this.targets.splice(i, 1);
        }
        disconnect() {
          this.targets.length = 0;
        }
      },
    );
    vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => {
      rafQueue.push(cb);
      return rafQueue.length;
    });
    vi.stubGlobal("cancelAnimationFrame", () => {});

    container = document.createElement("div");
    container.getBoundingClientRect = () => ({ height: CONTAINER_HEIGHT, top: 0 }) as DOMRect;
    Object.defineProperty(container, "clientHeight", { value: CONTAINER_HEIGHT });
    let scrollTop = 0;
    Object.defineProperty(container, "scrollTop", {
      get: () => scrollTop,
      set: (v: number) => {
        scrollTop = v;
      },
    });
    document.body.appendChild(container);

    wrapper = document.createElement("div");
    container.appendChild(wrapper);

    row = document.createElement("div");
    row.id = "comment-c1";
    targetTop = TOP_GAP;
    row.getBoundingClientRect = () => ({ height: 100, top: targetTop }) as DOMRect;
    wrapper.appendChild(row);

    scrollToIndex = vi.fn();
    virtuosoRef = { current: { scrollToIndex } as unknown as VirtuosoHandle };
  });

  afterEach(() => {
    document.body.innerHTML = "";
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  // The blocker this hook exists for: content ABOVE the target growing. The
  // target row's own box does not change and the scroll container is a
  // fixed-height viewport whose border-box never changes, so neither of those
  // observers fires. Only the auto-height content wrapper does.
  it("corrects when only the content wrapper resizes", () => {
    setup();
    act(() => flushFrame()); // initial check: already anchored, no-op
    expect(container.scrollTop).toBe(0);

    targetTop = TOP_GAP + 120; // a description image above the target decoded
    act(() => {
      fireResizeFor(wrapper);
      flushFrame();
    });

    expect(container.scrollTop).toBe(120);
  });

  it("corrects when only the target row resizes", () => {
    setup();
    act(() => flushFrame());

    targetTop = TOP_GAP + 40;
    act(() => {
      fireResizeFor(row);
      flushFrame();
    });

    expect(container.scrollTop).toBe(40);
  });

  it("still corrects when nothing resizes, via the bounded re-check", () => {
    // Net-zero case: content above grows while content below shrinks by the
    // same amount, so no observed box changes size but the target still moved.
    setup();
    act(() => flushFrame());

    targetTop = TOP_GAP + 30;
    act(() => {
      vi.advanceTimersByTime(250);
      flushFrame();
    });

    expect(container.scrollTop).toBe(30);
  });

  it("collapses multiple wake-ups in the same frame into one correction", () => {
    setup();
    act(() => flushFrame());

    targetTop = TOP_GAP + 50;
    act(() => {
      fireResizeFor(wrapper);
      fireResizeFor(row);
      vi.advanceTimersByTime(250);
      flushFrame();
    });

    // One correction, not three: three sources, one throttled dispatch. If each
    // source got its own write they would stack and overshoot.
    expect(container.scrollTop).toBe(50);
  });

  it("ignores deviation inside the dead zone", () => {
    setup();
    act(() => flushFrame());

    targetTop = TOP_GAP + 3;
    act(() => {
      fireResizeFor(wrapper);
      flushFrame();
    });

    expect(container.scrollTop).toBe(0);
    expect(scrollToIndex).not.toHaveBeenCalled();
  });

  it("hands a more-than-one-viewport deviation back to Virtuoso", () => {
    setup();
    act(() => flushFrame());

    targetTop = TOP_GAP + CONTAINER_HEIGHT + 10;
    act(() => {
      fireResizeFor(wrapper);
      flushFrame();
    });

    // Beyond a screen the row is likely unmounted; nudging scrollTop would be
    // guesswork against estimated heights, so Virtuoso re-anchors by index.
    expect(scrollToIndex).toHaveBeenCalledWith({ align: "start", index: 3, offset: -TOP_GAP });
    expect(container.scrollTop).toBe(0);
  });

  it("re-anchors by index when the target row is not mounted", () => {
    setup();
    act(() => flushFrame());
    row.remove();

    act(() => {
      fireResizeFor(wrapper);
      flushFrame();
    });

    expect(scrollToIndex).toHaveBeenCalledWith({ align: "start", index: 3, offset: -TOP_GAP });
  });

  it("stops for good once the user scrolls", () => {
    setup();
    act(() => flushFrame());

    act(() => container.dispatchEvent(new Event("wheel")));

    targetTop = TOP_GAP + 200;
    act(() => {
      fireResizeFor(wrapper);
      vi.advanceTimersByTime(1000);
      flushFrame();
    });

    // The user is reading. Never move the page under them, however wrong the
    // anchor now is.
    expect(container.scrollTop).toBe(0);
    expect(scrollToIndex).not.toHaveBeenCalled();
  });

  it("stops at the deadline", () => {
    setup();
    act(() => flushFrame());

    act(() => vi.advanceTimersByTime(5000));

    targetTop = TOP_GAP + 200;
    act(() => {
      fireResizeFor(wrapper);
      flushFrame();
    });

    expect(container.scrollTop).toBe(0);
  });

  it("does nothing when disabled", () => {
    setup({ enabled: false });

    targetTop = TOP_GAP + 200;
    act(() => {
      fireResizeFor(wrapper);
      flushFrame();
    });

    expect(container.scrollTop).toBe(0);
    expect(scrollToIndex).not.toHaveBeenCalled();
  });

  it("does not anchor the mount-time target, which Virtuoso already placed", () => {
    setup();
    act(() => flushFrame());

    // initialTopMostItemIndex owns the first landing; issuing scrollToIndex on
    // top of it would be a second writer for the same position.
    expect(scrollToIndex).not.toHaveBeenCalled();
  });

  it("re-anchors when the target changes while mounted", () => {
    const { rerender } = renderHook(
      ({ target }: { target: string }) =>
        useCommentAnchorCalibration({
          contentWrapperEl: wrapper,
          enabled: true,
          scrollContainerEl: container,
          targetCommentId: target,
          targetIndex: 7,
          topGap: TOP_GAP,
          virtuosoRef: virtuosoRef as never,
        }),
      { initialProps: { target: "c1" } },
    );
    act(() => flushFrame());
    expect(scrollToIndex).not.toHaveBeenCalled();

    act(() => rerender({ target: "c2" }));

    // Mount-only prop cannot move an already-mounted list — this is the only
    // way a passive inbox update lands on the newer comment.
    expect(scrollToIndex).toHaveBeenCalledWith({ align: "start", index: 7, offset: -TOP_GAP });
  });

  it("does not re-anchor a swapped target once the user has scrolled", () => {
    const { rerender } = renderHook(
      ({ target }: { target: string }) =>
        useCommentAnchorCalibration({
          contentWrapperEl: wrapper,
          enabled: true,
          scrollContainerEl: container,
          targetCommentId: target,
          targetIndex: 7,
          topGap: TOP_GAP,
          virtuosoRef: virtuosoRef as never,
        }),
      { initialProps: { target: "c1" } },
    );
    act(() => flushFrame());
    act(() => container.dispatchEvent(new Event("wheel")));

    act(() => rerender({ target: "c2" }));

    expect(scrollToIndex).not.toHaveBeenCalled();
  });
});
