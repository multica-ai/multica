import { computed, makeObservable } from "mobx";
import { isNonRetryableResumeErrorCode } from "../agent-gui/agentGuiNode/controller/agentGuiController.errors";
import {
  interactiveApprovalFromInteraction,
  interactivePromptFromInteraction
} from "../agent-gui/agentGuiNode/controller/agentGuiController.interactiveHelpers";
import type { AgentGUIPromptOperations } from "../agent-gui/agentGuiNode/controller/agentGuiController.promptOperations";
import type {
  AgentGUIComposerSettingsLane,
  AgentGUISessionSettingsUpdate
} from "../agent-gui/agentGuiNode/controller/agentGuiController.providerHelpers";
import { resolvePendingQuestionComposerTarget } from "../agent-gui/agentGuiNode/controller/pendingQuestionComposerResponse";
import { isPendingNewConversationActivation } from "../agent-gui/agentGuiNode/controller/useAgentGUIActivation";
import type {
  AgentSessionComposerSettings,
  AgentSessionReasoningEffort,
  AgentSessionSpeed,
  AgentSessionState
} from "../shared/agentSessionTypes";
import type { AppErrorCode } from "../shared/contracts/dto";
import type { AgentConversationServices } from "./conversation-services";

export type ActiveConversationStateInput = {
  activeConversationId: string | null;
  composer: AgentGUIComposerSettingsLane;
  prompts: AgentGUIPromptOperations;
  conversationServices: AgentConversationServices;
};

/** Observable session state; the view reads these getters instead of React snapshots. */
export class ActiveConversationState {
  constructor(private readonly input: ActiveConversationStateInput) {
    makeObservable(this, {
      activeSessionFamily: computed,
      activationPresentation: computed,
      activationError: computed,
      activationErrorCode: computed,
      activeLiveState: computed,
      pendingInteractions: computed,
      pendingApproval: computed,
      serverInteractivePrompt: computed,
      pendingQuestionComposerTarget: computed,
      relatedIsRespondingToInteraction: computed,
      activityDisplayStatus: computed,
      activeLatestPendingSubmit: computed,
      activePendingActivation: computed,
      isCreatingConversation: computed,
      isSubmitting: computed,
      hasUnconfirmedSubmit: computed,
      activeHasPendingSubmitStopTarget: computed,
      activeCancelStatus: computed,
      activeCanonicalSession: computed,
      activeSessionReconcile: computed,
      activeSessionDetailLoading: computed,
      activeSessionDetailHydrated: computed,
      activeSessionReconcileError: computed,
      activeSessionReconcileErrorCode: computed,
      isCancelPending: computed,
      isInterrupting: computed,
      activeCanonicalSessionDeleted: computed,
      activeEngineActiveTurn: computed,
      activeEngineLatestTurn: computed,
      activeEngineSettingsUpdate: computed,
      activeCanonicalComposerSettings: computed,
      activeSessionState: computed,
      activeEngineQueueError: computed,
      activeEngineInteractionError: computed,
      activeSessionError: computed,
      isRespondingToInteraction: computed,
      activeEngineAvailability: computed,
      activeEngineRuntimeAvailability: computed,
      activeEngineRuntimeActivity: computed,
      activeEngineHasPendingInteractions: computed,
      activeEnginePendingInteractions: computed
    });
  }
  get activationPresentation() {
    return this.conversationServices.activations.presentation(
      this.activeConversationId
    );
  }
  get activationError() {
    return this.activationPresentation.errorMessage;
  }
  get activationErrorCode() {
    return this.activationPresentation.errorCode as AppErrorCode | null;
  }
  get activeLiveState() {
    const state = this.activationPresentation.status;
    return state === "inactive" &&
      this.activeEngineActiveTurn &&
      this.activeEngineActiveTurn.phase !== "settled"
      ? "active"
      : state;
  }
  get activityDisplayStatus() {
    return (
      this.conversationServices.sessions.consumerSession(
        this.activeConversationId
      )?.displayStatus ?? null
    );
  }
  get activeSessionFamily() {
    return this.conversationServices.sessions.family(this.activeConversationId);
  }
  get pendingInteractions() {
    return this.activeSessionFamily.pendingInteractions;
  }
  get pendingApproval() {
    return isNonRetryableResumeErrorCode(this.activationErrorCode)
      ? null
      : interactiveApprovalFromInteraction(
          [...this.pendingInteractions]
            .reverse()
            .find((item) => item.kind === "approval") ?? null
        );
  }
  get serverInteractivePrompt() {
    return isNonRetryableResumeErrorCode(this.activationErrorCode)
      ? null
      : interactivePromptFromInteraction(
          [...this.pendingInteractions]
            .reverse()
            .find((item) => item.kind !== "approval") ?? null
        );
  }
  get pendingQuestionComposerTarget() {
    return isNonRetryableResumeErrorCode(this.activationErrorCode)
      ? null
      : resolvePendingQuestionComposerTarget({
          activeTurnId: this.activeEngineActiveTurn?.turnId ?? "",
          agentSessionId: this.activeConversationId ?? "",
          pendingInteractions: this.pendingInteractions
        });
  }
  get relatedIsRespondingToInteraction() {
    return this.pendingInteractions.some((item) =>
      this.prompts.isRespondingToInteraction(item.agentSessionId)
    );
  }
  get hasPendingSubmitStopTarget() {
    return this.activeHasPendingSubmitStopTarget;
  }
  get isCancelPending() {
    return this.activeCancelStatus === "awaitingTurn";
  }
  get isInterrupting() {
    return (
      this.activeCancelStatus === "requested" ||
      this.activeCancelStatus === "accepted"
    );
  }
  get activeSessionReconcileErrorCode() {
    return this.activeSessionReconcile?.errorCode ?? null;
  }
  get activeSessionReconcileError() {
    return this.activeSessionDetailHydrated
      ? null
      : (this.activeSessionReconcile?.errorMessage ?? null);
  }
  get activeConversationId() {
    return this.input.activeConversationId;
  }
  private get composer() {
    return this.input.composer;
  }
  private get prompts() {
    return this.input.prompts;
  }
  private get conversationServices() {
    return this.input.conversationServices;
  }
  get activeLatestPendingSubmit() {
    return this.conversationServices.prompts.latestPendingSubmit(
      this.activeConversationId
    );
  }

  get activePendingActivation() {
    return this.conversationServices.activations.latest(
      this.activeConversationId
    );
  }

  get isCreatingConversation() {
    return isPendingNewConversationActivation(this.activePendingActivation);
  }

  get isSubmitting() {
    return this.conversationServices.prompts.isSubmitting(
      this.activeConversationId
    );
  }

  get hasUnconfirmedSubmit() {
    return this.conversationServices.prompts.hasUnconfirmedSubmit(
      this.activeConversationId
    );
  }

  get activeHasPendingSubmitStopTarget() {
    return this.conversationServices.prompts.hasStopTarget(
      this.activeConversationId
    );
  }

  get activeCancelStatus() {
    return this.prompts.cancelStatus(this.activeConversationId);
  }

  get activeCanonicalSession() {
    return this.conversationServices.sessions.session(
      this.activeConversationId
    );
  }

  get activeSessionReconcile() {
    return this.conversationServices.sessionLoads.record(
      this.activeConversationId
    );
  }

  get activeSessionDetailLoading() {
    return this.conversationServices.sessionLoads.isDetailLoading(
      this.activeConversationId
    );
  }

  get activeSessionDetailHydrated() {
    return this.conversationServices.sessionLoads.isDetailHydrated(
      this.activeConversationId
    );
  }

  get activeCanonicalSessionDeleted() {
    return this.conversationServices.sessions.isDeleted(
      this.activeConversationId
    );
  }

  get activeEngineActiveTurn() {
    return this.conversationServices.sessions.activeTurn(
      this.activeConversationId
    );
  }

  get activeEngineLatestTurn() {
    return this.conversationServices.sessions.latestTurn(
      this.activeConversationId
    );
  }

  get activeEngineSettingsUpdate() {
    return this.composer.settingsUpdate(this.activeConversationId);
  }

  get activeCanonicalComposerSettings(): AgentSessionComposerSettings {
    return mergeOptimisticSessionSettings(
      {
        model: this.activeCanonicalSession?.settings?.model ?? undefined,
        permissionModeId:
          this.activeCanonicalSession?.settings?.permissionModeId ?? undefined,
        planMode: this.activeCanonicalSession?.settings?.planMode ?? undefined,
        browserUse:
          this.activeCanonicalSession?.settings?.browserUse ?? undefined,
        computerUse:
          this.activeCanonicalSession?.settings?.computerUse ?? undefined,
        reasoningEffort:
          (this.activeCanonicalSession?.settings?.reasoningEffort as
            | AgentSessionReasoningEffort
            | null
            | undefined) ?? undefined,
        speed:
          (this.activeCanonicalSession?.settings?.speed as
            | AgentSessionSpeed
            | null
            | undefined) ?? undefined
      },
      this.activeEngineSettingsUpdate
    );
  }

  get activeSessionState(): AgentSessionState | null {
    const value = (() => {
      if (!this.activeCanonicalSession) return null;
      return {
        workspaceId: this.activeCanonicalSession.workspaceId,
        agentSessionId: this.activeCanonicalSession.agentSessionId,
        agentTargetId: this.activeCanonicalSession.agentTargetId,
        provider: this.activeCanonicalSession
          .provider as AgentSessionState["provider"],
        providerSessionId:
          this.activeCanonicalSession.providerSessionId ?? undefined,
        resumable: this.activeCanonicalSession.resumable,
        status:
          this.activeEngineActiveTurn &&
          this.activeEngineActiveTurn.phase !== "settled"
            ? "working"
            : "ready",
        permissionModeId:
          this.activeCanonicalSession.settings?.permissionModeId ?? undefined,
        permissionConfig: this.activeCanonicalSession.permissionConfig,
        settings: this.activeCanonicalComposerSettings,
        pinnedAtUnixMs: this.activeCanonicalSession.pinnedAtUnixMs,
        updatedAtUnixMs:
          this.activeCanonicalSession.updatedAtUnixMs ??
          this.activeCanonicalSession.lastEventUnixMs ??
          this.activeCanonicalSession.createdAtUnixMs ??
          0
      };
    })();
    return value;
  }

  get activeEngineQueueError() {
    return this.conversationServices.prompts.queueError(
      this.activeConversationId
    );
  }

  get activeEngineInteractionError() {
    return this.prompts.interactionError(this.activeConversationId);
  }

  get activeSessionError() {
    return this.activeEngineInteractionError ?? this.activeEngineQueueError;
  }

  get isRespondingToInteraction() {
    return this.prompts.isRespondingToInteraction(this.activeConversationId);
  }

  get activeEngineAvailability(): "available" | "blocked" | "missing" {
    return (
      this.conversationServices.sessions.submitAvailability(
        this.activeConversationId
      )?.state ?? "missing"
    );
  }

  get activeEngineRuntimeAvailability() {
    return this.conversationServices.sessions.runtimeAvailability(
      this.activeConversationId
    );
  }

  get activeEngineRuntimeActivity() {
    return this.conversationServices.sessions.runtimeActivity(
      this.activeConversationId
    );
  }

  get activeEngineHasPendingInteractions() {
    return this.conversationServices.sessions.hasPendingInteractions(
      this.activeConversationId
    );
  }

  get activeEnginePendingInteractions() {
    return this.conversationServices.sessions.pendingInteractions(
      this.activeConversationId
    );
  }
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
