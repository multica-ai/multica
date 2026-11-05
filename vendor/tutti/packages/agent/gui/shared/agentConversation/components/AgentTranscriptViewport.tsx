import { observer } from "mobx-react-lite";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import {
  Fragment,
  createContext,
  useContext,
  useCallback,
  useLayoutEffect,
  useMemo,
  useState,
  type JSX,
  type Ref
} from "react";
import type { ReactVirtualizer, VirtualItem } from "@tanstack/react-virtual";
import type { AgentConversationFollowEndMode } from "../agentConversationFollowEndController";
import {
  AgentMessageLocatorRail,
  findMessageLocatorScrollParent,
  scrollTranscriptRowIntoView
} from "./AgentMessageLocatorRail";
import {
  escapeCssString,
  type AgentMessageLocatorItem
} from "./agentTranscriptModel";
import type { TranscriptPresentationModel, TranscriptGroupIdentity } from "../../../state/TranscriptPresentationModel";
import {
  AgentTranscriptAttachmentView,
  useAgentTranscriptTurnAttachments,
  type AgentTranscriptAttachmentLocator,
  type AgentTranscriptTurnAttachment
} from "./useAgentTranscriptTurnAttachments";
import {
  AGENT_TRANSCRIPT_ESTIMATED_TURN_HEIGHT_PX,
  useAgentTranscriptVirtualizer,
  type AgentTranscriptVirtualScrollController
} from "./useAgentTranscriptVirtualizer";
import { useTurnDisclosureMotion } from "./useTurnDisclosureMotion";
import { useAgentConversationNavigation } from "./useAgentConversationNavigation";
import { Button } from "@multica/ui/components/ui/button";
import { RotateCw } from "lucide-react";
import { useTranslation } from "../../../i18n";

const FALLBACK_TURN_COUNT = 3;
type DisclosureMotionChange = ReturnType<typeof useTurnDisclosureMotion>[1];
export type RenderAgentTranscriptTurn = (
  group: TranscriptGroupIdentity,
  onDisclosureMotionChange: DisclosureMotionChange
) => JSX.Element;

export interface AgentTranscriptViewportOptions {
  navigation?: import("@multica/conversation-client/services").ConversationNavigation;
  isVisible: boolean;
  followEndMode?: AgentConversationFollowEndMode;
  virtualListLayoutRevision: number;
  virtualScrollControllerRef?: Ref<AgentTranscriptVirtualScrollController>;
  turnAttachments: readonly AgentTranscriptTurnAttachment[];
  turnAttachmentLocatorRef?: Ref<AgentTranscriptAttachmentLocator>;
  onTurnAttachmentVisibilityChange?: (attachmentId: string, visible: boolean) => void;
}

// Viewport options cross the structural memo boundary without rebuilding it.
export const AgentTranscriptViewportOptionsContext =
  createContext<AgentTranscriptViewportOptions>({
    isVisible: true,
    virtualListLayoutRevision: 0,
    turnAttachments: Object.freeze([])
  });

interface AgentTranscriptViewportProps {
  model: TranscriptPresentationModel;
  virtualize?: boolean;
  userMessageLocatorLabel?: string;
  renderTurnGroup: RenderAgentTranscriptTurn;
}

/** Viewport updates stay below the conversation's structural projections. */
export const AgentTranscriptViewport = observer(function AgentTranscriptViewport({
  model,
  virtualize,
  userMessageLocatorLabel,
  renderTurnGroup
}: AgentTranscriptViewportProps) {
  const { t } = useTranslation();
  const agentSessionId = model.sessionId;
  const turnGroups = model.viewportGroups;
  const shouldVirtualize = virtualize ?? model.shouldVirtualize;
  const userMessageLocatorItems = model.locatorItems;
  const {
    navigation,
    isVisible,
    followEndMode,
    virtualListLayoutRevision,
    virtualScrollControllerRef,
    turnAttachments,
    turnAttachmentLocatorRef,
    onTurnAttachmentVisibilityChange
  } = useContext(AgentTranscriptViewportOptionsContext);
  const [hasMovingTurnDisclosure, handleDisclosureMotionChange] =
    useTurnDisclosureMotion();
  const [virtualScrollElement, setVirtualScrollElement] =
    useState<HTMLElement | null>(null);
  const [scrollMargin, setScrollMargin] = useState(0);
  const { rowVirtualizer, setVirtualizerHostElement, virtualizerHostRef } =
    useAgentTranscriptVirtualizer({
      agentSessionId,
      followEndMode: navigation?.window?.has_after || navigation?.locating ? "detached" : followEndMode,
      hasMovingTurnDisclosure,
      scrollElement: virtualScrollElement,
      scrollMargin,
      shouldVirtualize,
      turnGroups,
      virtualScrollControllerRef
    });
  const attachmentProjection = useAgentTranscriptTurnAttachments({
    attachments: turnAttachments,
    locatorRef: turnAttachmentLocatorRef,
    onVisibilityChange: onTurnAttachmentVisibilityChange,
    rowVirtualizer,
    shouldVirtualize,
    turnGroups,
    virtualizerHostRef
  });
  const directory = useAgentConversationNavigation({
    navigation, loadedItems: userMessageLocatorItems, turnGroups,
    virtualizer: rowVirtualizer, shouldVirtualize, isVisible, label: userMessageLocatorLabel,
  });
  const handleLocateUserMessage = useCallback(
    (item: AgentMessageLocatorItem) => {
      if (directory.locate(item)) return;
      const scrollParent = virtualizerHostRef.current
        ? findMessageLocatorScrollParent(virtualizerHostRef.current)
        : null;
      const scrollToRenderedRow = (): boolean => {
        const renderedRow = (scrollParent ?? document).querySelector<HTMLElement>(
          `[data-agent-transcript-row="${escapeCssString(item.rowKey)}"]`
        );
        if (!renderedRow) return false;
        scrollTranscriptRowIntoView(
          renderedRow,
          scrollParent ?? findMessageLocatorScrollParent(renderedRow)
        );
        return true;
      };
      if (scrollToRenderedRow()) return;
      if (shouldVirtualize) {
        rowVirtualizer.scrollToIndex(item.turnGroupIndex, { align: "center" });
        window.setTimeout(scrollToRenderedRow, 0);
      }
    },
    [rowVirtualizer, shouldVirtualize, virtualizerHostRef, directory.locate]
  );
  const handleLocateUserMessageCallback =
    usePersistCallback(handleLocateUserMessage);

  useLayoutEffect(() => {
    if (!isVisible || !shouldVirtualize) return;
    const host = virtualizerHostRef.current;
    const scrollElement = host ? findMessageLocatorScrollParent(host) : null;
    setVirtualScrollElement(scrollElement);
    if (!host || !scrollElement) {
      setScrollMargin(0);
      return;
    }
    const nextOffset = Math.max(
      0,
      host.getBoundingClientRect().top -
        scrollElement.getBoundingClientRect().top +
        scrollElement.scrollTop
    );
    setScrollMargin((previous) =>
      previous === nextOffset ? previous : nextOffset
    );
  }, [isVisible, shouldVirtualize, virtualListLayoutRevision, virtualizerHostRef]);

  const onAttachmentElementChange = attachmentProjection.onElementChange;
  const renderAttachment = useMemo(
    () => (attachment: AgentTranscriptTurnAttachment) => (
      <AgentTranscriptAttachmentView
        key={attachment.id}
        attachment={attachment}
        onElementChange={onAttachmentElementChange}
      />
    ),
    [onAttachmentElementChange]
  );
  const normalTurnStyle = useMemo(() => ({ display: "contents" }), []);

  const usesFallbackVirtualItems = virtualScrollElement === null;
  const fallbackStartIndex = Math.max(0, turnGroups.length - FALLBACK_TURN_COUNT);
  const virtualItems = shouldVirtualize
    ? usesFallbackVirtualItems
      ? turnGroups.slice(-FALLBACK_TURN_COUNT).map((group, fallbackIndex) => ({
          index: fallbackStartIndex + fallbackIndex,
          key: group.key,
          start: (fallbackStartIndex + fallbackIndex) * AGENT_TRANSCRIPT_ESTIMATED_TURN_HEIGHT_PX
        }))
      : rowVirtualizer.getVirtualItems()
    : [];

  return (
    <div ref={directory.containerRef} data-agent-transcript-session-id={agentSessionId} data-agent-navigation-target={directory.targetMessageId} style={normalTurnStyle}>
      <AgentMessageLocatorRail
        key={agentSessionId}
        followEndMode={followEndMode}
        items={directory.items}
        isVisible={isVisible}
        label={userMessageLocatorLabel}
        onLocate={handleLocateUserMessageCallback}
        onPreview={directory.preview}
        panelItems={directory.panelItems}
        onPanelMore={directory.more}
        panelLoading={directory.paging}
        virtualSelectionSource={shouldVirtualize ? rowVirtualizer : undefined}
      />
      {directory.error ? <div role="alert" className="flex items-center gap-2">
        <span>{directory.error}</span>
        <Button variant="ghost" size="icon-xs" aria-label={t("common.refresh")} onClick={directory.retry}><RotateCw /></Button>
      </div> : null}
      {shouldVirtualize ? (
        <div
          ref={setVirtualizerHostElement}
          className="agent-gui-transcript-virtual"
          data-agent-transcript-virtualized="true"
        >
          {virtualItems.map((virtualTurn) => {
            const group = turnGroups[virtualTurn.index];
            return group ? (
              <AgentTranscriptVirtualTurn
                key={virtualTurn.key}
                virtualTurn={virtualTurn}
                rowVirtualizer={rowVirtualizer}
                group={group}
                model={model}
                usesFallbackVirtualItems={usesFallbackVirtualItems}
                renderTurnGroup={renderTurnGroup}
                onDisclosureMotionChange={handleDisclosureMotionChange}
                attachments={attachmentProjection.byGroupIndex.get(virtualTurn.index)}
                onAttachmentElementChange={onAttachmentElementChange}
              />
            ) : null;
          })}
        </div>
      ) : turnGroups.map((group, groupIndex) => (
        <Fragment key={group.key}>
          <div style={normalTurnStyle} data-multica-turn-id={group.turnId ?? ""}>
            <AgentTranscriptTurnContent
              group={group}
              renderTurnGroup={renderTurnGroup}
              onDisclosureMotionChange={handleDisclosureMotionChange}
            />
          </div>
          {attachmentProjection.byGroupIndex.get(groupIndex)?.map(renderAttachment)}
        </Fragment>
      ))}
      {attachmentProjection.trailing.map(renderAttachment)}
    </div>
  );
});

interface TurnContentProps {
  group: TranscriptGroupIdentity;
  renderTurnGroup: RenderAgentTranscriptTurn;
  onDisclosureMotionChange: DisclosureMotionChange;
}

const AgentTranscriptTurnContent = observer(function AgentTranscriptTurnContent({
  group,
  renderTurnGroup,
  onDisclosureMotionChange
}: TurnContentProps) {
  return renderTurnGroup(group, onDisclosureMotionChange);
});

const AgentTranscriptVirtualTurn = observer(function AgentTranscriptVirtualTurn({
  virtualTurn,
  rowVirtualizer,
  group,
  model,
  usesFallbackVirtualItems,
  renderTurnGroup,
  onDisclosureMotionChange,
  attachments,
  onAttachmentElementChange
}: TurnContentProps & {
  virtualTurn: Pick<VirtualItem, "index" | "key" | "start">;
  rowVirtualizer: ReactVirtualizer<HTMLElement, Element>;
  model: TranscriptPresentationModel;
  usesFallbackVirtualItems: boolean;
  attachments?: readonly AgentTranscriptTurnAttachment[];
  onAttachmentElementChange: (id: string, element: HTMLElement | null) => void;
}) {
  const gap = model.workSection(group.key) ? 24 : 12;
  const virtualStart = virtualTurn.start;
  const style = useMemo(
    () => ({
      paddingBottom: `${gap}px`,
      ...(usesFallbackVirtualItems
        ? { transform: `translateY(${virtualStart}px)` }
        : {})
    }),
    [gap, usesFallbackVirtualItems, virtualStart]
  );
  const renderAttachment = useMemo(
    () => (attachment: AgentTranscriptTurnAttachment) => (
      <AgentTranscriptAttachmentView
        key={attachment.id}
        attachment={attachment}
        onElementChange={onAttachmentElementChange}
      />
    ),
    [onAttachmentElementChange]
  );
  return (
    <div
      ref={rowVirtualizer.measureElement}
      className="agent-gui-transcript-virtual-item"
      data-index={virtualTurn.index}
      data-agent-transcript-virtual-turn={group.key}
      data-multica-turn-id={group.turnId ?? ""}
      style={style}
    >
      <AgentTranscriptTurnContent
        group={group}
        renderTurnGroup={renderTurnGroup}
        onDisclosureMotionChange={onDisclosureMotionChange}
      />
      {attachments?.map(renderAttachment)}
    </div>
  );
});
