import type { AgentGUIPromptOperations } from "../../agent-gui/agentGuiNode/controller/agentGuiController.promptOperations";

/** A prompt service with nothing in flight, for specs of what reads it. */
export function createTestPromptOperations(
  overrides: Partial<AgentGUIPromptOperations> = {}
): AgentGUIPromptOperations {
  return {
    cancelStatus: () => null,
    editRetryAvailability: () => null,
    editRetryOperation: () => ({ status: "idle" }),
    interactionError: () => null,
    interactionResponse: () => null,
    isRespondingToInteraction: () => false,
    recoverEditRetry: async () => undefined,
    requestEditRetry: async () => undefined,
    respondToInteraction: () => false,
    stop: () => undefined,
    subscribeInteractionResponses: () => () => undefined,
    ...overrides
  };
}
