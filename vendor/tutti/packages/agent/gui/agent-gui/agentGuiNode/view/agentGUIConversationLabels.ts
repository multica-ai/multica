import type { TranslateFn } from "../../../i18n/index";
import type { AgentGUIViewLabels } from "../AgentGUINodeView";

type AgentGUIConversationLabels = Pick<
  AgentGUIViewLabels,
  | "cancel"
  | "conversationCopyFile"
  | "conversationCopyImage"
  | "conversationCopyImagesOmitted"
  | "conversationCopyInProgress"
  | "conversationCopyMentionPrefix"
  | "conversationCopyPreviousMessages"
  | "conversationUnavailable"
  | "copiedToClipboard"
  | "copyAsMarkdown"
  | "copyAsReference"
  | "copyFailed"
  | "markSessionUnread"
  | "moreSessionActions"
  | "openConversationWindow"
  | "renameSession"
  | "searchPlaceholder"
  | "selectConversation"
  | "startConversation"
  | "untitledConversationTitle"
>;

export function agentGUIConversationLabels(
  t: TranslateFn
): AgentGUIConversationLabels {
  return {
    cancel: t("common.cancel"),
    conversationUnavailable: t("agentHost.agentGui.conversationUnavailable"),
    copiedToClipboard: t("agentHost.agentGui.copiedToClipboard"),
    copyAsMarkdown: t("agentHost.agentGui.copyAsMarkdown"),
    copyAsReference: t("agentHost.agentGui.copyAsReference"),
    copyFailed: t("agentHost.agentGui.copyFailed"),
    conversationCopyFile: t("agentHost.agentGui.conversationCopyFile"),
    conversationCopyImage: t("agentHost.agentGui.conversationCopyImage"),
    conversationCopyImagesOmitted: t(
      "agentHost.agentGui.conversationCopyImagesOmitted"
    ),
    conversationCopyInProgress: t(
      "agentHost.agentGui.conversationCopyInProgress"
    ),
    conversationCopyMentionPrefix: t(
      "agentHost.agentGui.conversationCopyMentionPrefix"
    ),
    conversationCopyPreviousMessages: t(
      "agentHost.agentGui.conversationCopyPreviousMessages"
    ),
    moreSessionActions: t("agentHost.agentGui.moreSessionActions"),
    markSessionUnread: t("agentHost.agentGui.markSessionUnread"),
    openConversationWindow: t("agentHost.agentGui.openConversationWindow"),
    renameSession: t("agentHost.agentGui.renameSession"),
    searchPlaceholder: t("agentHost.agentGui.searchPlaceholder"),
    selectConversation: t("agentHost.agentGui.selectConversation"),
    startConversation: t("agentHost.agentGui.startConversation"),
    untitledConversationTitle: t(
      "agentHost.workspaceAgentsUntitledConversation"
    )
  };
}
