// Agent GUI controller — shared TypeScript types for the controller hook.

import type { AgentSessionComposerSettings } from "../../../shared/agentSessionTypes";
import type {
  AgentGUIAgentTarget,
  AgentGUIInteractionReadinessSource,
  AgentGUINodeData,
  AgentGUIObservationGapSource,
  AgentGUIProvider,
  AgentGUIProviderRailMode,
  AgentGUIProviderReadinessGate,
  AgentGUITargetConnectionSource
} from "../../../types";
import type { AgentGUIComposerSettingOption } from "../model/agentGuiNodeTypes";
import type { AgentGUIOpenSessionRequest } from "./agentGuiController.draftMessageHelpers";
import type {
  AgentGUIRememberComposerDefaultsInput,
  AgentGUIRememberComposerDefaultsResult
} from "./agentGuiController.providerHelpers";
import type { AgentGUIComposerAppendRequest } from "./useAgentGUIComposerAppendRequest";
import type { AgentGUIPrefillPromptRequest } from "./useAgentGUIConversationHome";

export type ConversationServicesErrorPhase =
  | "create_conversation"
  | "interrupt_current_turn"
  | "load_session_state"
  | "retry_activation"
  | "send_prompt"
  | "submit_interactive"
  | "toggle_conversation_pinned"
  | "rename_conversation"
  | "delete_conversation"
  | "update_session_settings"
  | "warmup_openclaw_gateway";

export interface QueuedPromptRetryBlock {
  queuedPromptId: string;
  sessionStateUpdatedAtUnixMs: number | null;
  conversationUpdatedAtUnixMs: number | null;
}

export interface QueuedComposerSettingsUpdate {
  sessionSettingsPatch: AgentSessionComposerSettings;
}

export interface ACPConfigOptionSelection {
  options: AgentGUIComposerSettingOption[];
  currentValue: string | null;
}
export interface AgentGUIPageInput {
  nodeId?: string;
  isSurfaceActive: boolean;
  isSurfaceVisible: boolean;
  workspaceId: string;
  currentUserId?: string | null;
  workspacePath: string;
  avoidGroupingEdits: boolean;
  data: AgentGUINodeData;
  agentTargets?: readonly AgentGUIAgentTarget[];
  agentTargetsLoading?: boolean;
  handoffAgentTargets?: readonly AgentGUIAgentTarget[];
  handoffAgentTargetsLoading?: boolean;
  providerRailMode?: AgentGUIProviderRailMode;
  comingSoonProviders?: readonly AgentGUIProvider[];
  providerReadinessGates?: Partial<
    Record<AgentGUIProvider, AgentGUIProviderReadinessGate | null>
  > | null;
  targetConnectionSource?: AgentGUITargetConnectionSource | null;
  interactionReadinessSource?: AgentGUIInteractionReadinessSource | null;
  observationGapSource?: AgentGUIObservationGapSource | null;
  defaultAgentTargetId?: string | null;
  composerAppendRequest?: AgentGUIComposerAppendRequest | null;
  openSessionRequest?: AgentGUIOpenSessionRequest | null;
  prefillPromptRequest?: AgentGUIPrefillPromptRequest | null;
  codexSaverModeEntryEnabled?: boolean;
  rtkSaverModeEntryEnabled?: boolean;
  onDataChange: (
    updater: (current: AgentGUINodeData) => AgentGUINodeData
  ) => void;
  onComposerAppendHandled?: (sequence: number) => void;
  onRememberComposerDefaults?: (
    input: AgentGUIRememberComposerDefaultsInput
  ) => void | Promise<AgentGUIRememberComposerDefaultsResult>;
  onShowMessage?: (
    message: string,
    tone?: "info" | "warning" | "error"
  ) => void;
}
