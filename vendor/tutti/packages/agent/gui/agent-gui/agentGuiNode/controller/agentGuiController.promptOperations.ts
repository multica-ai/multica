export type AgentGUIEditRetryRecoveryState =
  | "prepared"
  | "rolling_back"
  | "resend_pending"
  | "recovery_required"
  | "completed";

export type AgentGUIEditRetryRecoveryAction = "reconcile" | "retry_replacement";

export type AgentGUIEditRetryReasonCode =
  | "provider_unsupported"
  | "turn_not_found"
  | "turn_not_latest"
  | "turn_not_settled"
  | "history_revision_conflict"
  | "operation_conflict"
  | "recovery_required"
  | "provider_outcome_unknown"
  | "replacement_not_proven_absent";

export interface AgentGUIEditRetryAvailability {
  supported: boolean;
  eligible: boolean;
  turnId?: string;
  historyRevision: number;
  recoveryState: AgentGUIEditRetryRecoveryState;
  operationId?: string;
  availableActions: readonly AgentGUIEditRetryRecoveryAction[];
  reasonCode?: AgentGUIEditRetryReasonCode;
}

export type AgentGUIEditRetryStatus =
  | "failed"
  | "idle"
  | "pending"
  | "reconciling"
  | "succeeded";

export interface AgentGUIInteractionResponseInput {
  action?: string;
  agentSessionId: string;
  optionId?: string;
  payload?: Readonly<Record<string, unknown>>;
  requestId: string;
  turnId: string;
}

/** What the GUI uses of the prompt service: stops, answers and edit retries. */
export interface AgentGUIPromptOperations {
  stop(agentSessionId: string, clientSubmitId?: string | null): void;
  cancelStatus(agentSessionId: string | null | undefined): string | null;
  respondToInteraction(input: AgentGUIInteractionResponseInput): boolean;
  interactionResponse(
    agentSessionId: string,
    turnId: string,
    requestId: string
  ): { status: "responding" | "failed" | "unknown" } | null;
  isRespondingToInteraction(
    agentSessionId: string | null | undefined
  ): boolean;
  interactionError(agentSessionId: string | null | undefined): string | null;
  subscribeInteractionResponses(listener: () => void): () => void;
  editRetryAvailability(
    agentSessionId: string | null | undefined
  ): AgentGUIEditRetryAvailability | null;
  editRetryOperation(agentSessionId: string | null | undefined): {
    status: AgentGUIEditRetryStatus;
  };
  requestEditRetry(input: {
    agentSessionId: string;
    editedText: string;
    turnId: string;
  }): Promise<unknown>;
  recoverEditRetry(input: {
    action: AgentGUIEditRetryRecoveryAction;
    agentSessionId: string;
  }): Promise<unknown>;
}
