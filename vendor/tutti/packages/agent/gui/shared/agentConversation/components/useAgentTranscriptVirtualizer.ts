import {
  useCallback,
  useImperativeHandle,
  useRef,
  type Ref,
  type RefObject
} from "react";
import { useVirtualizer, type Virtualizer } from "@tanstack/react-virtual";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import type { AgentConversationFollowEndMode } from "../agentConversationFollowEndController";
import type { AgentTranscriptTurnGroup } from "./agentTranscriptModel";

const AGENT_TRANSCRIPT_VIRTUALIZATION_OVERSCAN = 6;
export const AGENT_TRANSCRIPT_ESTIMATED_TURN_HEIGHT_PX = 280;
const preventVirtualScrollAdjustment = () => false;

export interface AgentTranscriptVirtualScrollController {
  agentSessionId: string;
  enabled: boolean;
  isAtEnd(threshold?: number): boolean;
  scrollToOffset(offset: number, options?: { behavior?: ScrollBehavior }): void;
  scrollToEnd(options?: { behavior?: ScrollBehavior }): void;
}

interface AgentTranscriptVirtualizer {
  setVirtualizerHostElement(node: HTMLDivElement | null): void;
  rowVirtualizer: ReturnType<typeof useVirtualizer<HTMLElement, Element>>;
  virtualizerHostRef: RefObject<HTMLDivElement | null>;
}

export function useAgentTranscriptVirtualizer({
  agentSessionId,
  followEndMode = "following",
  hasMovingTurnDisclosure,
  scrollElement,
  scrollMargin,
  shouldVirtualize,
  turnGroups,
  virtualScrollControllerRef
}: {
  agentSessionId: string;
  followEndMode?: AgentConversationFollowEndMode;
  hasMovingTurnDisclosure: boolean;
  scrollElement: HTMLElement | null;
  scrollMargin: number;
  shouldVirtualize: boolean;
  turnGroups: readonly Pick<AgentTranscriptTurnGroup, "key" | "turnId">[];
  virtualScrollControllerRef?: Ref<AgentTranscriptVirtualScrollController>;
}): AgentTranscriptVirtualizer {
  const virtualizerHostRef = useRef<HTMLDivElement | null>(null);
  const followsEnd = followEndMode === "following";
  const getVirtualItemKey = useCallback(
    (index: number) =>
      `${agentSessionId}\u0000${turnGroups[index]?.key ?? index}`,
    [agentSessionId, turnGroups]
  );
  const followMeasuredEnd = usePersistCallback(
    (instance: Virtualizer<HTMLElement, Element>, sync: boolean) => {
      if (sync || !shouldVirtualize || !followsEnd || hasMovingTurnDisclosure) {
        return;
      }
      // Direct DOM sizing runs after the core's resize scroll adjustment.
      // Its intended offset can exceed the old DOM limit, so check the real
      // viewport after the sizer has grown rather than trusting isAtEnd().
      const viewport = instance.scrollElement;
      if (
        viewport &&
        viewport.scrollHeight - viewport.clientHeight - viewport.scrollTop > 1
      ) {
        instance.scrollToEnd({ behavior: "auto" });
      }
    }
  );
  const rowVirtualizer = useVirtualizer<HTMLElement, Element>({
    enabled: shouldVirtualize,
    anchorTo: shouldVirtualize && hasMovingTurnDisclosure ? "start" : "end",
    count: turnGroups.length,
    directDomUpdates: true,
    directDomUpdatesMode: "transform",
    estimateSize: () => AGENT_TRANSCRIPT_ESTIMATED_TURN_HEIGHT_PX,
    followOnAppend: shouldVirtualize && followsEnd && !hasMovingTurnDisclosure,
    getItemKey: getVirtualItemKey,
    getScrollElement: () => scrollElement,
    onChange: followMeasuredEnd,
    overscan: AGENT_TRANSCRIPT_VIRTUALIZATION_OVERSCAN,
    scrollMargin,
    scrollEndThreshold: 24,
    useFlushSync: true
  });
  rowVirtualizer.shouldAdjustScrollPositionOnItemSizeChange =
    shouldVirtualize && hasMovingTurnDisclosure
      ? preventVirtualScrollAdjustment
      : undefined;
  useImperativeHandle(
    virtualScrollControllerRef,
    () => ({
      agentSessionId,
      enabled: shouldVirtualize,
      isAtEnd: (threshold) =>
        shouldVirtualize && rowVirtualizer.isAtEnd(threshold),
      scrollToOffset: (offset, options) => {
        if (shouldVirtualize) {
          rowVirtualizer.scrollToOffset(offset, options);
        }
      },
      scrollToEnd: (options) => {
        if (shouldVirtualize) {
          rowVirtualizer.scrollToEnd(options);
        }
      }
    }),
    [agentSessionId, rowVirtualizer, shouldVirtualize]
  );
  const setVirtualizerHostElement = useCallback(
    (node: HTMLDivElement | null) => {
      virtualizerHostRef.current = node;
      rowVirtualizer.containerRef(node);
    },
    [rowVirtualizer]
  );

  return {
    setVirtualizerHostElement,
    rowVirtualizer,
    virtualizerHostRef
  };
}
