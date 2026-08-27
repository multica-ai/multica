import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { chatKeys } from "@multica/core/chat/queries";
import type { TaskMessagePayload } from "@multica/core/types";
import type { ReactElement } from "react";
import enChat from "../../locales/en/chat.json";
import { ChatMessageList } from "./chat-message-list";

// TIM-55 wiring: the list must keep the live end visible through streaming
// growth and composer resizes, releasing only on real reader input. The latch
// decision table is canonical in transcript-follow.test.ts and the scroll
// geometry in stick-to-bottom.test.ts; this suite drives the real component
// through the DOM signals those decisions are wired to. Reader gestures here
// are wheel INPUT followed by the scroll it causes — position-only moves model
// the browser (clamps), input-led moves model the reader.

// Virtuoso cannot render rows in jsdom's zero-height viewport. The stub keeps
// the row count visible, surfaces `followOutput` verdicts as attributes, and
// captures `totalListHeightChanged` so the harness can report content growth
// the way the real Virtuoso does.
let reportContentHeightChanged: (() => void) | undefined;

vi.mock("react-virtuoso", () => ({
  Virtuoso: ({
    data,
    itemContent,
    computeItemKey,
    followOutput,
    totalListHeightChanged,
  }: {
    data: unknown[];
    itemContent: (i: number, item: unknown) => ReactElement;
    computeItemKey: (i: number, item: unknown) => string;
    followOutput?: (atBottom: boolean) => "smooth" | "auto" | false;
    totalListHeightChanged?: () => void;
  }) => {
    reportContentHeightChanged = totalListHeightChanged;
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

function observedTargets(): Element[] {
  return observers.flatMap((o) => o.targets);
}

function fireResizeObservers() {
  act(() => {
    for (const observer of observers) observer.fire();
  });
}

// The follow latch treats input within its intent window as an ongoing
// gesture and defers pinning; tests control that clock directly.
let now = 0;
function gestureSettles() {
  now += 301;
}

beforeEach(() => {
  observers = [];
  reportContentHeightChanged = undefined;
  now = 0;
  vi.spyOn(Date, "now").mockImplementation(() => now);
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
  /** Reader wheel input landing `fromBottom` px above the live end. */
  readerScrollsTo(fromBottom: number): void;
}

function scroller(el: HTMLElement): Scroller {
  const state = { scrollTop: 0, contentHeight: 2000, viewportHeight: VIEWPORT };
  // Open at the bottom, matching Virtuoso's `initialTopMostItemIndex: LAST`.
  state.scrollTop = state.contentHeight - state.viewportHeight;

  Object.defineProperties(el, {
    scrollHeight: { configurable: true, get: () => state.contentHeight },
    clientHeight: { configurable: true, get: () => state.viewportHeight },
    scrollTop: {
      configurable: true,
      get: () => state.scrollTop,
      set: (value: number) => {
        state.scrollTop = value;
      },
    },
  });

  const scrollEvent = () => {
    act(() => {
      el.dispatchEvent(new Event("scroll"));
    });
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
      return state.contentHeight - state.scrollTop - state.viewportHeight;
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
    readerScrollsTo(fromBottom) {
      wheelBy(fromBottom - this.distanceFromBottom());
    },
  };
}

function taskMsg(seq: number, content: string): TaskMessagePayload {
  return { task_id: TASK_ID, seq, type: "text", content } as TaskMessagePayload;
}

function renderStreamingChat() {
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
    scroll: scroller(el),
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

describe("ChatMessageList auto-scroll (TIM-55 regression)", () => {
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

  it("releases on incremental upward scrolling during a fast stream", () => {
    const { scroll, streamChunk } = renderStreamingChat();

    streamChunk(1);
    scroll.grow(180);

    // 60px wheel ticks, each under the edge threshold on its own; the latch
    // accumulates them past it while suppressing pins mid-gesture.
    for (let seq = 2; seq <= 5; seq++) {
      scroll.readerScrollsUp(60);
      streamChunk(seq);
      scroll.grow(180);
    }
    gestureSettles();
    streamChunk(6);
    scroll.grow(180);

    expect(scroll.distanceFromBottom()).toBe(1140);
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
