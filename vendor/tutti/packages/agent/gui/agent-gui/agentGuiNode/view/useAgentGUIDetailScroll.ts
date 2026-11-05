import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState
} from "react";
import {
  createAgentConversationFollowEndController,
  type AgentConversationFollowEndEvent
} from "../../../shared/agentConversation/agentConversationFollowEndController";
import type { AgentTranscriptVirtualScrollController } from "../../../shared/agentConversation/components/AgentTranscriptView";
import {
  setTimelineScrollTopInstantly,
  setTimelineScrollTopWithUserTransition
} from "./AgentGUIConversationTimelinePane";
import {
  hasStaleVirtualScrollController,
  matchingVirtualScrollController,
  readBottomDockSafeArea,
  readTimelineGeometry,
  userScrollBehavior,
  writeBottomDockSafeArea,
  type BottomDockSafeArea
} from "./agentGUIDetailScrollHelpers";
import type { AgentGUIDetailScrollInput } from "./agentGUIDetailScrollTypes";
import {
  AgentGUIConversationScrollMemory,
  type TimelineScrollAnchor
} from "./agentGUIScrollMemory";

import { useTimelineEndPosition } from "@multica/tutti-agent-gui-host/timeline-end-position";

/** Older history is read while less than this many viewports remain above the visible part. */
const AGENT_GUI_TOP_HISTORY_PREFETCH_VIEWPORTS = 3;
const AGENT_GUI_TOP_MASK_SCROLL_EPSILON_PX = 1;
const AGENT_GUI_REACHED_END_EPSILON_PX = 1;

export function useAgentGUIDetailScroll(input: AgentGUIDetailScrollInput) {
  const {
    actions,
    bottomDockRef,
    bottomDockStoreRevision,
    conversation,
    isVisible,
    pendingPrependScrollAnchorRef,
    showTimelineSkeleton,
    submittedPromptScrollConversationRef,
    timelineConversationId,
    timelineContentRef,
    timelineRef,
    timelineScrollAnchorRef,
    virtualScrollControllerRef,
    activeConversationId: selectedConversationId,
    hasOlderMessages,
    isLoadingOlderMessages,
    historyWindowActive = false,
    historyNavigation
  } = input;
  const historyRange = historyNavigation?.window;
  const historyPaging = historyNavigation?.paging;
  const historyLocating = historyNavigation?.locating;
  const historyStartIndex = historyNavigation?.historyStartIndex ?? conversation?.sourceDetail?.turns?.[0]?.id;
  const initialWindowPending = historyNavigation?.initialWindowPending;
  const [isTimelineScrolledToTop, setIsTimelineScrolledToTop] = useState(true);
  const followEndControllerRef = useRef(
    createAgentConversationFollowEndController()
  );
  const followEndController = followEndControllerRef.current;
  const conversationScrollMemoryRef = useRef(
    new AgentGUIConversationScrollMemory()
  );
  const [followEndMode, setFollowEndMode] = useState(
    followEndController.getSnapshot
  );
  const dispatchFollowEnd = useCallback(
    (event: AgentConversationFollowEndEvent): void => {
      const nextMode = followEndController.dispatch(historyWindowActive && event === "user-reached-end" ? "user-scrolled-away" : event);
      const anchor = timelineScrollAnchorRef.current;
      if (anchor) {
        conversationScrollMemoryRef.current.write(anchor, nextMode);
      }
      setFollowEndMode(nextMode);
    },
    [followEndController, timelineScrollAnchorRef, historyWindowActive]
  );
  const writeTimelineScrollAnchor = useCallback(
    (anchor: TimelineScrollAnchor): void => {
      timelineScrollAnchorRef.current = anchor;
      conversationScrollMemoryRef.current.write(
        anchor,
        followEndController.getSnapshot()
      );
    },
    [followEndController, timelineScrollAnchorRef]
  );
  const pointerScrollConversationRef = useRef<string | null>(null);
  const userScrollDirectionRef = useRef<"away" | "toward-end" | null>(null);
  const lastShowTimelineSkeletonRef = useRef(showTimelineSkeleton);
  const bottomDockSafeAreaRef = useRef<BottomDockSafeArea | null>(null);
  const lastHistoryPrefetchRef = useRef<string | null>(null);
  const historyUserScrollRef = useRef(false);
  const [virtualScrollControllerRevision, setVirtualScrollControllerRevision] =
    useState(0);
  const setVirtualScrollController = useCallback(
    (controller: AgentTranscriptVirtualScrollController | null) => {
      if (virtualScrollControllerRef.current === controller) {
        return;
      }
      virtualScrollControllerRef.current = controller;
      if (controller) {
        setVirtualScrollControllerRevision((revision) => revision + 1);
      }
    },
    [virtualScrollControllerRef]
  );
  const lastVirtualScrollControllerRevisionRef = useRef(
    virtualScrollControllerRevision
  );
  useLayoutEffect(() => {
    if (historyWindowActive) dispatchFollowEnd("user-scrolled-away");
  }, [historyWindowActive, dispatchFollowEnd]);
  useLayoutEffect(() => {
    if (!isVisible) {
      return;
    }
    const timelineSkeletonChanged =
      lastShowTimelineSkeletonRef.current !== showTimelineSkeleton;
    lastShowTimelineSkeletonRef.current = showTimelineSkeleton;
    const timeline = timelineRef.current;
    if (!timeline) {
      return;
    }
    const activeConversationId = timelineConversationId;
    if (!activeConversationId) {
      timelineScrollAnchorRef.current = null;
      pendingPrependScrollAnchorRef.current = null;
      pointerScrollConversationRef.current = null;
      userScrollDirectionRef.current = null;
      submittedPromptScrollConversationRef.current = null;
      setIsTimelineScrolledToTop(true);
      return;
    }
    if (activeConversationId !== selectedConversationId) {
      return;
    }
    let anchor = timelineScrollAnchorRef.current;
    const conversationChanged =
      !anchor || anchor.conversationId !== activeConversationId;
    if (conversationChanged) {
      historyUserScrollRef.current = false;
      if (anchor) {
        conversationScrollMemoryRef.current.write(
          anchor,
          followEndController.getSnapshot()
        );
      }
      const rememberedScroll =
        conversationScrollMemoryRef.current.read(activeConversationId);
      const restoredFollowEndMode =
        rememberedScroll?.followEndMode ?? "following";
      setFollowEndMode(
        followEndController.dispatch(
          restoredFollowEndMode === "detached"
            ? "user-scrolled-away"
            : "conversation-changed"
        )
      );
      pointerScrollConversationRef.current = null;
      userScrollDirectionRef.current = null;
      if (showTimelineSkeleton) {
        timelineScrollAnchorRef.current = null;
        setIsTimelineScrolledToTop(true);
        return;
      }
      anchor = rememberedScroll?.anchor ?? {
        clientHeight: 0,
        conversationId: activeConversationId,
        scrollHeight: Number.POSITIVE_INFINITY,
        scrollTop: 0
      };
      timelineScrollAnchorRef.current = anchor;
      conversationScrollMemoryRef.current.write(
        anchor,
        rememberedScroll?.followEndMode ?? "following"
      );
    }
    if (!anchor) {
      return;
    }
    if (
      hasStaleVirtualScrollController(
        virtualScrollControllerRef,
        activeConversationId
      )
    ) {
      return;
    }

    const prependAnchor = pendingPrependScrollAnchorRef.current;
    const shouldScrollSubmittedPromptToBottom =
      submittedPromptScrollConversationRef.current === activeConversationId;
    const shouldRestorePrependAnchor =
      prependAnchor?.conversationId === activeConversationId;
    const virtualScrollControllerChanged =
      lastVirtualScrollControllerRevisionRef.current !==
      virtualScrollControllerRevision;
    lastVirtualScrollControllerRevisionRef.current =
      virtualScrollControllerRevision;
    if (
      !conversationChanged &&
      !shouldScrollSubmittedPromptToBottom &&
      !shouldRestorePrependAnchor &&
      !timelineSkeletonChanged &&
      !virtualScrollControllerChanged
    ) {
      return;
    }
    const virtualScrollController = matchingVirtualScrollController(
      virtualScrollControllerRef,
      activeConversationId
    );
    if (virtualScrollController) {
      const followsEnd = followEndController.getSnapshot() === "following";
      if (
        shouldScrollSubmittedPromptToBottom ||
        (followsEnd && (conversationChanged || virtualScrollControllerChanged))
      ) {
        if (shouldScrollSubmittedPromptToBottom) {
          dispatchFollowEnd("prompt-submitted");
        }
        pointerScrollConversationRef.current = null;
        userScrollDirectionRef.current = null;
        virtualScrollController.scrollToEnd({ behavior: "auto" });
        submittedPromptScrollConversationRef.current = null;
        if (shouldScrollSubmittedPromptToBottom) {
          pendingPrependScrollAnchorRef.current = null;
        }
      } else if (
        !followsEnd &&
        (conversationChanged ||
          virtualScrollControllerChanged ||
          (timelineSkeletonChanged && !showTimelineSkeleton))
      ) {
        virtualScrollController.scrollToOffset(anchor.scrollTop, {
          behavior: "auto"
        });
      }
      if (shouldRestorePrependAnchor && !isLoadingOlderMessages) {
        pendingPrependScrollAnchorRef.current = null;
      }
      const virtualAnchor = anchor;
      writeTimelineScrollAnchor({
        ...virtualAnchor,
        conversationId: activeConversationId
      });
      setIsTimelineScrolledToTop(
        virtualAnchor.scrollTop <= AGENT_GUI_TOP_MASK_SCROLL_EPSILON_PX
      );
      return;
    }
    const geometry = readTimelineGeometry(timeline);
    const maxScrollTop = geometry.maxScrollTop;
    let nextScrollTop: number;
    if (conversationChanged || shouldScrollSubmittedPromptToBottom) {
      if (shouldScrollSubmittedPromptToBottom) {
        dispatchFollowEnd("prompt-submitted");
      }
      pointerScrollConversationRef.current = null;
      userScrollDirectionRef.current = null;
    }
    const shouldKeepBottomLocked =
      followEndController.getSnapshot() === "following";

    if (shouldScrollSubmittedPromptToBottom || shouldKeepBottomLocked) {
      setTimelineScrollTopInstantly(timeline, maxScrollTop);
      nextScrollTop = maxScrollTop;
      submittedPromptScrollConversationRef.current = null;
      // Following the end needs no anchor; one left behind would pull the view
      // back here on the first streamed update after the user scrolls up.
      pendingPrependScrollAnchorRef.current = null;
    } else if (shouldRestorePrependAnchor && prependAnchor) {
      const nextScrollHeight = geometry.scrollHeight;
      if (isLoadingOlderMessages) {
        // Nothing has been prepended yet: growth now is output streaming below.
        nextScrollTop = Math.min(maxScrollTop, anchor.scrollTop);
        timeline.scrollTop = nextScrollTop;
        pendingPrependScrollAnchorRef.current = {
          conversationId: activeConversationId,
          scrollHeight: nextScrollHeight,
          scrollTop: nextScrollTop
        };
      } else {
        const delta = nextScrollHeight - prependAnchor.scrollHeight;
        nextScrollTop = Math.max(0, prependAnchor.scrollTop + delta);
        timeline.scrollTop = nextScrollTop;
        pendingPrependScrollAnchorRef.current = null;
      }
    } else {
      nextScrollTop = Math.min(maxScrollTop, anchor.scrollTop);
      timeline.scrollTop = nextScrollTop;
    }

    writeTimelineScrollAnchor({
      conversationId: activeConversationId,
      scrollHeight: geometry.scrollHeight,
      scrollTop: nextScrollTop,
      clientHeight: geometry.clientHeight
    });
    setIsTimelineScrolledToTop(
      nextScrollTop <= AGENT_GUI_TOP_MASK_SCROLL_EPSILON_PX
    );
  }, [
    conversation,
    dispatchFollowEnd,
    followEndController,
    isVisible,
    showTimelineSkeleton,
    timelineConversationId,
    virtualScrollControllerRevision,
    selectedConversationId,
    isLoadingOlderMessages,
    writeTimelineScrollAnchor
  ]);

  const hasTimelineConversation = timelineConversationId !== null;
  useLayoutEffect(() => {
    if (!isVisible) {
      return;
    }
    const timeline = timelineRef.current;
    const bottomDock = bottomDockRef.current;
    if (!hasTimelineConversation || !timeline || !bottomDock) {
      return;
    }

    let animationFrameId: number | null = null;
    const resolveBottomLockConversation = (): string | null => {
      const activeConversationId =
        timelineScrollAnchorRef.current?.conversationId ?? null;
      if (
        !activeConversationId ||
        followEndController.getSnapshot() !== "following"
      ) {
        return null;
      }
      const anchor = timelineScrollAnchorRef.current;
      if (!anchor || anchor.conversationId !== activeConversationId) {
        return null;
      }
      return activeConversationId;
    };

    const syncBottomDockSafeArea = (forceMeasurement: boolean): void => {
      const cachedSafeArea = bottomDockSafeAreaRef.current;
      if (
        !forceMeasurement &&
        cachedSafeArea?.bottomDock === bottomDock &&
        cachedSafeArea.revision === bottomDockStoreRevision
      ) {
        writeBottomDockSafeArea(timeline, cachedSafeArea);
        return;
      }
      const measuredSafeArea = readBottomDockSafeArea(bottomDock);
      if (
        forceMeasurement &&
        cachedSafeArea?.bottomDock === bottomDock &&
        cachedSafeArea.timelineOverflowHeight ===
          measuredSafeArea.timelineOverflowHeight &&
        cachedSafeArea.floatingOverflowHeight ===
          measuredSafeArea.floatingOverflowHeight
      ) {
        bottomDockSafeAreaRef.current = {
          bottomDock,
          revision: bottomDockStoreRevision,
          ...measuredSafeArea
        };
        return;
      }
      const nextSafeArea: BottomDockSafeArea = {
        bottomDock,
        revision: bottomDockStoreRevision,
        ...measuredSafeArea
      };
      bottomDockSafeAreaRef.current = nextSafeArea;
      writeBottomDockSafeArea(timeline, nextSafeArea);
    };

    const syncConversationBottomLock = (): void => {
      const scheduledConversationId = resolveBottomLockConversation();
      if (!scheduledConversationId) {
        return;
      }

      if (animationFrameId !== null) {
        window.cancelAnimationFrame(animationFrameId);
      }
      animationFrameId = window.requestAnimationFrame(() => {
        animationFrameId = null;
        if (
          resolveBottomLockConversation() !== scheduledConversationId ||
          timelineRef.current !== timeline
        ) {
          return;
        }
        if (
          hasStaleVirtualScrollController(
            virtualScrollControllerRef,
            scheduledConversationId
          )
        ) {
          return;
        }
        const virtualScrollController = matchingVirtualScrollController(
          virtualScrollControllerRef,
          scheduledConversationId
        );
        if (virtualScrollController) {
          virtualScrollController.scrollToEnd({ behavior: "auto" });
          return;
        }
        const geometry = readTimelineGeometry(timeline);
        const maxScrollTop = geometry.maxScrollTop;
        timeline.scrollTop = maxScrollTop;
        writeTimelineScrollAnchor({
          conversationId: scheduledConversationId,
          scrollHeight: geometry.scrollHeight,
          scrollTop: maxScrollTop,
          clientHeight: geometry.clientHeight
        });
        setIsTimelineScrolledToTop(
          maxScrollTop <= AGENT_GUI_TOP_MASK_SCROLL_EPSILON_PX
        );
      });
    };

    syncBottomDockSafeArea(false);
    syncConversationBottomLock();
    if (typeof ResizeObserver === "undefined") {
      return () => {
        timeline.style.removeProperty("--agent-gui-bottom-dock-safe-area");
        bottomDock.style.removeProperty(
          "--agent-gui-bottom-dock-floating-safe-area"
        );
        if (animationFrameId !== null) {
          window.cancelAnimationFrame(animationFrameId);
        }
      };
    }

    const observer = new ResizeObserver(() => {
      syncBottomDockSafeArea(true);
      syncConversationBottomLock();
    });
    observer.observe(bottomDock);
    const promptInputArea = bottomDock.querySelector(
      ".agent-gui-node__composer-prompt-input-area"
    );
    if (promptInputArea instanceof Element) {
      observer.observe(promptInputArea);
    }
    return () => {
      timeline.style.removeProperty("--agent-gui-bottom-dock-safe-area");
      bottomDock.style.removeProperty(
        "--agent-gui-bottom-dock-floating-safe-area"
      );
      if (animationFrameId !== null) {
        window.cancelAnimationFrame(animationFrameId);
      }
      observer.disconnect();
    };
  }, [
    bottomDockStoreRevision,
    followEndController,
    hasTimelineConversation,
    isVisible,
    writeTimelineScrollAnchor
  ]);

  useEffect(() => {
    if (!isVisible) {
      return;
    }
    const timeline = timelineRef.current;
    const timelineContent = timelineContentRef.current;
    const activeConversationId = timelineConversationId;
    if (!timeline || !activeConversationId) {
      return;
    }

    const loadOlderMessagesNearTop = (
      scrollTop: number,
      scrollHeight: number,
      clientHeight: number,
      userScrolled = false
    ): void => {
      if (historyNavigation?.window) {
        const range = historyNavigation.window;
        if (!clientHeight || historyNavigation.locating || showTimelineSkeleton) return;
        const direction = userScrollDirectionRef.current;
        const before = scrollTop < clientHeight && range.has_before && direction !== "toward-end";
        const after = scrollHeight - scrollTop - clientHeight < clientHeight && range.has_after && direction !== "away";
        const page = before ? "before" : after ? "after" : null;
        if (!page || (historyNavigation.paging && !direction)) return;
        const key = `${activeConversationId}:${page}:${page === "before" ? range.start_index : range.end_index}`;
        if (!userScrolled && lastHistoryPrefetchRef.current === key) return;
        lastHistoryPrefetchRef.current = key;
        if (page === "before" && !historyNavigation.paging) pendingPrependScrollAnchorRef.current = { conversationId: activeConversationId, scrollHeight, scrollTop };
        void historyNavigation.more(page).catch(() => {});
        return;
      }
      if (
        activeConversationId !== selectedConversationId ||
        !hasOlderMessages ||
        isLoadingOlderMessages ||
        showTimelineSkeleton ||
        initialWindowPending ||
        scrollTop >= AGENT_GUI_TOP_HISTORY_PREFETCH_VIEWPORTS * clientHeight
      ) {
        return;
      }
      // Measurement changes do not advance the history cursor or retry a failed page.
      const geometryKey = historyStartIndex === null || historyStartIndex === undefined
        ? `${activeConversationId}:${scrollHeight}:${clientHeight}`
        : `${activeConversationId}:before:${historyStartIndex}`;
      if (!userScrolled && lastHistoryPrefetchRef.current === geometryKey) {
        return;
      }
      lastHistoryPrefetchRef.current = geometryKey;
      pendingPrependScrollAnchorRef.current = {
        conversationId: activeConversationId,
        scrollHeight,
        scrollTop
      };
      actions.loadOlderConversationMessages();
    };

    const captureScrollAnchor = (): void => {
      const userScrolled = historyUserScrollRef.current;
      historyUserScrollRef.current = false;
      const previousAnchor = timelineScrollAnchorRef.current;
      if (
        !previousAnchor ||
        previousAnchor.conversationId !== activeConversationId
      ) {
        return;
      }
      let scrollTop = timeline.scrollTop;
      const pointerDrivenScrollAway =
        pointerScrollConversationRef.current === activeConversationId &&
        scrollTop < previousAnchor.scrollTop - AGENT_GUI_REACHED_END_EPSILON_PX;
      const pointerDrivenScrollTowardEnd =
        pointerScrollConversationRef.current === activeConversationId &&
        scrollTop > previousAnchor.scrollTop + AGENT_GUI_REACHED_END_EPSILON_PX;
      if (pointerDrivenScrollAway) {
        userScrollDirectionRef.current = "away";
        dispatchFollowEnd("user-scrolled-away");
      } else if (pointerDrivenScrollTowardEnd) {
        userScrollDirectionRef.current = "toward-end";
      }
      const virtualScrollController = matchingVirtualScrollController(
        virtualScrollControllerRef,
        activeConversationId
      );
      if (
        !virtualScrollController &&
        followEndController.getSnapshot() === "following"
      ) {
        const anchoredMaxScrollTop = Math.max(
          0,
          previousAnchor.scrollHeight - previousAnchor.clientHeight
        );
        if (
          anchoredMaxScrollTop - scrollTop >
          AGENT_GUI_REACHED_END_EPSILON_PX
        ) {
          setTimelineScrollTopInstantly(timeline, anchoredMaxScrollTop);
          scrollTop = anchoredMaxScrollTop;
        }
      }
      if (
        followEndController.getSnapshot() === "detached" &&
        userScrollDirectionRef.current === "toward-end" &&
        (virtualScrollController
          ? virtualScrollController.isAtEnd(AGENT_GUI_REACHED_END_EPSILON_PX)
          : previousAnchor.scrollHeight -
              scrollTop -
              previousAnchor.clientHeight <=
            AGENT_GUI_REACHED_END_EPSILON_PX)
      ) {
        dispatchFollowEnd("user-reached-end");
      }
      writeTimelineScrollAnchor({
        conversationId: activeConversationId,
        scrollHeight: previousAnchor.scrollHeight,
        scrollTop,
        clientHeight: previousAnchor.clientHeight
      });
      setIsTimelineScrolledToTop(
        scrollTop <= AGENT_GUI_TOP_MASK_SCROLL_EPSILON_PX
      );
      loadOlderMessagesNearTop(
        scrollTop,
        previousAnchor.scrollHeight,
        previousAnchor.clientHeight,
        userScrolled
      );
    };

    const syncObservedTimelineGeometry = (
      entries: readonly ResizeObserverEntry[]
    ): void => {
      const anchor = timelineScrollAnchorRef.current;
      if (!anchor || anchor.conversationId !== activeConversationId) {
        return;
      }

      const virtualScrollController = matchingVirtualScrollController(
        virtualScrollControllerRef,
        activeConversationId
      );
      if (virtualScrollController) {
        const scrollTop = timeline.scrollTop;
        const observedClientHeight = entries.find(
          (entry) => entry.target === timeline
        )?.contentRect.height;
        const observedScrollHeight = entries.find(
          (entry) => entry.target === timelineContent
        )?.contentRect.height;
        const clientHeight = observedClientHeight ?? anchor.clientHeight;
        const scrollHeight =
          observedScrollHeight === undefined
            ? anchor.scrollHeight
            : Math.max(clientHeight, observedScrollHeight);
        writeTimelineScrollAnchor({
          ...anchor,
          clientHeight,
          scrollHeight,
          scrollTop
        });
        setIsTimelineScrolledToTop(
          scrollTop <= AGENT_GUI_TOP_MASK_SCROLL_EPSILON_PX
        );
        loadOlderMessagesNearTop(scrollTop, scrollHeight, clientHeight);
        return;
      }
      const geometry = readTimelineGeometry(timeline);
      const { clientHeight, maxScrollTop, scrollHeight } = geometry;
      const bottomLocked = followEndController.getSnapshot() === "following";
      let scrollTop = Math.min(maxScrollTop, timeline.scrollTop);
      if (bottomLocked) {
        setTimelineScrollTopInstantly(timeline, maxScrollTop);
        scrollTop = maxScrollTop;
      }
      writeTimelineScrollAnchor({
        conversationId: activeConversationId,
        scrollHeight,
        scrollTop,
        clientHeight
      });
      setIsTimelineScrolledToTop(
        scrollTop <= AGENT_GUI_TOP_MASK_SCROLL_EPSILON_PX
      );
      // A taller window can need more history with no scroll to report it.
      loadOlderMessagesNearTop(scrollTop, scrollHeight, clientHeight);
    };

    const captureWheelIntent = (event: WheelEvent): void => {
      if (!historyNavigation?.window && event.deltaY !== 0) historyUserScrollRef.current = true;
      if (event.deltaY < 0) {
        userScrollDirectionRef.current = "away";
        dispatchFollowEnd("user-scrolled-away");
      } else if (event.deltaY > 0) {
        userScrollDirectionRef.current = "toward-end";
      }
      if (historyNavigation?.window && !(event.target instanceof Element && event.target.closest("[data-agent-transcript-scroll-away-intent]"))) {
        loadOlderMessagesNearTop(
          Math.max(0, timeline.scrollTop + event.deltaY),
          timeline.scrollHeight,
          timeline.clientHeight,
          true
        );
      }
    };
    const captureKeyboardIntent = (event: KeyboardEvent): void => {
      if (
        event.key === "ArrowUp" ||
        event.key === "Home" ||
        event.key === "PageUp"
      ) {
        historyUserScrollRef.current = true;
        userScrollDirectionRef.current = "away";
        dispatchFollowEnd("user-scrolled-away");
      } else if (
        event.key === "ArrowDown" ||
        event.key === "End" ||
        event.key === "PageDown" ||
        (event.key === " " && !event.shiftKey)
      ) {
        historyUserScrollRef.current = true;
        userScrollDirectionRef.current = "toward-end";
      }
    };
    const captureSemanticScrollAwayIntent = (event: MouseEvent): void => {
      if (
        event.target instanceof Element &&
        event.target.closest("[data-agent-transcript-scroll-away-intent]")
      ) {
        userScrollDirectionRef.current = "away";
        dispatchFollowEnd("user-scrolled-away");
      }
    };
    const capturePointerIntent = (): void => {
      historyUserScrollRef.current = true;
      pointerScrollConversationRef.current = activeConversationId;
    };
    const clearPointerIntent = (): void => {
      if (pointerScrollConversationRef.current === activeConversationId) {
        pointerScrollConversationRef.current = null;
      }
    };
    const clearScrollDirection = (): void => {
      historyUserScrollRef.current = false;
      userScrollDirectionRef.current = null;
    };

    const initialAnchor = timelineScrollAnchorRef.current;
    if (initialAnchor?.conversationId === activeConversationId) {
      loadOlderMessagesNearTop(
        initialAnchor.scrollTop,
        initialAnchor.scrollHeight,
        initialAnchor.clientHeight
      );
    }
    timeline.addEventListener("scroll", captureScrollAnchor, { passive: true });
    timeline.addEventListener("scrollend", clearScrollDirection);
    timeline.addEventListener("wheel", captureWheelIntent, { passive: true });
    timeline.addEventListener("keydown", captureKeyboardIntent);
    timeline.addEventListener("click", captureSemanticScrollAwayIntent);
    timeline.addEventListener("pointerdown", capturePointerIntent, {
      passive: true
    });
    window.addEventListener("pointerup", clearPointerIntent, { passive: true });
    window.addEventListener("pointercancel", clearPointerIntent, {
      passive: true
    });
    const geometryObserver =
      timelineContent && typeof ResizeObserver !== "undefined"
        ? new ResizeObserver(syncObservedTimelineGeometry)
        : null;
    geometryObserver?.observe(timeline);
    if (timelineContent) {
      geometryObserver?.observe(timelineContent);
    }
    return () => {
      geometryObserver?.disconnect();
      timeline.removeEventListener("scroll", captureScrollAnchor);
      timeline.removeEventListener("scrollend", clearScrollDirection);
      timeline.removeEventListener("wheel", captureWheelIntent);
      timeline.removeEventListener("keydown", captureKeyboardIntent);
      timeline.removeEventListener("click", captureSemanticScrollAwayIntent);
      timeline.removeEventListener("pointerdown", capturePointerIntent);
      window.removeEventListener("pointerup", clearPointerIntent);
      window.removeEventListener("pointercancel", clearPointerIntent);
    };
  }, [
    actions,
    dispatchFollowEnd,
    followEndController,
    isVisible,
    timelineConversationId,
    showTimelineSkeleton,
    selectedConversationId,
    hasOlderMessages,
    isLoadingOlderMessages,
    historyNavigation,
    historyRange,
    historyPaging,
    historyLocating,
    historyStartIndex,
    initialWindowPending,
    writeTimelineScrollAnchor
  ]);

  const scrollTimelineToBottom = useCallback(() => {
    const timeline = timelineRef.current;
    const activeConversationId = timelineConversationId;
    if (!isVisible || !timeline || !activeConversationId) {
      return;
    }
    if (activeConversationId !== selectedConversationId) {
      return;
    }
    if (
      hasStaleVirtualScrollController(
        virtualScrollControllerRef,
        activeConversationId
      )
    ) {
      return;
    }

    const virtualScrollController = matchingVirtualScrollController(
      virtualScrollControllerRef,
      activeConversationId
    );
    dispatchFollowEnd("scroll-to-end-requested");
    userScrollDirectionRef.current = null;
    if (virtualScrollController) {
      virtualScrollController.scrollToEnd({
        behavior: userScrollBehavior()
      });
      return;
    }
    const geometry = readTimelineGeometry(timeline);
    const maxScrollTop = geometry.maxScrollTop;
    setTimelineScrollTopWithUserTransition(timeline, maxScrollTop);
    writeTimelineScrollAnchor({
      conversationId: activeConversationId,
      scrollHeight: geometry.scrollHeight,
      scrollTop: maxScrollTop,
      clientHeight: geometry.clientHeight
    });
    setIsTimelineScrolledToTop(
      maxScrollTop <= AGENT_GUI_TOP_MASK_SCROLL_EPSILON_PX
    );
  }, [
    dispatchFollowEnd,
    isVisible,
    timelineConversationId,
    selectedConversationId,
    virtualScrollControllerRef,
    writeTimelineScrollAnchor
  ]);

  const isTimelineScrolledToBottom = useTimelineEndPosition(
    input,
    virtualScrollControllerRevision
  );
  return {
    followEndMode,
    isTimelineScrolledToBottom,
    isTimelineScrolledToTop,
    setVirtualScrollController,
    scrollTimelineToBottom
  };
}
