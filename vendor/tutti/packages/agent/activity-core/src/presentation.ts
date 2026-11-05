import type { AgentActivityDisplayStatus } from "./displayStatus.types.ts";
import type { AgentActivityRailPlacement } from "./railPlacement.types.ts";
import type { AgentActivityCapabilityReference } from "./tuttiMode.types.ts";
import type {
  AgentActivityInteraction,
  AgentActivitySession,
  AgentActivitySessionSettings,
  AgentActivitySubmitDiagnostics,
  AgentActivitySubmitSettingsPatch,
  AgentActivityTurn,
  AgentPromptContentBlock
} from "./types.ts";

export type CanonicalAgentSession = Omit<
  AgentActivitySession,
  "activeTurn" | "latestTurn" | "latestTurnInteractions" | "pendingInteractions"
> & {
  activeTurnId: string | null;
};

export type SessionRuntimeAvailability =
  | { state: "available" }
  | {
      state: "blocked";
      reason:
        | "agent_capability_checking"
        | "agent_capability_unavailable"
        | "transport_reconnecting"
        | "transport_unavailable";
    }
  | {
      state: "blocked";
      reason: "agent_sharing_revoked";
      ownerLabel: string;
    };

export interface AgentSubmitAvailability {
  reason?: Exclude<CanonicalSubmitAvailability["reason"], undefined>;
  state: "available" | "blocked";
}

export interface CanonicalSubmitAvailability {
  reason?:
    | "active_turn"
    | "agent_capability_checking"
    | "agent_capability_unavailable"
    | "agent_sharing_revoked"
    | "waiting"
    | "transport_reconnecting"
    | "transport_unavailable";
  state: "available" | "blocked" | "missing";
}

export interface WorkspaceAgentConsumerSession {
  activeTurn: AgentActivityTurn | null;
  displayStatus: AgentActivityDisplayStatus;
  latestTurn: AgentActivityTurn | null;
  pendingInteractions: readonly AgentActivityInteraction[];
  session: CanonicalAgentSession;
}

export interface AgentSessionFamilySnapshot {
  childSessions: readonly AgentActivitySession[];
  pendingInteractions: readonly AgentActivityInteraction[];
  rootSession: AgentActivitySession | null;
}

export type AgentSessionActivationInput =
  | (AgentSessionActivationInputBase & {
      agentTargetId: string;
      clientSubmitId: string;
      mode: "new";
      optimisticTitle?: string;
    })
  | (AgentSessionActivationInputBase & {
      agentTargetId?: string | null;
      clientSubmitId?: never;
      mode: "existing";
      optimisticTitle?: never;
    });

interface AgentSessionActivationInputBase {
  agentSessionId: string;
  capabilityRefs?: readonly AgentActivityCapabilityReference[];
  cwd?: string;
  initialContent?: readonly AgentPromptContentBlock[];
  initialDisplayPrompt?: string;
  isolation?: "worktree";
  modelExplicit?: boolean;
  initialTurnExpected?: boolean;
  railPlacement?: AgentActivityRailPlacement;
  reasoningEffortExplicit?: boolean;
  railSectionKey?: string;
  requestId: string;
  runtimeContent?: readonly AgentPromptContentBlock[];
  settings?: AgentActivitySessionSettings;
  submitDiagnostics?: Readonly<AgentActivitySubmitDiagnostics>;
  title?: string;
  visible?: boolean;
}

export interface AgentSessionSubmitPromptInput {
  agentSessionId: string;
  capabilityRefs?: readonly AgentActivityCapabilityReference[];
  clientSubmitId: string;
  content: readonly AgentPromptContentBlock[];
  displayPrompt?: string;
  requiredSettingsPatch?: Readonly<AgentActivitySubmitSettingsPatch>;
  routing?: "auto" | "immediate" | "send_now";
  /** Exact canonical active Turn targeted when routing as guidance. */
  targetTurnId?: string;
  runtimeContent?: readonly AgentPromptContentBlock[];
  submitDiagnostics?: Readonly<AgentActivitySubmitDiagnostics>;
}

export interface AgentSessionSubmitPromptResult {
  accepted: boolean;
  queued: boolean;
}

export interface AgentQueuedPrompt {
  capabilityRefs?: readonly AgentActivityCapabilityReference[];
  clientSubmitId?: string;
  content: readonly AgentPromptContentBlock[];
  createdAtUnixMs: number;
  displayPrompt?: string;
  guidance?: boolean;
  id: string;
  requiredSettingsPatch?: Readonly<AgentActivitySubmitSettingsPatch>;
  submitDiagnostics?: Readonly<AgentActivitySubmitDiagnostics>;
  /** Exact canonical active Turn targeted by a native guidance send. */
  targetTurnId?: string;
  runtimeContent?: readonly AgentPromptContentBlock[];
  visibleInQueue?: boolean;
}

export interface PromptQueueInFlightCommand {
  commandId: string;
  /** Set when the command was dispatched as an active-turn steer. */
  guidance?: true;
  kind: "send";
  promptId: string;
  runtimeContent?: readonly AgentPromptContentBlock[];
  stage?: "preparingSettings" | "sending";
}

export interface PromptQueueRecord {
  agentSessionId: string;
  deliveryBarrierTurnId: string | null;
  failedPromptId: string | null;
  failureMessage: string | null;
  inFlight: PromptQueueInFlightCommand | null;
  /** Omitted by snapshots produced before deferred send-now admission existed. */
  pendingSendNowByPromptId?: Readonly<
    Record<string, PromptQueuePendingSendNow>
  >;
  prompts: readonly AgentQueuedPrompt[];
  sendNextPromptId: string | null;
  suspendReason: PromptQueueSuspendReason | null;
  uncertainDelivery: PromptQueueInFlightCommand | null;
  workspaceId: string;
}

export interface PromptQueuePendingSendNow {
  awaitingTurnExpiresAtUnixMs: number;
  cancelCommandId: string;
  promptId: string;
  targetTurnId: string;
  timeoutMs: number;
}

export type PromptQueueSuspendReason = "user_stop";

export interface PromptQueueState {
  nextCommandSequence: number;
  recordsBySessionId: Readonly<Record<string, PromptQueueRecord>>;
}

export type PendingActivationIntentRecord =
  | (PendingActivationIntentRecordBase & {
      agentTargetId: string;
      clientSubmitId: string;
      mode: "new";
      optimisticTitle?: string;
    })
  | (PendingActivationIntentRecordBase & {
      agentTargetId: string | null;
      clientSubmitId: null;
      mode: "existing";
      optimisticTitle?: never;
    });

interface PendingActivationIntentRecordBase {
  agentSessionId: string;
  capabilityRefs?: readonly AgentActivityCapabilityReference[];
  content: readonly AgentPromptContentBlock[];
  displayPrompt?: string;
  cwd: string;
  errorCode: string | null;
  errorMessage: string | null;
  expiresAtUnixMs: number;
  initialPromptRetracted: boolean;
  initialTurnExpected: boolean;
  isolation?: "worktree";
  modelExplicit?: boolean;
  railSectionKey?: string;
  railPlacement?: AgentActivityRailPlacement;
  reasoningEffortExplicit?: boolean;
  submitDiagnostics?: Readonly<AgentActivitySubmitDiagnostics>;
  pendingSettingsPatch?: Readonly<Record<string, unknown>>;
  settingsUpdateStatus?: "failed" | "inFlight" | "unknown";
  requestedAtUnixMs: number;
  requestId: string;
  settings?: AgentActivitySessionSettings;
  status: PendingActivationStatus;
  title: string | null;
  workspaceId: string;
}

export type PendingActivationStatus =
  | "requested"
  | "confirmed"
  | "canceled"
  | "uncertain"
  | "failed";

export interface PendingSubmitIntentRecord {
  acceptedSessionVersion: number | null;
  agentSessionId: string;
  clientSubmitId: string;
  capabilityRefs?: readonly AgentActivityCapabilityReference[];
  content: readonly AgentPromptContentBlock[];
  displayPrompt?: string;
  errorCode: string | null;
  errorMessage: string | null;
  errorReason: string | null;
  expiresAtUnixMs: number;
  submitDiagnostics?: Readonly<AgentActivitySubmitDiagnostics>;
  requestedAtUnixMs: number;
  status: PendingSubmitStatus;
  turnId: string | null;
  workspaceId: string;
}

export type PendingSubmitStatus =
  | "requested"
  | "accepted"
  | "confirmed"
  | "uncertain"
  | "failed";

export function isPendingActivationViable(
  activation: { status: PendingActivationStatus } | null | undefined
): boolean {
  return (
    activation !== null &&
    activation !== undefined &&
    activation.status !== "failed" &&
    activation.status !== "canceled"
  );
}

export function canonicalInteractionKey(
  agentSessionId: string,
  turnId: string,
  requestId: string
): string {
  return scopedEntityKey(agentSessionId, scopedEntityKey(turnId, requestId));
}

function scopedEntityKey(agentSessionId: string, entityId: string): string {
  const session = agentSessionId.trim();
  const entity = entityId.trim();
  return `${session.length}:${session}${entity}`;
}

export function pendingSubmitRecordListsEqual(
  left: readonly PendingSubmitIntentRecord[],
  right: readonly PendingSubmitIntentRecord[]
): boolean {
  return (
    left.length === right.length &&
    left.every((record, index) => record === right[index])
  );
}

export function sessionActivationPresentationMapsEqual(
  left: Readonly<Record<string, SessionActivationPresentation>>,
  right: Readonly<Record<string, SessionActivationPresentation>>
): boolean {
  const leftKeys = Object.keys(left);
  const rightKeys = Object.keys(right);
  return (
    leftKeys.length === rightKeys.length &&
    leftKeys.every((key) => {
      const a = left[key];
      const b = right[key];
      return (
        b !== undefined &&
        a?.errorCode === b.errorCode &&
        a.errorMessage === b.errorMessage &&
        a.requestId === b.requestId &&
        a.status === b.status
      );
    })
  );
}

export interface SessionActivationPresentation {
  errorCode: string | null;
  errorMessage: string | null;
  requestId: string | null;
  status: "inactive" | "activating" | "active" | "failed";
}

export const AGENT_CONVERSATION_LOCAL_ORIGIN =
  "WORKSPACE_AGENT_SESSION_ORIGIN_RUNTIME";
