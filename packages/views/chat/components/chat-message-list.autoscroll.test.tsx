import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { chatKeys } from "@multica/core/chat/queries";
import type { TaskMessagePayload } from "@multica/core/types";
import type { ReactElement } from "react";
import enChat from "../../locales/en/chat.json";
import { ChatMessageList } from "./chat-message-list";

// TIM-55 regression: the chat viewport must stay at the live end while a reply
// streams in and while the composer grows underneath it.
//
// Both failed for the same reason: Virtuoso's `followOutput` reacts to the ITEM
// COUNT, and neither event changes it. A streaming reply is one row that keeps
// growing, and the composer only shrinks the viewport. The list therefore drives
// its own bottom-stick off a ResizeObserver — which is what these tests drive.
//
// This suite covers the WIRING: that the list measures the right boxes and
// applies the controller's decision to the real scroller. The decision matrix
// itself (thresholds, re-engagement, shrinking content) is canonical in
// stick-to-bottom.test.ts and is not re-run through a DOM mount.

// Real react-virtuoso renders no data rows under jsdom's zero-height viewport,
// and this suite fakes the scroll geometry anyway; the stub keeps the row count
// visible so the tests can assert that streaming leaves it unchanged.
vi.mock("react-virtuoso", () => ({
  Virtuoso: ({
    data,
    itemContent,
    computeItemKey,
  }: {
    data: unknown[];
    itemContent: (i: number, item: unknown) => ReactElement;
    computeItemKey: (i: number, item: unknown) => string;
  }) => (
    <div data-testid="virtuoso-rows">
      {data.map((item, i) => (
        <div key={computeItemKey(i, item)} data-row-key={computeItemKey(i, item)}>
          {itemContent(i, item)}
        </div>
      ))}
    </div>
  ),
}));

const TEST_RESOURCES = { en: { chat: enChat } };
const TASK_ID = "6af44cbe-80ab-4dfe-b07d-bd3cfd588f4d";

const VIEWPORT = 600;

// ─── A ResizeObserver the test can fire ──────────────────────────────────

interface FakeObserver {
  targets: Element[];
  fire: () => void;
}

let observers: FakeObserver[] = [];

/** Every element the list asked to be measured, across all its observers. */
function observedTargets(): Element[] {
  return observers.flatMap((o) => o.targets);
}

/** A box changed size: the browser would run every observer watching it. */
function resize() {
  act(() => {
    for (const observer of observers) observer.fire();
  });
}

beforeEach(() => {
  observers = [];
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
});

// ─── Fake scroll geometry ────────────────────────────────────────────────
//
// jsdom has no layout, so scrollHeight/clientHeight are hard 0 and scrollTop
// never moves. This wrapper gives the scroll container the geometry a browser
// would, and records the scrollTop the component writes.

interface Scroller {
  el: HTMLElement;
  scrollTop: number;
  contentHeight: number;
  viewportHeight: number;
  distanceFromBottom(): number;
  /** Content grew (a streamed chunk) — the browser then notifies observers. */
  grow(px: number): void;
  /** The composer grew, taking `px` off the list's height. */
  shrinkViewport(px: number): void;
  /** The reader scrolls, leaving `fromBottom` px of content below the fold. */
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

  return {
    el,
    get scrollTop() {
      return state.scrollTop;
    },
    get contentHeight() {
      return state.contentHeight;
    },
    get viewportHeight() {
      return state.viewportHeight;
    },
    distanceFromBottom() {
      return state.contentHeight - state.scrollTop - state.viewportHeight;
    },
    grow(px) {
      state.contentHeight += px;
      resize();
    },
    shrinkViewport(px) {
      state.viewportHeight -= px;
      resize();
    },
    readerScrollsTo(fromBottom) {
      state.scrollTop = state.contentHeight - state.viewportHeight - fromBottom;
      act(() => {
        el.dispatchEvent(new Event("scroll"));
      });
    },
  };
}

// ─── Fixture ─────────────────────────────────────────────────────────────

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
    /** One more streamed chunk on the SAME live row. */
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

// ─── Tests ───────────────────────────────────────────────────────────────

describe("ChatMessageList auto-scroll (TIM-55 regression)", () => {
  it("measures both the viewport and the content, not just the viewport", () => {
    const { scroll, view } = renderStreamingChat();

    const targets = observedTargets();
    expect(targets).toContain(scroll.el);
    // A ResizeObserver reports an element's own box, never its scroll extent,
    // so watching the container alone would miss a reply growing inside it.
    expect(targets.some((t) => t !== scroll.el && scroll.el.contains(t))).toBe(true);

    view.unmount();
  });

  it("follows a streaming reply whose row count never changes", () => {
    const { scroll, streamChunk, rowCount } = renderStreamingChat();

    const rowsBefore = rowCount();
    for (let seq = 1; seq <= 30; seq++) {
      streamChunk(seq);
      scroll.grow(180);
    }

    // The whole point: Virtuoso saw no item-count change to follow.
    expect(rowCount()).toBe(rowsBefore);
    expect(scroll.distanceFromBottom()).toBe(0);
  });

  it("keeps the newest content clear of a composer that grew", () => {
    const { scroll } = renderStreamingChat();

    // Three lines of draft text push the composer up into the list.
    scroll.shrinkViewport(72);

    expect(scroll.distanceFromBottom()).toBe(0);
  });

  it("leaves the viewport alone once the reader scrolls up to read history", () => {
    const { scroll, streamChunk } = renderStreamingChat();

    scroll.readerScrollsTo(900);
    const parked = scroll.scrollTop;

    streamChunk(1);
    scroll.grow(500);
    scroll.shrinkViewport(72);

    expect(scroll.scrollTop).toBe(parked);
  });

  it("stops measuring once the list unmounts", () => {
    const { view } = renderStreamingChat();

    view.unmount();

    expect(observedTargets()).toHaveLength(0);
  });
});
