import {
  useCallback,
  useState,
  type Dispatch,
  type RefObject,
  type SetStateAction
} from "react";
import { flushSync } from "react-dom";
import type { useAgentHostApi } from "../../../agentActivityHost";
import type { AgentSessionViewRef } from "../../../contexts/workspace/presentation/renderer/agentSessions/useAgentSessionPagingState";
import type { ConversationServices } from "../../../conversationServices";
import { resolveAgentComposerDraftScopeKey } from "../model/agentComposerDraftScope";
import { type AgentGUIConversationSummary } from "../model/agentGuiConversationModel";
import type {
  AgentComposerDraft,
  SubmittedDraftSnapshot
} from "../model/agentGuiNodeTypes";
import { deleteSubmittedDraftSnapshotsForScopes } from "./agentGuiController.draftMessageHelpers";
import { getAgentGUIErrorMessage } from "./agentGuiController.errors";
import {
  reportConversationServicesError,
  showAgentGUIControllerErrorToast
} from "./agentGuiController.reporting";
import {
  parseTuttiProtectedDeletionConflict,
  type TuttiProtectedDeletionConflict
} from "./tuttiProtectedDeletion";
import { type ConversationIntent } from "./useAgentConversationSelection";
import type { useAgentGUIActivation } from "./useAgentGUIActivation";

export interface UseAgentGUIConversationDeletionInput {
  isDeletingConversation: boolean;
  conversations: AgentGUIConversationSummary[];
  setPendingDeleteConversation: Dispatch<
    SetStateAction<AgentGUIConversationSummary | null>
  >;
  setDetailError: Dispatch<SetStateAction<string | null>>;
  pendingDeleteConversation: AgentGUIConversationSummary | null;
  setIsDeletingConversation: Dispatch<SetStateAction<boolean>>;
  activeConversationIdRef: RefObject<string | null>;
  setIsLoadingMessages: Dispatch<SetStateAction<boolean>>;
  sessionViewRef: (agentSessionId: string | null | undefined) => {
    workspaceId: string;
    agentSessionId: string | null | undefined;
    origin: string;
  };
  activation: ReturnType<typeof useAgentGUIActivation>;
  agentActivityRuntime: ConversationServices;
  setDraftByScopeKey: Dispatch<
    SetStateAction<Record<string, AgentComposerDraft>>
  >;
  submittedDraftSnapshotsRef: RefObject<Record<string, SubmittedDraftSnapshot>>;
  deleteAgentSessionView: (ref: AgentSessionViewRef) => void;
  setIntent: Dispatch<SetStateAction<ConversationIntent>>;
  setActiveConversationId: Dispatch<SetStateAction<string | null>>;
  persistActiveConversation: (agentSessionId: string | null) => void;
  removeConversations: (agentSessionIds: readonly string[]) => void;
  agentHostApi: ReturnType<typeof useAgentHostApi>;
  workspaceId: string;
}

export function useAgentGUIConversationDeletion(
  input: UseAgentGUIConversationDeletionInput
) {
  const [protectedDeleteConflict, setProtectedDeleteConflict] =
    useState<TuttiProtectedDeletionConflict | null>(null);
  const {
    isDeletingConversation,
    conversations,
    setPendingDeleteConversation,
    setDetailError,
    pendingDeleteConversation,
    setIsDeletingConversation,
    activeConversationIdRef,
    setIsLoadingMessages,
    sessionViewRef,
    activation,
    agentActivityRuntime,
    setDraftByScopeKey,
    submittedDraftSnapshotsRef,
    deleteAgentSessionView,
    setIntent,
    setActiveConversationId,
    persistActiveConversation,
    removeConversations,
    agentHostApi,
    workspaceId
  } = input;
  const requestDeleteConversation = useCallback(
    (agentSessionId: string) => {
      const normalized = agentSessionId.trim();
      if (!normalized || isDeletingConversation) {
        return;
      }
      const conversation = conversations.find(
        (candidate) => candidate.id === normalized
      );
      if (!conversation) {
        return;
      }
      setPendingDeleteConversation(conversation);
      setDetailError(null);
    },
    [conversations, isDeletingConversation]
  );

  const cancelDeleteConversation = useCallback(() => {
    if (isDeletingConversation) {
      return;
    }
    setPendingDeleteConversation(null);
  }, [isDeletingConversation]);

  const confirmDeleteConversation = useCallback(() => {
    const target = pendingDeleteConversation;
    if (!target || isDeletingConversation) {
      return;
    }
    setIsDeletingConversation(true);
    setDetailError(null);
    void agentActivityRuntime.mutations
      .deleteSessions({ agentSessionIds: [target.id] })
      .then(() => {
        if (activeConversationIdRef.current === target.id) {
          activeConversationIdRef.current = null;
          flushSync(() => {
            setIsLoadingMessages(false);
            setIntent({ tag: "home" });
            setActiveConversationId(null);
          });
          persistActiveConversation(null);
        }
        return activation.unactivate(target.id);
      })
      .then(() => {
        removeConversations([target.id]);
        const deletedScopeKey = resolveAgentComposerDraftScopeKey({
          agentSessionId: target.id
        });
        setDraftByScopeKey((current) => {
          const next = { ...current };
          delete next[deletedScopeKey];
          return next;
        });
        deleteSubmittedDraftSnapshotsForScopes({
          snapshots: submittedDraftSnapshotsRef.current,
          scopeKeys: new Set([deletedScopeKey]),
          targetAgentSessionIds: new Set([target.id])
        });
        deleteAgentSessionView(sessionViewRef(target.id));
        setPendingDeleteConversation(null);
      })
      .catch((error) => {
        const conflict = parseTuttiProtectedDeletionConflict(error);
        if (conflict) {
          setProtectedDeleteConflict(conflict);
          return;
        }
        const message = getAgentGUIErrorMessage(error);
        reportConversationServicesError({
          agentSessionId: target.id,
          error,
          phase: "delete_conversation",
          provider: target.provider,
          runtime: agentActivityRuntime,
          workspaceId
        });
        showAgentGUIControllerErrorToast(agentHostApi.toast, message);
      })
      .finally(() => {
        setIsDeletingConversation(false);
      });
  }, [
    activation,
    activeConversationIdRef,
    agentActivityRuntime,
    agentHostApi.toast,
    deleteAgentSessionView,
    isDeletingConversation,
    pendingDeleteConversation,
    persistActiveConversation,
    removeConversations,
    sessionViewRef,
    setActiveConversationId,
    setDetailError,
    setDraftByScopeKey,
    setIntent,
    setIsDeletingConversation,
    setIsLoadingMessages,
    setPendingDeleteConversation,
    submittedDraftSnapshotsRef,
    workspaceId
  ]);

  return {
    requestDeleteConversation,
    cancelDeleteConversation,
    confirmDeleteConversation,
    protectedDeleteConflict,
    dismissProtectedDeleteConflict: () => setProtectedDeleteConflict(null)
  };
}
