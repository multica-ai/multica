import type { ConversationNavigation } from "@multica/conversation-client/services";
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { findMessageLocatorScrollParent, scrollTranscriptRowIntoView } from "./AgentMessageLocatorRail";
import { escapeCssString, normalizeLocatorSummary, type AgentMessageLocatorItem, type AgentTranscriptTurnGroup } from "./agentTranscriptModel";
import type { ReactVirtualizer } from "@tanstack/react-virtual";
import { conversationUserMessageRowKey } from "../projection/agentConversationUserProjection";

export function useAgentConversationNavigation({ navigation, loadedItems, turnGroups, virtualizer, shouldVirtualize, isVisible, label }: {
  navigation?: ConversationNavigation;
  loadedItems: readonly AgentMessageLocatorItem[];
  turnGroups: readonly Pick<AgentTranscriptTurnGroup, "key" | "turnId">[];
  virtualizer: ReactVirtualizer<HTMLElement, Element>;
  shouldVirtualize: boolean;
  isVisible: boolean;
  label?: string;
}) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [target, setTarget] = useState<{ messageId: string; turnId: string } | null>(null);
  const locateIntent = useRef(0);
  const initialWindowPending = navigation?.initialWindowPending;
  const hasLoadedTurns = turnGroups.length > 0;
  useEffect(() => {
    if (!navigation || !isVisible || !hasLoadedTurns || !initialWindowPending) return;
    let frame = requestAnimationFrame(() => {
      frame = requestAnimationFrame(() => {
        if (document.visibilityState === "visible") void navigation.completeInitialWindow().catch(() => {});
      });
    });
    return () => cancelAnimationFrame(frame);
  }, [navigation, isVisible, hasLoadedTurns, initialWindowPending]);
  useEffect(() => {
    if (navigation && isVisible) void navigation.ensure().catch(() => {});
  }, [navigation, isVisible]);
  useEffect(() => () => { locateIntent.current++; }, [navigation]);

  const count = navigation?.count;
  const loadedByMessage = new Map(loadedItems.map((item) => [item.messageId, item]));
  const groupByTurn = new Map(turnGroups.map((group, index) => [group.turnId, index]));
  const items: readonly AgentMessageLocatorItem[] = count === null || count === undefined ? loadedItems : Array.from({ length: count }, (_, index) => {
    const node = navigation!.nodes.get(index);
    const loaded = node ? loadedByMessage.get(node.message_id) : undefined;
    return {
      ...loaded,
      key: `navigation:${index}`, nodeIndex: index,
      rowKey: loaded?.rowKey ?? (node ? conversationUserMessageRowKey(node.message_id) : ""), rowIndex: loaded?.rowIndex ?? -1,
      turnGroupIndex: loaded?.turnGroupIndex ?? (node ? groupByTurn.get(node.turn_id) : undefined) ?? -1,
      turnId: node?.turn_id ?? loaded?.turnId,
      messageId: node?.message_id ?? loaded?.messageId,
      summary: node ? normalizeLocatorSummary(node.summary) || `${label ?? ""} ${index + 1}` : loaded?.summary || `${label ?? ""} ${index + 1}`,
      hasAgentResponse: node?.has_agent_response || loaded?.hasAgentResponse || false,
      agentResponseKnown: loaded !== undefined,
    };
  });

  const preview = useCallback((item: AgentMessageLocatorItem) => {
    if (navigation && item.nodeIndex !== undefined) void navigation.previewDirectory(item.nodeIndex).catch(() => {});
  }, [navigation]);
  const navigationError = navigation?.error;
  const more = useCallback((direction: "before" | "after") => {
    if (navigation && !navigationError) void navigation.moreDirectory(direction).catch(() => {});
  }, [navigation, navigationError]);
  const range = navigation?.directory;
  const panelItems = count === null || count === undefined ? undefined : items.filter((item) => {
    const node = item.nodeIndex === undefined ? undefined : navigation?.nodes.get(item.nodeIndex);
    return node && range && node.turn_index >= range.start_index && node.turn_index <= range.end_index;
  });
  const locate = useCallback((item: AgentMessageLocatorItem): boolean => {
    if (!navigation || item.nodeIndex === undefined) return false;
    const intent = ++locateIntent.current;
    void navigation.locate(item.nodeIndex).then((node) => {
      if (intent === locateIntent.current && node) setTarget({ messageId: node.message_id, turnId: node.turn_id });
    }).catch(() => {});
    return true;
  }, [navigation]);

  useLayoutEffect(() => {
    if (!target || !isVisible) return;
    const item = loadedItems.find((value) => value.messageId === target.messageId);
    const groupIndex = item?.turnGroupIndex ?? turnGroups.findIndex((group) => group.turnId === target.turnId);
    if (groupIndex < 0) return;
    const rowKey = item?.rowKey ?? conversationUserMessageRowKey(target.messageId);
    const parent = containerRef.current ? findMessageLocatorScrollParent(containerRef.current) : null;
    let frame = 0;
    let attempts = 0;
    let stableFrames = 0;
    const position = () => {
      const row = parent?.querySelector<HTMLElement>(`[data-agent-transcript-row="${escapeCssString(rowKey)}"]`);
      if (row && parent) {
        const before = parent.scrollTop;
        scrollTranscriptRowIntoView(row, parent, "auto");
        stableFrames = Math.abs(before - parent.scrollTop) < 1 ? stableFrames + 1 : 0;
        if (stableFrames >= 3) { setTarget(null); return; }
      } else if (shouldVirtualize) virtualizer.scrollToIndex(groupIndex, { align: "center" });
      if (++attempts < 30) frame = requestAnimationFrame(position);
      else setTarget(null);
    };
    position();
    return () => cancelAnimationFrame(frame);
  }, [target, loadedItems, turnGroups, virtualizer, shouldVirtualize, isVisible]);

  const retry = useCallback(() => {
    if (navigation) void navigation.refreshDirectory().catch(() => {});
  }, [navigation]);
  return { containerRef, items, panelItems, more, preview, locate, retry, targetMessageId: target?.messageId, error: navigation?.error, paging: navigation?.directoryPaging };
}
