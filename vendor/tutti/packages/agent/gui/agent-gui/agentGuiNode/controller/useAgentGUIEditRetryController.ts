import { useCallback, useMemo } from "react";
import { useAgentGUIRuntime } from "../../../agentActivityRuntime";
import { useModelValue } from "../../../shared/engine/useEngineSelector";
import type { AgentGUIEditRetryRecoveryAction } from "./agentGuiController.promptOperations";
import { projectAgentGUIEditRetryPresentation } from "../model/agentGUIEditRetryModel";

export function useAgentGUIEditRetryController(input: {
  agentSessionId: string | null;
  workspaceId: string;
}) {
  const { prompts } = useAgentGUIRuntime();
  const availability = useModelValue(
    () => prompts.editRetryAvailability(input.agentSessionId),
    [prompts, input.agentSessionId]
  );
  const commandStatus = useModelValue(
    () => prompts.editRetryOperation(input.agentSessionId).status,
    [prompts, input.agentSessionId]
  );

  const submit = useCallback(
    async (request: { editedText: string; turnId: string }) => {
      const agentSessionId = input.agentSessionId?.trim() ?? "";
      if (
        !agentSessionId ||
        availability?.eligible !== true ||
        availability.turnId !== request.turnId
      ) {
        return false;
      }
      try {
        await prompts.requestEditRetry({
          agentSessionId,
          editedText: request.editedText,
          turnId: request.turnId
        });
        return true;
      } catch {
        // The service retains the typed failure/recovery state. Returning false
        // keeps the editor draft available without leaking a rejected UI event.
        return false;
      }
    },
    [input.agentSessionId, availability, prompts]
  );

  const recover = useCallback(
    async (action: AgentGUIEditRetryRecoveryAction) => {
      const agentSessionId = input.agentSessionId?.trim() ?? "";
      if (
        !agentSessionId ||
        !availability?.operationId ||
        !availability.availableActions.includes(action)
      ) {
        throw new Error("agent_edit_retry_recovery_unavailable");
      }
      await prompts.recoverEditRetry({ action, agentSessionId });
    },
    [input.agentSessionId, availability, prompts]
  );

  const presentation = useMemo(
    () =>
      projectAgentGUIEditRetryPresentation({ availability, commandStatus }),
    [availability, commandStatus]
  );

  return { presentation, recover, submit };
}
