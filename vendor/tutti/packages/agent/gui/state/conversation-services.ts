import type {
  PromptService,
  SessionLoadService
} from "@multica/conversation-client/services";
import type {
  AgentQueuedPrompt,
  AgentSessionActivationInput,
  AgentSessionSubmitPromptInput,
  PendingActivationIntentRecord
} from "@tutti-os/agent-activity-core";
import type { AgentSessionsModel } from "./AgentSessionsModel";

export interface AgentActivationPresentation {
  errorCode: string | null;
  errorMessage: string | null;
  requestId: string | null;
  status: "inactive" | "activating" | "active" | "failed";
}

export interface AgentActivationState {
  latest(id: string | null | undefined): PendingActivationIntentRecord | null;
  pending(): readonly PendingActivationIntentRecord[];
  presentation(id: string | null | undefined): AgentActivationPresentation;
  presentations(): Readonly<Record<string, AgentActivationPresentation>>;
  failedResolution(
    id: string | null | undefined,
    sessionExists: boolean,
    options?: { selectionSource?: "activation" | "user-selection" }
  ): "not-applicable" | "preserve" | "rollback";
  request(input: AgentSessionActivationInput): boolean;
  saved?(requestId: string): Promise<boolean> | undefined;
  patchSettings(
    id: string,
    settings: NonNullable<AgentSessionActivationInput["settings"]>
  ): void;
  unactivate(id: string): void;
  recordFailure(
    input: AgentSessionActivationInput,
    failure: { code: string | null; message: string | null }
  ): void;
  clearFailure(id: string): void;
  removeSession(id: string): void;
}

export interface AgentConversationServices {
  sessions: AgentSessionsModel;
  prompts: PromptService<AgentSessionSubmitPromptInput, AgentQueuedPrompt>;
  activations: AgentActivationState;
  sessionLoads: Pick<
    SessionLoadService,
    | "record"
    | "isDetailHydrated"
    | "isDetailLoading"
    | "canReload"
    | "request"
    | "markHydrated"
    | "remove"
  >;
}
