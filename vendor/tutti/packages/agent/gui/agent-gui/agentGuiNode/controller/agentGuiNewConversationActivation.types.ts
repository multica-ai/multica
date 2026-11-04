import { type AgentSessionEngine } from "@tutti-os/agent-activity-core";
import { type Dispatch, type RefObject, type SetStateAction } from "react";
import type { AgentGUIRuntime } from "../../../agentActivityRuntime";
import { type AgentGUIConversationListQuery } from "../../../contexts/workspace/presentation/renderer/agentGuiConversationList/useAgentGuiConversationList";
import type {
  AgentSessionComposerSettings,
  AgentSessionState
} from "../../../shared/agentSessionTypes";
import type { AgentGUINodeData, AgentGUIAgentTarget } from "../../../types";
import { type AgentGUIConversationSummary } from "../model/agentGuiConversationModel";
import type {
  AgentComposerDraft,
  SubmittedDraftSnapshot
} from "../model/agentGuiNodeTypes";
import { type AgentGUIComposerTargetData } from "./agentGuiController.composerPresentation";
import type { useAgentGUIActivation } from "./useAgentGUIActivation";
import type { ConversationIntent } from "./useAgentConversationSelection";
import type { AgentGUIConversationRailRevealReason } from "../model/agentGuiConversationRailViewState";

export interface UseAgentGUINewConversationActivationInput {
  codexSaverModeEntryEnabled: boolean;
  rtkSaverModeEntryEnabled: boolean;
  getCachedComposerOptions: () =>
    | import("@tutti-os/agent-activity-core").AgentActivityComposerOptions
    | null;
  selectedAgentTargetRef: RefObject<AgentGUIAgentTarget>;
  selectedComposerTargetDataRef: RefObject<AgentGUIComposerTargetData>;
  agentTargetsProvidedRef: RefObject<boolean>;
  selectedAgentTargetIsExplicitRef: RefObject<boolean>;
  setDetailError: Dispatch<SetStateAction<string | null>>;
  isCreatingConversationRef: RefObject<boolean>;
  onDataChangeRef: RefObject<
    (updater: (current: AgentGUINodeData) => AgentGUINodeData) => void
  >;
  selectedProjectPathRef: RefObject<string | null>;
  userProjectsRef: RefObject<
    import("../../../host/agentHostApi").AgentHostUserProject[]
  >;
  draftByScopeKeyRef: RefObject<Record<string, AgentComposerDraft>>;
  submittedDraftSnapshotsRef: RefObject<Record<string, SubmittedDraftSnapshot>>;
  draftSettingsBySessionIdRef: RefObject<
    Record<string, AgentSessionComposerSettings>
  >;
  agentActivityRuntime: AgentGUIRuntime;
  workspaceId: string;
  activeConversationIdRef: RefObject<string | null>;
  isComposerHomeRef: RefObject<boolean>;
  conversationsRef: RefObject<AgentGUIConversationSummary[]>;
  activeSessionState: AgentSessionState | null;
  sessionEngine: AgentSessionEngine;
  conversationListQuery: AgentGUIConversationListQuery | null;
  currentUserId: string | null | undefined;
  requestRailReveal(
    agentSessionId: string,
    reason: AgentGUIConversationRailRevealReason
  ): void;
  setActiveConversationId: Dispatch<SetStateAction<string | null>>;
  setIntent: Dispatch<SetStateAction<ConversationIntent>>;
  setIsComposerHome: Dispatch<SetStateAction<boolean>>;
  setIsLoadingMessages: Dispatch<SetStateAction<boolean>>;
  activation: ReturnType<typeof useAgentGUIActivation>;
  isCurrentConversation: (agentSessionId: string) => boolean;
  isConversationStale: (agentSessionId: string) => boolean;
  data: AgentGUINodeData;
  defaultReasoningEffort: string;
}

export interface AgentGUINewConversationActivationResult {
  agentSessionId: string;
  requestId: string;
}
