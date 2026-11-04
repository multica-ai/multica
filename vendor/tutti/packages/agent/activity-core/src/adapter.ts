import type {
  AgentActivityCreateSessionInput,
  AgentActivityDeleteSessionInput,
  AgentActivityDeleteSessionResult,
  AgentActivityDeleteSessionsInput,
  AgentActivityDeleteSessionsResult,
  AgentActivityComposerOptions,
  AgentActivityLoadComposerOptionsInput,
  AgentActivityMessageOrder,
  AgentActivityMessagePage,
  AgentActivityRenameSessionInput,
  AgentActivitySendInput,
  AgentActivitySendInputResult,
  AgentActivitySetSessionPinnedInput,
  AgentActivitySession,
  AgentActivitySessionList,
  AgentActivitySubmitInteractiveInput,
  AgentActivitySubmitInteractiveResult,
} from "./types.ts";
import type {
  AgentActivityForkSessionResult,
  AgentActivityForkSessionThroughTurnInput
} from "./sessionFork.types.ts";

export interface AgentActivityAdapter {
  listSessions(input: {
    workspaceId: string;
    signal?: AbortSignal;
  }): Promise<AgentActivitySessionList>;

  listSessionMessages(input: {
    workspaceId: string;
    agentSessionId: string;
    afterVersion?: number;
    beforeVersion?: number;
    limit?: number;
    order?: AgentActivityMessageOrder;
    signal?: AbortSignal;
  }): Promise<AgentActivityMessagePage>;

  loadComposerOptions(
    input: AgentActivityLoadComposerOptionsInput
  ): Promise<AgentActivityComposerOptions>;

  createSession(
    input: AgentActivityCreateSessionInput
  ): Promise<AgentActivitySession>;
  sendInput(
    input: AgentActivitySendInput
  ): Promise<AgentActivitySendInputResult>;
  submitInteractive(
    input: AgentActivitySubmitInteractiveInput
  ): Promise<AgentActivitySubmitInteractiveResult>;
  deleteSession(
    input: AgentActivityDeleteSessionInput
  ): Promise<AgentActivityDeleteSessionResult>;
  deleteSessions(
    input: AgentActivityDeleteSessionsInput
  ): Promise<AgentActivityDeleteSessionsResult>;
  renameSession(
    input: AgentActivityRenameSessionInput
  ): Promise<AgentActivitySession>;
  setSessionPinned(
    input: AgentActivitySetSessionPinnedInput
  ): Promise<AgentActivitySession>;
  forkSession(
    input: AgentActivityForkSessionThroughTurnInput
  ): Promise<AgentActivityForkSessionResult>;
}
