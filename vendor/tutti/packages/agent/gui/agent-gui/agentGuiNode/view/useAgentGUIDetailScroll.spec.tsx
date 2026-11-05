import { act, renderHook } from "@testing-library/react";
import type { MutableRefObject, RefObject } from "react";
import type { Mock } from "vitest";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AgentTranscriptVirtualScrollController } from "../../../shared/agentConversation/components/AgentTranscriptView";
import type { AgentConversationVM } from "../../../shared/agentConversation/contracts/agentConversationVM";
import type { AgentGUINodeViewProps } from "../AgentGUINodeView";
import { useAgentGUIDetailScroll } from "./useAgentGUIDetailScroll";

vi.mock("@multica/tutti-agent-gui-host/timeline-end-position", () => ({
  useTimelineEndPosition: () => true
}));

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("useAgentGUIDetailScroll", () => {
  it("starts a newly selected conversation at the bottom", () => {
    const harness = createHarness({ scrollHeight: 5_000 });
    const { rerender } = renderHook(
      ({ activeConversationId }) =>
        useAgentGUIDetailScroll(
          harness.input({ activeConversationId, showTimelineSkeleton: false })
        ),
      { initialProps: { activeConversationId: "conversation-a" } }
    );

    expect(harness.timeline.scrollTop).toBe(4_900);
    act(() => {
      harness.timeline.dispatchEvent(new WheelEvent("wheel", { deltaY: -100 }));
      harness.timeline.scrollTop = 2_000;
      harness.timeline.dispatchEvent(new Event("scroll"));
    });
    expect(harness.timeline.scrollTop).toBe(2_000);

    harness.setScrollHeight(8_000);
    rerender({ activeConversationId: "conversation-b" });

    expect(harness.timeline.scrollTop).toBe(7_900);
  });

  it("restores a conversation's detached scroll position when it is reselected", () => {
    const harness = createHarness({ scrollHeight: 5_000 });
    const { result, rerender } = renderHook(
      ({ activeConversationId }) =>
        useAgentGUIDetailScroll(
          harness.input({ activeConversationId, showTimelineSkeleton: false })
        ),
      { initialProps: { activeConversationId: "conversation-a" } }
    );

    act(() => {
      harness.timeline.dispatchEvent(new WheelEvent("wheel", { deltaY: -100 }));
      harness.timeline.scrollTop = 2_000;
      harness.timeline.dispatchEvent(new Event("scroll"));
    });
    expect(result.current.followEndMode === "following").toBe(false);

    harness.setScrollHeight(8_000);
    rerender({ activeConversationId: "conversation-b" });
    expect(harness.timeline.scrollTop).toBe(7_900);
    expect(result.current.followEndMode === "following").toBe(true);

    harness.setScrollHeight(5_000);
    rerender({ activeConversationId: "conversation-a" });
    expect(harness.timeline.scrollTop).toBe(2_000);
    expect(result.current.followEndMode === "following").toBe(false);
  });

  it("keeps a detached position while the reselected conversation hydrates", () => {
    const harness = createHarness({ scrollHeight: 5_000 });
    const { result, rerender } = renderHook(
      ({ activeConversationId, showTimelineSkeleton }) =>
        useAgentGUIDetailScroll(
          harness.input({ activeConversationId, showTimelineSkeleton })
        ),
      {
        initialProps: {
          activeConversationId: "conversation-a",
          showTimelineSkeleton: false
        }
      }
    );

    act(() => {
      harness.timeline.dispatchEvent(new WheelEvent("wheel", { deltaY: -100 }));
      harness.timeline.scrollTop = 2_000;
      harness.timeline.dispatchEvent(new Event("scroll"));
    });

    harness.setScrollHeight(8_000);
    rerender({
      activeConversationId: "conversation-b",
      showTimelineSkeleton: false
    });
    expect(harness.timeline.scrollTop).toBe(7_900);

    rerender({
      activeConversationId: "conversation-a",
      showTimelineSkeleton: true
    });
    expect(harness.timeline.scrollTop).toBe(7_900);
    expect(result.current.followEndMode === "following").toBe(false);

    harness.setScrollHeight(5_000);
    rerender({
      activeConversationId: "conversation-a",
      showTimelineSkeleton: false
    });
    expect(harness.timeline.scrollTop).toBe(2_000);
    expect(result.current.followEndMode === "following").toBe(false);
  });

  it("restores a virtualized conversation's detached scroll position", () => {
    const harness = createHarness({ scrollHeight: 5_000 });
    const firstController = virtualScrollController("conversation-a", false);
    harness.virtualScrollControllerRef.current = firstController;
    const { result, rerender } = renderHook(
      ({ activeConversationId }) =>
        useAgentGUIDetailScroll(
          harness.input({ activeConversationId, showTimelineSkeleton: false })
        ),
      { initialProps: { activeConversationId: "conversation-a" } }
    );

    act(() => {
      harness.timeline.dispatchEvent(new WheelEvent("wheel", { deltaY: -100 }));
      harness.timeline.scrollTop = 2_000;
      harness.timeline.dispatchEvent(new Event("scroll"));
    });
    expect(result.current.followEndMode === "following").toBe(false);

    const secondController = virtualScrollController("conversation-b");
    harness.virtualScrollControllerRef.current = secondController;
    rerender({ activeConversationId: "conversation-b" });
    expect(secondController.scrollToEnd).toHaveBeenCalledWith({
      behavior: "auto"
    });

    harness.virtualScrollControllerRef.current = firstController;
    firstController.scrollToEnd.mockClear();
    rerender({ activeConversationId: "conversation-a" });

    expect(firstController.scrollToOffset).toHaveBeenCalledWith(2_000, {
      behavior: "auto"
    });
    expect(firstController.scrollToEnd).not.toHaveBeenCalled();
    expect(result.current.followEndMode === "following").toBe(false);
  });

  it("reads timeline geometry once for a semantic conversation switch", () => {
    const harness = createHarness({ scrollHeight: 5_000 });
    const { rerender } = renderHook(
      ({ activeConversationId }) =>
        useAgentGUIDetailScroll(
          harness.input({ activeConversationId, showTimelineSkeleton: false })
        ),
      { initialProps: { activeConversationId: "conversation-a" } }
    );

    expect(harness.geometryReadCounts()).toEqual({
      clientHeight: 1,
      scrollHeight: 1,
      scrollTop: 0
    });

    harness.resetGeometryReadCounts();
    harness.setScrollHeight(8_000);
    rerender({ activeConversationId: "conversation-b" });

    expect(harness.geometryReadCounts()).toEqual({
      clientHeight: 1,
      scrollHeight: 1,
      scrollTop: 0
    });
  });

  it("delegates a virtualized conversation switch and bottom request to TanStack", () => {
    const harness = createHarness({ scrollHeight: 5_000 });
    const controller = virtualScrollController("conversation-virtualized");
    harness.virtualScrollControllerRef.current = controller;
    const { result } = renderHook(() =>
      useAgentGUIDetailScroll(
        harness.input({
          activeConversationId: "conversation-virtualized",
          showTimelineSkeleton: false
        })
      )
    );

    expect(controller.scrollToEnd).toHaveBeenCalledWith({ behavior: "auto" });
    expect(harness.scrollTopWriteCount()).toBe(0);
    expect(harness.geometryReadCounts()).toEqual({
      clientHeight: 0,
      scrollHeight: 0,
      scrollTop: 0
    });
    controller.scrollToEnd.mockClear();

    act(() => result.current.scrollTimelineToBottom());

    expect(controller.scrollToEnd).toHaveBeenCalledWith({
      behavior: "smooth"
    });
    expect(harness.scrollTopWriteCount()).toBe(0);
  });

  it("positions a virtualized conversation when its controller becomes ready after layout", () => {
    const harness = createHarness({ scrollHeight: 5_000 });
    const controller = virtualScrollController("conversation-virtualized");
    const { result } = renderHook(() =>
      useAgentGUIDetailScroll(
        harness.input({
          activeConversationId: "conversation-virtualized",
          showTimelineSkeleton: false
        })
      )
    );

    act(() => result.current.setVirtualScrollController(controller));

    expect(controller.scrollToEnd).toHaveBeenCalledWith({ behavior: "auto" });
  });

  it("does not use a previous Session virtual controller", () => {
    const harness = createHarness({ scrollHeight: 5_000 });
    const staleController = virtualScrollController("conversation-previous");
    harness.virtualScrollControllerRef.current = staleController;
    const { result } = renderHook(() =>
      useAgentGUIDetailScroll(
        harness.input({
          activeConversationId: "conversation-current",
          showTimelineSkeleton: false
        })
      )
    );

    act(() => result.current.scrollTimelineToBottom());

    expect(staleController.scrollToEnd).not.toHaveBeenCalled();
    expect(harness.scrollTopWriteCount()).toBe(0);
  });

  it("explicitly returns a submitted prompt to the virtualized end", () => {
    const harness = createHarness({ scrollHeight: 5_000 });
    const controller = virtualScrollController("conversation-submit");
    harness.virtualScrollControllerRef.current = controller;
    const { rerender } = renderHook(
      ({ conversation }) =>
        useAgentGUIDetailScroll(
          harness.input({
            activeConversationId: "conversation-submit",
            conversation,
            showTimelineSkeleton: false
          })
        ),
      { initialProps: { conversation: conversationVM("first") } }
    );
    controller.scrollToEnd.mockClear();
    harness.submittedPromptScrollConversationRef.current =
      "conversation-submit";

    rerender({ conversation: conversationVM("second") });

    expect(controller.scrollToEnd).toHaveBeenCalledWith({ behavior: "auto" });
  });

  it("leaves virtualized prepend compensation to TanStack", () => {
    const harness = createHarness({ scrollHeight: 5_000 });
    const controller = virtualScrollController("conversation-prepend", false);
    harness.virtualScrollControllerRef.current = controller;
    const { rerender } = renderHook(
      ({ isLoadingOlderMessages }) =>
        useAgentGUIDetailScroll(
          harness.input({
            activeConversationId: "conversation-prepend",
            isLoadingOlderMessages,
            showTimelineSkeleton: false
          })
        ),
      { initialProps: { isLoadingOlderMessages: false } }
    );
    controller.scrollToEnd.mockClear();
    harness.timeline.scrollTop = 200;
    harness.pendingPrependScrollAnchorRef.current = {
      conversationId: "conversation-prepend",
      scrollHeight: 5_000,
      scrollTop: 200
    };
    harness.setScrollHeight(6_000);
    harness.resetScrollTopWriteCount();

    rerender({ isLoadingOlderMessages: true });

    expect(controller.scrollToEnd).not.toHaveBeenCalled();
    expect(harness.timeline.scrollTop).toBe(200);
    expect(harness.scrollTopWriteCount()).toBe(0);
  });

  it("does not re-follow virtualized content after user scroll-away intent", () => {
    const resizeObservers = installResizeObserverMock();
    const harness = createHarness({ scrollHeight: 5_000 });
    const controller = virtualScrollController("conversation-away", false);
    harness.virtualScrollControllerRef.current = controller;
    renderHook(() =>
      useAgentGUIDetailScroll(
        harness.input({
          activeConversationId: "conversation-away",
          showTimelineSkeleton: false
        })
      )
    );
    controller.scrollToEnd.mockClear();
    const timelineObserver = resizeObservers.find((observer) =>
      observer.observed.has(harness.timelineContent)
    );

    act(() => {
      harness.timeline.dispatchEvent(new WheelEvent("wheel", { deltaY: -1 }));
      harness.timeline.dispatchEvent(
        new KeyboardEvent("keydown", { key: "PageUp" })
      );
      harness.timeline.dispatchEvent(new Event("pointerdown"));
      harness.timeline.scrollTop = 2_000;
      harness.timeline.dispatchEvent(new Event("scroll"));
      window.dispatchEvent(new Event("pointerup"));
      timelineObserver?.callback([], timelineObserver);
    });

    expect(controller.scrollToEnd).not.toHaveBeenCalled();
    expect(harness.timeline.scrollTop).toBe(2_000);
  });

  it("releases virtualized bottom lock after one upward mouse-wheel step", () => {
    const resizeObservers = installResizeObserverMock();
    const harness = createHarness({ scrollHeight: 5_000 });
    const controller = virtualScrollController("conversation-wheel-step");
    harness.virtualScrollControllerRef.current = controller;
    const { result, rerender } = renderHook(
      ({ conversation }) =>
        useAgentGUIDetailScroll(
          harness.input({
            activeConversationId: "conversation-wheel-step",
            conversation,
            showTimelineSkeleton: false
          })
        ),
      { initialProps: { conversation: conversationVM("first") } }
    );
    controller.scrollToEnd.mockClear();
    const timelineObserver = resizeObservers.find((observer) =>
      observer.observed.has(harness.timelineContent)
    );

    act(() => {
      harness.timeline.dispatchEvent(new WheelEvent("wheel", { deltaY: -1 }));
      harness.timeline.scrollTop = 4_899;
      harness.timeline.dispatchEvent(new Event("scroll"));
      timelineObserver?.callback([], timelineObserver);
    });

    expect(controller.scrollToEnd).not.toHaveBeenCalled();
    expect(harness.timeline.scrollTop).toBe(4_899);
    expect(result.current.followEndMode === "following").toBe(false);

    rerender({ conversation: conversationVM("second") });
    act(() => timelineObserver?.callback([], timelineObserver));

    expect(controller.scrollToEnd).not.toHaveBeenCalled();
    expect(harness.timeline.scrollTop).toBe(4_899);
    expect(result.current.followEndMode === "following").toBe(false);
  });

  it("releases and restores the DOM bottom lock around a mouse-wheel step", () => {
    const resizeObservers = installResizeObserverMock();
    const harness = createHarness({ scrollHeight: 5_000 });
    const { result, rerender } = renderHook(
      ({ conversation }) =>
        useAgentGUIDetailScroll(
          harness.input({
            activeConversationId: "conversation-dom-wheel-step",
            conversation,
            showTimelineSkeleton: false
          })
        ),
      { initialProps: { conversation: conversationVM("first") } }
    );
    const timelineObserver = resizeObservers.find((observer) =>
      observer.observed.has(harness.timelineContent)
    );
    harness.resetScrollTopWriteCount();

    act(() => {
      harness.timeline.dispatchEvent(new WheelEvent("wheel", { deltaY: -1 }));
      timelineObserver?.callback([], timelineObserver);
      harness.timeline.scrollTop = 4_899;
      harness.timeline.dispatchEvent(new Event("scroll"));
    });

    expect(harness.scrollTopWriteCount()).toBe(1);
    expect(harness.timeline.scrollTop).toBe(4_899);
    expect(result.current.followEndMode === "following").toBe(false);

    harness.setScrollHeight(5_100);
    rerender({ conversation: conversationVM("second") });
    act(() => timelineObserver?.callback([], timelineObserver));

    expect(harness.scrollTopWriteCount()).toBe(1);
    expect(harness.timeline.scrollTop).toBe(4_899);
    expect(result.current.followEndMode === "following").toBe(false);

    act(() => {
      harness.timeline.dispatchEvent(new WheelEvent("wheel", { deltaY: 1 }));
      harness.timeline.scrollTop = 5_000;
      harness.timeline.dispatchEvent(new Event("scroll"));
    });

    expect(result.current.followEndMode === "following").toBe(true);
  });

  it("does not scroll the retained previous timeline when selection changes", () => {
    const harness = createHarness({ scrollHeight: 5_000 });
    const { rerender } = renderHook(
      ({ activeConversationId, timelineConversationId }) =>
        useAgentGUIDetailScroll(
          harness.input({
            activeConversationId,
            showTimelineSkeleton: false,
            timelineConversationId
          })
        ),
      {
        initialProps: {
          activeConversationId: "conversation-a",
          timelineConversationId: "conversation-a"
        }
      }
    );

    act(() => {
      harness.timeline.dispatchEvent(new WheelEvent("wheel", { deltaY: -100 }));
      harness.timeline.scrollTop = 2_000;
      harness.timeline.dispatchEvent(new Event("scroll"));
    });
    rerender({
      activeConversationId: "conversation-b",
      timelineConversationId: "conversation-a"
    });
    expect(harness.timeline.scrollTop).toBe(2_000);

    harness.setScrollHeight(8_000);
    rerender({
      activeConversationId: "conversation-b",
      timelineConversationId: "conversation-b"
    });
    expect(harness.timeline.scrollTop).toBe(7_900);
  });

  it("restores bottom lock when a retained conversation is reselected", () => {
    const resizeObservers = installResizeObserverMock();
    const harness = createHarness({ scrollHeight: 5_000 });
    const { rerender } = renderHook(
      ({ activeConversationId }) =>
        useAgentGUIDetailScroll(
          harness.input({
            activeConversationId,
            showTimelineSkeleton: false,
            timelineConversationId: "conversation-a"
          })
        ),
      { initialProps: { activeConversationId: "conversation-a" } }
    );
    expect(harness.timeline.scrollTop).toBe(4_900);

    rerender({ activeConversationId: "conversation-b" });
    rerender({ activeConversationId: "conversation-a" });
    harness.setScrollHeight(6_000);
    const timelineObserver = resizeObservers.find((observer) =>
      observer.observed.has(harness.timelineContent)
    );
    expect(timelineObserver).toBeDefined();
    act(() => {
      timelineObserver?.callback([], timelineObserver);
    });

    expect(harness.timeline.scrollTop).toBe(5_900);
  });

  it("defers conversation-switch geometry until the timeline skeleton resolves", () => {
    const harness = createHarness({ scrollHeight: 5_000 });
    const { rerender, result } = renderHook(
      ({ activeConversationId, showTimelineSkeleton }) =>
        useAgentGUIDetailScroll(
          harness.input({
            activeConversationId,
            showTimelineSkeleton
          })
        ),
      {
        initialProps: {
          activeConversationId: "conversation-a",
          showTimelineSkeleton: false
        }
      }
    );

    expect(harness.timeline.scrollTop).toBe(4_900);
    act(() => {
      harness.timeline.dispatchEvent(new WheelEvent("wheel", { deltaY: -100 }));
      harness.timeline.scrollTop = 2_000;
      harness.timeline.dispatchEvent(new Event("scroll"));
    });
    expect(result.current.isTimelineScrolledToTop).toBe(false);
    expect(result.current.followEndMode === "following").toBe(false);
    harness.resetGeometryReadCounts();

    harness.setScrollHeight(100);
    rerender({
      activeConversationId: "conversation-b",
      showTimelineSkeleton: true
    });

    expect(harness.geometryReadCounts()).toEqual({
      clientHeight: 0,
      scrollHeight: 0,
      scrollTop: 0
    });
    expect(result.current.isTimelineScrolledToTop).toBe(true);
    expect(result.current.followEndMode === "following").toBe(true);
    expect(harness.timeline.scrollTop).toBe(2_000);

    harness.setScrollHeight(8_000);
    rerender({
      activeConversationId: "conversation-b",
      showTimelineSkeleton: false
    });

    expect(harness.geometryReadCounts()).toEqual({
      clientHeight: 1,
      scrollHeight: 1,
      scrollTop: 1
    });
    expect(harness.timeline.scrollTop).toBe(7_900);
  });

  it("does not let a previous conversation bottom frame override newer user scroll", () => {
    const harness = createHarness({ scrollHeight: 5_000 });
    const animationFrames: FrameRequestCallback[] = [];
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      animationFrames.push(callback);
      return animationFrames.length;
    });
    const { rerender } = renderHook(
      ({ activeConversationId, showTimelineSkeleton }) =>
        useAgentGUIDetailScroll(
          harness.input({
            activeConversationId,
            showTimelineSkeleton
          })
        ),
      {
        initialProps: {
          activeConversationId: "conversation-a",
          showTimelineSkeleton: false
        }
      }
    );
    expect(animationFrames).toHaveLength(1);

    rerender({
      activeConversationId: "conversation-b",
      showTimelineSkeleton: true
    });
    harness.setScrollHeight(5_000);
    rerender({
      activeConversationId: "conversation-b",
      showTimelineSkeleton: false
    });
    act(() => {
      harness.timeline.dispatchEvent(new WheelEvent("wheel", { deltaY: -100 }));
      harness.timeline.scrollTop = 4_000;
      harness.timeline.dispatchEvent(new Event("scroll"));
    });
    act(() => animationFrames[0]?.(0));

    expect(harness.timeline.scrollTop).toBe(4_000);
  });

  it("starts observing content growth after the timeline skeleton resolves", () => {
    const resizeObservers = installResizeObserverMock();
    const harness = createHarness({ scrollHeight: 100 });

    const { rerender } = renderHook(
      ({ showTimelineSkeleton }) =>
        useAgentGUIDetailScroll(
          harness.input({
            activeConversationId: "conversation-growing",
            showTimelineSkeleton
          })
        ),
      { initialProps: { showTimelineSkeleton: true } }
    );

    let timelineObserver = resizeObservers.find((observer) =>
      observer.observed.has(harness.timelineContent)
    );
    expect(timelineObserver).toBeDefined();
    expect(harness.geometryReadCounts()).toEqual({
      clientHeight: 0,
      scrollHeight: 0,
      scrollTop: 0
    });

    harness.setScrollHeight(5_000);
    act(() => {
      timelineObserver?.callback([], timelineObserver);
    });
    expect(harness.geometryReadCounts()).toEqual({
      clientHeight: 0,
      scrollHeight: 0,
      scrollTop: 0
    });
    expect(harness.timeline.scrollTop).toBe(0);

    rerender({ showTimelineSkeleton: false });

    expect(harness.geometryReadCounts()).toEqual({
      clientHeight: 1,
      scrollHeight: 1,
      scrollTop: 1
    });
    expect(harness.timeline.scrollTop).toBe(4_900);

    timelineObserver = resizeObservers.find((observer) =>
      observer.observed.has(harness.timelineContent)
    );
    harness.setScrollHeight(6_000);
    act(() => {
      timelineObserver?.callback([], timelineObserver);
    });
    expect(harness.timeline.scrollTop).toBe(5_900);
  });

  it("keeps the bottom lock through virtualizer-driven scroll changes", () => {
    const resizeObservers = installResizeObserverMock();
    const harness = createHarness({ scrollHeight: 5_000 });

    const { result } = renderHook(() =>
      useAgentGUIDetailScroll(
        harness.input({
          activeConversationId: "conversation-virtualized",
          hasOlderMessages: true,
          showTimelineSkeleton: false
        })
      )
    );
    const timelineObserver = resizeObservers.find((observer) =>
      observer.observed.has(harness.timelineContent)
    );
    expect(timelineObserver).toBeDefined();

    act(() => {
      harness.timeline.scrollTop = 100;
      harness.timeline.dispatchEvent(new Event("scroll"));
    });

    expect(harness.timeline.scrollTop).toBe(4_900);
    expect(result.current.followEndMode === "following").toBe(true);
    expect(harness.loadOlderConversationMessages).not.toHaveBeenCalled();

    harness.setScrollHeight(6_000);
    act(() => {
      timelineObserver?.callback([], timelineObserver);
    });

    expect(harness.timeline.scrollTop).toBe(5_900);
  });

  it("releases the bottom lock after the user scrolls upward", () => {
    const harness = createHarness({ scrollHeight: 5_000 });

    renderHook(() =>
      useAgentGUIDetailScroll(
        harness.input({
          activeConversationId: "conversation-user-scroll",
          showTimelineSkeleton: false
        })
      )
    );

    act(() => {
      harness.timeline.dispatchEvent(new WheelEvent("wheel", { deltaY: -100 }));
      harness.timeline.scrollTop = 4_000;
      harness.timeline.dispatchEvent(new Event("scroll"));
    });
    expect(harness.timeline.scrollTop).toBe(4_000);

    harness.setScrollHeight(6_000);
    act(() => {
      harness.timeline.dispatchEvent(new Event("scroll"));
    });

    expect(harness.timeline.scrollTop).toBe(4_000);
  });

  it("releases the bottom lock during pointer-driven scrolling", () => {
    const resizeObservers = installResizeObserverMock();
    const harness = createHarness({ scrollHeight: 5_000 });

    renderHook(() =>
      useAgentGUIDetailScroll(
        harness.input({
          activeConversationId: "conversation-pointer-scroll",
          showTimelineSkeleton: false
        })
      )
    );
    const timelineObserver = resizeObservers.find((observer) =>
      observer.observed.has(harness.timelineContent)
    );
    expect(timelineObserver).toBeDefined();

    act(() => {
      harness.timeline.dispatchEvent(new Event("pointerdown"));
      harness.timeline.scrollTop = 4_000;
      harness.timeline.dispatchEvent(new Event("scroll"));
      window.dispatchEvent(new Event("pointerup"));
    });

    harness.setScrollHeight(6_000);
    act(() => {
      timelineObserver?.callback([], timelineObserver);
    });

    expect(harness.timeline.scrollTop).toBe(4_000);
  });

  it("releases the bottom lock when a locator initiates scrolling", () => {
    const resizeObservers = installResizeObserverMock();
    const harness = createHarness({ scrollHeight: 5_000 });
    const locator = document.createElement("button");
    locator.setAttribute("data-agent-transcript-scroll-away-intent", "");
    harness.timeline.appendChild(locator);

    renderHook(() =>
      useAgentGUIDetailScroll(
        harness.input({
          activeConversationId: "conversation-locator-scroll",
          showTimelineSkeleton: false
        })
      )
    );
    const timelineObserver = resizeObservers.find((observer) =>
      observer.observed.has(harness.timelineContent)
    );
    expect(timelineObserver).toBeDefined();

    act(() => {
      locator.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      harness.timeline.scrollTop = 2_000;
      harness.timeline.dispatchEvent(new Event("scroll"));
    });

    harness.setScrollHeight(6_000);
    act(() => {
      timelineObserver?.callback([], timelineObserver);
    });

    expect(harness.timeline.scrollTop).toBe(2_000);
  });

  it("does not synchronously read full timeline geometry during scrolling", () => {
    const harness = createHarness({ scrollHeight: 5_000 });

    renderHook(() =>
      useAgentGUIDetailScroll(
        harness.input({
          activeConversationId: "conversation-scroll-hot-path",
          showTimelineSkeleton: false
        })
      )
    );
    harness.resetGeometryReadCounts();

    act(() => {
      harness.timeline.dispatchEvent(new WheelEvent("wheel", { deltaY: -100 }));
      harness.timeline.scrollTop = 4_000;
      harness.timeline.dispatchEvent(new Event("scroll"));
    });

    expect(harness.geometryReadCounts()).toEqual({
      clientHeight: 0,
      scrollHeight: 0,
      scrollTop: 1
    });
  });

  it("does not synchronously read timeline geometry for a streaming update", () => {
    const harness = createHarness({ scrollHeight: 5_000 });
    const { rerender } = renderHook(
      ({ conversation }) =>
        useAgentGUIDetailScroll(
          harness.input({
            activeConversationId: "conversation-streaming",
            conversation,
            showTimelineSkeleton: false
          })
        ),
      { initialProps: { conversation: conversationVM("first") } }
    );
    harness.resetGeometryReadCounts();

    rerender({ conversation: conversationVM("second") });

    expect(harness.geometryReadCounts()).toEqual({
      clientHeight: 0,
      scrollHeight: 0,
      scrollTop: 0
    });
  });

  it("prefetches older messages from the initialized anchor without rereading scrollTop", () => {
    const harness = createHarness({ scrollHeight: 100 });
    harness.renderTranscriptRow();

    renderHook(() =>
      useAgentGUIDetailScroll(
        harness.input({
          activeConversationId: "conversation-prefetch",
          hasOlderMessages: true,
          showTimelineSkeleton: false
        })
      )
    );

    expect(harness.loadOlderConversationMessages).toHaveBeenCalledOnce();
    expect(harness.geometryReadCounts()).toEqual({
      clientHeight: 1,
      scrollHeight: 1,
      scrollTop: 0
    });
  });

  it("waits for the timeline skeleton to resolve before filling the viewport", () => {
    const harness = createHarness({ scrollHeight: 100 });
    harness.renderTranscriptRow();
    const { rerender } = renderHook(
      ({ showTimelineSkeleton }) =>
        useAgentGUIDetailScroll(
          harness.input({
            activeConversationId: "conversation-skeleton-prefetch",
            hasOlderMessages: true,
            showTimelineSkeleton
          })
        ),
      { initialProps: { showTimelineSkeleton: true } }
    );

    expect(harness.loadOlderConversationMessages).not.toHaveBeenCalled();

    rerender({ showTimelineSkeleton: false });

    expect(harness.loadOlderConversationMessages).toHaveBeenCalledOnce();
  });

  it("restores a prepend anchor from one timeline geometry snapshot", () => {
    const harness = createHarness({ scrollHeight: 5_000 });
    const { rerender } = renderHook(
      ({ isLoadingOlderMessages }) =>
        useAgentGUIDetailScroll(
          harness.input({
            activeConversationId: "conversation-prepend",
            isLoadingOlderMessages,
            showTimelineSkeleton: false
          })
        ),
      { initialProps: { isLoadingOlderMessages: true } }
    );

    act(() => {
      harness.timeline.dispatchEvent(new WheelEvent("wheel", { deltaY: -100 }));
      harness.timeline.scrollTop = 200;
      harness.timeline.dispatchEvent(new Event("scroll"));
    });
    harness.pendingPrependScrollAnchorRef.current = {
      conversationId: "conversation-prepend",
      scrollHeight: 5_000,
      scrollTop: 200
    };
    harness.setScrollHeight(6_000);
    harness.resetGeometryReadCounts();

    rerender({ isLoadingOlderMessages: false });

    expect(harness.geometryReadCounts()).toEqual({
      clientHeight: 1,
      scrollHeight: 1,
      scrollTop: 0
    });
    expect(harness.timeline.scrollTop).toBe(1_200);
  });

  it("reads timeline geometry once for an explicit scroll to bottom", () => {
    const harness = createHarness({ scrollHeight: 5_000 });
    const { result } = renderHook(() =>
      useAgentGUIDetailScroll(
        harness.input({
          activeConversationId: "conversation-scroll-bottom",
          showTimelineSkeleton: false
        })
      )
    );
    harness.timeline.scrollTop = 2_000;
    harness.resetGeometryReadCounts();

    act(() => result.current.scrollTimelineToBottom());

    expect(harness.geometryReadCounts()).toEqual({
      clientHeight: 1,
      scrollHeight: 1,
      scrollTop: 0
    });
    expect(harness.timeline.scrollTop).toBe(4_900);
  });

  it("keeps the bottom lock through observed streaming content growth", () => {
    const resizeObservers = installResizeObserverMock();
    const harness = createHarness({ scrollHeight: 5_000 });

    renderHook(() =>
      useAgentGUIDetailScroll(
        harness.input({
          activeConversationId: "conversation-observed",
          showTimelineSkeleton: false
        })
      )
    );
    const timelineObserver = resizeObservers.find((observer) =>
      observer.observed.has(harness.timelineContent)
    );
    expect(timelineObserver).toBeDefined();

    harness.setScrollHeight(6_000);
    act(() => {
      timelineObserver?.callback([], timelineObserver);
    });

    expect(harness.timeline.scrollTop).toBe(5_900);
  });

  it("preserves user scroll-away through observed streaming content growth", () => {
    const resizeObservers = installResizeObserverMock();
    const harness = createHarness({ scrollHeight: 5_000 });

    renderHook(() =>
      useAgentGUIDetailScroll(
        harness.input({
          activeConversationId: "conversation-observed-away",
          showTimelineSkeleton: false
        })
      )
    );
    const timelineObserver = resizeObservers.find((observer) =>
      observer.observed.has(harness.timelineContent)
    );
    expect(timelineObserver).toBeDefined();
    act(() => {
      harness.timeline.dispatchEvent(new WheelEvent("wheel", { deltaY: -100 }));
      harness.timeline.scrollTop = 4_000;
      harness.timeline.dispatchEvent(new Event("scroll"));
    });

    harness.setScrollHeight(6_000);
    act(() => {
      timelineObserver?.callback([], timelineObserver);
    });

    expect(harness.timeline.scrollTop).toBe(4_000);
  });

  it("moves floating dock controls above a growing composer without reserving timeline space", () => {
    const harness = createHarness({ scrollHeight: 5_000 });
    const composerInputShell = document.createElement("div");
    const promptInputArea = document.createElement("div");
    const clippedEditorContent = document.createElement("div");
    composerInputShell.className = "agent-gui-node__composer-input-shell";
    promptInputArea.className = "agent-gui-node__composer-prompt-input-area";
    promptInputArea.appendChild(clippedEditorContent);
    composerInputShell.appendChild(promptInputArea);
    harness.bottomDock.appendChild(composerInputShell);
    harness.bottomDock.getBoundingClientRect = vi.fn(() =>
      mockRect({ top: 400, bottom: 500, width: 600, height: 100 })
    );
    composerInputShell.getBoundingClientRect = vi.fn(() =>
      mockRect({ top: 320, bottom: 500, width: 600, height: 180 })
    );
    promptInputArea.getBoundingClientRect = vi.fn(() =>
      mockRect({ top: 320, bottom: 450, width: 600, height: 130 })
    );
    clippedEditorContent.getBoundingClientRect = vi.fn(() =>
      mockRect({ top: 240, bottom: 440, width: 560, height: 200 })
    );

    renderHook(() =>
      useAgentGUIDetailScroll(
        harness.input({
          activeConversationId: "conversation-growing-composer",
          showTimelineSkeleton: false
        })
      )
    );

    expect(
      harness.timeline.style.getPropertyValue(
        "--agent-gui-bottom-dock-safe-area"
      )
    ).toBe("0px");
    expect(
      harness.bottomDock.style.getPropertyValue(
        "--agent-gui-bottom-dock-floating-safe-area"
      )
    ).toBe("80px");
  });

  it("reuses dock safe-area geometry across a conversation switch", () => {
    const resizeObservers = installResizeObserverMock();
    const harness = createHarness({ scrollHeight: 5_000 });
    const animationFrames: FrameRequestCallback[] = [];
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      animationFrames.push(callback);
      return animationFrames.length;
    });
    const dockRect = vi
      .spyOn(harness.bottomDock, "getBoundingClientRect")
      .mockReturnValue(
        mockRect({ top: 400, bottom: 500, width: 600, height: 100 })
      );
    const { rerender } = renderHook(
      ({ activeConversationId }) =>
        useAgentGUIDetailScroll(
          harness.input({
            activeConversationId,
            showTimelineSkeleton: false
          })
        ),
      { initialProps: { activeConversationId: "conversation-a" } }
    );
    expect(dockRect).toHaveBeenCalledOnce();
    const dockObserver = resizeObservers.find((observer) =>
      observer.observed.has(harness.bottomDock)
    );
    expect(dockObserver).toBeDefined();
    act(() => {
      dockObserver?.callback([], dockObserver);
    });
    expect(dockRect).toHaveBeenCalledTimes(2);
    dockRect.mockClear();

    harness.setScrollHeight(8_000);
    rerender({ activeConversationId: "conversation-b" });

    expect(dockRect).not.toHaveBeenCalled();
    expect(
      resizeObservers.filter((observer) =>
        observer.observed.has(harness.bottomDock)
      )
    ).toEqual([dockObserver]);
    expect(harness.timeline.scrollTop).toBe(7_900);
    act(() => animationFrames.at(-1)?.(0));
    expect(harness.timeline.scrollTop).toBe(7_900);

    harness.setScrollHeight(9_000);
    const timelineObserver = resizeObservers.find((observer) =>
      observer.observed.has(harness.timelineContent)
    );
    expect(timelineObserver).toBeDefined();
    act(() => {
      timelineObserver?.callback([], timelineObserver);
    });

    expect(harness.timeline.scrollTop).toBe(8_900);
    expect(dockRect).not.toHaveBeenCalled();
  });

  it("disconnects dock observation when the timeline conversation clears", () => {
    const resizeObservers = installResizeObserverMock();
    const harness = createHarness({ scrollHeight: 5_000 });
    const { rerender } = renderHook(
      ({ timelineConversationId }: { timelineConversationId: string | null }) =>
        useAgentGUIDetailScroll(
          harness.input({
            activeConversationId: "conversation-cleared",
            showTimelineSkeleton: false,
            timelineConversationId
          })
        ),
      {
        initialProps: {
          timelineConversationId: "conversation-cleared" as string | null
        }
      }
    );
    const dockObserver = resizeObservers.find((observer) =>
      observer.observed.has(harness.bottomDock)
    );
    expect(dockObserver).toBeDefined();

    rerender({ timelineConversationId: null });

    expect(dockObserver?.observed.size).toBe(0);
    expect(
      resizeObservers.some((observer) =>
        observer.observed.has(harness.bottomDock)
      )
    ).toBe(false);
  });

  it("disconnects layout observation while fully occluded and catches up when exposed", () => {
    const resizeObservers = installResizeObserverMock();
    const harness = createHarness({ scrollHeight: 5_000 });
    const { rerender } = renderHook(
      ({ conversation, isVisible }) =>
        useAgentGUIDetailScroll(
          harness.input({
            activeConversationId: "conversation-visibility",
            conversation,
            isVisible,
            showTimelineSkeleton: false
          })
        ),
      {
        initialProps: {
          conversation: conversationVM("initial"),
          isVisible: true
        }
      }
    );
    const visibleDockObserver = resizeObservers.find((observer) =>
      observer.observed.has(harness.bottomDock)
    );
    const visibleTimelineObserver = resizeObservers.find((observer) =>
      observer.observed.has(harness.timelineContent)
    );
    expect(visibleDockObserver).toBeDefined();
    expect(visibleTimelineObserver).toBeDefined();

    rerender({ conversation: conversationVM("hidden"), isVisible: false });

    expect(visibleDockObserver?.observed.size).toBe(0);
    expect(visibleTimelineObserver?.observed.size).toBe(0);
    harness.resetGeometryReadCounts();
    harness.resetScrollTopWriteCount();
    harness.setScrollHeight(8_000);
    rerender({ conversation: conversationVM("streaming"), isVisible: false });
    expect(harness.geometryReadCounts()).toEqual({
      clientHeight: 0,
      scrollHeight: 0,
      scrollTop: 0
    });
    expect(harness.scrollTopWriteCount()).toBe(0);

    rerender({ conversation: conversationVM("streaming"), isVisible: true });

    const resumedTimelineObserver = resizeObservers.find(
      (observer) =>
        observer !== visibleTimelineObserver &&
        observer.observed.has(harness.timelineContent)
    );
    expect(resumedTimelineObserver).toBeDefined();
    act(() => {
      resumedTimelineObserver?.callback([], resumedTimelineObserver);
    });
    expect(harness.timeline.scrollTop).toBe(7_900);
    expect(harness.geometryReadCounts()).toEqual({
      clientHeight: 1,
      scrollHeight: 1,
      scrollTop: 2
    });
    expect(harness.scrollTopWriteCount()).toBe(1);
  });

  it("restores a following virtualized conversation after exposure", () => {
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      callback(0);
      return 1;
    });
    const harness = createHarness({ scrollHeight: 5_000 });
    const controller = virtualScrollController("conversation-exposure");
    harness.virtualScrollControllerRef.current = controller;
    const hiddenConversation = conversationVM("hidden-update");
    const { rerender } = renderHook(
      ({ conversation, isVisible }) =>
        useAgentGUIDetailScroll(
          harness.input({
            activeConversationId: "conversation-exposure",
            conversation,
            isVisible,
            showTimelineSkeleton: false
          })
        ),
      {
        initialProps: {
          conversation: conversationVM("initial"),
          isVisible: true
        }
      }
    );
    controller.scrollToEnd.mockClear();

    rerender({ conversation: hiddenConversation, isVisible: false });
    rerender({ conversation: hiddenConversation, isVisible: true });

    expect(controller.scrollToEnd).toHaveBeenCalledWith({ behavior: "auto" });
  });

  it("remeasures dock safe-area after store and ResizeObserver invalidation", () => {
    const resizeObservers = installResizeObserverMock();
    const harness = createHarness({ scrollHeight: 5_000 });
    const liftedChrome = document.createElement("div");
    harness.bottomDock.appendChild(liftedChrome);
    let liftedTop = 350;
    harness.bottomDock.getBoundingClientRect = vi.fn(() =>
      mockRect({ top: 400, bottom: 500, width: 600, height: 100 })
    );
    const liftedRect = vi
      .spyOn(liftedChrome, "getBoundingClientRect")
      .mockImplementation(() =>
        mockRect({
          top: liftedTop,
          bottom: liftedTop + 40,
          width: 600,
          height: 40
        })
      );
    const { rerender } = renderHook(
      ({ bottomDockStoreRevision }) =>
        useAgentGUIDetailScroll(
          harness.input({
            activeConversationId: "conversation-dock-invalidation",
            bottomDockStoreRevision,
            showTimelineSkeleton: false
          })
        ),
      { initialProps: { bottomDockStoreRevision: "first" } }
    );

    expect(
      harness.timeline.style.getPropertyValue(
        "--agent-gui-bottom-dock-safe-area"
      )
    ).toBe("50px");
    liftedRect.mockClear();
    liftedTop = 320;

    rerender({ bottomDockStoreRevision: "second" });

    expect(liftedRect).toHaveBeenCalledOnce();
    expect(
      harness.timeline.style.getPropertyValue(
        "--agent-gui-bottom-dock-safe-area"
      )
    ).toBe("80px");

    liftedRect.mockClear();
    liftedTop = 300;
    const dockObserver = resizeObservers.find((observer) =>
      observer.observed.has(harness.bottomDock)
    );
    expect(dockObserver).toBeDefined();
    act(() => {
      dockObserver?.callback([], dockObserver);
    });

    expect(liftedRect).toHaveBeenCalledOnce();
    expect(
      harness.timeline.style.getPropertyValue(
        "--agent-gui-bottom-dock-safe-area"
      )
    ).toBe("100px");
  });
});

function mockRect(input: {
  top: number;
  bottom: number;
  width: number;
  height: number;
}): DOMRect {
  return {
    ...input,
    left: 0,
    right: input.width,
    x: 0,
    y: input.top,
    toJSON: () => ({})
  } as DOMRect;
}

function createHarness(input: { scrollHeight: number }) {
  const timeline = document.createElement("div");
  const timelineContent = document.createElement("div");
  const bottomDock = document.createElement("div");
  let scrollTop = 0;
  let scrollHeight = input.scrollHeight;
  let clientHeightReadCount = 0;
  let scrollHeightReadCount = 0;
  let scrollTopReadCount = 0;
  let scrollTopWriteCount = 0;
  Object.defineProperties(timeline, {
    clientHeight: {
      configurable: true,
      get: () => {
        clientHeightReadCount += 1;
        return 100;
      }
    },
    scrollHeight: {
      configurable: true,
      get: () => {
        scrollHeightReadCount += 1;
        return scrollHeight;
      }
    },
    scrollTop: {
      configurable: true,
      get: () => {
        scrollTopReadCount += 1;
        return scrollTop;
      },
      set: (value: number) => {
        scrollTopWriteCount += 1;
        scrollTop = value;
      }
    }
  });
  timeline.scrollTo = ((options: ScrollToOptions) => {
    scrollTop = options.top ?? scrollTop;
  }) as typeof timeline.scrollTo;

  const timelineScrollAnchorRef = mutableRef<{
    conversationId: string;
    scrollHeight: number;
    scrollTop: number;
    clientHeight: number;
  } | null>(null);
  const pendingPrependScrollAnchorRef = mutableRef<{
    conversationId: string;
    scrollHeight: number;
    scrollTop: number;
  } | null>(null);
  const submittedPromptScrollConversationRef = mutableRef<string | null>(null);
  const virtualScrollControllerRef =
    mutableRef<AgentTranscriptVirtualScrollController | null>(null);
  const loadOlderConversationMessages = vi.fn();
  const actions = {
    loadOlderConversationMessages
  } as unknown as AgentGUINodeViewProps["actions"];

  return {
    bottomDock,
    loadOlderConversationMessages,
    pendingPrependScrollAnchorRef,
    submittedPromptScrollConversationRef,
    timeline,
    timelineContent,
    virtualScrollControllerRef,
    resetGeometryReadCounts() {
      clientHeightReadCount = 0;
      scrollHeightReadCount = 0;
      scrollTopReadCount = 0;
    },
    resetScrollTopWriteCount() {
      scrollTopWriteCount = 0;
    },
    renderTranscriptRow() {
      const row = document.createElement("div");
      row.dataset.agentTranscriptRow = "row";
      timelineContent.append(row);
    },
    scrollTopWriteCount() {
      return scrollTopWriteCount;
    },
    geometryReadCounts() {
      return {
        clientHeight: clientHeightReadCount,
        scrollHeight: scrollHeightReadCount,
        scrollTop: scrollTopReadCount
      };
    },
    setScrollHeight(value: number) {
      scrollHeight = value;
    },
    input(options: {
      activeConversationId: string;
      bottomDockStoreRevision?: string;
      conversation?: AgentConversationVM;
      hasOlderMessages?: boolean;
      isLoadingOlderMessages?: boolean;
      isVisible?: boolean;
      showTimelineSkeleton: boolean;
      timelineConversationId?: string | null;
    }) {
      return {
        actions,
        bottomDockRef: ref(bottomDock),
        bottomDockStoreRevision: options.bottomDockStoreRevision ?? "stable",
        conversation: options.conversation ?? null,
        isVisible: options.isVisible ?? true,
        pendingPrependScrollAnchorRef,
        showTimelineSkeleton: options.showTimelineSkeleton,
        submittedPromptScrollConversationRef,
        timelineConversationId:
          options.timelineConversationId === undefined
            ? options.activeConversationId
            : options.timelineConversationId,
        timelineContentRef: ref(timelineContent),
        timelineRef: ref(timeline),
        timelineScrollAnchorRef,
        virtualScrollControllerRef,
        ...featureProps(
          options.activeConversationId,
          options.hasOlderMessages,
          options.isLoadingOlderMessages
        )
      };
    }
  };
}

function conversationVM(id: string): AgentConversationVM {
  return { id } as unknown as AgentConversationVM;
}

function virtualScrollController(
  agentSessionId: string,
  atEnd = true
): AgentTranscriptVirtualScrollController & {
  isAtEnd: Mock<() => boolean>;
  scrollToEnd: Mock<(options?: { behavior?: ScrollBehavior }) => void>;
  scrollToOffset: Mock<
    (offset: number, options?: { behavior?: ScrollBehavior }) => void
  >;
} {
  return {
    agentSessionId,
    enabled: true,
    isAtEnd: vi.fn(() => atEnd),
    scrollToEnd: vi.fn<(options?: { behavior?: ScrollBehavior }) => void>(),
    scrollToOffset:
      vi.fn<(offset: number, options?: { behavior?: ScrollBehavior }) => void>()
  };
}

interface ResizeObserverMock extends ResizeObserver {
  readonly callback: ResizeObserverCallback;
  readonly observed: Set<Element>;
}

function installResizeObserverMock(): ResizeObserverMock[] {
  const observers: ResizeObserverMock[] = [];
  class TestResizeObserver implements ResizeObserverMock {
    readonly observed = new Set<Element>();

    constructor(readonly callback: ResizeObserverCallback) {
      observers.push(this);
    }

    observe(target: Element): void {
      this.observed.add(target);
    }

    unobserve(target: Element): void {
      this.observed.delete(target);
    }

    disconnect(): void {
      this.observed.clear();
    }
  }
  vi.stubGlobal("ResizeObserver", TestResizeObserver);
  return observers;
}

function featureProps(
  activeConversationId: string,
  hasOlderMessages = false,
  isLoadingOlderMessages = false
) {
  return { activeConversationId, hasOlderMessages, isLoadingOlderMessages };
}

function mutableRef<T>(current: T): MutableRefObject<T> {
  return { current };
}

function ref<T>(current: T): RefObject<T> {
  return { current };
}
