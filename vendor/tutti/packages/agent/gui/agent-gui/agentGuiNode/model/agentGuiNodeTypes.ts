import type {
  AgentActivityUsage,
  AgentActivityComposerOptionsLoadStatus,
  CanonicalAgentSession,
  SessionRuntimeAvailability
} from "@tutti-os/agent-activity-core";
import type {
  AgentGUINodeData,
  AgentGUIProvider,
  AgentGUIProviderRailMode,
  AgentGUIProviderReadinessGate,
  AgentGUIAgentTarget
} from "../../../types";
import type {
  AgentGUIApprovalRequest,
  AgentGUIConversationSummary,
  AgentGUIConversationUserProject,
  AgentGUIInteractivePrompt
} from "./agentGuiConversationModel";
import type { AgentGUIConversationFilter } from "./agentGuiConversationFilter";
import type { AgentGUIConversationRailRevealRequest } from "./agentGuiConversationRailViewState";
import type {
  AgentSessionCommand,
  AgentSessionComposerSettings,
  AgentSessionPermissionConfig,
  AgentSessionReasoningEffort,
  AgentSessionSpeed
} from "../../../shared/agentSessionTypes";
import type { AgentSlashCommandPolicy } from "./agentSlashCommandProviderPolicy";
import type { AgentConversationVM } from "../../../shared/agentConversation/contracts/agentConversationVM";
import type { WorkspaceAgentSessionDetailViewModel } from "../../../shared/workspaceAgentSessionDetailViewModel";
import type { AgentPromptContentBlock } from "../../../shared/contracts/dto";
import { AGENT_PASTED_TEXT_BLOCK_KIND } from "../../../shared/pastedTextKinds";

export {
  AGENT_PASTED_TEXT_BLOCK_KIND,
  AGENT_PASTED_TEXT_MENTION_KIND
} from "../../../shared/pastedTextKinds";

export interface AgentGUISessionChrome {
  auth: {
    message: string;
  } | null;
  approval: AgentGUIApprovalRequest | null;
  recovery:
    | {
        kind:
          | "activating"
          | "failed"
          | "warning"
          | "agent-sharing-revoked"
          | "transport-connecting"
          | "transport-unavailable";
        message: string;
        canRetry?: boolean;
        followupAction?: never;
        /**
         * The recovery only gates the currently displayed interaction. Keep
         * that prompt visible while rendering the recovery as inline chrome.
         */
        interactionScoped?: boolean;
      }
    | {
        kind: "resume-unavailable";
        message: string;
        followupAction: "continue-in-new-conversation";
        canRetry?: never;
      }
    | null;
  rawState: Pick<CanonicalAgentSession, "agentSessionId"> | null;
}

export interface AgentGUIInlineNotice {
  id: string;
  message: string;
  tone: "warning" | "error";
  autoDismissMs: number | null;
}

export interface AgentGUIProjectConversationDeleteTarget {
  conversationCount: number;
  label: string;
  path: string;
}

export interface AgentGUIComposerSettingOption {
  value: string;
  label: string;
  description?: string;
  consumptionMultiplier?: string;
  supportsImageInput?: boolean;
  /** Bound plan identity for options aggregated from model access plans. */
  modelPlanId?: string | null;
  /** Display name of the plan (or provider) the option originates from. */
  sourceName?: string;
  /** When the option takes effect after selection. */
  effect?: "new_session" | "next_call";
}

export interface AgentGUIComposerModelCatalogTestimonyVM {
  /** Only an authoritative catalog may retire a remembered recent model. */
  authoritative: boolean;
  /** Provider-native model discovery is still in flight. */
  loading: boolean;
  /** Effective selected model used to recognize selected-only bootstrap echoes. */
  effectiveModel: string | null;
  /** Narrow catalog provenance needed by local recent-model reconciliation. */
  models: readonly {
    value: string;
    requested?: boolean;
  }[];
}

export interface AgentGUIComposerModelChoiceHistoryVM {
  /** Exact Agent Target identity; null fails closed and disables persistence. */
  targetId: string | null;
  /** Null until the composer has any provider-native catalog testimony. */
  catalog: AgentGUIComposerModelCatalogTestimonyVM | null;
}

export interface AgentGUIProviderSkillOption {
  name: string;
  trigger: string;
  /** Stable daemon connector key used for host-owned setup navigation. */
  connectorKey?: string;
  /** Presentation icon projected by the connector catalog. */
  iconUrl?: string;
  /** Successful installation time used only for stable composer ordering. */
  installedAtUnixMs?: number;
  /** Daemon-issued invocation contract; never infer this from provider id. */
  invocation?: "promptItem" | "textTrigger";
  sourceKind:
    | "project"
    | "personal"
    | "bundled"
    | "plugin"
    | "system"
    | "tutti-injected"
    | "connector";
  description?: string;
  pluginName?: string;
  path?: string;
  kind?: "skill" | "connector";
  status?:
    | "available"
    | "disabled"
    | "authRequired"
    | "setupRequired"
    | "unsupported";
}

export interface AgentComposerTextBlock {
  type: "text";
  text: string;
}

export interface AgentComposerImageBlock {
  type: "image";
  id: string;
  name: string;
  mimeType: "image/png" | "image/jpeg" | "image/webp";
  attachmentId?: string;
  data?: string;
  url?: string;
  path?: string;
  previewUrl: string;
  uploading?: boolean;
  uploadError?: string;
}

interface AgentComposerFileBlockBase {
  type: "file";
  id: string;
  name: string;
  mimeType?: string;
  path?: string;
  hostPath?: string;
  url?: string;
  uri?: string;
  assetId?: string;
  uploadStatus?: string;
  sizeBytes?: number;
  uploading?: boolean;
  uploadError?: string;
  uploadErrorCode?: string;
  uploadRetryable?: boolean;
}

export interface AgentComposerRegularFileBlock extends AgentComposerFileBlockBase {
  kind: "file";
  text?: never;
}

export interface AgentComposerPastedTextBlock extends AgentComposerFileBlockBase {
  kind: typeof AGENT_PASTED_TEXT_BLOCK_KIND;
  /** Empty only when a queued pasted-text attachment is restored by path. */
  text: string;
}

/** Transcript text retained as visible, unsent composer context. */
export interface AgentComposerQuoteBlock {
  type: "quote";
  id: string;
  text: string;
}

export type AgentComposerFileBlock =
  | AgentComposerRegularFileBlock
  | AgentComposerPastedTextBlock;

export type AgentComposerAttachmentBlock =
  | AgentComposerImageBlock
  | AgentComposerFileBlock;

export interface AgentComposerConnectorBlock {
  type: "connector";
  connectorKey: string;
}

export type AgentComposerSupplementaryBlock =
  | AgentComposerAttachmentBlock
  | AgentComposerQuoteBlock
  | AgentComposerConnectorBlock;

export type AgentComposerDraftBlock =
  | AgentComposerTextBlock
  | AgentComposerSupplementaryBlock;

export type AgentComposerDraftContent = [
  AgentComposerTextBlock,
  ...AgentComposerSupplementaryBlock[]
];

/** One atomic, unsent composer message. */
export type AgentComposerDraft = AgentComposerDraftContent;

export interface SubmittedDraftSnapshot {
  sourceScopeKey: string;
  content: AgentComposerDraftContent;
  /** Existing-session destination; may differ from source after recovery. */
  targetAgentSessionId?: string;
}

/** UI aliases retained for focused attachment components. */
export type AgentComposerDraftImage = Omit<AgentComposerImageBlock, "type">;
export type AgentComposerDraftFile = Omit<
  AgentComposerRegularFileBlock,
  "type" | "kind"
>;
export type AgentComposerDraftLargeText = Omit<
  AgentComposerPastedTextBlock,
  "type" | "kind"
>;
export type AgentComposerDraftConnector = Omit<
  AgentComposerConnectorBlock,
  "type"
>;

/**
 * Built-in glyph for a home-suggestion category chip. Keeps the localized data
 * free of any component references so it can live in the i18n label bundle.
 */
export type AgentHomeSuggestionIcon =
  | "write"
  | "code"
  | "research"
  | "handoff"
  | "breakdown"
  | "review"
  | "interaction"
  | "github"
  | "about"
  | "import";

/**
 * Host-level action a chip can trigger. A chip may carry an action alongside a
 * `prompt` — both fire on click (the prompt fills, then the action runs).
 * `import-session` opens the external-agent session import wizard.
 */
export type AgentHomeSuggestionAction = "import-session";

export interface AgentHomeSuggestionItem {
  id: string;
  /** Text shown in the suggestion row. */
  label: string;
  /**
   * Prompt inserted into the composer when the suggestion is chosen. Defaults
   * to `label` when omitted so short labels can double as the prompt.
   */
  prompt?: string;
}

export interface AgentHomeSuggestionCategory {
  id: string;
  /** Chip / card header label. */
  label: string;
  icon?: AgentHomeSuggestionIcon;
  /**
   * Suggestions revealed in an expandable card when the chip is chosen. Omit
   * (or leave empty) for a direct-fill chip that uses `prompt` instead.
   */
  items?: AgentHomeSuggestionItem[];
  /**
   * When set, the chip fills the composer with this prompt immediately on click
   * instead of expanding a card of `items`.
   */
  prompt?: string;
  /**
   * When set, the chip triggers a host action on click instead of filling the
   * composer or expanding `items`.
   */
  action?: AgentHomeSuggestionAction;
}

export interface AgentGUIComposerModelPlanVM {
  id: string;
  name: string;
  protocol?: string | null;
}

export interface AgentGUIComposerSettingsVM {
  sessionSettings: AgentSessionComposerSettings | null;
  draftSettings: {
    codexSaverMode?: boolean;
    rtkSaverMode?: boolean;
    model: string | null;
    reasoningEffort: AgentSessionReasoningEffort | null;
    speed: AgentSessionSpeed | null;
    planMode: boolean;
    // Optional like permissionModeId: the controller always sets it, fixtures
    // and consumers default an unset value to on.
    browserUse?: boolean;
    computerUse?: boolean;
    permissionModeId?: string | null;
  };
  supportsModel: boolean;
  supportsCodexSaverMode?: boolean;
  supportsRTKSaverMode?: boolean;
  supportsReasoningEffort: boolean;
  supportsSpeed: boolean;
  supportsPermissionMode?: boolean;
  supportsPlanMode: boolean;
  // Descriptor-derived plan/permission exclusivity.
  planExclusiveWithPermissionMode?: boolean;
  supportsBrowser?: boolean;
  supportsComputerUse?: boolean;
  permissionModeChangeDuringTurn?: boolean;
  slashCommandPolicy?: AgentSlashCommandPolicy | null;
  isSettingsLoading: boolean;
  /** Terminal composer-options failure with no cached catalog to render. */
  composerOptionsError?: boolean;
  /** Activity-core request lifecycle for the target-scoped options catalog. */
  composerOptionsLoadStatus?: AgentActivityComposerOptionsLoadStatus;
  /** Initial slash command and capability catalog request is in flight. */
  isCapabilityOptionsLoading?: boolean;
  /** Local Connector Market projection is being loaded or refreshed. */
  isConnectorOptionsLoading?: boolean;
  isModelOptionsLoading?: boolean;
  /** Device-local model recents/favorites identity and catalog testimony. */
  modelChoiceHistory?: AgentGUIComposerModelChoiceHistoryVM;
  modelUnavailable: boolean;
  reasoningUnavailable: boolean;
  speedUnavailable: boolean;
  permissionModeUnavailable?: boolean;
  selectedModelValue?: string | null;
  /** Actual provider-resolved model while selectedModelValue remains inherited. */
  effectiveModelValue?: string | null;
  selectedReasoningEffortValue?: AgentSessionReasoningEffort | null;
  selectedSpeedValue?: AgentSessionSpeed | null;
  selectedPermissionModeValue?: string | null;
  permissionConfig?: AgentSessionPermissionConfig | null;
  selectedProjectPath?: string | null;
  /** Persisted rail membership used to scope Agent-generated file mentions. */
  selectedProjectSectionKey?: string | null;
  /** Resolve the durable default only before the home project intent is known. */
  shouldApplyPreparedProjectSelection?: boolean;
  projectLocked?: boolean;
  // Mirrors the injected runtime's `projectPathIsRemote`. When true the session
  // cwd is not on the local filesystem (e.g. a shared/cloud sandbox), so the
  // local "working directory missing" existence check is skipped. Project
  // selection/listing stays available. Absent/false => local (legacy behaviour).
  projectPathIsRemote?: boolean;
  // Collapse the model list to the latest version per model family (providers
  // whose live lists span many vendors and versions, e.g. Cursor). The
  // currently selected model always stays visible even if older.
  collapseModelOptionsToLatest?: boolean;
  // Bound model access plan for the composer target, projected from the
  // daemon composer options `runtimeContext.modelPlan`. Absent/null when the
  // target keeps its provider-native model source.
  modelPlan?: AgentGUIComposerModelPlanVM | null;
  // Active session with a provider advertising the "modelSwitch" capability:
  // a new model pick applies from the next request, so the model menu shows
  // the switch-effect footer hint.
  modelSwitchTakesEffectNextTurn?: boolean;
  availableModels: AgentGUIComposerSettingOption[];
  availableReasoningEfforts: AgentGUIComposerSettingOption[];
  availableSpeeds: AgentGUIComposerSettingOption[];
  availablePermissionModes?: AgentGUIComposerSettingOption[];
}

export interface AgentGUIQueuedPromptVM {
  id: string;
  content: AgentPromptContentBlock[];
  /** 仅展示用文本(bundle 折叠成一个 chip);content 仍带展开后的文件。 */
  displayPrompt?: string;
  createdAtUnixMs: number;
}

export type AgentGUIQueueStatus = "active" | "paused_by_user";

export interface AgentGUIShellViewModel {
  nodeId?: string | null;
  workspaceId: string;
  workspacePath?: string | null;
  currentUserId?: string | null;
  data: AgentGUINodeData;
}

export interface AgentGUIRailViewModel {
  selectedAgentTarget: AgentGUIAgentTarget;
  agentTargets: readonly AgentGUIAgentTarget[];
  agentTargetsLoading: boolean;
  /** How the rail composes its list — "exact" renders targets verbatim with no static injection. */
  providerRailMode: AgentGUIProviderRailMode;
  /** Providers gated by the host (feature-gated) — rail renders coming-soon placeholders. */
  comingSoonProviders: readonly AgentGUIProvider[];
  conversationFilter: AgentGUIConversationFilter;
  conversations: AgentGUIConversationSummary[];
  userProjects: AgentGUIConversationUserProject[];
  activeConversation: AgentGUIConversationSummary | null;
  activeConversationId: string | null;
  revealRequest: AgentGUIConversationRailRevealRequest | null;
  isLoadingConversations: boolean;
  listError: string | null;
}

export interface AgentGUIDetailViewModel {
  availability: "loading" | "ready" | "not_found" | "error";
  isLoadingMessages: boolean;
  isLoadingOlderMessages: boolean;
  hasOlderMessages: boolean;
  usage: AgentActivityUsage | null;
  hasSentUserMessage: boolean;
  avoidGroupingEdits: boolean;
  conversation?: AgentConversationVM | null;
  conversationDetail: WorkspaceAgentSessionDetailViewModel | null;
}

export type AgentGUIRuntimeBlockedReason = Extract<
  SessionRuntimeAvailability,
  { state: "blocked" }
>["reason"];

export type AgentGUIComposerRuntimeGate =
  | {
      status: "ready";
      reason: null;
      sessionRuntimeReason: null;
    }
  | {
      status: "blocked";
      reason: "target_connection";
      sessionRuntimeReason: null;
    }
  | {
      status: "blocked";
      reason: "session_runtime";
      sessionRuntimeReason: AgentGUIRuntimeBlockedReason;
    };

export type AgentGUIComposerEditorBlockedReason =
  | "collaborator_read_only"
  | "creating_conversation"
  | "interrupting"
  | "non_retryable_recovery"
  | "pending_approval"
  | "pending_interactive_prompt"
  | "provider_readiness"
  | "runtime_blocked"
  | "submitting";

export type AgentGUIComposerSubmissionBlockedReason =
  | AgentGUIComposerEditorBlockedReason
  | "activation_failed"
  | "activation_pending"
  | "agent_targets_loading"
  | "authentication_required"
  | "conversation_busy"
  | "resume_unavailable"
  | "settings_update_pending";

export interface AgentGUIComposerGate {
  /** Canonical busy projection captured with the same gate snapshot. */
  conversationBusy: boolean;
  /** A submitted prompt is waiting for its canonical Turn to appear. */
  isAwaitingTurnStart?: boolean;
  /**
   * Runtime-dependent command availability used by Composer-adjacent
   * controls such as Stop and interactive responses.
   */
  runtime: AgentGUIComposerRuntimeGate;
  editor:
    | {
        status: "editable";
        reason: null;
      }
    | {
        status: "blocked";
        reason: AgentGUIComposerEditorBlockedReason;
      };
  submission:
    | {
        status: "ready";
        reason: null;
      }
    | {
        status: "queue";
        reason: "conversation_busy";
      }
    | {
        status: "blocked";
        reason: AgentGUIComposerSubmissionBlockedReason;
      };
}

export interface AgentGUIComposerViewModel {
  handoffAgentTargets: readonly AgentGUIAgentTarget[];
  availableCommands: AgentSessionCommand[];
  availableSkills: AgentGUIProviderSkillOption[];
  draftPrompt: string;
  draftContent: AgentComposerDraft;
  isCreatingConversation: boolean;
  isSubmitting: boolean;
  isInterrupting: boolean;
  isCancelPending: boolean;
  /** The Engine can stop a pending prompt before its Turn is visible. */
  hasPendingSubmitStopTarget?: boolean;
  promptImagesSupported: boolean;
  compactSupported: boolean | null;
  gate: AgentGUIComposerGate;
  composerSettings: AgentGUIComposerSettingsVM;
  queuedPrompts: AgentGUIQueuedPromptVM[];
  queueStatus: AgentGUIQueueStatus;
  drainingQueuedPromptId: string | null;
}

export interface AgentGUIInteractionViewModel {
  approvalDisabledReason: string | null;
  /** The visible prompt is the one exact pending question Composer can answer. */
  canAnswerPendingInteractivePromptFromComposer?: boolean;
  interactivePromptDisabledReason: string | null;
  isRespondingApproval: boolean;
  isRespondingInteractivePrompt: boolean;
  pendingApproval: AgentGUIApprovalRequest | null;
  pendingInteractivePrompt: AgentGUIInteractivePrompt | null;
  sessionChrome: AgentGUISessionChrome;
  inlineNotice: AgentGUIInlineNotice | null;
}

export interface AgentGUIReadinessViewModel {
  activeLiveState: "inactive" | "activating" | "active" | "failed";
  activationError: string | null;
  providerReadinessGate: AgentGUIProviderReadinessGate | null;
}

export interface AgentGUIOperationsViewModel {
  forkThroughTurnPendingTurnIds: readonly string[];
  isDeletingConversation: boolean;
  isUserProjectMutationPending: boolean;
  pendingDeleteConversation: AgentGUIConversationSummary | null;
  pendingDeleteProjectConversations: AgentGUIProjectConversationDeleteTarget | null;
}

export interface AgentGUINodeViewModel {
  shell: AgentGUIShellViewModel;
  rail: AgentGUIRailViewModel;
  detail: AgentGUIDetailViewModel;
  composer: AgentGUIComposerViewModel;
  interaction: AgentGUIInteractionViewModel;
  readiness: AgentGUIReadinessViewModel;
  operations: AgentGUIOperationsViewModel;
}
