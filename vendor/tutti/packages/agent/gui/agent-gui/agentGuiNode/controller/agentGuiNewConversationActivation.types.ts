import type { AgentConversationServices } from "../../../state/conversation-services";

import { type Dispatch, type RefObject, type SetStateAction } from "react";
import { type AgentGUIConversationListQuery } from "../../../contexts/workspace/presentation/renderer/agentGuiConversationList/useAgentGuiConversationList";
import type { ConversationServices } from "../../../conversationServices";
import type {
  AgentSessionComposerSettings,
  AgentSessionState
} from "../../../shared/agentSessionTypes";
import type { AgentGUIAgentTarget, AgentGUINodeData } from "../../../types";
import { type AgentGUIConversationSummary } from "../model/agentGuiConversationModel";
import type { AgentGUIConversationRailRevealReason } from "../model/agentGuiConversationRailViewState";
import type {
  AgentComposerDraft,
  SubmittedDraftSnapshot
} from "../model/agentGuiNodeTypes";
import { type AgentGUIComposerTargetData } from "./agentGuiController.composerPresentation";
import type { ConversationIntent } from "./useAgentConversationSelection";
import type { useAgentGUIActivation } from "./useAgentGUIActivation";

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
  agentActivityRuntime: ConversationServices;
  workspaceId: string;
  activeConversationIdRef: RefObject<string | null>;
  isComposerHomeRef: RefObject<boolean>;
  conversationsRef: RefObject<AgentGUIConversationSummary[]>;
  activeSessionState: AgentSessionState | null;
  conversationServices: AgentConversationServices;
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
  ready?: Promise<boolean>;
  agentSessionId: string;
  requestId: string;
}
