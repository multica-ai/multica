import {
  Fragment,
  memo,
  useCallback,
  useLayoutEffect,
  useMemo,
  useState,
  type JSX,
  type ReactNode,
  type Ref
} from "react";
import { providerForkBindingAllowsAttempt } from "@tutti-os/agent-activity-core";
import type { WorkspaceLinkAction } from "../../../contexts/workspace/presentation/renderer/actions/workspaceLinkActions";
import type { AgentMessageMarkdownWorkspaceAppIcon } from "../../AgentMessageMarkdown";
import type { AgentGUIProviderSkillOption } from "../../../agent-gui/agentGuiNode/model/agentGuiNodeTypes";
import type { AgentConversationVM } from "../contracts/agentConversationVM";
import type { AgentConversationParticipantPresentation } from "../contracts/agentConversationParticipantPresentation";
import type { AgentConversationFollowEndMode } from "../agentConversationFollowEndController";
import { AgentTranscriptItemView } from "./AgentTranscriptItemView";
import {
  AgentForkThroughTurnButton,
  AgentForkThroughTurnFooter
} from "./AgentForkThroughTurnButton";
import { useAgentTurnDisclosureStore } from "./AgentTurnDisclosureContext";
import { AgentTurnWorkSection } from "./AgentTurnWorkSection";
import {
  buildAgentTurnWorkSectionModel,
  findParticipantHeaderRenderKeys
} from "./agentTurnWorkSectionModel";
import { assessAgentTranscriptComplexity } from "./agentTranscriptComplexity";
import { useTurnDisclosureMotion } from "./useTurnDisclosureMotion";
import {
  AgentMessageLocatorRail,
  findMessageLocatorScrollParent,
  scrollTranscriptRowIntoView
} from "./AgentMessageLocatorRail";
import {
  buildAgentTranscriptTurnGroups,
  buildTurnGroupIndexByRowIndex,
  buildUserMessageLocatorItems,
  escapeCssString,
  findLastMessageRowIndex,
  findTurnDividerRowIndexes,
  isAssistantParticipantContentRow,
  transcriptRowKey,
  useAgentTranscriptDisplayRows,
  useEnteringTranscriptRows,
  type AgentMessageLocatorItem
} from "./agentTranscriptModel";
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
import {
  editRetryControlsEqual,
  type AgentTranscriptEditRetryControl,
  useAgentTranscriptEditRetryProjection
} from "./useAgentTranscriptEditRetryProjection";
import { useAgentObservationGap } from "../AgentObservationGapContext";

const AGENT_TRANSCRIPT_DISCLOSURE_TURN_GAP_PX = 24;
const AGENT_TRANSCRIPT_LEGACY_TURN_GAP_PX = 12;
const AGENT_TRANSCRIPT_FALLBACK_TURN_COUNT = 3;

export type {
  AgentTranscriptAttachmentLocator,
  AgentTranscriptTurnAttachment
} from "./useAgentTranscriptTurnAttachments";
export type { AgentTranscriptVirtualScrollController } from "./useAgentTranscriptVirtualizer";
export interface AgentTranscriptViewProps {
  conversation: AgentConversationVM;
  isVisible?: boolean;
  editRetry?: AgentTranscriptEditRetryControl;
  turnAttachments?: readonly AgentTranscriptTurnAttachment[];
  turnAttachmentLocatorRef?: Ref<AgentTranscriptAttachmentLocator>;
  onTurnAttachmentVisibilityChange?: (
    attachmentId: string,
    visible: boolean
  ) => void;
  onLinkAction?: (action: WorkspaceLinkAction) => void;
  onAuthLogin?: (provider?: string | null) => void;
  onForkThroughTurn?: (turnId: string) => void;
  availableSkills?: readonly AgentGUIProviderSkillOption[];
  workspaceAppIcons?: readonly AgentMessageMarkdownWorkspaceAppIcon[];
  showRawTimelineJson?: boolean;
  participantPresentation?: AgentConversationParticipantPresentation;
  followEndMode?: AgentConversationFollowEndMode;
  forkThroughTurnPendingTurnIds?: readonly string[];
  virtualListLayoutRevision?: number;
  virtualScrollControllerRef?: Ref<AgentTranscriptVirtualScrollController>;
  labels: {
    toolCallsLabel: (count: number) => string;
    thinkingLabel: string;
    processing: string;
    turnSummary: string;
    rawTimelineJson?: string;
    userMessageLocator?: string;
  };
}

function participantPresentationEqual(
  previous: AgentConversationParticipantPresentation | undefined,
  next: AgentConversationParticipantPresentation | undefined
): boolean {
  if (previous === next) {
    return true;
  }
  if ((!previous || !previous.enabled) && (!next || !next.enabled)) {
    return true;
  }
  if (!previous?.enabled || !next?.enabled) {
    return false;
  }
  if (previous.status !== next.status) {
    return false;
  }
  if (previous.status === "loading" || next.status === "loading") {
    return true;
  }
  return (
    previous.user.name === next.user.name &&
    previous.user.avatarUrl === next.user.avatarUrl &&
    previous.agent.name === next.agent.name &&
    previous.agent.avatarUrl === next.agent.avatarUrl
  );
}

function transcriptLabelsEqual(
  previous: AgentTranscriptViewProps["labels"],
  next: AgentTranscriptViewProps["labels"]
): boolean {
  return (
    previous === next ||
    (previous.thinkingLabel === next.thinkingLabel &&
      previous.processing === next.processing &&
      previous.turnSummary === next.turnSummary &&
      previous.rawTimelineJson === next.rawTimelineJson &&
      previous.userMessageLocator === next.userMessageLocator &&
      previous.toolCallsLabel === next.toolCallsLabel)
  );
}

function transcriptTurnIdentityEquals(
  previous: AgentConversationVM["sourceDetail"]["turns"],
  next: AgentConversationVM["sourceDetail"]["turns"]
): boolean {
  return (
    previous === next ||
    (previous.length === next.length &&
      previous.every((turn, index) => turn.id === next[index]?.id))
  );
}

function transcriptCanonicalTurnsEqual(
  previous: AgentConversationVM["sourceDetail"]["sessionTurns"],
  next: AgentConversationVM["sourceDetail"]["sessionTurns"]
): boolean {
  return (
    previous === next ||
    (previous?.length === next?.length &&
      (previous?.every((turn, index) => {
        const nextTurn = next?.[index];
        return (
          turn.turnId === nextTurn?.turnId &&
          turn.phase === nextTurn.phase &&
          turn.outcome === nextTurn.outcome &&
          turn.providerForkBindingAvailable ===
            nextTurn.providerForkBindingAvailable &&
          turn.providerForkBindingState === nextTurn.providerForkBindingState &&
          turn.startedAtUnixMs === nextTurn.startedAtUnixMs &&
          turn.settledAtUnixMs === nextTurn.settledAtUnixMs
        );
      }) ??
        true))
  );
}

function transcriptConversationRenderInputEquals(
  previous: AgentConversationVM,
  next: AgentConversationVM
): boolean {
  return (
    previous === next ||
    (previous.rows === next.rows &&
      previous.workspaceRoot === next.workspaceRoot &&
      previous.sourceDetail.session.agentSessionId ===
        next.sourceDetail.session.agentSessionId &&
      previous.sourceDetail.session.activeTurnId ===
        next.sourceDetail.session.activeTurnId &&
      previous.sourceDetail.session.activeTurn?.turnId ===
        next.sourceDetail.session.activeTurn?.turnId &&
      previous.sourceDetail.session.activeTurn?.phase ===
        next.sourceDetail.session.activeTurn?.phase &&
      previous.sourceDetail.session.imported ===
        next.sourceDetail.session.imported &&
      previous.sourceDetail.session.kind === next.sourceDetail.session.kind &&
      previous.sourceDetail.session.lifecycleCapabilities.forkThroughTurn ===
        next.sourceDetail.session.lifecycleCapabilities.forkThroughTurn &&
      previous.sourceDetail.session.pendingInteractions ===
        next.sourceDetail.session.pendingInteractions &&
      previous.sourceDetail.cwd === next.sourceDetail.cwd &&
      transcriptTurnIdentityEquals(
        previous.sourceDetail.turns,
        next.sourceDetail.turns
      ) &&
      transcriptCanonicalTurnsEqual(
        previous.sourceDetail.sessionTurns,
        next.sourceDetail.sessionTurns
      ))
  );
}

export function areAgentTranscriptViewPropsEqual(
  previous: AgentTranscriptViewProps,
  next: AgentTranscriptViewProps
): boolean {
  return (
    transcriptConversationRenderInputEquals(
      previous.conversation,
      next.conversation
    ) &&
    (previous.isVisible ?? true) === (next.isVisible ?? true) &&
    previous.onLinkAction === next.onLinkAction &&
    previous.onAuthLogin === next.onAuthLogin &&
    previous.onForkThroughTurn === next.onForkThroughTurn &&
    previous.forkThroughTurnPendingTurnIds ===
      next.forkThroughTurnPendingTurnIds &&
    previous.availableSkills === next.availableSkills &&
    previous.workspaceAppIcons === next.workspaceAppIcons &&
    previous.turnAttachments === next.turnAttachments &&
    previous.turnAttachmentLocatorRef === next.turnAttachmentLocatorRef &&
    previous.onTurnAttachmentVisibilityChange ===
      next.onTurnAttachmentVisibilityChange &&
    editRetryControlsEqual(previous.editRetry, next.editRetry) &&
    previous.showRawTimelineJson === next.showRawTimelineJson &&
    previous.followEndMode === next.followEndMode &&
    previous.virtualListLayoutRevision === next.virtualListLayoutRevision &&
    previous.virtualScrollControllerRef === next.virtualScrollControllerRef &&
    participantPresentationEqual(
      previous.participantPresentation,
      next.participantPresentation
    ) &&
    transcriptLabelsEqual(previous.labels, next.labels)
  );
}

export const AgentTranscriptView = memo(function AgentTranscriptView({
  conversation,
  isVisible = true,
  editRetry,
  turnAttachments = [],
  turnAttachmentLocatorRef,
  onTurnAttachmentVisibilityChange,
  onLinkAction,
  onAuthLogin,
  onForkThroughTurn,
  availableSkills,
  workspaceAppIcons,
  showRawTimelineJson = false,
  participantPresentation,
  followEndMode,
  forkThroughTurnPendingTurnIds = [],
  virtualListLayoutRevision = 0,
  virtualScrollControllerRef,
  labels
}: AgentTranscriptViewProps): JSX.Element {
  "use memo";
  const [expandedToolRows, setExpandedToolRows] = useState<
    Record<string, boolean>
  >({});
  const [hasMovingTurnDisclosure, handleDisclosureMotionChange] =
    useTurnDisclosureMotion();
  const turnDisclosureStore = useAgentTurnDisclosureStore();
  const [virtualScrollElement, setVirtualScrollElement] =
    useState<HTMLElement | null>(null);
  const [
    virtualListOffsetFromScrollOrigin,
    setVirtualListOffsetFromScrollOrigin
  ] = useState(0);
  const agentSessionId = conversation.sourceDetail.session.agentSessionId;
  const activeTurnId = conversation.sourceDetail.session.activeTurnId;
  const observationGap = useAgentObservationGap(agentSessionId, activeTurnId);
  const participantHeadersEnabled = participantPresentation?.enabled === true;
  // Participant-header presentation (Agent board session detail): tool-group
  // rows attach to the assistant message that follows them instead of sitting
  // after the previous message, and presentation turns key off user messages.
  // Canonical Turn groups remain responsible for disclosure and timing. The
  // row projection lives in the transcript model read hook so this component
  // stays within the degradation-check memo budget.
  const transcriptRowSet = useAgentTranscriptDisplayRows(
    conversation.rows,
    participantHeadersEnabled
  );
  const displayRows = transcriptRowSet.rows;
  const rowKeys = transcriptRowSet.rowKeys;
  const { editableUserMessageRowId, scopedEditRetry } =
    useAgentTranscriptEditRetryProjection(
      displayRows,
      conversation.sourceDetail.session.agentSessionId,
      editRetry
    );
  const participantTurnProjection = transcriptRowSet.participantTurnProjection;
  const turnGroups = useMemo(
    () => buildAgentTranscriptTurnGroups(displayRows, rowKeys),
    [displayRows, rowKeys]
  );
  const turnGroupIndexByRowIndex = useMemo(
    () => buildTurnGroupIndexByRowIndex(turnGroups),
    [turnGroups]
  );
  const userMessageLocatorItems = useMemo(
    () =>
      buildUserMessageLocatorItems(
        displayRows,
        rowKeys,
        turnGroupIndexByRowIndex
      ),
    [displayRows, rowKeys, turnGroupIndexByRowIndex]
  );
  const enteringRowKeys = useEnteringTranscriptRows(rowKeys);
  const handleToolGroupExpandedChange = useCallback(
    (key: string, expanded: boolean) => {
      setExpandedToolRows((previous) => {
        if (previous[key] === expanded) {
          return previous;
        }
        return {
          ...previous,
          [key]: expanded
        };
      });
    },
    []
  );
  const turnIndexById = useMemo(
    () =>
      new Map(
        conversation.sourceDetail.turns.map((turn, index) => [turn.id, index])
      ),
    [conversation.sourceDetail.turns]
  );
  const dividerRowIndexes = useMemo(
    () =>
      participantTurnProjection
        ? participantTurnProjection.dividerRowIndexes
        : findTurnDividerRowIndexes(turnIndexById, displayRows),
    [displayRows, turnIndexById, participantTurnProjection]
  );
  const canonicalTurnById = new Map(
    (conversation.sourceDetail.sessionTurns ?? []).map((turn) => [
      turn.turnId,
      turn
    ])
  );
  const turnWorkSectionModelByKey = new Map(
    turnGroups.map((group) => {
      const isActiveTurn =
        group.turnId !== null &&
        group.turnId === conversation.sourceDetail.session.activeTurnId;
      return [
        group.key,
        buildAgentTurnWorkSectionModel(
          group,
          group.turnId ? (canonicalTurnById.get(group.turnId) ?? null) : null,
          isActiveTurn,
          {
            collapseIntermediateAssistantReplies:
              !conversation.sourceDetail.session.imported,
            liveFrozenAtUnixMs:
              group.turnId !== null && group.turnId === activeTurnId
                ? observationGap?.startedAtUnixMs
                : null,
            liveObservationGapPresentationState:
              group.turnId !== null && group.turnId === activeTurnId
                ? observationGap?.presentationState
                : null
          }
        )
      ] as const;
    })
  );
  const participantHeaderRenderKeys = participantTurnProjection
    ? findParticipantHeaderRenderKeys(
        turnGroups,
        rowKeys,
        turnWorkSectionModelByKey,
        participantTurnProjection.turnIndexByRowIndex
      )
    : null;
  const basePath = conversation.sourceDetail.cwd;
  const workspaceRoot = conversation.workspaceRoot;
  const provider = conversation.activity.agentProvider;
  const shouldVirtualize = useMemo(
    () => assessAgentTranscriptComplexity(turnGroups).shouldVirtualize,
    [turnGroups]
  );
  const { rowVirtualizer, setVirtualizerHostElement, virtualizerHostRef } =
    useAgentTranscriptVirtualizer({
      agentSessionId,
      followEndMode,
      hasMovingTurnDisclosure,
      scrollElement: virtualScrollElement,
      scrollMargin: virtualListOffsetFromScrollOrigin,
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
  const handleLocateUserMessage = useCallback(
    (item: AgentMessageLocatorItem) => {
      const scrollParent = virtualizerHostRef.current
        ? findMessageLocatorScrollParent(virtualizerHostRef.current)
        : null;
      const scrollToRenderedRow = (): boolean => {
        const renderedRow = (
          scrollParent ?? document
        ).querySelector<HTMLElement>(
          `[data-agent-transcript-row="${escapeCssString(item.rowKey)}"]`
        );
        if (!renderedRow) {
          return false;
        }
        scrollTranscriptRowIntoView(
          renderedRow,
          scrollParent ?? findMessageLocatorScrollParent(renderedRow)
        );
        return true;
      };

      if (scrollToRenderedRow()) {
        return;
      }
      if (shouldVirtualize) {
        rowVirtualizer.scrollToIndex(item.turnGroupIndex, {
          align: "center"
        });
        window.setTimeout(scrollToRenderedRow, 0);
      }
    },
    [rowVirtualizer, shouldVirtualize]
  );

  useLayoutEffect(() => {
    if (!isVisible || !shouldVirtualize) {
      return;
    }
    const virtualizerHost = virtualizerHostRef.current;
    const scrollElement = virtualizerHost
      ? findMessageLocatorScrollParent(virtualizerHost)
      : null;
    setVirtualScrollElement(scrollElement);
    if (!virtualizerHost || !scrollElement) {
      setVirtualListOffsetFromScrollOrigin(0);
      return;
    }
    const nextOffset = Math.max(
      0,
      virtualizerHost.getBoundingClientRect().top -
        scrollElement.getBoundingClientRect().top +
        scrollElement.scrollTop
    );
    setVirtualListOffsetFromScrollOrigin((previousOffset) =>
      previousOffset === nextOffset ? previousOffset : nextOffset
    );
  }, [isVisible, shouldVirtualize, virtualListLayoutRevision]);

  const renderRow = (
    row: AgentConversationVM["rows"][number],
    rowIndex: number,
    renderKey?: string,
    footerAction?: ReactNode
  ): JSX.Element => {
    const rowKey =
      renderKey ??
      (displayRows[rowIndex] === row
        ? (rowKeys[rowIndex] ?? transcriptRowKey(row))
        : transcriptRowKey(row));
    const shouldAnimateEnter =
      row.kind !== "processing" && enteringRowKeys.has(rowKey);
    const showParticipantHeader =
      participantHeaderRenderKeys?.has(rowKey) ?? false;
    const participantContent =
      participantHeadersEnabled &&
      !showParticipantHeader &&
      isAssistantParticipantContentRow(row)
        ? "assistant"
        : undefined;
    const activeTurn = conversation.sourceDetail.session.activeTurn;
    const canonicalTurn =
      row.turnId === null ? null : (canonicalTurnById.get(row.turnId) ?? null);
    const isActiveTurn =
      row.turnId !== null &&
      canonicalTurn?.phase !== "settled" &&
      (activeTurn?.turnId === row.turnId
        ? activeTurn.phase !== "settled"
        : conversation.sourceDetail.session.activeTurnId === row.turnId);
    return (
      <div
        key={rowKey}
        className="agent-gui-transcript-row"
        data-agent-transcript-row={rowKey}
        data-agent-transcript-row-kind={row.kind}
        data-agent-transcript-row-speaker={
          row.kind === "message" ? row.speaker : undefined
        }
        data-agent-transcript-row-thinking-first={
          row.kind === "message" &&
          row.speaker === "assistant" &&
          row.thinking.length > 0
            ? "true"
            : undefined
        }
        data-agent-transcript-row-thinking-last={
          row.kind === "message" &&
          row.speaker === "assistant" &&
          row.thinking.length > 0 &&
          row.messages.length === 0
            ? "true"
            : undefined
        }
        data-agent-transcript-row-index={rowIndex}
        data-agent-transcript-row-participant-content={participantContent}
        data-agent-transcript-row-enter={
          shouldAnimateEnter ? "true" : undefined
        }
      >
        <AgentTranscriptItemView
          workspaceRoot={workspaceRoot}
          basePath={basePath}
          row={row}
          editRetry={
            row.id === editableUserMessageRowId ? scopedEditRetry : undefined
          }
          labels={labels}
          onLinkAction={onLinkAction}
          onAuthLogin={onAuthLogin}
          provider={provider}
          availableSkills={availableSkills}
          workspaceAppIcons={workspaceAppIcons}
          showRawTimelineJson={showRawTimelineJson}
          participantPresentation={participantPresentation}
          showParticipantHeader={showParticipantHeader}
          isActiveTurn={isActiveTurn}
          processingPaused={
            isActiveTurn &&
            row.turnId !== null &&
            row.turnId === activeTurnId &&
            observationGap !== null
          }
          toolGroupExpanded={
            row.kind === "tool-group"
              ? expandedToolRows[rowKey] === true
              : undefined
          }
          toolGroupExpansionKey={row.kind === "tool-group" ? rowKey : undefined}
          onToolGroupExpandedChange={handleToolGroupExpandedChange}
          footerAction={footerAction}
        />
      </div>
    );
  };

  const resolveForkThroughTurnAction = (
    turnId: string | null
  ): { disabled: boolean; pending: boolean; turnId: string } | null => {
    const lifecycleCapabilities =
      conversation.sourceDetail.session.lifecycleCapabilities;
    const canonicalTurn =
      turnId === null ? null : (canonicalTurnById.get(turnId) ?? null);
    if (
      !turnId ||
      !onForkThroughTurn ||
      conversation.sourceDetail.session.kind !== "root" ||
      lifecycleCapabilities.forkThroughTurn !== true ||
      !canonicalTurn ||
      canonicalTurn.phase !== "settled" ||
      !providerForkBindingAllowsAttempt(canonicalTurn)
    ) {
      return null;
    }
    const pending = forkThroughTurnPendingTurnIds.includes(turnId);
    return {
      disabled: pending,
      pending,
      turnId
    };
  };

  const renderLegacyTurnGroup = (
    group: (typeof turnGroups)[number]
  ): JSX.Element => {
    const forkAction = resolveForkThroughTurnAction(group.turnId);
    const forkButton = forkAction ? (
      <AgentForkThroughTurnButton
        disabled={forkAction.disabled}
        pending={forkAction.pending}
        onFork={() => onForkThroughTurn?.(forkAction.turnId)}
      />
    ) : null;
    const footerRowIndex = findLastMessageRowIndex(group.rows);
    return (
      <Fragment key={group.key}>
        {group.rows.map(({ row, rowIndex }) => {
          const rowKey = rowKeys[rowIndex] ?? transcriptRowKey(row);
          return (
            <Fragment key={rowKey}>
              {dividerRowIndexes.has(rowIndex) ? (
                <div
                  className="h-px w-full flex-none bg-[var(--line-2,var(--tutti-line-2))]"
                  data-testid="agent-transcript-turn-divider"
                  aria-hidden="true"
                />
              ) : null}
              {renderRow(
                row,
                rowIndex,
                undefined,
                rowIndex === footerRowIndex ? forkButton : null
              )}
            </Fragment>
          );
        })}
        {forkAction && footerRowIndex === null ? (
          <AgentForkThroughTurnFooter
            disabled={forkAction.disabled}
            pending={forkAction.pending}
            onFork={() => onForkThroughTurn?.(forkAction.turnId)}
          />
        ) : null}
      </Fragment>
    );
  };

  const renderTurnGroup = (group: (typeof turnGroups)[number]): JSX.Element => {
    const model = turnWorkSectionModelByKey.get(group.key) ?? null;
    if (!model) {
      return renderLegacyTurnGroup(group);
    }
    const forkAction = resolveForkThroughTurnAction(group.turnId);
    const forkButton = forkAction ? (
      <AgentForkThroughTurnButton
        disabled={forkAction.disabled}
        pending={forkAction.pending}
        onFork={() => onForkThroughTurn?.(forkAction.turnId)}
      />
    ) : null;
    const footerRowIndex = findLastMessageRowIndex(group.rows);

    return (
      <AgentTurnWorkSection
        key={group.key}
        model={model}
        sessionId={conversation.sourceDetail.session.agentSessionId}
        turnKey={group.turnId ?? group.key}
        showDivider={group.rows.some(({ rowIndex }) =>
          dividerRowIndexes.has(rowIndex)
        )}
        disclosureStore={turnDisclosureStore}
        onDisclosureMotionChange={handleDisclosureMotionChange}
        renderRow={(row, rowIndex, renderKey) =>
          renderRow(
            row,
            rowIndex,
            renderKey,
            rowIndex === footerRowIndex ? forkButton : null
          )
        }
        footer={
          forkAction && footerRowIndex === null ? (
            <AgentForkThroughTurnFooter
              disabled={forkAction.disabled}
              pending={forkAction.pending}
              onFork={() => onForkThroughTurn?.(forkAction.turnId)}
            />
          ) : null
        }
      />
    );
  };

  const renderAttachment = (
    attachment: AgentTranscriptTurnAttachment
  ): JSX.Element => (
    <AgentTranscriptAttachmentView
      key={attachment.id}
      attachment={attachment}
      onElementChange={attachmentProjection.onElementChange}
    />
  );

  if (shouldVirtualize) {
    const usesFallbackVirtualItems = virtualScrollElement === null;
    const fallbackStartIndex = Math.max(
      0,
      turnGroups.length - AGENT_TRANSCRIPT_FALLBACK_TURN_COUNT
    );
    const virtualItems = usesFallbackVirtualItems
      ? turnGroups
          .slice(-AGENT_TRANSCRIPT_FALLBACK_TURN_COUNT)
          .map((group, fallbackIndex) => ({
            index: fallbackStartIndex + fallbackIndex,
            key: group.key,
            start:
              (fallbackStartIndex + fallbackIndex) *
              AGENT_TRANSCRIPT_ESTIMATED_TURN_HEIGHT_PX
          }))
      : rowVirtualizer.getVirtualItems();
    return (
      <>
        <AgentMessageLocatorRail
          followEndMode={followEndMode}
          items={userMessageLocatorItems}
          isVisible={isVisible}
          label={labels.userMessageLocator}
          onLocate={handleLocateUserMessage}
          virtualSelectionSource={rowVirtualizer}
        />
        <div
          ref={setVirtualizerHostElement}
          className="agent-gui-transcript-virtual"
          data-agent-transcript-virtualized="true"
        >
          {virtualItems.map((virtualTurn) => {
            const group = turnGroups[virtualTurn.index];
            if (!group) {
              return null;
            }
            return (
              <div
                key={virtualTurn.key}
                ref={rowVirtualizer.measureElement}
                className="agent-gui-transcript-virtual-item"
                data-index={virtualTurn.index}
                data-agent-transcript-virtual-turn={group.key} data-multica-turn-id={group.turnId ?? ""}
                style={{
                  paddingBottom: `${
                    turnWorkSectionModelByKey.get(group.key)
                      ? AGENT_TRANSCRIPT_DISCLOSURE_TURN_GAP_PX
                      : AGENT_TRANSCRIPT_LEGACY_TURN_GAP_PX
                  }px`,
                  ...(usesFallbackVirtualItems
                    ? {
                        transform: `translateY(${virtualTurn.start}px)`
                      }
                    : {})
                }}
              >
                {renderTurnGroup(group)}
                {attachmentProjection.byGroupIndex
                  .get(virtualTurn.index)
                  ?.map(renderAttachment)}
              </div>
            );
          })}
        </div>
        {attachmentProjection.trailing.map(renderAttachment)}
      </>
    );
  }

  return (
    <>
      <AgentMessageLocatorRail
        followEndMode={followEndMode}
        items={userMessageLocatorItems}
        isVisible={isVisible}
        label={labels.userMessageLocator}
        onLocate={handleLocateUserMessage}
      />
      {turnGroups.map((group, groupIndex) => (
        <Fragment key={group.key}>
          <div style={{ display: "contents" }} data-multica-turn-id={group.turnId ?? ""}>{renderTurnGroup(group)}</div>
          {attachmentProjection.byGroupIndex
            .get(groupIndex)
            ?.map(renderAttachment)}
        </Fragment>
      ))}
      {attachmentProjection.trailing.map(renderAttachment)}
    </>
  );
}, areAgentTranscriptViewPropsEqual);
