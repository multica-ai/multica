import { memo, type Ref } from "react";
import type { WorkspaceLinkAction } from "../../../actions/workspaceLinkActions";
import type { AgentMessageMarkdownWorkspaceAppIcon } from "../../../shared/AgentMessageMarkdown";
import type { AgentConversationFollowEndMode } from "../../../shared/agentConversation/agentConversationFollowEndController";
import { AgentConversationFlow } from "../../../shared/agentConversation/components/AgentConversationFlow";
import type {
  AgentTranscriptAttachmentLocator,
  AgentTranscriptTurnAttachment,
  AgentTranscriptVirtualScrollController
} from "../../../shared/agentConversation/components/AgentTranscriptView";
import type { AgentTranscriptEditRetryControl } from "../../../shared/agentConversation/components/useAgentTranscriptEditRetryProjection";
import type { AgentGUIProviderSkillOption } from "../model/agentGuiNodeTypes";
import { userScrollBehavior } from "./agentGUIDetailScrollHelpers";
import { observer } from "mobx-react-lite";

const EMPTY_WORKSPACE_APP_ICONS: readonly AgentMessageMarkdownWorkspaceAppIcon[] =
  [];

interface AgentGUIConversationTimelinePaneProps {
  agentSessionId: string | null;
  editRetry?: AgentTranscriptEditRetryControl;
  turnAttachments?: readonly AgentTranscriptTurnAttachment[];
  turnAttachmentLocatorRef?: Ref<AgentTranscriptAttachmentLocator>;
  onTurnAttachmentVisibilityChange?: (
    attachmentId: string,
    visible: boolean
  ) => void;
  isLoading: boolean;
  isLoadingOlderMessages: boolean;
  isVisible: boolean;
  followEndMode: AgentConversationFollowEndMode;

  virtualScrollControllerRef: Ref<AgentTranscriptVirtualScrollController>;
  loadingLabel: string;
  empty: React.JSX.Element;
  onLinkAction?: (action: WorkspaceLinkAction) => void;
  onAuthLogin?: (provider?: string | null) => void;
  onForkThroughTurn?: (turnId: string) => void;
  availableSkills?: readonly AgentGUIProviderSkillOption[];
  workspaceAppIcons?: readonly AgentMessageMarkdownWorkspaceAppIcon[];
  labels: {
    thinkingLabel: string;
    toolCallsLabel: (count: number) => string;
    processing: string;
    turnSummary: string;
    userMessageLocator: string;
  };
}

export const AgentGUIConversationTimelinePane = observer(
  function AgentGUIConversationTimelinePane({
    agentSessionId,
    editRetry,
    turnAttachments,
    turnAttachmentLocatorRef,
    onTurnAttachmentVisibilityChange,
    isLoading,
    isLoadingOlderMessages,
    isVisible,
    followEndMode,

    virtualScrollControllerRef,
    loadingLabel,
    empty,
    onLinkAction,
    onAuthLogin,
    onForkThroughTurn,
    availableSkills,
    workspaceAppIcons = EMPTY_WORKSPACE_APP_ICONS,
    labels
  }: AgentGUIConversationTimelinePaneProps): React.JSX.Element {
    "use memo";

    return (
      <>
        {isLoadingOlderMessages && !isLoading ? (
          <div
            className="mx-auto flex h-8 items-center justify-center text-[12px] text-[var(--text-secondary)]"
            data-testid="agent-gui-older-messages-loading"
            role="status"
          >
            <span className="tsh-inline-loading-ellipsis">{loadingLabel}</span>
          </div>
        ) : null}
        <AgentConversationFlow
          agentSessionId={agentSessionId}
          followEndMode={followEndMode}
          editRetry={editRetry}
          turnAttachments={turnAttachments}
          turnAttachmentLocatorRef={turnAttachmentLocatorRef}
          onTurnAttachmentVisibilityChange={onTurnAttachmentVisibilityChange}
          isLoading={isLoading}
          isVisible={isVisible}
          loadingLabel={loadingLabel}
          empty={empty}
          onLinkAction={onLinkAction}
          onAuthLogin={onAuthLogin}
          onForkThroughTurn={onForkThroughTurn}
          availableSkills={availableSkills}
          workspaceAppIcons={workspaceAppIcons}
          labels={labels}
          virtualListLayoutRevision={isLoadingOlderMessages ? 1 : 0}
          virtualScrollControllerRef={virtualScrollControllerRef}
        />
      </>
    );
  }
);

export function setTimelineScrollTopInstantly(
  element: HTMLElement,
  top: number
): void {
  // Timeline anchoring runs for high-frequency streaming updates. Smooth scrolling
  // queues animations that can overlap with incoming layout commits and make the transcript flicker.
  element.scrollTop = top;
}

export function setTimelineScrollTopWithUserTransition(
  element: HTMLElement,
  top: number
): void {
  if (typeof element.scrollTo === "function") {
    element.scrollTo({
      top,
      behavior: userScrollBehavior()
    });
    return;
  }
  element.scrollTop = top;
}
