import type {
  AgentActivityComposerOptions,
  AgentActivityComposerOptionsLoadStatus
} from "./composerOptions.types.ts";
import type {
  AgentActivityDurableMessage,
  AgentActivityMessage,
  AgentActivityMessageDeltaEvent
} from "./message.types.ts";
import type { AgentActivitySessionMessageWindow } from "./messageWindow.types.ts";
import type { AgentActivityRailPlacement } from "./railPlacement.types.ts";
import type { AgentActivitySessionCapabilities } from "./sessionCapabilities.types.ts";
import type { AgentActivityCapabilityReference } from "./tuttiMode.types.ts";

export type {
  AgentActivityDurableMessage,
  AgentActivityMessage,
  AgentActivityMessageDeltaEvent,
  AgentActivityMessageSemantics,
  AgentActivityTransientMessage
} from "./message.types.ts";

export type { AgentActivityCapabilityReference } from "./tuttiMode.types.ts";

export type {
  AgentActivityComposerBehavior,
  AgentActivityComposerCapabilityOption,
  AgentActivityComposerCommandOption,
  AgentActivityComposerOptions,
  AgentActivityComposerOptionsLoadStatus,
  AgentActivityComposerPermissionConfig,
  AgentActivityComposerPermissionModeOption,
  AgentActivityComposerSettingOption,
  AgentActivityComposerSettings,
  AgentActivityComposerSkillOption,
  AgentActivityLoadComposerOptionsInput,
  AgentActivitySlashCommandEffect,
  AgentActivitySlashCommandPolicy
} from "./composerOptions.types.ts";

export type { AgentActivitySessionCapabilities } from "./sessionCapabilities.types.ts";

export type AgentActivitySessionKind = "root" | "child";

export interface AgentActivitySessionLifecycleCapabilities {
  fork: boolean;
  forkThroughTurn: boolean;
}

export interface AgentActivitySessionForkLineage {
  sourceAgentSessionId: string;
  sourceTurnId: string;
  targetTurnId: string;
  operationId: string;
  forkedAtUnixMs: number;
}

export interface AgentActivitySessionIsolation {
  mode: "worktree";
  worktreeId?: string;
  worktreePath: string;
  branch: string;
  baseCommit: string;
}

export interface AgentActivitySession {
  workspaceId: string;
  agentSessionId: string;
  kind: AgentActivitySessionKind;
  rootAgentSessionId: string | null;
  rootTurnId: string | null;
  parentAgentSessionId: string | null;
  parentTurnId: string | null;
  parentToolCallId: string | null;
  agentTargetId: string | null;
  provider: string;
  providerSessionId: string | null;
  userId?: string;
  model?: string | null;
  noProject?: boolean | null;
  cwd: string;
  isolation?: AgentActivitySessionIsolation | null;
  /** Backend-owned conversation-rail membership; absent for non-rail runtimes. */
  railSectionKey?: string;
  title: string;
  activeTurnId: string | null;
  activeTurn: AgentActivityTurn | null;
  latestTurn: AgentActivityTurn | null;
  latestTurnInteractions: readonly AgentActivityInteraction[];
  pendingInteractions: readonly AgentActivityInteraction[];
  settings: AgentActivitySessionSettings;
  permissionConfig: AgentActivitySessionPermissionConfig;
  capabilities: AgentActivitySessionCapabilities | null;
  lifecycleCapabilities: AgentActivitySessionLifecycleCapabilities;
  /**
   * True only when lifecycle capabilities came from an authoritative Session
   * detail projection. Lightweight rail/list projections intentionally leave
   * provider-backed lifecycle capabilities unresolved.
   */
  lifecycleCapabilitiesProjected?: boolean;
  forkedFrom: AgentActivitySessionForkLineage | null;
  usage: AgentActivitySessionUsage | null;
  imported: boolean;
  visible: boolean;
  resumable: boolean;
  /** Latest accepted durable message change cursor. */
  messageVersion: number;
  lastEventUnixMs: number;
  startedAtUnixMs: number;
  endedAtUnixMs: number | null;
  pinnedAtUnixMs: number | null;
  createdAtUnixMs: number;
  updatedAtUnixMs: number;
}

export type AgentActivityActivationMode = "new" | "existing";
export type AgentActivityActivationStatus =
  | "attached"
  | "already_attached"
  | "failed";

export interface AgentActivityActivateSessionResult {
  session: AgentActivitySession;
  activation: {
    mode: AgentActivityActivationMode;
    status: AgentActivityActivationStatus;
  };
  error?: {
    code: string;
    message: string;
    debugMessage?: string;
  };
}

export interface AgentActivityInteractivePrompt {
  kind: string;
  requestId?: string;
  toolName?: string;
  status?: string;
  input?: Record<string, unknown>;
  output?: Record<string, unknown>;
  error?: Record<string, unknown>;
  metadata?: Record<string, unknown>;
}

export interface AgentActivitySessionList {
  sessions: AgentActivitySession[];
  presences?: AgentActivityPresence[];
}

export interface AgentActivityRenameSessionInput {
  workspaceId: string;
  agentSessionId: string;
  title: string;
  signal?: AbortSignal;
}

export interface AgentActivityPresence {
  id: string | number;
  workspaceId: string;
  provider: string;
  status: string;
  userId?: string | null;
}

export interface AgentActivityMessagePage {
  messages: AgentActivityDurableMessage[];
  hasMore: boolean;
  latestVersion: number;
}

export type AgentActivityMessageOrder = "asc" | "desc";

export interface AgentActivitySnapshot {
  workspaceId: string;
  sessions: AgentActivitySession[];
  presences: AgentActivityPresence[];
}

export type AgentActivitySnapshotListener = (
  snapshot: AgentActivitySnapshot
) => void;

export type AgentActivityUpdatedEvent =
  | AgentActivityRuntimeActivityUpdatedEvent
  | AgentActivitySessionReconcileRequiredEvent
  | AgentActivitySessionDeletedEvent
  | AgentActivitySessionRestoredEvent
  | AgentActivitySessionAuditEvent
  | AgentActivityMessageDeltaEvent
  | AgentActivityMessageUpdatedEvent
  | AgentActivityTurnUpdatedEvent
  | AgentActivityInteractionUpdatedEvent;

export interface AgentActivityRuntimeActivityUpdatedEvent {
  workspaceId: string;
  agentSessionId: string;
  eventType: "runtime_activity_update";
  data: {
    workspaceId: string;
    agentSessionId: string;
    eventType: "runtime_activity_update";
    state: "idle" | "running";
    occurredAtUnixMs: number;
  };
}

export interface AgentActivitySessionReconcileRequiredEvent {
  workspaceId: string;
  agentSessionId: string;
  eventType: "session_reconcile_required";
  data: {
    workspaceId: string;
    agentSessionId: string;
    agentTargetId?: string;
    eventType: "session_reconcile_required";
    lastEventUnixMs: number;
  };
}

export interface AgentActivitySessionDeletedEvent {
  workspaceId: string;
  agentSessionId: string;
  eventType: "session_deleted";
  data: {
    workspaceId: string;
    agentSessionId: string;
    eventType: "session_deleted";
    deletedAtUnixMs: number;
  };
}

export interface AgentActivitySessionRestoredEvent {
  workspaceId: string;
  agentSessionId: string;
  eventType: "session_restored";
  data: {
    workspaceId: string;
    agentSessionId: string;
    eventType: "session_restored";
    restoredAtUnixMs: number;
  };
}

export interface AgentActivityMessageUpdatedEvent {
  workspaceId: string;
  agentSessionId: string;
  eventType: "message_update";
  data: {
    workspaceId: string;
    agentSessionId: string;
    eventType: "message_update";
    latestVersion: number;
    acceptedCount: number;
    messages: readonly AgentActivityEventMessage[];
  };
}

export interface AgentActivitySessionAuditEvent {
  workspaceId: string;
  agentSessionId: string;
  eventType: "session_audit";
  data: {
    workspaceId: string;
    agentSessionId: string;
    eventType: "session_audit";
    audit: {
      auditId: string;
      role: string;
      payload: Record<string, unknown>;
      occurredAtUnixMs: number;
      version: number;
    };
  };
}

export interface AgentActivityEventMessage {
  agentSessionId: string;
  kind: string;
  messageId: string;
  payload: Record<string, unknown>;
  role: string;
  version: number;
  turnId: string | null;
  status?: string;
  sequence: number;
  occurredAtUnixMs: number;
  startedAtUnixMs?: number;
  completedAtUnixMs?: number;
  createdAtUnixMs?: number;
  updatedAtUnixMs?: number;
}

export interface AgentActivityTurnUpdatedEvent {
  workspaceId: string;
  agentSessionId: string;
  eventType: "turn_update";
  data: {
    workspaceId: string;
    agentSessionId: string;
    eventType: "turn_update";
    occurredAtUnixMs: number;
    activeTurnId: string | null;
    turn: AgentActivityEventTurn;
  };
}

export interface AgentActivityEventTurn {
  turnId: string;
  agentSessionId: string;
  capabilityRefs?: readonly AgentActivityCapabilityReference[];
  phase: AgentActivityTurnPhase;
  origin: AgentActivityTurnOrigin;
  sourceGoalOperationId?: string | null;
  sourceGoalRevision?: number | null;
  sourceGoalRepairEpoch?: number | null;
  outcome: AgentActivityTurnOutcome | null;
  error: Record<string, unknown> | null;
  fileChanges: unknown;
  completedCommand: Record<string, unknown> | null;
  startedAtUnixMs: number;
  settledAtUnixMs: number | null;
  updatedAtUnixMs: number;
}

export interface AgentActivityInteractionUpdatedEvent {
  workspaceId: string;
  agentSessionId: string;
  eventType: "interaction_update";
  data: {
    workspaceId: string;
    agentSessionId: string;
    eventType: "interaction_update";
    occurredAtUnixMs: number;
    interaction: AgentActivityInteraction;
  };
}

export type AgentActivitySessionEventEnvelope = Extract<
  AgentActivityUpdatedEvent,
  { eventType: "message_update" | "session_audit" }
>;

export interface AgentActivityUpdatedApplyResult {
  applied: boolean;
  messages: AgentActivityMessage[];
  session: AgentActivitySession | null;
}

export interface AgentActivityCreateSessionInput {
  clientSubmitId: string;
  workspaceId: string;
  agentSessionId?: string | null;
  agentTargetId: string;
  cwd?: string | null;
  isolation?: AgentActivitySessionIsolation["mode"] | null;
  noProject?: boolean | null;
  capabilityRefs?: readonly AgentActivityCapabilityReference[] | null;
  railPlacement?: AgentActivityRailPlacement;
  initialContent?: AgentPromptContentBlock[] | null;
  /** 仅展示用的首轮文本(bundle 折叠成一个 chip);initialContent 仍带展开后的文件。 */
  initialDisplayPrompt?: string | null;
  submitDiagnostics?: AgentActivitySubmitDiagnostics;
  browserUse?: boolean | null;
  codexSaverMode?: boolean | null;
  rtkSaverMode?: boolean | null;
  model?: string | null;
  modelExplicit?: boolean;
  planMode?: boolean | null;
  permissionModeId?: string | null;
  reasoningEffort?: string | null;
  reasoningEffortExplicit?: boolean;
  speed?: string | null;
  title?: string | null;
  visible?: boolean | null;
  signal?: AbortSignal;
}

export interface AgentActivitySendInput {
  clientSubmitId: string;
  workspaceId: string;
  agentSessionId: string;
  capabilityRefs?: readonly AgentActivityCapabilityReference[] | null;
  content: AgentPromptContentBlock[];
  /** 仅展示用文本(bundle 折叠成一个 chip);content 仍带展开后的文件。 */
  displayPrompt?: string | null;
  guidance?: boolean;
  /** Exact canonical active Turn targeted by guidance. */
  targetTurnId?: string | null;
  submitDiagnostics?: AgentActivitySubmitDiagnostics;
  signal?: AbortSignal;
}

export interface AgentActivitySubmitSettingsPatch {
  browserUse?: boolean;
  computerUse?: boolean;
}

export interface AgentActivitySubmitDiagnostics {
  submittedAtUnixMs?: number;
  blockCount?: number;
  hasImage?: boolean;
  promptLength?: number;
  queued?: boolean;
  source?: string;
  uiMode?: "os" | "agent";
}

export interface AgentActivitySendInputResult {
  session: AgentActivitySession;
  turnId: string;
  turn: AgentActivityTurn;
}

export interface AgentPromptContentBlock {
  type: "text" | "image" | "file" | "skill" | "mention" | "connector";
  text?: string;
  mimeType?: "image/png" | "image/jpeg" | "image/webp" | string;
  data?: string;
  url?: string;
  attachmentId?: string;
  name?: string;
  path?: string;
  connectorKey?: string;
  uri?: string;
  hostPath?: string;
  uploadStatus?: string;
  assetId?: string;
  kind?: string;
  sizeBytes?: number;
}

export interface AgentActivitySubmitInteractiveInput {
  workspaceId: string;
  agentSessionId: string;
  requestId: string;
  turnId: string;
  action?: string | null;
  optionId?: string | null;
  payload?: Record<string, unknown> | null;
  signal?: AbortSignal;
}

export interface AgentActivitySubmitInteractiveResult {
  session: AgentActivitySession;
}

export interface AgentActivityDeleteSessionInput {
  workspaceId: string;
  agentSessionId: string;
  signal?: AbortSignal;
}

export interface AgentActivityDeleteSessionResult {
  cleanupFailed: boolean;
  removed: boolean;
}

export interface AgentActivityDeleteSessionsInput {
  workspaceId: string;
  agentSessionIds: readonly string[];
  signal?: AbortSignal;
}

export interface AgentActivityDeleteSessionsResult {
  cleanupFailedSessionIds: string[];
  removedMessages: number;
  removedSessionIds: string[];
  removedSessions: number;
}

export interface AgentActivitySetSessionPinnedInput {
  workspaceId: string;
  agentSessionId: string;
  pinned: boolean;
  signal?: AbortSignal;
}

export * from "./collaboration.types.ts";
export type {
  AgentActivityModelPlanModel,
  AgentActivityModelPlanSummary
} from "./modelPlans.types.ts";

export type AgentActivityNeedsAttentionKind =
  | "permission"
  | "question"
  | "constraint"
  | "other";

export interface AgentActivityNeedsAttentionItem {
  id: string;
  workspaceId: string;
  agentSessionId: string;
  provider: string;
  title: string;
  cwd: string;
  kind: AgentActivityNeedsAttentionKind;
  summary: string;
  occurredAtUnixMs: number;
}
export type AgentActivityTurnPhase =
  | "submitted"
  | "running"
  | "waiting"
  | "settling"
  | "settled";

export type AgentActivityTurnOutcome =
  | "completed"
  | "failed"
  | "canceled"
  | "interrupted";

export type AgentActivityProviderForkBindingState =
  | "bound"
  | "recovery_required"
  | "unavailable";

/**
 * Durable provenance assigned when the Turn is created. Historical Turns must
 * remain `legacy_unknown`; clients must never infer an origin from later state.
 */
export type AgentActivityTurnOrigin =
  | "user_prompt"
  | "goal_arm"
  | "goal_continuation"
  | "provider_initiated"
  | "legacy_unknown";

export interface AgentActivityCompletedCommand {
  kind: "compact" | "review" | "undo" | "goal";
  status: "completed" | "failed" | "canceled";
}

export interface AgentActivityTurn {
  agentSessionId: string;
  /** Whether the exact provider Turn binding is already durably persisted. */
  providerForkBindingAvailable?: boolean;
  /** Distinguishes a bound Turn from one that may enter on-demand recovery. */
  providerForkBindingState?: AgentActivityProviderForkBindingState;
  /** Audit-only capability provenance for the turn; never current mode state. */
  capabilityRefs?: readonly AgentActivityCapabilityReference[];
  completedCommand?: AgentActivityCompletedCommand | null;
  error?: { code?: string; message: string; detail?: string } | null;
  fileChanges?: Record<string, unknown> | null;
  outcome?: AgentActivityTurnOutcome | null;
  origin: AgentActivityTurnOrigin;
  phase: AgentActivityTurnPhase;
  sourceGoalOperationId?: string | null;
  sourceGoalRevision?: number | null;
  sourceGoalRepairEpoch?: number | null;
  settledAtUnixMs?: number | null;
  startedAtUnixMs: number;
  turnId: string;
  updatedAtUnixMs: number;
}
export interface AgentActivityInteraction {
  agentSessionId: string;
  createdAtUnixMs: number;
  input?: Record<string, unknown> | null;
  kind: "approval" | "question" | "plan";
  metadata?: Record<string, unknown> | null;
  output?: Record<string, unknown> | null;
  requestId: string;
  status: "pending" | "answered" | "superseded";
  toolName?: string | null;
  turnId: string;
  updatedAtUnixMs: number;
}

export type AgentActivitySessionSettings = {
  codexSaverMode?: boolean | null;
  rtkSaverMode?: boolean | null;
  model?: string | null;
  permissionModeId?: string | null;
  planMode?: boolean | null;
  browserUse?: boolean | null;
  computerUse?: boolean | null;
  reasoningEffort?: string | null;
  speed?: string | null;
};

export type AgentActivityPermissionModeSemantic =
  | "ask-before-write"
  | "accept-edits"
  | "locked-down"
  | "auto"
  | "full-access"
  | "unconfigurable";

export interface AgentActivitySessionPermissionModeOption {
  id: string;
  label: string;
  description?: string;
  semantic: AgentActivityPermissionModeSemantic;
}

export interface AgentActivitySessionPermissionConfig {
  configurable: boolean;
  defaultValue?: string;
  modes: AgentActivitySessionPermissionModeOption[];
}

export interface AgentActivitySessionUsage {
  contextWindow: {
    usedTokens: number;
    totalTokens: number;
  } | null;
  quotas: {
    quotaType: string;
    percentRemaining: number;
    resetsAtUnixMs: number | null;
  }[];
}

export interface AgentActivityTurnCancelResponse {
  cancel: {
    canceled: boolean;
    reason:
      | "cancel_requested"
      | "turn_canceled"
      | "already_settled"
      | "not_found";
  };
  turn?: AgentActivityTurn;
}
