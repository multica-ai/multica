import { useEffect, useRef } from "react";
import { useOptionalAgentHostApi } from "../../../agentActivityHost";
import type { UiLanguage } from "../../../contexts/settings/domain/agentSettings";
import {
  AGENT_GUI_WORKBENCH_COMMAND_EVENT,
  normalizeAgentGuiWorkbenchCommand
} from "../../../workbench/commands";
import type {
  AgentGUIComposerViewModel,
  AgentGUIDetailViewModel,
  AgentGUIInteractionViewModel,
  AgentGUIOperationsViewModel,
  AgentGUIRailViewModel,
  AgentGUIReadinessViewModel,
  AgentGUIShellViewModel
} from "../model/agentGuiNodeTypes";
import { useAgentGUIConversationCopyAction } from "./agentGUIConversationCopy";
import type {
  AgentGUINodeViewProps,
  AgentGUIViewLabels
} from "./AgentGUINodeView.types";

type Conversation = AgentGUIRailViewModel["conversations"][number];

function resolveSessionActionConversation(
  {
    shell,
    rail
  }: { rail: AgentGUIRailViewModel; shell: AgentGUIShellViewModel },
  agentSessionId: string | null
): Conversation | null {
  const active = rail.activeConversation;
  if (!agentSessionId) {
    return active;
  }
  if (active?.id === agentSessionId) {
    return active;
  }
  return (
    rail.conversations.find(
      (conversation) => conversation.id === agentSessionId
    ) ?? null
  );
}

/**
 * Handles requests dispatched by the host chrome (workbench header) into the
 * AgentGUI node: creating a conversation and session actions (rename, copy
 * variants) targeting the conversation the header menu was rendered for.
 *
 * The header chrome cannot see whether the target conversation is loaded, so
 * failures here surface as an error toast rather than a silent drop.
 */
export function useAgentGUIExternalRequests(input: {
  createConversationDisabled: boolean;
  labels: Pick<
    AgentGUIViewLabels,
    | "conversationCopyFile"
    | "conversationCopyImage"
    | "conversationCopyImagesOmitted"
    | "conversationCopyInProgress"
    | "conversationCopyMentionPrefix"
    | "conversationCopyPreviousMessages"
    | "copiedToClipboard"
    | "copyFailed"
    | "sessionActionUnavailable"
    | "untitledConversationTitle"
  >;
  requestCreateConversation: (options?: { source?: string }) => void;
  requestRenameConversation: (conversation: Conversation) => void;
  uiLanguage: UiLanguage;
  shell: AgentGUIShellViewModel;
  rail: AgentGUIRailViewModel;
  detail: AgentGUIDetailViewModel;
  composer: AgentGUIComposerViewModel;
  interaction: AgentGUIInteractionViewModel;
  readiness: AgentGUIReadinessViewModel;
  operations: AgentGUIOperationsViewModel;
  workbenchCommandBridge: NonNullable<
    AgentGUINodeViewProps["workbenchCommandBridge"]
  > | null;
}): void {
  const {
    createConversationDisabled,
    labels,
    requestCreateConversation,
    requestRenameConversation,
    uiLanguage,
    shell,
    rail,
    detail,
    composer,
    interaction,
    readiness,
    operations,
    workbenchCommandBridge
  } = input;
  const agentHostApi = useOptionalAgentHostApi();
  const copyConversationValue = useAgentGUIConversationCopyAction(labels);
  const requestContextRef = useRef({
    agentHostApi,
    copyConversationValue,
    createConversationDisabled,
    labels,
    requestCreateConversation,
    requestRenameConversation,
    uiLanguage,
    ...{ shell, rail, detail, composer, interaction, readiness, operations },
    workbenchCommandBridge
  });
  requestContextRef.current = {
    agentHostApi,
    copyConversationValue,
    createConversationDisabled,
    labels,
    requestCreateConversation,
    requestRenameConversation,
    uiLanguage,
    ...{ shell, rail, detail, composer, interaction, readiness, operations },
    workbenchCommandBridge
  };
  useEffect(() => {
    if (!workbenchCommandBridge) {
      return;
    }
    const instanceId = workbenchCommandBridge.instanceId;
    const handleCommand = (event: Event): void => {
      const command = normalizeAgentGuiWorkbenchCommand(
        (event as CustomEvent<unknown>).detail
      );
      if (!command || command.instanceId !== instanceId) {
        return;
      }
      const current = requestContextRef.current;
      if (command.type === "conversation-rail-toggle") {
        current.workbenchCommandBridge?.onConversationRailToggle?.(
          command.conversationRailCollapsed
        );
        return;
      }
      if (command.type === "new-conversation") {
        if (!current.createConversationDisabled) {
          current.requestCreateConversation({ source: "external_request" });
        }
        return;
      }
      const conversation = resolveSessionActionConversation(
        current,
        command.agentSessionId
      );
      if (!conversation) {
        current.agentHostApi?.toast?.error(
          current.labels.sessionActionUnavailable
        );
      } else if (command.action === "rename") {
        current.requestRenameConversation(conversation);
      } else {
        current.copyConversationValue(command.action, {
          conversation,
          uiLanguage: current.uiLanguage,
          workspaceId: current.shell.workspaceId
        });
      }
    };
    window.addEventListener(AGENT_GUI_WORKBENCH_COMMAND_EVENT, handleCommand);
    return () => {
      window.removeEventListener(
        AGENT_GUI_WORKBENCH_COMMAND_EVENT,
        handleCommand
      );
    };
  }, [workbenchCommandBridge?.instanceId]);
}
