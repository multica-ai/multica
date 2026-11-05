import { observer } from "mobx-react-lite";
import { memo, useMemo, type Ref } from "react";
import type { AgentGUIProviderSkillOption } from "../../../agent-gui/agentGuiNode/model/agentGuiNodeTypes";
import type { WorkspaceLinkAction } from "../../../contexts/workspace/presentation/renderer/actions/workspaceLinkActions";
import { useConversationServices } from "../../../conversationServices";
import type { ConversationRowsModel } from "../../../state/ConversationRowsModel";
import { TranscriptPresentationModel, TranscriptPresentationState } from "../../../state/TranscriptPresentationModel";
import type { AgentMessageMarkdownWorkspaceAppIcon } from "../../AgentMessageMarkdown";
import type { AgentConversationFollowEndMode } from "../agentConversationFollowEndController";
import type { AgentConversationParticipantPresentation } from "../contracts/agentConversationParticipantPresentation";
import type { AgentConversationVM } from "../contracts/agentConversationVM";
import type { AgentTranscriptEditRetryControl } from "./useAgentTranscriptEditRetryProjection";
import { editRetryControlsEqual } from "./useAgentTranscriptEditRetryProjection";
import type { AgentTranscriptAttachmentLocator, AgentTranscriptTurnAttachment } from "./useAgentTranscriptTurnAttachments";
import type { AgentTranscriptVirtualScrollController } from "./useAgentTranscriptVirtualizer";
import { AgentTranscriptGroup, type TranscriptContentOptions } from "./AgentTranscriptGroup";
import {
  AgentTranscriptViewport,
  AgentTranscriptViewportOptionsContext,
  type AgentTranscriptViewportOptions,
  type RenderAgentTranscriptTurn
} from "./AgentTranscriptViewport";

export type {
  AgentTranscriptAttachmentLocator,
  AgentTranscriptTurnAttachment
} from "./useAgentTranscriptTurnAttachments";
export type { AgentTranscriptVirtualScrollController } from "./useAgentTranscriptVirtualizer";
export interface AgentTranscriptViewProps {
  navigation?: import("@multica/conversation-client/services").ConversationNavigation;
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
    previous.navigation === next.navigation &&
    participantPresentationEqual(
      previous.participantPresentation,
      next.participantPresentation
    ) &&
    transcriptLabelsEqual(previous.labels, next.labels)
  );
}

const EMPTY_TURN_ATTACHMENTS: readonly AgentTranscriptTurnAttachment[] = Object.freeze([]);

type PresentationProps = Omit<AgentTranscriptViewProps, "conversation" | "rowsModel"> & {
  model: TranscriptPresentationModel;
};

function createTranscriptStateCache() {
  const live = new WeakMap<ConversationRowsModel, TranscriptPresentationState>();
  const snapshots = new Map<string, TranscriptPresentationState>();
  return (source: ConversationRowsModel | string) => {
    if (typeof source === "string") {
      let state = snapshots.get(source);
      if (!state) snapshots.set(source, state = new TranscriptPresentationState());
      return state;
    }
    let state = live.get(source);
    if (!state) live.set(source, state = new TranscriptPresentationState());
    return state;
  };
}

export const AgentTranscriptLayout = memo(
  observer(function AgentTranscriptLayout({ conversation, rowsModel, ...props }: AgentTranscriptViewProps) {
    const participantMode = props.participantPresentation?.enabled === true;
    const sessionId = conversation.sourceDetail.session.agentSessionId;
    const stateFor = useMemo(createTranscriptStateCache, []);
    const state = stateFor(rowsModel ?? `${conversation.sourceDetail.session.workspaceId}:${sessionId}`);
    const snapshot = rowsModel ? null : conversation;
    const model = useMemo(() => rowsModel
      ? TranscriptPresentationModel.fromRows(rowsModel, participantMode, state)
      : TranscriptPresentationModel.fromConversation(snapshot!, participantMode, state),
      [rowsModel, snapshot, participantMode, state]
    );
    return <AgentTranscriptPresentation {...props} model={model} />;
  }),
  areAgentTranscriptViewPropsEqual
);

export const AgentTranscriptView = observer(function AgentTranscriptView({
  agentSessionId, ...props
}: Omit<AgentTranscriptViewProps, "conversation" | "rowsModel"> & { agentSessionId: string }) {
  const runtime = useConversationServices();
  const rows = runtime.conversationRows(agentSessionId);
  const participantMode = props.participantPresentation?.enabled === true;
  const stateFor = useMemo(createTranscriptStateCache, []);
  const state = stateFor(rows);
  const model = useMemo(() => TranscriptPresentationModel.fromRows(rows, participantMode, state), [rows, participantMode, state]);
  return <AgentTranscriptPresentation {...props} model={model} navigation={runtime.navigation?.(agentSessionId)} />;
});

const AgentTranscriptPresentation = observer(function AgentTranscriptPresentation({
  model,
  navigation,
  isVisible = true,
  followEndMode,
  virtualListLayoutRevision = 0,
  virtualScrollControllerRef,
  turnAttachments = EMPTY_TURN_ATTACHMENTS,
  turnAttachmentLocatorRef,
  onTurnAttachmentVisibilityChange,
  virtualize,
  labels,
  editRetry,
  onLinkAction,
  onAuthLogin,
  onForkThroughTurn,
  availableSkills,
  workspaceAppIcons,
  showRawTimelineJson,
  participantPresentation
}: PresentationProps) {
  const viewportOptions = useMemo<AgentTranscriptViewportOptions>(() => ({
    navigation, isVisible, followEndMode, virtualListLayoutRevision, virtualScrollControllerRef,
    turnAttachments, turnAttachmentLocatorRef, onTurnAttachmentVisibilityChange
  }), [navigation, isVisible, followEndMode, virtualListLayoutRevision, virtualScrollControllerRef,
    turnAttachments, turnAttachmentLocatorRef, onTurnAttachmentVisibilityChange]);
  const contentOptions = useMemo<TranscriptContentOptions>(() => ({
    labels, editRetry, onLinkAction, onAuthLogin, onForkThroughTurn, availableSkills,
    workspaceAppIcons, showRawTimelineJson, participantPresentation
  }), [labels, editRetry, onLinkAction, onAuthLogin, onForkThroughTurn, availableSkills,
    workspaceAppIcons, showRawTimelineJson, participantPresentation]);
  const renderTurnGroup = useMemo<RenderAgentTranscriptTurn>(() => (group, onDisclosureMotionChange) => (
    <AgentTranscriptGroup model={model} groupKey={group.key} options={contentOptions} onDisclosureMotionChange={onDisclosureMotionChange} />
  ), [model, contentOptions]);
  return (
    <AgentTranscriptViewportOptionsContext.Provider value={viewportOptions}>
      <AgentTranscriptViewport model={model} virtualize={virtualize} userMessageLocatorLabel={labels.userMessageLocator} renderTurnGroup={renderTurnGroup} />
    </AgentTranscriptViewportOptionsContext.Provider>
  );
});
