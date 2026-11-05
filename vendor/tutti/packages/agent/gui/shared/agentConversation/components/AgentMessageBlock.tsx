import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { useMemo } from "react";
import { InvokedAgentReply } from "@multica/tutti-agent-gui-host/invoked-agent-reply";
import { toast } from "@tutti-os/ui-system";
import { observer } from "mobx-react-lite";
import { Fragment, useCallback, type JSX, type ReactNode } from "react";
import styles from "../../../agent-gui/agentGuiNode/AgentGUIConversation.styles";
import { parseMentionItemFromHref } from "../../../agent-gui/agentGuiNode/agentRichText/agentFileMentionExtension";
import { notifyComposerFileMentionBlocked } from "../../../agent-gui/agentGuiNode/composer/resolveComposerFileMentionLinkAction";
import type { AgentGUIProviderSkillOption } from "../../../agent-gui/agentGuiNode/model/agentGuiNodeTypes";
import { useOptionalAgentHostApi } from "../../../agentActivityHost";
import {
  AGENT_EXTERNAL_LINK_ACTION_SOURCE,
  type WorkspaceLinkAction
} from "../../../contexts/workspace/presentation/renderer/actions/workspaceLinkActions";
import {
  AgentMessageMarkdown,
  type AgentMessageMarkdownWorkspaceAppIcon
} from "../../AgentMessageMarkdown";
import { AgentRichTextReadonly } from "../../AgentRichTextReadonly";
import { resolveAgentConversationLinkAction } from "../actions/agentConversationLinkActions";
import type { AgentConversationParticipantPresentation } from "../contracts/agentConversationParticipantPresentation";
import type {
  AgentMessageContentVM,
  AgentMessageRowVM
} from "../contracts/agentMessageRowVM";
import { AgentCollaborationRow } from "./AgentCollaborationRow";
import { AgentConversationParticipantHeader } from "./AgentConversationParticipant";
import { AgentCopyableMessageGroup } from "./AgentMessageActions";
import { AgentUserImageGrid } from "./AgentMessageImages";
import { AgentPlanCard } from "./AgentPlanCard";
import { AgentSelectedTextChip } from "./AgentSelectedTextChip";
import { AgentSystemNoticeMessage } from "./AgentSystemNoticeMessage";
import { AgentThinkingDisclosure } from "./AgentThinkingDisclosure";
import { AgentToolGroupRow } from "./AgentToolGroupRow";
import { AgentTuttiModeCheckpointWakeCard } from "./AgentTuttiModeCheckpointWakeCard";
import { AgentTuttiPlanIssueLinkCard } from "./AgentTuttiPlanIssueLinkCard";
import {
  AgentUserMessageEditor,
  type AgentUserMessageEditRetryControl
} from "./AgentUserMessageEditRetry";
import {
  AgentVisibleErrorMessage,
  recoverVisibleErrorFromMessage
} from "./AgentVisibleErrorMessage";
import { RawTimelineJsonDisclosure } from "./RawTimelineJsonDisclosure";
import { useAgentUserMessageEditRetry } from "./useAgentUserMessageEditRetry";
const AgentPlanCardMessage = observer(AgentPlanCardMessageRender);

const DEFAULT_TOOL_CALLS_LABEL = (count: number): string =>
  `${count} tool calls`;
interface AgentMessageBlockProps {
  workspaceRoot: string | null;
  basePath: string;
  row: AgentMessageRowVM;
  editRetry?: AgentUserMessageEditRetryControl;
  onLinkAction?: (action: WorkspaceLinkAction) => void;
  thinkingLabel: string;
  toolCallsLabel?: (count: number) => string;
  onAuthLogin?: (provider?: string | null) => void;
  // Routes a recovered environment-error CTA to the conversation provider.
  provider?: string | null;
  availableSkills?: readonly AgentGUIProviderSkillOption[];
  workspaceAppIcons?: readonly AgentMessageMarkdownWorkspaceAppIcon[];
  showRawTimelineJson?: boolean;
  rawTimelineJsonLabel?: string;
  participantPresentation?: AgentConversationParticipantPresentation;
  showParticipantHeader?: boolean;
  isActiveTurn?: boolean;
  footerAction?: ReactNode;
}

export const AgentMessageBlock = observer(function AgentMessageBlock({
  workspaceRoot,
  basePath,
  row,
  editRetry,
  onLinkAction,
  thinkingLabel,
  toolCallsLabel = DEFAULT_TOOL_CALLS_LABEL,
  onAuthLogin,
  provider,
  availableSkills,
  workspaceAppIcons,
  showRawTimelineJson = false,
  rawTimelineJsonLabel = "",
  participantPresentation,
  showParticipantHeader = true,
  isActiveTurn = false,
  footerAction
}: AgentMessageBlockProps): JSX.Element {
  "use memo";
  const agentHostApi = useOptionalAgentHostApi();
  const isUser = row.speaker === "user";
  const {
    canEdit,
    editableTargetMessageId,
    editPending,
    editState,
    handleCancelEdit,
    handleEditorKeyDown,
    handleEditTextChange,
    handleStartEdit,
    handleSubmitEdit,
    isEditing
  } = useAgentUserMessageEditRetry({ editRetry, isUser, row });
  const handleLinkClick = useCallback(
    (href: string): void => {
      const action = resolveAgentConversationLinkAction({
        workspaceRoot,
        basePath,
        href,
        source: "agent-markdown"
      });
      if (action) {
        onLinkAction?.(action);
        return;
      }
      // Sent transcripts can retain draft-only file hrefs; never fail silently.
      const mention = parseMentionItemFromHref({ name: "", href });
      if (
        mention?.kind === "file" &&
        mention.attachmentId &&
        !mention.path.trim()
      ) {
        notifyComposerFileMentionBlocked({
          reason: "unavailable",
          showError: (message) => {
            if (agentHostApi?.toast?.error) {
              agentHostApi.toast.error(message);
              return;
            }
            toast.error(message);
          },
          showInfo: (message) => {
            if (agentHostApi?.toast?.info) {
              agentHostApi.toast.info(message);
              return;
            }
            toast.error(message);
          }
        });
      }
    },
    [agentHostApi, basePath, onLinkAction, workspaceRoot]
  );
  const handleLinkClickCallback = usePersistCallback(handleLinkClick);
  const handleExternalLinkClick = useCallback(
    (href: string): void => {
      const action = resolveAgentConversationLinkAction({
        workspaceRoot,
        basePath,
        href,
        source: AGENT_EXTERNAL_LINK_ACTION_SOURCE
      });
      if (action) {
        onLinkAction?.(action);
      }
    },
    [basePath, onLinkAction, workspaceRoot]
  );
  const handleExternalLinkClickCallback = usePersistCallback(
    handleExternalLinkClick
  );
  const handleCopyMessageText = useCallback(
    async (text: string): Promise<boolean> => {
      if (!text.trim()) {
        return false;
      }

      try {
        const hostWriteText = agentHostApi?.clipboard?.writeText;
        if (typeof hostWriteText === "function") {
          await hostWriteText(text);
          return true;
        }
        if (
          typeof navigator !== "undefined" &&
          typeof navigator.clipboard?.writeText === "function"
        ) {
          await navigator.clipboard.writeText(text);
          return true;
        }
      } catch {
        return false;
      }
      return false;
    },
    [agentHostApi]
  );
  const handleCopyMessageTextCallback = usePersistCallback(
    handleCopyMessageText
  );
  const thinkingContent = !isUser
    ? row.thinking.map((thinking) => (
        <AgentThinkingDisclosure
          key={thinking.id}
          thinking={thinking}
          label={thinkingLabel}
          onLinkClick={handleLinkClickCallback}
          showRawTimelineJson={showRawTimelineJson}
          rawTimelineJsonLabel={rawTimelineJsonLabel}
        />
      ))
    : null;

  const leadingToolContent =
    !isUser && row.leadingToolRows && row.leadingToolRows.length > 0
      ? row.leadingToolRows.map((toolRow) => (
          <AgentToolGroupRow
            key={toolRow.id}
            row={toolRow}
            label={toolCallsLabel}
            thinkingLabel={thinkingLabel}
            onLinkClick={handleLinkClickCallback}
            showRawTimelineJson={showRawTimelineJson}
            rawTimelineJsonLabel={rawTimelineJsonLabel}
          />
        ))
      : null;

  const handleStartEditCallback = usePersistCallback(handleStartEdit);
  const editActionAvailable = !isEditing && Boolean(editRetry);
  const messageEditLabel = editRetry?.labels.edit;
  const messageEditAction = useMemo(
    () =>
      editActionAvailable
        ? {
            disabled: editPending,
            label: messageEditLabel!,
            onClick: handleStartEditCallback
          }
        : null,
    [
      editActionAvailable,
      editPending,
      messageEditLabel,
      handleStartEditCallback
    ]
  );

  const messageContent = row.messages.map((message, messageIndex) => {
    const messageFooterAction =
      messageIndex === row.messages.length - 1 ? footerAction : null;
    const isEditTarget = canEdit && message.id === editableTargetMessageId;
    const isEditingTarget = isEditing && isEditTarget;
    const rawTimelineJson =
      showRawTimelineJson &&
      rawTimelineJsonLabel &&
      (message.sourceTimelineItems?.length ?? 0) > 0 ? (
        <RawTimelineJsonDisclosure
          items={message.sourceTimelineItems}
          label={rawTimelineJsonLabel}
        />
      ) : null;
    // Recover structured errors, including Claude SDK's standalone login notice.
    const recoveredError =
      !isUser && !message.visibleError
        ? recoverVisibleErrorFromMessage(message, provider)
        : null;
    const renderedContent =
      isUser &&
      message.contentKind === "selected-text" &&
      message.selectedText ? (
        <AgentSelectedTextChip selectedText={message.selectedText} />
      ) : isUser && message.contentKind === "image-grid" ? (
        <AgentUserImageGrid message={message} />
      ) : isUser &&
        message.contentKind === "tutti-checkpoint-wake" &&
        message.checkpointWake ? (
        <AgentTuttiModeCheckpointWakeCard
          checkpointWake={message.checkpointWake}
          fullText={message.body}
        />
      ) : isUser ? (
        <AgentRichTextReadonly
          value={message.body}
          documentCacheKey={message.id}
          className={`workspace-agents-status-panel__detail-user-message ${styles.userMessageBubble}`}
          editorClassName="text-[inherit]"
          onLinkClick={handleLinkClickCallback}
          availableSkills={availableSkills}
          workspaceAppIcons={workspaceAppIcons}
        />
      ) : message.contentKind === "tutti-plan-issue-link" &&
        message.planIssueLink ? (
        <AgentTuttiPlanIssueLinkCard
          planIssueLink={message.planIssueLink}
          workspaceRoot={workspaceRoot}
          basePath={basePath}
          onLinkAction={onLinkAction}
          workspaceAppIcons={workspaceAppIcons}
        />
      ) : message.visibleError ? (
        <AgentVisibleErrorMessage
          message={message}
          onAuthLogin={onAuthLogin}
          onExternalLink={handleExternalLinkClickCallback}
        />
      ) : recoveredError ? (
        <AgentVisibleErrorMessage
          message={recoveredError}
          onAuthLogin={onAuthLogin}
          onExternalLink={handleExternalLinkClickCallback}
        />
      ) : message.systemNotice ? (
        <AgentSystemNoticeMessage message={message} />
      ) : message.contentKind === "collaboration" && message.collaboration ? (
        <AgentCollaborationRow
          collaboration={message.collaboration}
          workspaceRoot={workspaceRoot}
          basePath={basePath}
          onLinkAction={onLinkAction}
          workspaceAppIcons={workspaceAppIcons}
        />
      ) : message.contentKind === "plan" ? (
        <AgentPlanCardMessage
          message={message}
          workspaceRoot={workspaceRoot}
          basePath={basePath}
          onLinkAction={onLinkAction}
          workspaceAppIcons={workspaceAppIcons}
        />
      ) : (
        <AgentMessageMarkdownBinding1
          message={message}
          onLinkAction={onLinkAction}
          workspaceRoot={workspaceRoot}
          basePath={basePath}
          workspaceAppIcons={workspaceAppIcons}
          isActiveTurn={isActiveTurn}
        />
      );
    const editor =
      isEditingTarget && editState && editRetry ? (
        <AgentUserMessageEditor
          value={editState.draft}
          pending={editPending}
          labels={editRetry.labels}
          onChange={handleEditTextChange}
          onCancel={handleCancelEdit}
          onSubmit={handleSubmitEdit}
          onKeyDown={handleEditorKeyDown}
        />
      ) : null;
    const content =
      editor && message.contentKind !== "image-grid" ? (
        editor
      ) : editor ? (
        <>
          {renderedContent}
          {editor}
        </>
      ) : isUser ? (
        renderedContent
      ) : (
        <InvokedAgentReply message={message}>
          {renderedContent}
        </InvokedAgentReply>
      );
    const editAction = isEditTarget ? messageEditAction : null;

    if (rawTimelineJson) {
      return (
        <AgentCopyableMessageGroup
          key={message.id}
          copyText={message.copyText ?? null}
          editAction={editAction}
          occurredAtUnixMs={message.occurredAtUnixMs}
          speaker={row.speaker}
          onCopyMessageText={handleCopyMessageTextCallback}
          footerAction={messageFooterAction}
        >
          {content}
          {rawTimelineJson}
        </AgentCopyableMessageGroup>
      );
    }

    const copyText = message.copyText ?? null;
    if (copyText || editAction || isEditingTarget) {
      return (
        <AgentCopyableMessageGroup
          key={message.id}
          copyText={copyText}
          editAction={editAction}
          occurredAtUnixMs={message.occurredAtUnixMs}
          speaker={row.speaker}
          onCopyMessageText={handleCopyMessageTextCallback}
          footerAction={messageFooterAction}
        >
          {content}
        </AgentCopyableMessageGroup>
      );
    }

    if (messageFooterAction) {
      return (
        <AgentCopyableMessageGroup
          key={message.id}
          copyText={null}
          editAction={null}
          occurredAtUnixMs={message.occurredAtUnixMs}
          speaker={row.speaker}
          onCopyMessageText={handleCopyMessageTextCallback}
          footerAction={messageFooterAction}
        >
          {content}
        </AgentCopyableMessageGroup>
      );
    }

    return <Fragment key={message.id}>{content}</Fragment>;
  });
  const enabledParticipantPresentation =
    participantPresentation?.enabled === true ? participantPresentation : null;
  const showParticipant =
    enabledParticipantPresentation !== null &&
    showParticipantHeader &&
    row.messages.length > 0;

  return (
    <div
      className={isUser ? styles.userMessageFlow : styles.assistantMessageFlow}
      data-agent-message-flow-thinking-first={
        !isUser && row.thinking.length > 0 ? "true" : undefined
      }
      data-agent-message-flow-thinking-last={
        !isUser && row.thinking.length > 0 && row.messages.length === 0
          ? "true"
          : undefined
      }
      data-agent-message-flow-participant={showParticipant ? "true" : undefined}
    >
      {showParticipant ? (
        <AgentConversationParticipantHeader
          presentation={enabledParticipantPresentation}
          speaker={row.speaker}
        />
      ) : null}
      {thinkingContent}
      {leadingToolContent}
      {showParticipant ? (
        <div
          className={styles.participantMessageLayout}
          data-agent-message-speaker={row.speaker}
        >
          <div className={styles.participantMessageContent}>
            {messageContent}
          </div>
        </div>
      ) : (
        messageContent
      )}
    </div>
  );
});

// Codex plan-mode proposals render as a framed card (mirrors the codex TUI
// treating the plan item as a distinct artifact rather than chat text).
function AgentPlanCardMessageRender({
  message,
  workspaceRoot,
  basePath,
  onLinkAction,
  workspaceAppIcons
}: AgentPlanCardMessageProps): JSX.Element {
  "use memo";

  const workspaceLinkContextProp1 = useMemo<
    NonNullable<
      React.ComponentProps<typeof AgentMessageMarkdown>["workspaceLinkContext"]
    >
  >(
    () => ({
      workspaceRoot: workspaceRoot,
      basePath: basePath,
      source: "agent-markdown"
    }),
    [workspaceRoot, basePath]
  );
  return (
    <AgentPlanCard copyText={message.body}>
      <AgentMessageMarkdown
        content={message.body}
        className={styles.assistantMarkdown}
        onLinkAction={onLinkAction}
        workspaceLinkContext={workspaceLinkContextProp1}
        workspaceAppIcons={workspaceAppIcons}
        enableImageZoom
      />
    </AgentPlanCard>
  );
}

const AgentMessageMarkdownBinding1 = observer(
  function AgentMessageMarkdownBinding1({
    message,
    onLinkAction,
    workspaceRoot,
    basePath,
    workspaceAppIcons,
    isActiveTurn
  }: AgentMessageMarkdownBinding1Props) {
    const workspaceLinkContextProp2 = useMemo<
      NonNullable<
        React.ComponentProps<
          typeof AgentMessageMarkdown
        >["workspaceLinkContext"]
      >
    >(
      () => ({
        workspaceRoot: workspaceRoot,
        basePath: basePath,
        source: "agent-markdown"
      }),
      [workspaceRoot, basePath]
    );
    return (
      <AgentMessageMarkdown
        content={message.body}
        documentCacheKey={message.id}
        className={styles.assistantMarkdown}
        onLinkAction={onLinkAction}
        workspaceLinkContext={workspaceLinkContextProp2}
        workspaceAppIcons={workspaceAppIcons}
        enableImageZoom
        streaming={
          isActiveTurn ||
          message.statusKind === "working" ||
          message.statusKind === "waiting"
        }
      />
    );
  }
);

type AgentPlanCardMessageProps = {
  message: AgentMessageContentVM;
  workspaceRoot: string | null;
  basePath: string;
  onLinkAction?: (action: WorkspaceLinkAction) => void;
  workspaceAppIcons?: readonly AgentMessageMarkdownWorkspaceAppIcon[];
};

type AgentMessageMarkdownBinding1Props = {
  message: AgentMessageContentVM;
  onLinkAction: NonNullable<AgentMessageBlockProps>["onLinkAction"];
  workspaceRoot: NonNullable<AgentMessageBlockProps>["workspaceRoot"];
  basePath: NonNullable<AgentMessageBlockProps>["basePath"];
  workspaceAppIcons: NonNullable<AgentMessageBlockProps>["workspaceAppIcons"];
  isActiveTurn: boolean;
};
