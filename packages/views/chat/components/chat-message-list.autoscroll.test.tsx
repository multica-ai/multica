import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { chatKeys } from "@multica/core/chat/queries";
import type { TaskMessagePayload } from "@multica/core/types";
import type { ReactElement } from "react";
import enChat from "../../locales/en/chat.json";
import { ChatMessageList } from "./chat-message-list";

// Auto-scroll wiring: the list must keep the live end visible through streaming
// growth and composer resizes, releasing only on real reader input. The latch
// decision table is canonical in transcript-follow.test.ts and the scroll
// geometry in stick-to-bottom.test.ts; this suite drives the real component
// through the DOM signals those decisions are wired to. Reader gestures here
// are wheel INPUT followed by the scroll it causes — position-only moves model
// the browser (clamps), input-led moves model the reader.

// Virtuoso cannot render rows in jsdom's zero-height viewport. The stub keeps
// the row count visible, surfaces `followOutput` verdicts as attributes,
// captures `totalListHeightChanged` so the harness can report content growth
// the way the real Virtuoso does, and answers `scrollToIndex` by moving the
// scroller to the bottom — the real component's job, and the only route the
// bottom-stick may take (MUL-6879).
let reportContentHeightChanged: (() => void) | undefined;
let pinCalls: unknown[] = [];
let scrollToLastItemEnd: (() => void) | undefined;

vi.mock("react-virtuoso", () => ({
  Virtuoso: ({
    ref,
    data,
    itemContent,
    computeItemKey,
    followOutput,
    totalListHeightChanged,
  }: {
    ref?: { current: unknown } | ((handle: unknown) => void);
    data: unknown[];
    itemContent: (i: number, item: unknown) => ReactElement;
    computeItemKey: (i: number, item: unknown) => string;
    followOutput?: (atBottom: boolean) => "smooth" | "auto" | false;
    totalListHeightChanged?: () => void;
  }) => {
    reportContentHeightChanged = totalListHeightChanged;
    const handle = {
      scrollToIndex: (location: unknown) => {
        pinCalls.push(location);
        scrollToLastItemEnd?.();
      },
    };
    if (typeof ref === "function") ref(handle);
    else if (ref) ref.current = handle;
    return (
      <div
        data-testid="virtuoso-rows"
        data-follow-at-bottom={String(followOutput?.(true))}
        data-follow-away-from-bottom={String(followOutput?.(false))}
      >
        {data.map((item, i) => (
          <div key={computeItemKey(i, item)} data-row-key={computeItemKey(i, item)}>
            {itemContent(i, item)}
          </div>
        ))}
      </div>
    );
  },
}));

const TEST_RESOURCES = { en: { chat: enChat } };
const TASK_ID = "6af44cbe-80ab-4dfe-b07d-bd3cfd588f4d";

const VIEWPORT = 600;

interface FakeObserver {
  targets: Element[];
  fire: () => void;
}

let observers: FakeObserver[] = [];
let animationFrames = new Map<number, FrameRequestCallback>();
let nextAnimationFrameId = 1;

function observedTargets(): Element[] {
  return observers.flatMap((o) => o.targets);
}

function fireResizeObservers() {
  act(() => {
    for (const observer of observers) observer.fire();
  });
}

function renderFrame() {
  const callbacks = [...animationFrames.values()];
  animationFrames.clear();
  now += 16;
  for (const callback of callbacks) callback(now);
}

// Confirmed motion gets a short settle window; tests control that clock.
let now = 0;
function gestureSettles() {
  now += 301;
}

beforeEach(() => {
  observers = [];
  animationFrames = new Map();
  nextAnimationFrameId = 1;
  reportContentHeightChanged = undefined;
  pinCalls = [];
  scrollToLastItemEnd = undefined;
  now = 0;
  vi.spyOn(Date, "now").mockImplementation(() => now);
  vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
    const id = nextAnimationFrameId++;
    animationFrames.set(id, callback);
    return id;
  });
  vi.stubGlobal("cancelAnimationFrame", (id: number) => {
    animationFrames.delete(id);
  });
  vi.stubGlobal(
    "ResizeObserver",
    class {
      targets: Element[] = [];
      constructor(private callback: () => void) {
        observers.push(this as unknown as FakeObserver);
      }
      observe(target: Element) {
        this.targets.push(target);
      }
      unobserve(target: Element) {
        this.targets = this.targets.filter((t) => t !== target);
      }
      disconnect() {
        this.targets = [];
        observers = observers.filter((o) => (o as unknown as this) !== this);
      }
      fire() {
        this.callback();
      }
    },
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

interface Scroller {
  el: HTMLElement;
  scrollTop: number;
  distanceFromBottom(): number;
  /** A streamed chunk grew the content; Virtuoso reports the new height. */
  grow(px: number): void;
  /** Content shrank (a fold closing); the browser clamps scrollTop. */
  shrinkContent(px: number): void;
  /** The composer collapsed, giving the list `px` back; scrollTop clamps. */
  growViewport(px: number): void;
  /** The composer grew, taking `px` off the list's height. */
  shrinkViewport(px: number): void;
  /** Reader wheel input scrolling up by `px`, and the scroll it causes. */
  readerScrollsUp(px: number): void;
  /** Starts a one-finger touch gesture at `clientY`. */
  touchStart(clientY: number): void;
  /** Moves that finger and then scrolls the list up by `scrollPx`. */
  touchMove(clientY: number, scrollPx: number): void;
  touchEnd(): void;
  /** Touch momentum scrolls the list after the finger lifts. */
  touchMomentumScrollsUp(px: number): void;
  /** Reader wheel input landing `fromBottom` px above the live end. */
  readerScrollsTo(fromBottom: number): void;
  /**
   * Wheel input the list never consumed: it bubbles from a nested scroller
   * (or hits a list with nothing to scroll), so no scroll event follows.
   */
  wheelWithoutScroll(px: number): void;
  /** PageUp bubbling from a focused control inside a row. */
  pageUpFromRowControl(): void;
  /** Shift+Space (pages UP) bubbling from a focused control inside a row. */
  shiftSpaceFromRowControl(): void;
  /** The browser scrolls the list itself (answering a key), no input seen. */
  browserScrollsListUp(px: number): void;
  /** Every `scrollToIndex` the bottom-stick asked Virtuoso for. */
  pinCalls(): unknown[];
  /** Writes to `scrollTop` from outside Virtuoso — always a bug (MUL-6879). */
  directScrollTopWrites(): number;
}

function scroller(el: HTMLElement, initialContent = 2000): Scroller {
  const state = { scrollTop: 0, contentHeight: initialContent, viewportHeight: VIEWPORT };
  // Open at the bottom, matching Virtuoso's `initialTopMostItemIndex: LAST`.
  state.scrollTop = Math.max(0, state.contentHeight - state.viewportHeight);
  let directScrollTopWrites = 0;

  Object.defineProperties(el, {
    scrollHeight: { configurable: true, get: () => state.contentHeight },
    clientHeight: { configurable: true, get: () => state.viewportHeight },
    scrollTop: {
      configurable: true,
      get: () => state.scrollTop,
      set: (value: number) => {
        directScrollTopWrites++;
        state.scrollTop = value;
      },
    },
  });

  // What the mock Virtuoso does for `scrollToIndex({ index: "LAST", align:
  // "end" })`: the list moves its own scroller. Nothing else may write
  // scrollTop, so this bypasses the counted setter.
  scrollToLastItemEnd = () => {
    state.scrollTop = Math.max(0, state.contentHeight - state.viewportHeight);
  };
  pinCalls = [];

  const scrollEvent = () => {
    act(() => {
      el.dispatchEvent(new Event("scroll"));
    });
  };

  const touchEvent = (type: string, clientY?: number) => {
    const event = new Event(type, { bubbles: true }) as TouchEvent;
    const points =
      clientY === undefined
        ? []
        : [{ identifier: 1, clientY } satisfies Pick<Touch, "identifier" | "clientY">];
    Object.defineProperty(event, "touches", {
      value: points,
    });
    Object.defineProperty(event, "changedTouches", { value: points });
    return event;
  };

  // Reader input: the wheel delta the hook judges intent from, then the
  // scroll it produces. Wheel up is a negative deltaY.
  const wheelBy = (px: number) => {
    act(() => {
      el.dispatchEvent(new WheelEvent("wheel", { deltaY: -px }));
    });
    state.scrollTop -= px;
    scrollEvent();
  };

  // Browser clamp after the scrollable extent shrank: scrollTop drops to the
  // new maximum and a scroll event fires, with no input anywhere.
  const clampAfterShrink = () => {
    state.scrollTop = Math.min(
      state.scrollTop,
      Math.max(0, state.contentHeight - state.viewportHeight),
    );
    scrollEvent();
  };

  const contentChanged = () => {
    act(() => {
      reportContentHeightChanged?.();
    });
  };

  return {
    el,
    get scrollTop() {
      return state.scrollTop;
    },
    distanceFromBottom() {
      return Math.max(0, state.contentHeight - state.scrollTop - state.viewportHeight);
    },
    grow(px) {
      state.contentHeight += px;
      contentChanged();
    },
    shrinkContent(px) {
      state.contentHeight -= px;
      clampAfterShrink();
      contentChanged();
    },
    growViewport(px) {
      state.viewportHeight += px;
      clampAfterShrink();
      fireResizeObservers();
    },
    shrinkViewport(px) {
      state.viewportHeight -= px;
      fireResizeObservers();
    },
    readerScrollsUp(px) {
      wheelBy(px);
    },
    touchStart(clientY) {
      act(() => {
        el.dispatchEvent(touchEvent("touchstart", clientY));
      });
    },
    touchMove(clientY, scrollPx) {
      act(() => {
        el.dispatchEvent(touchEvent("touchmove", clientY));
      });
      state.scrollTop -= scrollPx;
      scrollEvent();
    },
    touchEnd() {
      act(() => {
        el.dispatchEvent(touchEvent("touchend"));
      });
    },
    touchMomentumScrollsUp(px) {
      state.scrollTop -= px;
      scrollEvent();
    },
    readerScrollsTo(fromBottom) {
      wheelBy(fromBottom - this.distanceFromBottom());
    },
    wheelWithoutScroll(px) {
      const nested = document.createElement("pre");
      el.appendChild(nested);
      act(() => {
        nested.dispatchEvent(new WheelEvent("wheel", { deltaY: -px, bubbles: true }));
      });
    },
    pageUpFromRowControl() {
      const control = document.createElement("button");
      el.appendChild(control);
      act(() => {
        control.dispatchEvent(new KeyboardEvent("keydown", { key: "PageUp", bubbles: true }));
      });
    },
    shiftSpaceFromRowControl() {
      const control = document.createElement("button");
      el.appendChild(control);
      act(() => {
        control.dispatchEvent(
          new KeyboardEvent("keydown", { key: " ", shiftKey: true, bubbles: true }),
        );
      });
    },
    browserScrollsListUp(px) {
      state.scrollTop -= px;
      scrollEvent();
    },
    pinCalls() {
      return pinCalls;
    },
    directScrollTopWrites() {
      return directScrollTopWrites;
    },
  };
}

function taskMsg(seq: number, content: string): TaskMessagePayload {
  return { task_id: TASK_ID, seq, type: "text", content } as TaskMessagePayload;
}

function renderStreamingChat({ contentHeight = 2000 } = {}) {
  const qc = new QueryClient();
  qc.setQueryData(chatKeys.taskMessages(TASK_ID), [taskMsg(0, "Looking into it. ")]);

  const view = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>
        <ChatMessageList
          messages={[]}
          pendingTask={{ task_id: TASK_ID, status: "running" }}
          availability={undefined}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );

  const el = view.container.querySelector<HTMLElement>("[data-tab-scroll-root]");
  if (!el) throw new Error("chat list did not render a scroll container");

  return {
    qc,
    view,
    scroll: scroller(el, contentHeight),
    rowCount: () => view.container.querySelectorAll("[data-row-key]").length,
    followsAtBottom: () =>
      view.container
        .querySelector("[data-follow-at-bottom]")
        ?.getAttribute("data-follow-at-bottom"),
    streamChunk: (seq: number) => {
      act(() => {
        qc.setQueryData<TaskMessagePayload[]>(
          chatKeys.taskMessages(TASK_ID),
          (old = []) => [...old, taskMsg(seq, `chunk ${seq} `)],
        );
      });
    },
  };
}

describe("ChatMessageList auto-scroll", () => {
  it("follows a streaming reply whose row count never changes", () => {
    const { scroll, streamChunk, rowCount } = renderStreamingChat();

    const rowsBefore = rowCount();
    for (let seq = 1; seq <= 30; seq++) {
      streamChunk(seq);
      scroll.grow(180);
    }

    expect(rowCount()).toBe(rowsBefore);
    expect(scroll.distanceFromBottom()).toBe(0);
  });

  // MUL-6879: the pin used to write `scrollTop = scrollHeight - clientHeight`
  // on the container. In a virtualised list that height is an estimate over
  // the unrendered rows, so the pin landed Virtuoso somewhere it then
  // re-measured, moving the estimate — and the next height change pinned
  // again. The loop swallowed the reader's wheel scrolls, leaving the list
  // shaking and immovable. Corrections must go through Virtuoso.
  it("corrects through Virtuoso rather than writing scrollTop on the container", () => {
    const { scroll } = renderStreamingChat();

    scroll.grow(180);
    scroll.shrinkViewport(72);

    expect(scroll.pinCalls()).toEqual([
      { index: "LAST", align: "end" },
      { index: "LAST", align: "end" },
    ]);
    expect(scroll.directScrollTopWrites()).toBe(0);
  });

  // MUL-6879: Virtuoso opens the list by scrolling to where it PREDICTS the
  // newest message is, paints there, and only then measures the rows and
  // corrects — so the first painted frame showed the wrong part of the
  // conversation and the next one jumped. Readiness is judged from the
  // newest row's own box, because during the estimate the scroll geometry
  // reports the viewport as being at the live end.
  it("stays hidden until the newest message has actually landed at the bottom", () => {
    const { view, scroll } = renderStreamingChat();

    const box = (el: Element, top: number, bottom: number) => {
      el.getBoundingClientRect = () => ({ top, bottom }) as DOMRect;
    };
    box(scroll.el, 0, VIEWPORT);
    const liveEndRow = view.container.querySelector("[data-chat-live-end]");
    if (!liveEndRow) throw new Error("the newest row is not marked as the live end");

    const frames = (count: number) => {
      for (let i = 0; i < count; i++) act(() => renderFrame());
    };

    expect(scroll.el.className).toContain("invisible");

    // Rows are on screen, but sitting where the estimate put them: below the
    // fold, still to be corrected. This is the frame that used to flicker.
    box(liveEndRow, 4600, 5200);
    frames(20);
    expect(scroll.el.className).toContain("invisible");

    // The correction lands and the newest row reaches the bottom — but the
    // list holds until the content has stopped moving there.
    box(liveEndRow, VIEWPORT - 200, VIEWPORT - 16);
    frames(2);
    expect(scroll.el.className).toContain("invisible");

    frames(20);
    expect(scroll.el.className).not.toContain("invisible");
  });

  // A row settling after its first paint — an image decoding, a code block
  // highlighting — moves the content under a reader pinned to the bottom.
  // Reaching the live end once is not enough to reveal (MUL-6879).
  it("keeps waiting while rows are still changing size at the live end", () => {
    const { view, scroll } = renderStreamingChat();

    const box = (el: Element, top: number, bottom: number) => {
      el.getBoundingClientRect = () => ({ top, bottom }) as DOMRect;
    };
    box(scroll.el, 0, VIEWPORT);
    const liveEndRow = view.container.querySelector("[data-chat-live-end]");
    if (!liveEndRow) throw new Error("the newest row is not marked as the live end");
    box(liveEndRow, VIEWPORT - 200, VIEWPORT - 16);

    // Content keeps arriving: every settle window restarts.
    for (let i = 0; i < 6; i++) {
      scroll.grow(120);
      act(() => renderFrame());
      act(() => renderFrame());
    }
    expect(scroll.el.className).toContain("invisible");

    // It stops, and the list appears.
    for (let i = 0; i < 20; i++) act(() => renderFrame());
    expect(scroll.el.className).not.toContain("invisible");
  });

  it("keeps the newest content clear of a composer that grew", () => {
    const { scroll } = renderStreamingChat();

    scroll.shrinkViewport(72);

    expect(scroll.distanceFromBottom()).toBe(0);
  });

  it("stays pinned when the composer collapses", () => {
    const { scroll, streamChunk } = renderStreamingChat();

    streamChunk(1);
    scroll.grow(180);
    scroll.growViewport(72);

    for (let seq = 2; seq <= 3; seq++) {
      streamChunk(seq);
      scroll.grow(180);
    }

    expect(scroll.distanceFromBottom()).toBe(0);
  });

  it("stays pinned when content shrinks", () => {
    const { scroll, streamChunk } = renderStreamingChat();

    streamChunk(1);
    scroll.grow(180);
    scroll.shrinkContent(400);

    for (let seq = 2; seq <= 3; seq++) {
      streamChunk(seq);
      scroll.grow(180);
    }

    expect(scroll.distanceFromBottom()).toBe(0);
  });

  // The base behavior `atBottomThreshold` always granted: a trackpad nudge
  // inside the edge zone is not the reader leaving.
  it("keeps following after a small trackpad nudge near the live end", () => {
    const { scroll, streamChunk } = renderStreamingChat();

    scroll.readerScrollsUp(2);
    gestureSettles();

    streamChunk(1);
    scroll.grow(180);

    expect(scroll.distanceFromBottom()).toBe(0);
  });

  it("releases when touch momentum carries a flick past the threshold", () => {
    const { scroll, streamChunk } = renderStreamingChat();

    scroll.touchStart(100);
    scroll.touchMove(180, 80);
    scroll.touchEnd();
    scroll.touchMomentumScrollsUp(80);
    const parked = scroll.scrollTop;

    streamChunk(1);
    scroll.grow(180);

    expect(scroll.scrollTop).toBe(parked);
  });

  it("leaves a fractional touch drag in progress", () => {
    const { scroll, streamChunk, followsAtBottom } = renderStreamingChat();

    scroll.touchStart(100);
    scroll.touchMove(180, 80.5);
    streamChunk(1);

    expect(scroll.distanceFromBottom()).toBe(80.5);
    expect(followsAtBottom()).toBe("auto");
    scroll.touchEnd();
  });

  it("resumes pinning after a sub-threshold touch ends", () => {
    const { scroll, streamChunk } = renderStreamingChat();

    scroll.touchStart(100);
    scroll.touchMove(180, 80);
    scroll.touchEnd();
    gestureSettles();
    streamChunk(1);
    scroll.grow(180);

    expect(scroll.distanceFromBottom()).toBe(0);
  });

  it("releases on incremental upward scrolling during a fast stream", () => {
    const { scroll, streamChunk } = renderStreamingChat();

    streamChunk(1);
    scroll.grow(180);

    // 60px wheel ticks, each under the edge threshold on its own and each
    // answered by a chunk that pins the reader back — but the displacement
    // they actually took accumulates across the pins, so the third tick
    // crosses the threshold and releases.
    for (let seq = 2; seq <= 5; seq++) {
      scroll.readerScrollsUp(60);
      streamChunk(seq);
      scroll.grow(180);
    }
    gestureSettles();
    streamChunk(6);
    scroll.grow(180);

    expect(scroll.distanceFromBottom()).toBe(660);
  });

  // Discrete mouse wheel at reading pace: one notch per event, spaced past
  // the intent window. Release must not require a single fast burst.
  it("releases for wheel notches spaced past the intent window", () => {
    const { scroll, streamChunk } = renderStreamingChat();

    scroll.readerScrollsUp(100);
    gestureSettles();
    scroll.readerScrollsUp(100);
    gestureSettles();
    const parked = scroll.scrollTop;

    streamChunk(1);
    scroll.grow(180);

    expect(scroll.scrollTop).toBe(parked);
  });

  // A reply's final chunk always exists and has a whole intent window to land
  // in after any small nudge; no later event will re-evaluate a declined pin.
  it("pins a chunk that lands inside the reader's intent window", () => {
    const { scroll, streamChunk } = renderStreamingChat();

    scroll.readerScrollsUp(2);
    streamChunk(1);
    scroll.grow(180);

    expect(scroll.distanceFromBottom()).toBe(0);
  });

  it("honors Shift+Space paging from a focused row control", () => {
    const { scroll, streamChunk } = renderStreamingChat();

    scroll.shiftSpaceFromRowControl();
    scroll.browserScrollsListUp(VIEWPORT);
    const parked = scroll.scrollTop;
    gestureSettles();

    streamChunk(1);
    scroll.grow(180);

    expect(scroll.scrollTop).toBe(parked);
  });

  it("leaves the viewport alone once the reader scrolls up to read history", () => {
    const { scroll, streamChunk } = renderStreamingChat();

    scroll.readerScrollsTo(900);
    gestureSettles();
    const parked = scroll.scrollTop;

    streamChunk(1);
    scroll.grow(500);
    scroll.shrinkViewport(72);

    expect(scroll.scrollTop).toBe(parked);
  });

  // Wheel over a capped code block scrolls the block; the event bubbles to
  // the list without moving it. Unconsumed input must not release the follow.
  it("keeps following through wheel input the list never consumed", () => {
    const { scroll, streamChunk } = renderStreamingChat();

    scroll.wheelWithoutScroll(200);
    gestureSettles();

    streamChunk(1);
    scroll.grow(180);

    expect(scroll.distanceFromBottom()).toBe(0);
  });

  it("pins a system shift after nested scrolling left input unconsumed", () => {
    const { scroll } = renderStreamingChat();

    scroll.wheelWithoutScroll(300);
    renderFrame();
    scroll.browserScrollsListUp(200);

    expect(scroll.distanceFromBottom()).toBe(0);
  });

  it("keeps following after a flick on a conversation too short to scroll", () => {
    const { scroll, streamChunk } = renderStreamingChat({ contentHeight: 400 });

    scroll.wheelWithoutScroll(200);
    gestureSettles();

    for (let seq = 1; seq <= 8; seq++) {
      streamChunk(seq);
      scroll.grow(180);
    }

    expect(scroll.distanceFromBottom()).toBe(0);
  });

  // Focus sits on a row control, the reader presses PageUp, and the browser
  // scrolls the list. The scroll confirms the staged key intent: the reader
  // is released, not pinned back over their own keypress.
  it("honors keyboard paging from a focused row control", () => {
    const { scroll, streamChunk } = renderStreamingChat();

    scroll.pageUpFromRowControl();
    scroll.browserScrollsListUp(VIEWPORT);
    const parked = scroll.scrollTop;
    gestureSettles();

    streamChunk(1);
    scroll.grow(180);

    expect(scroll.scrollTop).toBe(parked);
  });

  it("re-engages when the reader scrolls back down to the live end", () => {
    const { scroll, streamChunk } = renderStreamingChat();

    scroll.readerScrollsTo(900);
    scroll.readerScrollsTo(0);
    gestureSettles();

    streamChunk(1);
    scroll.grow(500);

    expect(scroll.distanceFromBottom()).toBe(0);
  });

  // `followOutput` is `atBottom && isFollowing()`: a released follow must turn
  // it off even while Virtuoso still reports the reader at the bottom.
  it("turns followOutput off while released, even when Virtuoso reports atBottom", () => {
    const { scroll, streamChunk, followsAtBottom } = renderStreamingChat();

    expect(followsAtBottom()).toBe("auto");

    scroll.readerScrollsTo(900);
    streamChunk(1);

    expect(followsAtBottom()).toBe("false");
  });

  it("stops measuring once the list unmounts", () => {
    const { view } = renderStreamingChat();

    view.unmount();

    expect(observedTargets()).toHaveLength(0);
  });
});
