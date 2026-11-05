import { Slot } from "radix-ui";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { providerForkBindingAllowsAttempt } from "@tutti-os/agent-activity-core";
import { observer } from "mobx-react-lite";
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
import type { AgentGUIProviderSkillOption } from "../../../agent-gui/agentGuiNode/model/agentGuiNodeTypes";
import type { WorkspaceLinkAction } from "../../../contexts/workspace/presentation/renderer/actions/workspaceLinkActions";
import { useConversationServices } from "../../../conversationServices";
import type { ConversationRowsModel } from "../../../state/ConversationRowsModel";
import type { AgentMessageMarkdownWorkspaceAppIcon } from "../../AgentMessageMarkdown";
import type { AgentConversationFollowEndMode } from "../agentConversationFollowEndController";
import { useAgentObservationGap } from "../AgentObservationGapContext";
import type { AgentConversationParticipantPresentation } from "../contracts/agentConversationParticipantPresentation";
import type { AgentConversationVM } from "../contracts/agentConversationVM";
import {
  AgentForkThroughTurnButton,
  AgentForkThroughTurnFooter
} from "./AgentForkThroughTurnButton";
import {
  AgentMessageLocatorRail,
  findMessageLocatorScrollParent,
  scrollTranscriptRowIntoView
} from "./AgentMessageLocatorRail";
import { assessAgentTranscriptComplexity } from "./agentTranscriptComplexity";
import {
  AgentTranscriptItemContent,
  AgentTranscriptItemView
} from "./AgentTranscriptItemView";
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
import { useAgentTurnDisclosureStore } from "./AgentTurnDisclosureContext";
import { AgentTurnWorkSection } from "./AgentTurnWorkSection";
import {
  buildAgentTurnWorkSectionModel,
  findParticipantHeaderRenderKeys
} from "./agentTurnWorkSectionModel";
import {
  editRetryControlsEqual,
  useAgentTranscriptEditRetryProjection,
  type AgentTranscriptEditRetryControl
} from "./useAgentTranscriptEditRetryProjection";
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
const AgentTranscriptBoundItem = observer(AgentTranscriptBoundItemRender);

const AGENT_TRANSCRIPT_DISCLOSURE_TURN_GAP_PX = 24;
const AGENT_TRANSCRIPT_LEGACY_TURN_GAP_PX = 12;
const AGENT_TRANSCRIPT_FALLBACK_TURN_COUNT = 3;

export type {
  AgentTranscriptAttachmentLocator,
  AgentTranscriptTurnAttachment
} from "./useAgentTranscriptTurnAttachments";
export type { AgentTranscriptVirtualScrollController } from "./useAgentTranscriptVirtualizer";
export interface AgentTranscriptViewProps {
  rowsModel?: ConversationRowsModel;
  virtualize?: boolean;
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

export const AgentTranscriptLayout = memo(
  observer(function AgentTranscriptLayout({
    conversation,
    rowsModel,
    virtualize,
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
    const participantTurnProjection =
      transcriptRowSet.participantTurnProjection;
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
    const handleToolGroupExpandedChangeCallback = usePersistCallback(
      handleToolGroupExpandedChange
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
      () =>
        virtualize ??
        assessAgentTranscriptComplexity(turnGroups).shouldVirtualize,
      [turnGroups, virtualize]
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
    const handleLocateUserMessageCallback = usePersistCallback(
      handleLocateUserMessage
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

    const renderRow = useMemo(
      () =>
        (
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
            row.turnId === null
              ? null
              : (canonicalTurnById.get(row.turnId) ?? null);
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
              <AgentTranscriptBoundItem
                workspaceRoot={workspaceRoot}
                basePath={basePath}
                row={row}
                agentSessionId={agentSessionId}
                rowsModel={rowsModel}
                editRetry={
                  row.id === editableUserMessageRowId
                    ? scopedEditRetry
                    : undefined
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
                toolGroupExpansionKey={
                  row.kind === "tool-group" ? rowKey : undefined
                }
                onToolGroupExpandedChange={
                  handleToolGroupExpandedChangeCallback
                }
                footerAction={footerAction}
              />
            </div>
          );
        },
      [
        displayRows,
        rowKeys,
        enteringRowKeys,
        participantHeaderRenderKeys,
        participantHeadersEnabled,
        conversation,
        canonicalTurnById,
        workspaceRoot,
        basePath,
        agentSessionId,
        rowsModel,
        editableUserMessageRowId,
        scopedEditRetry,
        labels,
        onLinkAction,
        onAuthLogin,
        provider,
        availableSkills,
        workspaceAppIcons,
        showRawTimelineJson,
        participantPresentation,
        activeTurnId,
        observationGap,
        expandedToolRows,
        handleToolGroupExpandedChangeCallback
      ]
    );
    const renderRowCallback = renderRow;

    const resolveForkThroughTurnAction = (
      turnId: string | null
    ): { turnId: string } | null => {
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
      return {
        turnId
      };
    };

    const renderLegacyTurnGroup = (
      group: (typeof turnGroups)[number]
    ): JSX.Element => {
      const forkAction = resolveForkThroughTurnAction(group.turnId);
      const forkButton = forkAction ? (
        <AgentForkThroughTurnButtonBinding1
          conversation={conversation}
          forkAction={forkAction}
          onForkThroughTurn={onForkThroughTurn}
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
            <AgentForkThroughTurnFooterBinding2
              conversation={conversation}
              forkAction={forkAction}
              onForkThroughTurn={onForkThroughTurn}
            />
          ) : null}
        </Fragment>
      );
    };

    const renderTurnGroup = useMemo(
      () =>
        (group: (typeof turnGroups)[number]): JSX.Element => {
          const model = turnWorkSectionModelByKey.get(group.key) ?? null;
          if (!model) {
            return renderLegacyTurnGroup(group);
          }
          const forkAction = resolveForkThroughTurnAction(group.turnId);
          const forkButton = forkAction ? (
            <AgentForkThroughTurnButtonBinding3
              conversation={conversation}
              forkAction={forkAction}
              onForkThroughTurn={onForkThroughTurn}
            />
          ) : null;
          const footerRowIndex = findLastMessageRowIndex(group.rows);

          return (
            <AgentTurnWorkSectionBinding1
              key={group.key}
              group={group}
              model={model}
              conversation={conversation}
              dividerRowIndexes={dividerRowIndexes}
              turnDisclosureStore={turnDisclosureStore}
              handleDisclosureMotionChange={handleDisclosureMotionChange}
              renderRow={renderRowCallback}
              footerRowIndex={footerRowIndex}
              forkButton={forkButton}
              forkAction={forkAction}
              onForkThroughTurn={onForkThroughTurn}
            />
          );
        },
      [
        turnWorkSectionModelByKey,
        renderLegacyTurnGroup,
        resolveForkThroughTurnAction,
        conversation,
        onForkThroughTurn,
        dividerRowIndexes,
        turnDisclosureStore,
        handleDisclosureMotionChange,
        renderRowCallback
      ]
    );
    const renderTurnGroupCallback = renderTurnGroup;

    const renderAttachment = useMemo(
      () =>
        (attachment: AgentTranscriptTurnAttachment): JSX.Element => (
          <AgentTranscriptAttachmentView
            key={attachment.id}
            attachment={attachment}
            onElementChange={attachmentProjection.onElementChange}
          />
        ),
      [attachmentProjection]
    );
    const renderAttachmentCallback = renderAttachment;

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
            onLocate={handleLocateUserMessageCallback}
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
                <DivBinding5
                  key={virtualTurn.key}
                  virtualTurn={virtualTurn}
                  rowVirtualizer={rowVirtualizer}
                  group={group}
                  turnWorkSectionModelByKey={turnWorkSectionModelByKey}
                  usesFallbackVirtualItems={usesFallbackVirtualItems}
                  renderTurnGroup={renderTurnGroupCallback}
                  attachmentProjection={attachmentProjection}
                  renderAttachment={renderAttachmentCallback}
                />
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
          onLocate={handleLocateUserMessageCallback}
        />
        {turnGroups.map((group, groupIndex) => (
          <Fragment key={group.key}>
            <DivBinding6
              group={group}
              renderTurnGroup={renderTurnGroupCallback}
            />
            {attachmentProjection.byGroupIndex
              .get(groupIndex)
              ?.map(renderAttachment)}
          </Fragment>
        ))}
        {attachmentProjection.trailing.map(renderAttachment)}
      </>
    );
  }),
  areAgentTranscriptViewPropsEqual
);

function AgentTranscriptBoundItemRender({
  row,
  rowsModel,
  agentSessionId,
  ...props
}: import("./AgentTranscriptItemView").AgentTranscriptItemViewProps & {
  rowsModel?: ConversationRowsModel;
  agentSessionId: string;
}) {
  return rowsModel ? (
    <AgentTranscriptItemView
      {...props}
      rowsModel={rowsModel}
      agentSessionId={agentSessionId}
      rowKey={transcriptRowKey(row)}
      descriptor={row}
    />
  ) : (
    <AgentTranscriptItemContent {...props} row={row} />
  );
}

export const AgentTranscriptView = observer(function AgentTranscriptView({
  agentSessionId,
  ...props
}: Omit<AgentTranscriptViewProps, "conversation" | "rowsModel"> & {
  agentSessionId: string;
}) {
  const runtime = useConversationServices();
  const rows = runtime.conversationRows(agentSessionId);
  const conversation = rows.structure;
  // Locator text is user-owned content and can change without a row being added.
  void rows.locatorItems;
  return conversation ? (
    <AgentTranscriptLayout
      {...props}
      conversation={conversation}
      rowsModel={rows}
      virtualize={rows.shouldVirtualize}
    />
  ) : null;
});

const AgentForkThroughTurnButtonBinding1 = observer(
  function AgentForkThroughTurnButtonBinding1({
    conversation,
    forkAction,
    onForkThroughTurn
  }: AgentForkThroughTurnButtonBinding1Props) {
    const onForkProp1 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof AgentForkThroughTurnButton>["onFork"]
      >
    >(() => onForkThroughTurn?.(forkAction.turnId));
    return (
      <AgentForkThroughTurnButton
        agentSessionId={conversation.sourceDetail.session.agentSessionId}
        turnId={forkAction.turnId}
        onFork={onForkProp1}
      />
    );
  }
);

const AgentForkThroughTurnFooterBinding2 = observer(
  function AgentForkThroughTurnFooterBinding2({
    conversation,
    forkAction,
    onForkThroughTurn
  }: AgentForkThroughTurnButtonBinding1Props) {
    const onForkProp2 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof AgentForkThroughTurnFooter>["onFork"]
      >
    >(() => onForkThroughTurn?.(forkAction.turnId));
    return (
      <AgentForkThroughTurnFooter
        agentSessionId={conversation.sourceDetail.session.agentSessionId}
        turnId={forkAction.turnId}
        onFork={onForkProp2}
      />
    );
  }
);

const AgentForkThroughTurnButtonBinding3 = observer(
  function AgentForkThroughTurnButtonBinding3({
    conversation,
    forkAction,
    onForkThroughTurn
  }: AgentForkThroughTurnButtonBinding1Props) {
    const onForkProp3 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof AgentForkThroughTurnButton>["onFork"]
      >
    >(() => onForkThroughTurn?.(forkAction.turnId));
    return (
      <AgentForkThroughTurnButton
        agentSessionId={conversation.sourceDetail.session.agentSessionId}
        turnId={forkAction.turnId}
        onFork={onForkProp3}
      />
    );
  }
);

const AgentForkThroughTurnFooterBinding4 = observer(
  function AgentForkThroughTurnFooterBinding4({
    conversation,
    forkAction,
    onForkThroughTurn
  }: AgentForkThroughTurnFooterBinding4Props) {
    const onForkProp4 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof AgentForkThroughTurnFooter>["onFork"]
      >
    >(() => onForkThroughTurn?.(forkAction.turnId));
    return (
      <AgentForkThroughTurnFooter
        agentSessionId={conversation.sourceDetail.session.agentSessionId}
        turnId={forkAction.turnId}
        onFork={onForkProp4}
      />
    );
  }
);

const DivBinding5 = observer(function DivBinding5({
  virtualTurn,
  rowVirtualizer,
  group,
  turnWorkSectionModelByKey,
  usesFallbackVirtualItems,
  renderTurnGroup,
  attachmentProjection,
  renderAttachment,
  ...slotProps
}: DivBinding5Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof DivBinding5Props>) {
  const styleProp5Value1 = `${
    turnWorkSectionModelByKey.get(group.key)
      ? AGENT_TRANSCRIPT_DISCLOSURE_TURN_GAP_PX
      : AGENT_TRANSCRIPT_LEGACY_TURN_GAP_PX
  }px`;
  const virtualStart = virtualTurn.start;
  const styleProp5Value2 = useMemo(
    () =>
      usesFallbackVirtualItems
        ? { transform: `translateY(${virtualStart}px)` }
        : {},
    [usesFallbackVirtualItems, virtualStart]
  );
  const styleProp5 = useMemo<NonNullable<React.ComponentProps<"div">["style"]>>(
    () => ({ paddingBottom: styleProp5Value1, ...styleProp5Value2 }),
    [styleProp5Value1, styleProp5Value2]
  );
  return (
    <Slot.Root {...slotProps}>
      <div
        key={virtualTurn.key}
        ref={rowVirtualizer.measureElement}
        className="agent-gui-transcript-virtual-item"
        data-index={virtualTurn.index}
        data-agent-transcript-virtual-turn={group.key}
        data-multica-turn-id={group.turnId ?? ""}
        style={styleProp5}
      >
        {renderTurnGroup(group)}
        {attachmentProjection.byGroupIndex
          .get(virtualTurn.index)
          ?.map(renderAttachment)}
      </div>
    </Slot.Root>
  );
});

const DivBinding6 = observer(function DivBinding6({
  group,
  renderTurnGroup,
  ...slotProps
}: {
  group: import("./agentTranscriptModel").AgentTranscriptTurnGroup;
  renderTurnGroup: (
    group: import("./agentTranscriptModel").AgentTranscriptTurnGroup
  ) => JSX.Element;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    group: import("./agentTranscriptModel").AgentTranscriptTurnGroup;
    renderTurnGroup: (
      group: import("./agentTranscriptModel").AgentTranscriptTurnGroup
    ) => JSX.Element;
  }
>) {
  const styleProp6 = useMemo<NonNullable<React.ComponentProps<"div">["style"]>>(
    () => ({ display: "contents" }),
    []
  );
  return (
    <Slot.Root {...slotProps}>
      <div style={styleProp6} data-multica-turn-id={group.turnId ?? ""}>
        {renderTurnGroup(group)}
      </div>
    </Slot.Root>
  );
});

const AgentTurnWorkSectionBinding1 = observer(
  function AgentTurnWorkSectionBinding1({
    group,
    model,
    conversation,
    dividerRowIndexes,
    turnDisclosureStore,
    handleDisclosureMotionChange,
    renderRow,
    footerRowIndex,
    forkButton,
    forkAction,
    onForkThroughTurn
  }: AgentTurnWorkSectionBinding1Props) {
    const renderRowProp7 = useMemo<
      NonNullable<
        React.ComponentProps<typeof AgentTurnWorkSection>["renderRow"]
      >
    >(
      () => (row, rowIndex, renderKey) =>
        renderRow(
          row,
          rowIndex,
          renderKey,
          rowIndex === footerRowIndex ? forkButton : null
        ),
      [renderRow, footerRowIndex, forkButton]
    );
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
        renderRow={renderRowProp7}
        footer={
          forkAction && footerRowIndex === null ? (
            <AgentForkThroughTurnFooterBinding4
              conversation={conversation}
              forkAction={forkAction}
              onForkThroughTurn={onForkThroughTurn}
            />
          ) : null
        }
      />
    );
  }
);

type AgentForkThroughTurnButtonBinding1Props = {
  conversation: NonNullable<AgentTranscriptViewProps>["conversation"];
  forkAction: { turnId: string };
  onForkThroughTurn: NonNullable<AgentTranscriptViewProps>["onForkThroughTurn"];
};

type AgentForkThroughTurnFooterBinding2Props = {
  conversation: NonNullable<AgentTranscriptViewProps>["conversation"];
  forkAction: { turnId: string };
  onForkThroughTurn: NonNullable<AgentTranscriptViewProps>["onForkThroughTurn"];
};

type AgentForkThroughTurnButtonBinding3Props = {
  conversation: NonNullable<AgentTranscriptViewProps>["conversation"];
  forkAction: { turnId: string };
  onForkThroughTurn: NonNullable<AgentTranscriptViewProps>["onForkThroughTurn"];
};

type AgentForkThroughTurnFooterBinding4Props = {
  conversation: AgentConversationVM;
  forkAction: { turnId: string };
  onForkThroughTurn: ((turnId: string) => void) | undefined;
};

type DivBinding5Props = {
  virtualTurn:
    | { index: number; key: string; start: number }
    | import("@tanstack/react-virtual").VirtualItem;
  rowVirtualizer: import("@tanstack/react-virtual").ReactVirtualizer<
    HTMLElement,
    Element
  >;
  group: import("./agentTranscriptModel").AgentTranscriptTurnGroup;
  turnWorkSectionModelByKey: Map<
    string,
    import("./agentTurnWorkSectionModel").AgentTurnWorkSectionModel | null
  >;
  usesFallbackVirtualItems: boolean;
  renderTurnGroup: (
    group: import("./agentTranscriptModel").AgentTranscriptTurnGroup
  ) => JSX.Element;
  attachmentProjection: {
    byGroupIndex: ReadonlyMap<number, readonly AgentTranscriptTurnAttachment[]>;
    onElementChange: (
      attachmentId: string,
      element: HTMLElement | null
    ) => void;
    trailing: readonly AgentTranscriptTurnAttachment[];
  };
  renderAttachment: (attachment: AgentTranscriptTurnAttachment) => JSX.Element;
};

type AgentTurnWorkSectionBinding1Props = {
  group: import("./agentTranscriptModel").AgentTranscriptTurnGroup;
  model: import("./agentTurnWorkSectionModel").AgentTurnWorkSectionModel;
  conversation: NonNullable<AgentTranscriptViewProps>["conversation"];
  dividerRowIndexes: ReadonlySet<number>;
  turnDisclosureStore: import("./AgentTurnDisclosureContext").AgentTurnDisclosureStore;
  handleDisclosureMotionChange: (
    turnKey: string,
    active: boolean,
    anchorElement?: HTMLElement | null
  ) => void;
  renderRow: (
    row: AgentConversationVM["rows"][number],
    rowIndex: number,
    renderKey?: string,
    footerAction?: ReactNode
  ) => JSX.Element;
  footerRowIndex: number | null;
  forkButton: import("react").JSX.Element | null;
  forkAction: { turnId: string } | null;
  onForkThroughTurn: NonNullable<AgentTranscriptViewProps>["onForkThroughTurn"];
};
