import { useComposerDrafts } from "@multica/tutti-agent-gui-host/composer-drafts";
import {
  useCallback,
  useRef,
  useState,
  type Dispatch,
  type SetStateAction
} from "react";
import type { AgentHostUserProject } from "../../../host/agentHostApi";
import type { AgentSessionComposerSettings } from "../../../shared/agentSessionTypes";
import type { AgentGUINodeData } from "../../../types";
import type { AgentGUIConversationSummary } from "../model/agentGuiConversationModel";
import type {
  AgentGUIConversationRailRevealReason,
  AgentGUIConversationRailRevealRequest
} from "../model/agentGuiConversationRailViewState";
import type {
  AgentComposerDraft,
  AgentGUIProjectConversationDeleteTarget,
  SubmittedDraftSnapshot
} from "../model/agentGuiNodeTypes";
import {
  readAgentGUIUserProjectMutationPending,
  readAgentGUIUserProjectSnapshot
} from "./agentGuiController.interactiveHelpers";
import type { ConversationIntent } from "./useAgentConversationSelection";

interface UseAgentGUILocalStateInput {
  data: AgentGUINodeData;
  userProjectsApi: Parameters<typeof readAgentGUIUserProjectSnapshot>[0];
}

type AgentGUIHomeProjectSelection =
  | { kind: "unresolved_default" }
  | { kind: "resolved"; projectPath: string | null };

export function useAgentGUILocalState({
  data,
  userProjectsApi
}: UseAgentGUILocalStateInput) {
  const [userProjects, setUserProjects] = useState<AgentHostUserProject[]>(() =>
    readAgentGUIUserProjectSnapshot(userProjectsApi)
  );
  const [isUserProjectMutationPending, setIsUserProjectMutationPending] =
    useState(() => readAgentGUIUserProjectMutationPending(userProjectsApi));
  const [activeConversationId, setActiveConversationId] = useState<
    string | null
  >(data.lastActiveAgentSessionId);
  const [intent, setIntent] = useState<ConversationIntent>(() =>
    data.lastActiveAgentSessionId
      ? { tag: "requested", id: data.lastActiveAgentSessionId }
      : { tag: "home" }
  );
  const [homeProjectSelection, setHomeProjectSelection] =
    useState<AgentGUIHomeProjectSelection>({ kind: "unresolved_default" });
  const selectedProjectPath =
    homeProjectSelection.kind === "resolved"
      ? homeProjectSelection.projectPath
      : null;
  const setSelectedProjectPath = useCallback<
    Dispatch<SetStateAction<string | null>>
  >((nextProjectPath) => {
    setHomeProjectSelection((current) => {
      const currentProjectPath =
        current.kind === "resolved" ? current.projectPath : null;
      const projectPath =
        typeof nextProjectPath === "function"
          ? nextProjectPath(currentProjectPath)
          : nextProjectPath;
      return { kind: "resolved", projectPath };
    });
  }, []);
  const [isComposerHome, setIsComposerHome] = useState(
    data.lastActiveAgentSessionId === null
  );
  const [draftByScopeKey, setDraftByScopeKey] = useComposerDrafts();
  const submittedDraftSnapshotsRef = useRef<
    Record<string, SubmittedDraftSnapshot>
  >({});
  const [draftSettingsBySessionId, setDraftSettingsBySessionId] = useState<
    Record<string, AgentSessionComposerSettings>
  >({});
  const [isLoadingMessages, setIsLoadingMessages] = useState(false);
  const [isDeletingConversation, setIsDeletingConversation] = useState(false);
  const [isDeletingProjectConversations, setIsDeletingProjectConversations] =
    useState(false);
  const [pendingDeleteConversation, setPendingDeleteConversation] =
    useState<AgentGUIConversationSummary | null>(null);
  const [
    pendingDeleteProjectConversations,
    setPendingDeleteProjectConversations
  ] = useState<AgentGUIProjectConversationDeleteTarget | null>(null);
  const [listError, setListError] = useState<string | null>(null);
  const [detailError, setDetailError] = useState<string | null>(null);
  const [goalClearNoticeSequence, setGoalClearNoticeSequence] = useState(0);
  const railRevealRevisionRef = useRef(0);
  const [railRevealRequest, setRailRevealRequest] =
    useState<AgentGUIConversationRailRevealRequest | null>(null);
  const clearRailRevealRequest = useCallback(() => {
    setRailRevealRequest(null);
  }, []);
  const requestRailReveal = useCallback(
    (agentSessionId: string, reason: AgentGUIConversationRailRevealReason) => {
      const normalized = agentSessionId.trim();
      if (!normalized) return;
      setRailRevealRequest({
        agentSessionId: normalized,
        reason,
        revision: ++railRevealRevisionRef.current
      });
    },
    []
  );

  return {
    activeConversationId,
    clearRailRevealRequest,
    detailError,
    draftByScopeKey,
    draftSettingsBySessionId,
    goalClearNoticeSequence,
    intent,
    isComposerHome,
    isDeletingConversation,
    isDeletingProjectConversations,
    isLoadingMessages,
    isUserProjectMutationPending,
    listError,
    pendingDeleteConversation,
    pendingDeleteProjectConversations,
    railRevealRequest,
    requestRailReveal,
    selectedProjectPath,
    shouldApplyPreparedProjectSelection:
      homeProjectSelection.kind === "unresolved_default",
    setActiveConversationId,
    setDetailError,
    setDraftByScopeKey,
    setDraftSettingsBySessionId,
    setGoalClearNoticeSequence,
    setIntent,
    setIsComposerHome,
    setIsDeletingConversation,
    setIsDeletingProjectConversations,
    setIsLoadingMessages,
    setIsUserProjectMutationPending,
    setListError,
    setPendingDeleteConversation,
    setPendingDeleteProjectConversations,
    setSelectedProjectPath,
    setUserProjects,
    submittedDraftSnapshotsRef,
    userProjects
  };
}
