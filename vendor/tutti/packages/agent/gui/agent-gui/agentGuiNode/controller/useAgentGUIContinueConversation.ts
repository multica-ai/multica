import type { PendingActivationIntentRecord } from "@tutti-os/agent-activity-core";
import { useCallback, useRef, type Dispatch, type SetStateAction } from "react";
import type { ConversationServices } from "../../../conversationServices";
import { translate } from "../../../i18n/index";
import type { AgentHostAccountUserProfile } from "../../../shared/contracts/dto";
import {
  agentComposerDraftPrompt,
  emptyAgentComposerDraft,
  updateAgentComposerDraft
} from "../model/agentComposerDraft";
import { resolveAgentComposerDraftScopeKey } from "../model/agentComposerDraftScope";
import type { AgentGUIConversationSummary } from "../model/agentGuiConversationModel";
import type { AgentGUIConversationUserProject } from "../model/agentGuiConversationProjectResolver";
import type { AgentComposerDraft } from "../model/agentGuiNodeTypes";
import { resolveAgentGUIExplicitConversationTitle } from "../model/agentGuiProviderIdentity";
import { buildContinueInNewConversationPrompt } from "./agentGuiController.conversationHelpers";
import { reportAgentGUIActiveConversationCleared } from "./agentGuiController.reporting";
import { resolveAgentGUINewConversationProjectSelection } from "./agentGuiNewConversationRequest";
import {
  resolveConversationSummaryById,
  type ConversationIntent
} from "./useAgentConversationSelection";
import { isPendingNewConversationActivationForSession } from "./useAgentGUIActivation";

interface CurrentValue<T> {
  current: T;
}

interface UseAgentGUIContinueConversationInput {
  accountProfilesByUserId: Readonly<
    Record<string, AgentHostAccountUserProfile>
  >;
  activeConversationIdRef: CurrentValue<string | null>;
  activePendingActivation: PendingActivationIntentRecord | null;
  agentActivityRuntime: ConversationServices;
  conversations: readonly AgentGUIConversationSummary[];
  currentUserId: string | null | undefined;
  draftByScopeKey: Record<string, AgentComposerDraft>;
  isComposerHomeRef: CurrentValue<boolean>;
  loadDraftComposerOptions(): void;
  persistActiveConversation(agentSessionId: string | null): void;
  setActiveConversationId: Dispatch<SetStateAction<string | null>>;
  setDetailError: Dispatch<SetStateAction<string | null>>;
  setDraftByScopeKey: Dispatch<
    SetStateAction<Record<string, AgentComposerDraft>>
  >;
  setIntent: Dispatch<SetStateAction<ConversationIntent>>;
  setIsComposerHome: Dispatch<SetStateAction<boolean>>;
  setIsLoadingMessages: Dispatch<SetStateAction<boolean>>;
  selectedProjectPathRef: CurrentValue<string | null>;
  setSelectedProjectPath: Dispatch<SetStateAction<string | null>>;
  transientConversation: AgentGUIConversationSummary | null;
  unactivate(agentSessionId: string): Promise<void>;
  userProjectsRef: CurrentValue<readonly AgentGUIConversationUserProject[]>;
  workspaceId: string;
}

export function buildContinueInNewConversationDraft(input: {
  sourceDraft: AgentComposerDraft;
  prompt: string;
}): AgentComposerDraft {
  return updateAgentComposerDraft(input.sourceDraft, { prompt: input.prompt });
}

export function useAgentGUIContinueConversation(
  input: UseAgentGUIContinueConversationInput
) {
  const inputRef = useRef(input);
  inputRef.current = input;
  return useCallback(() => {
    const current = inputRef.current;
    const currentConversationId = current.activeConversationIdRef.current;
    if (!currentConversationId) return;
    const projectSelection = resolveAgentGUINewConversationProjectSelection({
      activeConversationId: currentConversationId,
      conversations: current.conversations,
      transientConversation: current.transientConversation,
      userProjects: current.userProjectsRef.current
    });
    if (projectSelection.kind !== "replace") {
      current.setDetailError(
        translate("agentHost.agentGui.sessionActivationFailed")
      );
      return;
    }
    const activeConversation = resolveConversationSummaryById(
      current.conversations,
      currentConversationId,
      current.transientConversation
    );
    if (!activeConversation) return;
    const sourceDraftScopeKey = resolveAgentComposerDraftScopeKey({
      agentSessionId: currentConversationId
    });
    const sourceDraft =
      current.draftByScopeKey[sourceDraftScopeKey] ?? emptyAgentComposerDraft();
    const nextDraftPrompt = buildContinueInNewConversationPrompt({
      workspaceId: current.workspaceId,
      agentSessionId: activeConversation.id,
      conversationUserId: activeConversation.userId,
      currentUserId: current.currentUserId,
      userProfilesByUserId: current.accountProfilesByUserId,
      provider: activeConversation.provider,
      agentTargetId: activeConversation.agentTargetId,
      conversationTitle:
        resolveAgentGUIExplicitConversationTitle(activeConversation) ??
        translate("agentHost.workspaceAgentsUntitledConversation"),
      existingDraftPrompt: agentComposerDraftPrompt(sourceDraft)
    });
    reportAgentGUIActiveConversationCleared({
      details: { sourceConversationId: activeConversation.id },
      previousAgentSessionId: currentConversationId,
      reason: "continue_in_new_conversation",
      runtime: current.agentActivityRuntime,
      workspaceId: current.workspaceId
    });
    if (
      !isPendingNewConversationActivationForSession(
        current.activePendingActivation,
        currentConversationId
      )
    ) {
      void current.unactivate(currentConversationId);
    }
    current.setIntent({ tag: "home" });
    current.selectedProjectPathRef.current = projectSelection.projectPath;
    current.setSelectedProjectPath(projectSelection.projectPath);
    current.isComposerHomeRef.current = true;
    current.setIsComposerHome(true);
    current.activeConversationIdRef.current = null;
    current.setActiveConversationId(null);
    current.setIsLoadingMessages(false);
    current.setDetailError(null);
    current.setDraftByScopeKey((drafts) => ({
      ...drafts,
      [resolveAgentComposerDraftScopeKey({})]:
        buildContinueInNewConversationDraft({
          sourceDraft,
          prompt: nextDraftPrompt
        })
    }));
    current.persistActiveConversation(null);
    current.loadDraftComposerOptions();
  }, []);
}
