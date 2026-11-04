import {
  pendingSubmitRecordListsEqual,
  selectEngineActiveTurn,
  selectEngineHasPendingInteractions,
  selectEngineLatestTurn,
  selectEnginePendingInteractions,
  selectEnginePromptQueue,
  selectEnginePromptQueueError,
  selectEngineSession,
  selectEngineSessionDeleted,
  selectEngineSessionDetailHydrated,
  selectEngineSessionDetailLoading,
  selectEngineSessionOperationError,
  selectEngineSessionRuntimeAvailability,
  selectEngineSessionRuntimeActivity,
  selectEngineSessionReconcile,
  selectEngineSubmitAvailability,
  selectLatestActivationForSession,
  selectLatestPendingSubmitForSession,
  selectPendingSubmitsForSession,
  selectSessionHasUnconfirmedSubmit,
  selectSessionHasPendingSubmitStopTarget,
  selectSessionIsSubmitting,
  type AgentSessionEngine,
  type EngineQueuedPrompt
} from "@tutti-os/agent-activity-core";
import { useMemo } from "react";
import type {
  AgentSessionComposerSettings,
  AgentSessionReasoningEffort,
  AgentSessionSpeed,
  AgentSessionState
} from "../../../shared/agentSessionTypes";
import {
  useEngineSelector,
  useModelValue
} from "../../../shared/engine/useEngineSelector";
import type {
  AgentGUIComposerSettingsLane,
  AgentGUISessionSettingsUpdate
} from "./agentGuiController.providerHelpers";
import type { AgentGUIPromptOperations } from "./agentGuiController.promptOperations";
import { EMPTY_QUEUED_PROMPTS } from "./agentGuiController.draftMessageHelpers";
import { agentActivityInteractionListsEqual } from "./agentGuiController.providerHelpers";
import { agentGUIQueueStatusFromPromptQueue } from "./agentGuiQueueStatus";
import { isPendingNewConversationActivation } from "./useAgentGUIActivation";

export function useAgentGUISessionEngineState(input: {
  activeConversationId: string | null;
  composer: AgentGUIComposerSettingsLane;
  prompts: AgentGUIPromptOperations;
  sessionEngine: AgentSessionEngine;
}) {
  const { activeConversationId, composer, prompts, sessionEngine } = input;
  const activeQueuedPromptSnapshot = useEngineSelector(sessionEngine, (state) =>
    selectEnginePromptQueue(state, activeConversationId)
  );
  const activeQueuedPrompts = useMemo<readonly EngineQueuedPrompt[]>(
    () =>
      activeQueuedPromptSnapshot?.prompts.filter(
        (prompt) =>
          prompt.visibleInQueue !== false &&
          prompt.id !== activeQueuedPromptSnapshot.inFlight?.promptId &&
          prompt.id !== activeQueuedPromptSnapshot.sendNextPromptId
      ) ?? EMPTY_QUEUED_PROMPTS,
    [activeQueuedPromptSnapshot]
  );
  const activePendingSubmits = useEngineSelector(
    sessionEngine,
    (state) => selectPendingSubmitsForSession(state, activeConversationId),
    pendingSubmitRecordListsEqual
  );
  const activeLatestPendingSubmit = useEngineSelector(sessionEngine, (state) =>
    selectLatestPendingSubmitForSession(state, activeConversationId)
  );
  const activePendingActivation = useEngineSelector(sessionEngine, (state) =>
    selectLatestActivationForSession(state, activeConversationId)
  );
  const isCreatingConversation = isPendingNewConversationActivation(
    activePendingActivation
  );
  const isSubmitting = useEngineSelector(sessionEngine, (state) =>
    selectSessionIsSubmitting(state, activeConversationId)
  );
  const hasUnconfirmedSubmit = useEngineSelector(sessionEngine, (state) =>
    selectSessionHasUnconfirmedSubmit(state, activeConversationId)
  );
  const activeHasPendingSubmitStopTarget = useEngineSelector(
    sessionEngine,
    (state) =>
      selectSessionHasPendingSubmitStopTarget(state, activeConversationId)
  );
  const activeCancelStatus = useModelValue(
    () => prompts.cancelStatus(activeConversationId),
    [prompts, activeConversationId]
  );
  const activeEngineSession = useEngineSelector(sessionEngine, (state) =>
    selectEngineSession(state, activeConversationId)
  );
  const activeSessionReconcile = useEngineSelector(sessionEngine, (state) =>
    selectEngineSessionReconcile(state, activeConversationId)
  );
  const activeSessionDetailLoading = useEngineSelector(sessionEngine, (state) =>
    selectEngineSessionDetailLoading(state, activeConversationId)
  );
  const activeSessionDetailHydrated = useEngineSelector(
    sessionEngine,
    (state) => selectEngineSessionDetailHydrated(state, activeConversationId)
  );
  const activeEngineSessionDeleted = useEngineSelector(sessionEngine, (state) =>
    selectEngineSessionDeleted(state, activeConversationId)
  );
  const activeEngineActiveTurn = useEngineSelector(sessionEngine, (state) =>
    selectEngineActiveTurn(state, activeConversationId)
  );
  const activeEngineLatestTurn = useEngineSelector(sessionEngine, (state) =>
    selectEngineLatestTurn(state, activeConversationId)
  );
  const activeEngineSettingsUpdate = useModelValue(
    () => composer.settingsUpdate(activeConversationId),
    [composer, activeConversationId]
  );
  const activeCanonicalComposerSettings = useMemo<AgentSessionComposerSettings>(
    () =>
      mergeOptimisticSessionSettings(
        {
          model: activeEngineSession?.settings?.model ?? undefined,
          permissionModeId:
            activeEngineSession?.settings?.permissionModeId ?? undefined,
          planMode: activeEngineSession?.settings?.planMode ?? undefined,
          browserUse: activeEngineSession?.settings?.browserUse ?? undefined,
          computerUse: activeEngineSession?.settings?.computerUse ?? undefined,
          reasoningEffort:
            (activeEngineSession?.settings?.reasoningEffort as
              | AgentSessionReasoningEffort
              | null
              | undefined) ?? undefined,
          speed:
            (activeEngineSession?.settings?.speed as
              | AgentSessionSpeed
              | null
              | undefined) ?? undefined
        },
        activeEngineSettingsUpdate
      ),
    [activeEngineSession?.settings, activeEngineSettingsUpdate]
  );
  const activeSessionState = useMemo<AgentSessionState | null>(() => {
    if (!activeEngineSession) return null;
    return {
      workspaceId: activeEngineSession.workspaceId,
      agentSessionId: activeEngineSession.agentSessionId,
      agentTargetId: activeEngineSession.agentTargetId,
      provider: activeEngineSession.provider as AgentSessionState["provider"],
      providerSessionId: activeEngineSession.providerSessionId ?? undefined,
      resumable: activeEngineSession.resumable,
      status:
        activeEngineActiveTurn && activeEngineActiveTurn.phase !== "settled"
          ? "working"
          : "ready",
      permissionModeId:
        activeEngineSession.settings?.permissionModeId ?? undefined,
      permissionConfig: activeEngineSession.permissionConfig,
      settings: activeCanonicalComposerSettings,
      pinnedAtUnixMs: activeEngineSession.pinnedAtUnixMs,
      updatedAtUnixMs:
        activeEngineSession.updatedAtUnixMs ??
        activeEngineSession.lastEventUnixMs ??
        activeEngineSession.createdAtUnixMs ??
        0
    };
  }, [
    activeCanonicalComposerSettings,
    activeEngineActiveTurn,
    activeEngineSession
  ]);
  const activeEngineOperationError = useEngineSelector(sessionEngine, (state) =>
    selectEngineSessionOperationError(state, activeConversationId)
  );
  const activeEngineQueueError = useEngineSelector(sessionEngine, (state) =>
    selectEnginePromptQueueError(state, activeConversationId)
  );
  const activeEngineInteractionError = useModelValue(
    () => prompts.interactionError(activeConversationId),
    [prompts, activeConversationId]
  );
  const activeEngineError =
    activeEngineOperationError ??
    activeEngineInteractionError ??
    activeEngineQueueError;
  const isRespondingToInteraction = useModelValue(
    () => prompts.isRespondingToInteraction(activeConversationId),
    [prompts, activeConversationId]
  );
  const activeEngineAvailability: "available" | "blocked" | "missing" =
    useEngineSelector(
      sessionEngine,
      (state) =>
        selectEngineSubmitAvailability(state, activeConversationId)?.state ??
        "missing"
    );
  const activeEngineRuntimeAvailability = useEngineSelector(
    sessionEngine,
    (state) =>
      selectEngineSessionRuntimeAvailability(state, activeConversationId)
  );
  const activeEngineRuntimeActivity = useEngineSelector(
    sessionEngine,
    (state) => selectEngineSessionRuntimeActivity(state, activeConversationId)
  );
  const activeEngineHasPendingInteractions = useEngineSelector(
    sessionEngine,
    (state) => selectEngineHasPendingInteractions(state, activeConversationId)
  );
  const activeEnginePendingInteractions = useEngineSelector(
    sessionEngine,
    (state) => selectEnginePendingInteractions(state, activeConversationId),
    agentActivityInteractionListsEqual
  );

  return {
    activeCancelStatus,
    activeCanonicalComposerSettings,
    activeEngineActiveTurn,
    activeEngineAvailability,
    activeEngineError,
    activeEngineHasPendingInteractions,
    activeEngineLatestTurn,
    activeEnginePendingInteractions,
    activeEngineRuntimeAvailability,
    activeEngineRuntimeActivity,
    activeEngineSession,
    activeEngineSessionDeleted,
    activeEngineSettingsUpdate,
    activeHasPendingSubmitStopTarget,
    activeLatestPendingSubmit,
    activePendingActivation,
    activePendingSubmits,
    activeQueuedPromptInFlight: activeQueuedPromptSnapshot?.inFlight ?? null,
    activeQueuedPrompts,
    activeQueueStatus: agentGUIQueueStatusFromPromptQueue(
      activeQueuedPromptSnapshot
    ),
    activeSessionState,
    activeSessionDetailLoading,
    activeSessionReconcileErrorCode: activeSessionReconcile?.errorCode ?? null,
    activeSessionReconcileError: activeSessionDetailHydrated
      ? null
      : (activeSessionReconcile?.errorMessage ?? null),
    isCreatingConversation,
    hasUnconfirmedSubmit,
    isRespondingToInteraction,
    isSubmitting
  };
}

function mergeOptimisticSessionSettings(
  canonical: AgentSessionComposerSettings,
  update: AgentGUISessionSettingsUpdate | null
): AgentSessionComposerSettings {
  if (
    !update ||
    !SESSION_SETTINGS_STATUS_SHOWS_OPTIMISTIC_VALUE[update.status]
  ) {
    return canonical;
  }
  const optimistic = {
    ...(update.settings ?? {}),
    ...(update.queuedSettings ?? {})
  };
  return {
    ...canonical,
    ...(typeof optimistic.model === "string"
      ? { model: optimistic.model }
      : {}),
    ...(typeof optimistic.permissionModeId === "string"
      ? { permissionModeId: optimistic.permissionModeId }
      : {}),
    ...(typeof optimistic.planMode === "boolean"
      ? { planMode: optimistic.planMode }
      : {}),
    ...(typeof optimistic.browserUse === "boolean"
      ? { browserUse: optimistic.browserUse }
      : {}),
    ...(typeof optimistic.computerUse === "boolean"
      ? { computerUse: optimistic.computerUse }
      : {}),
    ...(typeof optimistic.reasoningEffort === "string"
      ? {
          reasoningEffort:
            optimistic.reasoningEffort as AgentSessionReasoningEffort
        }
      : {}),
    ...(typeof optimistic.speed === "string"
      ? { speed: optimistic.speed as AgentSessionSpeed }
      : {})
  };
}

const SESSION_SETTINGS_STATUS_SHOWS_OPTIMISTIC_VALUE = {
  failed: false,
  inFlight: true,
  // Timed-out updates must not keep showing the optimistic value (e.g. High)
  // or the composer looks settled while the durable session is still medium.
  unknown: false,
  waitingForRuntime: true
} satisfies Record<AgentGUISessionSettingsUpdate["status"], boolean>;
