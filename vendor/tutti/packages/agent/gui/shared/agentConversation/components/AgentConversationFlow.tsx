import { observer } from "mobx-react-lite";
import { memo, type JSX, type ReactNode, type Ref } from "react";
import type { AgentGUIProviderSkillOption } from "../../../agent-gui/agentGuiNode/model/agentGuiNodeTypes";
import type { WorkspaceLinkAction } from "../../../contexts/workspace/presentation/renderer/actions/workspaceLinkActions";
import { useConversationServices } from "../../../conversationServices";
import type { AgentMessageMarkdownWorkspaceAppIcon } from "../../AgentMessageMarkdown";
import {
  AgentTargetPresentationProvider,
  type AgentMessageMarkdownAgentTarget
} from "../../AgentTargetPresentationContext";
import type { AgentConversationFollowEndMode } from "../agentConversationFollowEndController";
import type { AgentConversationParticipantPresentation } from "../contracts/agentConversationParticipantPresentation";
import type { AgentConversationVM } from "../contracts/agentConversationVM";
import { AgentConversationClockProvider } from "./AgentConversationClock";
import { AgentTranscriptSkeleton } from "./AgentTranscriptSkeleton";
import {
  AgentTranscriptLayout,
  AgentTranscriptView,
  type AgentTranscriptAttachmentLocator,
  type AgentTranscriptTurnAttachment,
  type AgentTranscriptVirtualScrollController
} from "./AgentTranscriptView";
import { AgentTurnDisclosureProvider } from "./AgentTurnDisclosureContext";
import type { AgentTranscriptEditRetryControl } from "./useAgentTranscriptEditRetryProjection";
const BoundTranscript = observer(BoundTranscriptRender);

export interface AgentConversationFlowProps {
  conversation: AgentConversationVM | null;
  agentSessionId?: string;
  editRetry?: AgentTranscriptEditRetryControl;
  turnAttachments?: readonly AgentTranscriptTurnAttachment[];
  turnAttachmentLocatorRef?: Ref<AgentTranscriptAttachmentLocator>;
  onTurnAttachmentVisibilityChange?: (
    attachmentId: string,
    visible: boolean
  ) => void;
  isLoading: boolean;
  isVisible?: boolean;
  loadingLabel: string;
  loadingTestId?: string;
  empty: ReactNode;
  onLinkAction?: (action: WorkspaceLinkAction) => void;
  onAuthLogin?: (provider?: string | null) => void;
  onForkThroughTurn?: (turnId: string) => void;
  availableSkills?: readonly AgentGUIProviderSkillOption[];
  agentTargets?: readonly AgentMessageMarkdownAgentTarget[];
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

export const AgentConversationFlowContent = observer(
  function AgentConversationFlowContent({
    conversation,
    agentSessionId,
    editRetry,
    turnAttachments,
    turnAttachmentLocatorRef,
    onTurnAttachmentVisibilityChange,
    isLoading,
    isVisible = true,
    loadingLabel,
    loadingTestId,
    empty,
    onLinkAction,
    onAuthLogin,
    onForkThroughTurn,
    availableSkills,
    agentTargets,
    workspaceAppIcons,
    showRawTimelineJson = false,
    participantPresentation,
    followEndMode,

    virtualListLayoutRevision,
    virtualScrollControllerRef,
    labels
  }: AgentConversationFlowProps): JSX.Element {
    "use memo";

    const Transcript = BoundTranscript;
    let content: JSX.Element;
    if (isLoading) {
      content = (
        <AgentTranscriptSkeleton label={loadingLabel} testId={loadingTestId} />
      );
    } else if (
      !conversation ||
      (conversation.rows.length === 0 && !turnAttachments?.length)
    ) {
      content = <>{empty}</>;
    } else {
      content = (
        <Transcript
          conversation={conversation}
          agentSessionId={agentSessionId}
          isVisible={isVisible}
          editRetry={editRetry}
          turnAttachments={turnAttachments}
          turnAttachmentLocatorRef={turnAttachmentLocatorRef}
          onTurnAttachmentVisibilityChange={onTurnAttachmentVisibilityChange}
          onLinkAction={onLinkAction}
          onAuthLogin={onAuthLogin}
          onForkThroughTurn={onForkThroughTurn}
          availableSkills={availableSkills}
          workspaceAppIcons={workspaceAppIcons}
          labels={labels}
          showRawTimelineJson={showRawTimelineJson}
          followEndMode={followEndMode}
          participantPresentation={participantPresentation}
          virtualListLayoutRevision={virtualListLayoutRevision}
          virtualScrollControllerRef={virtualScrollControllerRef}
        />
      );
    }

    const disclosedContent = (
      <AgentTurnDisclosureProvider>{content}</AgentTurnDisclosureProvider>
    );
    const presentedContent =
      agentTargets === undefined ? (
        disclosedContent
      ) : (
        <AgentTargetPresentationProvider agentTargets={agentTargets}>
          {disclosedContent}
        </AgentTargetPresentationProvider>
      );
    return (
      <AgentConversationClockProvider isVisible={isVisible}>
        {presentedContent}
      </AgentConversationClockProvider>
    );
  }
);

function BoundTranscriptRender({
  agentSessionId,
  conversation,
  ...props
}: import("./AgentTranscriptView").AgentTranscriptViewProps & {
  agentSessionId?: string;
}) {
  return agentSessionId ? (
    <AgentTranscriptView {...props} agentSessionId={agentSessionId} />
  ) : (
    <AgentTranscriptLayout {...props} conversation={conversation} />
  );
}

export const AgentConversationFlow = observer(function AgentConversationFlow({
  agentSessionId,
  ...props
}: Omit<AgentConversationFlowProps, "conversation" | "agentSessionId"> & {
  agentSessionId: string | null;
}) {
  const runtime = useConversationServices();
  const conversation = agentSessionId
    ? runtime.conversationRows(agentSessionId).structure
    : null;
  return (
    <AgentConversationFlowContent
      {...props}
      conversation={conversation}
      agentSessionId={agentSessionId ?? undefined}
    />
  );
});
