import {
  useCallback,
  type Dispatch,
  type RefObject,
  type SetStateAction
} from "react";
import {
  isPendingActivationViable,
  selectLatestActivationForSession,
  selectEngineSession,
  type AgentSessionEngine
} from "@tutti-os/agent-activity-core";
import { areWorkspaceUserProjectPathsEqual } from "@tutti-os/workspace-user-project/core";
import type { AgentGUIRuntime } from "../../../agentActivityRuntime";
import type { useAgentHostApi } from "../../../agentActivityHost";
import type { AgentHostUserProject } from "../../../host/agentHostApi";
import { translate } from "../../../i18n/index";
import type { AgentGUINodeData } from "../../../types";
import {
  getAgentGUIErrorCode,
  getAgentGUIErrorMessage,
  getAgentGUIErrorReason,
  isSessionNotFoundErrorCode
} from "./agentGuiController.errors";
import {
  reportAgentGUIRuntimeError,
  showAgentGUIControllerErrorToast
} from "./agentGuiController.reporting";

export interface UseAgentGUIConversationMetadataActionsInput {
  agentHostApi: ReturnType<typeof useAgentHostApi>;
  setListError: Dispatch<SetStateAction<string | null>>;
  setUserProjectsSnapshot: (projects: readonly AgentHostUserProject[]) => void;
  userProjectsRef: RefObject<AgentHostUserProject[]>;
  setDetailError: Dispatch<SetStateAction<string | null>>;
  agentActivityRuntime: AgentGUIRuntime;
  dataRef: RefObject<AgentGUINodeData>;
  workspaceId: string;
  sessionEngine: AgentSessionEngine;
  currentUserId: string | null | undefined;
  selectConversation?: (agentSessionId: string) => void;
}

const CANONICAL_SESSION_WAIT_TIMEOUT_MS = 30_000;

type CanonicalSessionWaitResult = "ready" | "unavailable" | "timed_out";

async function waitForCanonicalSession(
  sessionEngine: AgentSessionEngine,
  agentSessionId: string
): Promise<CanonicalSessionWaitResult> {
  const hasCanonicalSession = (): boolean =>
    selectEngineSession(sessionEngine.getSnapshot(), agentSessionId) !== null;
  if (hasCanonicalSession()) {
    return "ready";
  }

  const pendingActivation = selectLatestActivationForSession(
    sessionEngine.getSnapshot(),
    agentSessionId
  );
  if (!isPendingActivationViable(pendingActivation)) {
    return "unavailable";
  }

  return new Promise((resolve) => {
    let unsubscribe: (() => void) | null = null;
    let active = true;
    const timeoutSignal = AbortSignal.timeout(
      CANONICAL_SESSION_WAIT_TIMEOUT_MS
    );
    const finish = (result: CanonicalSessionWaitResult): void => {
      if (!active) {
        return;
      }
      active = false;
      timeoutSignal.removeEventListener("abort", onTimeout);
      unsubscribe?.();
      unsubscribe = null;
      resolve(result);
    };
    const check = (): void => {
      if (hasCanonicalSession()) {
        finish("ready");
        return;
      }
      const activation = selectLatestActivationForSession(
        sessionEngine.getSnapshot(),
        agentSessionId
      );
      if (!isPendingActivationViable(activation)) {
        finish("unavailable");
      }
    };
    function onTimeout(): void {
      finish("timed_out");
    }
    timeoutSignal.addEventListener("abort", onTimeout, { once: true });
    const registeredUnsubscribe = sessionEngine.subscribe(check);
    if (active) {
      unsubscribe = registeredUnsubscribe;
    } else {
      registeredUnsubscribe();
    }
    check();
  });
}

export function useAgentGUIConversationMetadataActions(
  input: UseAgentGUIConversationMetadataActionsInput
) {
  const {
    agentHostApi,
    setListError,
    setUserProjectsSnapshot,
    userProjectsRef,
    setDetailError,
    agentActivityRuntime,
    dataRef,
    workspaceId,
    sessionEngine,
    currentUserId
  } = input;

  const removeProject = useCallback(
    async (path: string): Promise<boolean> => {
      const normalizedPath = path.trim();
      const remove = agentHostApi.userProjects?.remove;
      if (!normalizedPath || !remove) {
        return false;
      }
      setListError(null);
      // Filter the visible list only after the backend confirms the delete
      // (mirroring registerProjectPath's "backend confirm -> local store
      // update" ordering). Filtering optimistically before the delete
      // resolves would flip userProjectPathKey early and race the runtime
      // rail sections refetch against the in-flight backend delete: if the
      // section list query lands before the delete commits, it still
      // reports the removed project's section, and nothing re-triggers a
      // refetch afterwards, so the row stays visible until an unrelated
      // remount forces a fresh fetch.
      const handleRemoveError = (error: unknown): false => {
        const message = getAgentGUIErrorMessage(error);
        setListError(message);
        showAgentGUIControllerErrorToast(agentHostApi.toast, message);
        return false;
      };
      try {
        await remove({ path: normalizedPath });
        setUserProjectsSnapshot(
          userProjectsRef.current.filter(
            (project) =>
              !areWorkspaceUserProjectPathsEqual(project.path, normalizedPath)
          )
        );
        return true;
      } catch (error) {
        return handleRemoveError(error);
      }
    },
    [agentHostApi.toast, agentHostApi.userProjects, setUserProjectsSnapshot]
  );

  const moveProject = useCallback(
    async (projectId: string, beforeProjectId: string | null) => {
      const move = agentHostApi.userProjects?.move;
      if (!move) return;
      agentHostApi.debug?.logRuntimeDiagnostics?.({
        beforeProjectId,
        phase: "move_user_project_requested",
        projectId
      });
      try {
        await move({ beforeProjectId, projectId });
        agentHostApi.debug?.logRuntimeDiagnostics?.({
          beforeProjectId,
          phase: "move_user_project_succeeded",
          projectId
        });
      } catch (error) {
        agentHostApi.debug?.logRuntimeDiagnostics?.({
          beforeProjectId,
          error: getAgentGUIErrorMessage(error),
          phase: "move_user_project_failed",
          projectId
        });
      }
    },
    [agentHostApi.debug, agentHostApi.userProjects]
  );

  const toggleProjectPinned = useCallback(
    async (projectId: string, pinned: boolean) => {
      const normalizedProjectId = projectId.trim();
      const pin = agentHostApi.userProjects?.pin;
      if (!normalizedProjectId || !pin) return;
      agentHostApi.debug?.logRuntimeDiagnostics?.({
        phase: "pin_user_project_requested",
        pinned,
        projectId: normalizedProjectId
      });
      try {
        await pin({ projectId: normalizedProjectId, pinned });
        agentHostApi.debug?.logRuntimeDiagnostics?.({
          phase: "pin_user_project_succeeded",
          pinned,
          projectId: normalizedProjectId
        });
      } catch (error) {
        agentHostApi.debug?.logRuntimeDiagnostics?.({
          error: getAgentGUIErrorMessage(error),
          phase: "pin_user_project_failed",
          pinned,
          projectId: normalizedProjectId
        });
      }
    },
    [agentHostApi.debug, agentHostApi.userProjects]
  );

  const toggleConversationPinned = useCallback(
    (agentSessionId: string, pinned: boolean) => {
      const normalizedAgentSessionId = agentSessionId.trim();
      if (!normalizedAgentSessionId) {
        return;
      }
      setDetailError(null);
      void agentActivityRuntime.mutations
        .setPinned({
          agentSessionId: normalizedAgentSessionId,
          pinned
        })
        .catch((error) => {
          const message = getAgentGUIErrorMessage(error);
          reportAgentGUIRuntimeError({
            agentSessionId: normalizedAgentSessionId,
            context: { pinned },
            error,
            phase: "toggle_conversation_pinned",
            provider: dataRef.current.provider,
            runtime: agentActivityRuntime,
            workspaceId
          });
          showAgentGUIControllerErrorToast(agentHostApi.toast, message);
        });
    },
    [agentActivityRuntime, agentHostApi.toast, workspaceId]
  );

  const renameConversation = useCallback(
    async (agentSessionId: string, title: string) => {
      const normalizedAgentSessionId = agentSessionId.trim();
      const normalizedTitle = title.trim();
      if (!normalizedAgentSessionId) {
        return;
      }
      setDetailError(null);
      try {
        const canonicalSessionWait = await waitForCanonicalSession(
          sessionEngine,
          normalizedAgentSessionId
        );
        if (canonicalSessionWait !== "ready") {
          throw Object.assign(
            new Error(translate("agentHost.agentGui.sessionActionUnavailable")),
            {
              reason: `agent_gui_rename_session_${canonicalSessionWait}`
            }
          );
        }
        await agentActivityRuntime.mutations.rename({
          agentSessionId: normalizedAgentSessionId,
          title: normalizedTitle
        });
      } catch (error) {
        const message = getAgentGUIErrorMessage(error);
        reportAgentGUIRuntimeError({
          agentSessionId: normalizedAgentSessionId,
          context: { titleLength: normalizedTitle.length },
          error,
          phase: "rename_conversation",
          provider: dataRef.current.provider,
          runtime: agentActivityRuntime,
          workspaceId
        });
        showAgentGUIControllerErrorToast(agentHostApi.toast, message);
        throw error;
      }
    },
    [
      agentActivityRuntime,
      agentHostApi.toast,
      dataRef,
      sessionEngine,
      setDetailError,
      workspaceId
    ]
  );

  const forkConversationThroughTurn = useCallback(
    async (sourceAgentSessionId: string, turnId: string) => {
      const normalizedSourceAgentSessionId = sourceAgentSessionId.trim();
      const normalizedTurnId = turnId.trim();
      if (!normalizedSourceAgentSessionId || !normalizedTurnId) {
        return;
      }
      setDetailError(null);
      try {
        const result = await agentActivityRuntime.mutations.forkThroughTurn({
          sourceAgentSessionId: normalizedSourceAgentSessionId,
          turnId: normalizedTurnId
        });
        if (dataRef.current.lastActiveAgentSessionId === normalizedSourceAgentSessionId) input.selectConversation?.(result.targetAgentSessionId);
      } catch (error) {
        const message = getAgentGUIErrorMessage(error);
        reportAgentGUIRuntimeError({
          agentSessionId: normalizedSourceAgentSessionId,
          context: { turnId: normalizedTurnId },
          error,
          phase: "fork_conversation_through_turn",
          provider: dataRef.current.provider,
          runtime: agentActivityRuntime,
          workspaceId
        });
        showAgentGUIControllerErrorToast(agentHostApi.toast, message);
      }
    },
    [
      agentActivityRuntime,
      agentHostApi.toast,
      dataRef,
      input.selectConversation,
      setDetailError,
      workspaceId
    ]
  );

  const openForkSourceConversation = useCallback(
    async (sourceAgentSessionId: string) => {
      const normalizedSourceAgentSessionId = sourceAgentSessionId.trim();
      if (!normalizedSourceAgentSessionId) {
        return;
      }
      setDetailError(null);
      try {
        const sourceSession = await agentActivityRuntime.getSession(
          workspaceId,
          normalizedSourceAgentSessionId
        );
        if (
          sourceSession.agentSessionId.trim() !== normalizedSourceAgentSessionId
        ) {
          throw new Error(
            "source agent Session query returned a different Session"
          );
        }
        input.selectConversation?.(normalizedSourceAgentSessionId);
      } catch (error) {
        const reason = getAgentGUIErrorReason(error);
        const message =
          isSessionNotFoundErrorCode(getAgentGUIErrorCode(error)) ||
          reason === "workspace_agent_session_not_found"
            ? translate("agentHost.agentGui.sourceConversationNotFound")
            : getAgentGUIErrorMessage(error);
        reportAgentGUIRuntimeError({
          agentSessionId: normalizedSourceAgentSessionId,
          error,
          phase: "open_fork_source_conversation",
          provider: dataRef.current.provider,
          runtime: agentActivityRuntime,
          workspaceId
        });
        showAgentGUIControllerErrorToast(agentHostApi.toast, message);
      }
    },
    [
      agentActivityRuntime,
      agentHostApi.toast,
      dataRef,
      input.selectConversation,
      setDetailError,
      workspaceId
    ]
  );

  return {
    forkConversationThroughTurn,
    openForkSourceConversation,
    moveProject,
    removeProject,
    toggleProjectPinned,
    toggleConversationPinned,
    renameConversation
  };
}
