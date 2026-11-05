import { computed, makeObservable } from "mobx";
import type {
  AgentGUIEditRetryRecoveryAction,
  AgentGUIPromptOperations
} from "../agent-gui/agentGuiNode/controller/agentGuiController.promptOperations";
import { projectAgentGUIEditRetryPresentation } from "../agent-gui/agentGuiNode/model/agentGUIEditRetryModel";
import type { AgentTranscriptEditRetryControl } from "../shared/agentConversation/components/useAgentTranscriptEditRetryProjection";

export class EditRetryPresentationState {
  constructor(
    private readonly prompts: AgentGUIPromptOperations,
    private readonly agentSessionId: string | null,
    private readonly labels: AgentTranscriptEditRetryControl["labels"]
  ) {
    makeObservable(this, {
      presentation: computed.struct,
      control: computed.struct
    });
  }
  get presentation() {
    return projectAgentGUIEditRetryPresentation({
      availability: this.prompts.editRetryAvailability(this.agentSessionId),
      commandStatus: this.prompts.editRetryOperation(this.agentSessionId).status
    });
  }
  get control(): AgentTranscriptEditRetryControl | undefined {
    const turnId = this.presentation.editableTurnId?.trim();
    return turnId
      ? {
          agentSessionId: this.agentSessionId ?? "",
          eligibleTurnId: turnId,
          pending: this.presentation.state === "processing",
          labels: this.labels,
          onSubmit: this.submit
        }
      : undefined;
  }
  readonly submit = async (request: {
    editedText: string;
    turnId: string;
  }): Promise<boolean> => {
    const id = this.agentSessionId?.trim(),
      availability = this.prompts.editRetryAvailability(this.agentSessionId);
    if (
      !id ||
      availability?.eligible !== true ||
      availability.turnId !== request.turnId
    )
      return false;
    try {
      await this.prompts.requestEditRetry({ agentSessionId: id, ...request });
      return true;
    } catch {
      return false;
    }
  };
  readonly recover = async (
    action: AgentGUIEditRetryRecoveryAction
  ): Promise<void> => {
    const id = this.agentSessionId?.trim(),
      availability = this.prompts.editRetryAvailability(this.agentSessionId);
    if (
      !id ||
      !availability?.operationId ||
      !availability.availableActions.includes(action)
    )
      throw new Error("agent_edit_retry_recovery_unavailable");
    await this.prompts.recoverEditRetry({ action, agentSessionId: id });
  };
}
