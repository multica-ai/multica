import {
  isPendingActivationViable,
  type AgentActivityMessage,
  type AgentQueuedPrompt
} from "@tutti-os/agent-activity-core";
import { createPendingOptimisticTurnId } from "../agent-gui/agentGuiNode/controller/agentGuiController.draftMessageHelpers";
import {
  createOptimisticPromptMessage,
  projectAgentGUIMessagesToTimelineItems
} from "../agent-gui/agentGuiNode/controller/agentGuiController.promptHelpers";
import { mergeWorkspaceAgentMessages } from "../host/workspaceAgentSessionMessages";
import type { AgentConversationServices } from "./conversation-services";

export function readPendingConversationMessages(input: {
  activeConversationId: string | null;
  activePendingActivation: ReturnType<
    AgentConversationServices["activations"]["latest"]
  >;
  activePendingSubmits: ReturnType<
    AgentConversationServices["prompts"]["pendingSubmits"]
  >;
  activeQueuedPrompts: readonly AgentQueuedPrompt[];
  currentUserId: string | null | undefined;
  storedMessages: readonly AgentActivityMessage[];
  workspaceId: string;
}) {
  const {
    activeConversationId,
    activePendingActivation,
    activePendingSubmits,
    activeQueuedPrompts,
    currentUserId,
    storedMessages,
    workspaceId
  } = input;
  const activeMessages = (() => {
    if (!activeConversationId) return storedMessages;
    const visibleQueuedSubmitIds = new Set(
      activeQueuedPrompts
        .map((prompt) =>
          "clientSubmitId" in prompt ? prompt.clientSubmitId : undefined
        )
        .filter((value): value is string => Boolean(value))
    );
    const pendingMessages = activePendingSubmits
      .filter(
        (pending) =>
          pending.agentSessionId === activeConversationId &&
          pending.status !== "failed" &&
          !visibleQueuedSubmitIds.has(pending.clientSubmitId)
      )
      .map((pending) =>
        createOptimisticPromptMessage({
          agentSessionId: pending.agentSessionId,
          clientSubmitId: pending.clientSubmitId,
          content: [...pending.content],
          displayPrompt: pending.displayPrompt,
          occurredAtUnixMs: pending.requestedAtUnixMs,
          turnId:
            pending.turnId ??
            createPendingOptimisticTurnId(pending.clientSubmitId),
          userId: currentUserId?.trim() || "user",
          workspaceId
        })
      );
    const pendingActivationMessage =
      activePendingActivation?.mode === "new" &&
      isPendingActivationViable(activePendingActivation) &&
      !activePendingActivation.initialPromptRetracted &&
      activePendingActivation.clientSubmitId &&
      activePendingActivation.content.length > 0
        ? createOptimisticPromptMessage({
            agentSessionId: activePendingActivation.agentSessionId,
            clientSubmitId: activePendingActivation.clientSubmitId,
            content: [...activePendingActivation.content],
            displayPrompt: activePendingActivation.displayPrompt,
            occurredAtUnixMs: activePendingActivation.requestedAtUnixMs,
            turnId: createPendingOptimisticTurnId(
              activePendingActivation.clientSubmitId
            ),
            userId: currentUserId?.trim() || "user",
            workspaceId
          })
        : null;
    const optimisticMessages = pendingActivationMessage
      ? [...pendingMessages, pendingActivationMessage]
      : pendingMessages;
    return optimisticMessages.length > 0
      ? mergeWorkspaceAgentMessages(storedMessages, optimisticMessages)
      : storedMessages;
  })();
  const activeTimelineItems =
    projectAgentGUIMessagesToTimelineItems(activeMessages);
  return { activeMessages, activeTimelineItems };
}
