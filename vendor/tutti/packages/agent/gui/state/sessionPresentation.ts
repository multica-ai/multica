import {
  isPendingActivationViable,
  type AgentActivityDisplayStatus,
  type AgentActivityMessage,
  type AgentActivityTurn,
  type CanonicalAgentSession,
  type PendingActivationIntentRecord,
  type SessionRuntimeAvailability
} from "@tutti-os/agent-activity-core";
import { promptRequestId } from "../agent-gui/agentGuiNode/controller/agentGuiController.diagnostics";
import {
  agentActivityDisplayStatusBusy,
  conversationBusyStatus
} from "../agent-gui/agentGuiNode/controller/agentGuiController.draftMessageHelpers";
import { isNonRetryableResumeErrorCode } from "../agent-gui/agentGuiNode/controller/agentGuiController.errors";
import { projectAgentGUIMessagesToTimelineItems } from "../agent-gui/agentGuiNode/controller/agentGuiController.promptHelpers";
import { reportAgentGUIRenderStateDiagnostic } from "../agent-gui/agentGuiNode/controller/agentGuiController.reporting";
import {
  isDifferentKnownConversationOwner,
  resolveAgentGUIComposerGate
} from "../agent-gui/agentGuiNode/model/agentGuiComposerGate";
import type {
  AgentGUIConversationSummary,
  AgentGUIInteractivePrompt
} from "../agent-gui/agentGuiNode/model/agentGuiConversationModel";
import type { AgentGUISessionChrome } from "../agent-gui/agentGuiNode/model/agentGuiNodeTypes";
import { composerSettingsSupportFromOptions } from "../agent-gui/agentGuiNode/model/composerSettingsSupport";
import type { ConversationServices } from "../conversationServices";
import { translate } from "../i18n/index";
import type { AgentApprovalItemVM } from "../shared/agentConversation/contracts/agentApprovalItemVM";
import type { AgentSessionState } from "../shared/agentSessionTypes";
import type { AppErrorCode } from "../shared/contracts/dto";
import type {
  AgentGUIInteractionReadiness,
  AgentGUIInteractionReadinessReason,
  AgentGUIInteractionReadinessSource,
  AgentGUIObservationGap,
  AgentGUIObservationGapSource,
  AgentGUITargetConnectionSource,
  AgentGUITargetConnectionState
} from "../types";
import type { AgentConversationServices } from "./conversation-services";
interface CurrentValue<T> {
  current: T;
}
interface ExactInteractionPromptIdentity {
  agentSessionId?: string | null;
  requestId?: string | null;
  turnId?: string | null;
}
export function resolveAgentGUISharingRevokedRecovery(input: {
  activeConversationId: string | null;
  selectedAgentTargetOwnerLabel: string | null;
  selectedAgentTargetUnavailable: boolean;
  selectedAgentTargetUnavailableReason: string | null;
  sessionRuntimeBlock: Extract<
    SessionRuntimeAvailability,
    { state: "blocked" }
  > | null;
}): AgentGUISessionChrome["recovery"] {
  const selectedTargetSharingRevoked =
    input.activeConversationId === null &&
    input.selectedAgentTargetUnavailable &&
    input.selectedAgentTargetUnavailableReason === "agent_sharing_revoked";
  if (
    input.sessionRuntimeBlock?.reason !== "agent_sharing_revoked" &&
    !selectedTargetSharingRevoked
  ) {
    return null;
  }
  return {
    kind: "agent-sharing-revoked",
    message: translate("agentHost.agentGui.agentSharingRevoked", {
      owner:
        (input.sessionRuntimeBlock?.reason === "agent_sharing_revoked"
          ? input.sessionRuntimeBlock.ownerLabel
          : null) ??
        input.selectedAgentTargetOwnerLabel ??
        translate("agentHost.agentGui.sharedDeviceLabel")
    }),
    canRetry: false
  };
}
export interface SessionPresentationInput {
  hasSentUserMessage?: boolean;
  activeConversation: AgentGUIConversationSummary | null;
  activeConversationId: string | null;
  activeEngineActiveTurn: AgentActivityTurn | null;
  activeEngineAvailability: "available" | "blocked" | "missing";
  activeEngineHasPendingInteractions: boolean;
  activeHasPendingSubmitStopTarget: boolean;
  activeEngineLatestTurn: AgentActivityTurn | null;
  activeEngineRuntimeAvailability: SessionRuntimeAvailability | null;
  activeEngineRuntimeActivity: "idle" | "running";
  activeCanonicalSession: CanonicalAgentSession | null;
  /** In-flight / waiting session settings update; blocks submit until settled. */
  activeEngineSettingsUpdate: {
    status: string;
  } | null;
  activeLatestPendingSubmitTurnId: string | null;
  activeLiveState: "inactive" | "activating" | "active" | "failed";
  activeMessages: readonly AgentActivityMessage[];
  activePendingActivation: PendingActivationIntentRecord | null;
  activeSessionState: AgentSessionState | null;
  activeTimelineItems: ReturnType<
    typeof projectAgentGUIMessagesToTimelineItems
  >;
  activationError: string | null;
  activationErrorCode: AppErrorCode | null;
  activationState: "inactive" | "activating" | "active" | "failed" | null;
  activityDisplayStatus: AgentActivityDisplayStatus | null;
  agentActivityRuntime: ConversationServices;
  composerSupport: ReturnType<typeof composerSettingsSupportFromOptions>;
  currentUserId?: string | null;
  hasUnconfirmedSubmit: boolean;
  isCreatingConversation: boolean;
  isInterrupting: boolean;
  isLoadingMessages: boolean;
  isRespondingToInteraction: boolean;
  isSubmitting: boolean;
  lastRenderStateDiagnosticKeyRef: CurrentValue<string | null>;
  pendingApproval: AgentApprovalItemVM | null;
  providerReadinessGate:
    | import("../types").AgentGUIProviderReadinessGate
    | null;
  selectedAgentTargetUnavailable: boolean;
  selectedAgentTargetUnavailableReason: string | null;
  selectedAgentTargetOwnerLabel: string | null;
  agentTargetsLoading: boolean;
  ownerDeviceLabel?: string | null;
  serverInteractivePrompt: AgentGUIInteractivePrompt | null;
  conversationServices: AgentConversationServices;
  targetConnectionAgentTargetId?: string | null;
  targetConnectionSource?: AgentGUITargetConnectionSource | null;
  interactionReadinessSource?: AgentGUIInteractionReadinessSource | null;
  observationGapSource?: AgentGUIObservationGapSource | null;
  workspaceId: string;
}
function interactionReadinessReasonMessage(
  reason: AgentGUIInteractionReadinessReason
): string {
  switch (reason) {
    case "owner_offline":
      return translate("agentHost.agentGui.interactionOwnerOffline");
    case "binding_revoked":
      return translate("agentHost.agentGui.interactionBindingRevoked");
    case "synchronizing":
    default:
      return translate("agentHost.agentGui.interactionSynchronizing");
  }
}
export interface SessionPresentationSignals {
  interactiveReadiness: {
    block: Extract<AgentGUIInteractionReadiness, { status: "blocked" }> | null;
    ownsInteraction: boolean;
  };
  approvalReadiness: SessionPresentationSignals["interactiveReadiness"];
  targetConnection: {
    blocked: boolean;
    visibleState: AgentGUITargetConnectionState | null;
  };
  observationGap: AgentGUIObservationGap | null;
}
export function buildSessionPresentation(
  input: SessionPresentationInput,
  signals: SessionPresentationSignals
) {
  const unresolvedInteractivePrompt = input.serverInteractivePrompt;
  const interactiveReadiness = signals.interactiveReadiness;
  const approvalReadiness = signals.approvalReadiness;
  const pendingInteractivePrompt =
    interactiveReadiness.block?.reason === "binding_revoked"
      ? null
      : unresolvedInteractivePrompt;
  const pendingApproval =
    approvalReadiness.block?.reason === "binding_revoked"
      ? null
      : input.pendingApproval;
  const activeInteractionReadiness =
    pendingInteractivePrompt !== null
      ? interactiveReadiness
      : pendingApproval !== null
        ? approvalReadiness
        : unresolvedInteractivePrompt !== null
          ? interactiveReadiness
          : approvalReadiness;
  const interactionReadinessBlock = activeInteractionReadiness.block;
  const interactionReadinessOwnsInteraction =
    activeInteractionReadiness.ownsInteraction;
  const interactivePromptResponsePending =
    input.isRespondingToInteraction || interactiveReadiness.block !== null;
  const approvalResponsePending =
    input.isRespondingToInteraction || approvalReadiness.block !== null;
  const activeActivationAwaitsInitialTurn = Boolean(
    input.activeConversationId &&
      input.activePendingActivation?.mode === "new" &&
      input.activePendingActivation.agentSessionId ===
        input.activeConversationId &&
      input.activePendingActivation.initialTurnExpected &&
      isPendingActivationViable(input.activePendingActivation) &&
      !input.activeEngineLatestTurn
  );
  const hasPendingTurnStartEvidence = Boolean(
    input.activeConversationId &&
      (activeActivationAwaitsInitialTurn ||
        input.hasUnconfirmedSubmit ||
        input.isSubmitting ||
        (!input.activeCanonicalSession &&
          input.activeLatestPendingSubmitTurnId))
  );
  const activeSubmitBlocked = input.activeEngineAvailability === "blocked";
  const sessionRuntimeBlock =
    input.activeEngineRuntimeAvailability?.state === "blocked"
      ? input.activeEngineRuntimeAvailability
      : null;
  const sessionRuntimeBlockedReason = sessionRuntimeBlock?.reason ?? null;
  const targetConnection = signals.targetConnection;
  const observationGap = signals.observationGap;
  const hasCanonicalTerminalActivity =
    input.activityDisplayStatus === "completed" ||
    input.activityDisplayStatus === "failed" ||
    input.activityDisplayStatus === "canceled";
  const executing =
    !hasCanonicalTerminalActivity &&
    (agentActivityDisplayStatusBusy(input.activityDisplayStatus) ||
      (input.activeEngineActiveTurn !== null &&
        input.activeEngineActiveTurn.phase !== "settled") ||
      (!input.activeCanonicalSession &&
        input.activeEngineRuntimeActivity === "running"));
  const isAwaitingTurnStart = hasPendingTurnStartEvidence && !executing;
  const activeConversationBusy =
    !hasCanonicalTerminalActivity &&
    (executing ||
      (!input.activeCanonicalSession &&
        conversationBusyStatus(input.activeConversation?.status ?? null)));
  const activeSessionResumable =
    input.activeCanonicalSession?.resumable ??
    input.activeConversation?.resumable ??
    input.activeSessionState?.resumable;
  const activeConversationRequiresResume =
    input.activeConversationId !== null && input.activationState !== "active";
  const activeConversationResumeUnavailable =
    activeConversationRequiresResume && activeSessionResumable === false;
  const sessionChromeRawState = ((): AgentGUISessionChrome["rawState"] => {
    const agentSessionId =
      input.activeCanonicalSession?.agentSessionId ??
      input.activeConversationId;
    if (!agentSessionId) {
      return null;
    }
    return { agentSessionId };
  })();
  const sessionChrome = ((): AgentGUISessionChrome => {
    if (interactionReadinessBlock?.reason === "binding_revoked") {
      return {
        auth: null,
        approval: null,
        recovery: {
          kind: "agent-sharing-revoked",
          message: translate("agentHost.agentGui.agentSharingRevoked", {
            owner:
              input.selectedAgentTargetOwnerLabel ??
              translate("agentHost.agentGui.sharedDeviceLabel")
          }),
          canRetry: false
        },
        rawState: sessionChromeRawState
      };
    }
    if (interactionReadinessBlock !== null) {
      const synchronizing =
        interactionReadinessBlock.reason === "synchronizing";
      const device =
        input.ownerDeviceLabel?.trim() ||
        translate("agentHost.agentGui.sharedDeviceLabel");
      return {
        auth: null,
        approval: pendingApproval,
        recovery: {
          kind: synchronizing
            ? "transport-connecting"
            : "transport-unavailable",
          message: translate(
            synchronizing
              ? "agentHost.agentGui.runtimeSynchronizingProgress"
              : input.activeEngineActiveTurn !== null
                ? "agentHost.agentGui.runtimeUnavailableActive"
                : "agentHost.agentGui.runtimeUnavailable",
            { device }
          ),
          canRetry: false,
          interactionScoped: true
        },
        rawState: sessionChromeRawState
      };
    }
    if (!interactionReadinessOwnsInteraction) {
      const sharingRevokedRecovery = resolveAgentGUISharingRevokedRecovery({
        activeConversationId: input.activeConversationId,
        selectedAgentTargetOwnerLabel: input.selectedAgentTargetOwnerLabel,
        selectedAgentTargetUnavailable: input.selectedAgentTargetUnavailable,
        selectedAgentTargetUnavailableReason:
          input.selectedAgentTargetUnavailableReason,
        sessionRuntimeBlock
      });
      if (sharingRevokedRecovery) {
        return {
          auth: null,
          approval: null,
          recovery: sharingRevokedRecovery,
          rawState: sessionChromeRawState
        };
      }
    }
    if (
      !interactionReadinessOwnsInteraction &&
      (targetConnection.visibleState?.status === "connecting" ||
        targetConnection.visibleState?.status === "unavailable")
    ) {
      const device =
        input.ownerDeviceLabel?.trim() ||
        translate("agentHost.agentGui.sharedDeviceLabel");
      const reconnecting =
        targetConnection.visibleState.status === "connecting";
      const retryAttempt = targetConnection.visibleState.retryAttempt;
      return {
        auth: null,
        approval: null,
        recovery: {
          kind: reconnecting ? "transport-connecting" : "transport-unavailable",
          message: translate(
            reconnecting
              ? retryAttempt > 0
                ? "agentHost.agentGui.runtimeReconnectingAttempt"
                : "agentHost.agentGui.runtimeConnecting"
              : input.activeEngineActiveTurn !== null
                ? "agentHost.agentGui.runtimeUnavailableActive"
                : "agentHost.agentGui.runtimeUnavailable",
            { attempt: retryAttempt, device }
          ),
          canRetry: false
        },
        rawState: sessionChromeRawState
      };
    }
    if (!interactionReadinessOwnsInteraction && observationGap !== null) {
      return {
        auth: null,
        approval: null,
        recovery: {
          kind: "transport-connecting",
          message: translate("agentHost.agentGui.runtimeSynchronizingProgress"),
          canRetry: false
        },
        rawState: sessionChromeRawState
      };
    }
    const normalizedError = input.activationError?.trim() ?? "";
    const authState = input.activeSessionState?.authState?.trim() ?? "";
    const providerSessionMissing = isNonRetryableResumeErrorCode(
      input.activationErrorCode
    );
    const isAuthError =
      !providerSessionMissing &&
      (authState !== "" ||
        (normalizedError !== "" &&
          /auth|sign in|log in|login|unauthorized|authenticated/i.test(
            normalizedError
          )));
    const isResumeNotLocalRecovery =
      providerSessionMissing || activeConversationResumeUnavailable;
    const recoveryMessage = isResumeNotLocalRecovery
      ? translate(
          input.activeConversation?.isImported === true
            ? "messages.agentImportedSessionResumeUnavailable"
            : "messages.agentResumeSessionNotLocal"
        )
      : normalizedError;
    return {
      auth: providerSessionMissing
        ? null
        : authState !== ""
          ? { message: authState }
          : isAuthError
            ? { message: normalizedError }
            : null,
      approval: pendingApproval,
      recovery:
        input.activeLiveState === "activating" &&
        input.activePendingActivation?.mode !== "new"
          ? {
              kind: "activating",
              message: translate("messages.agentSessionReconnecting")
            }
          : !isAuthError && recoveryMessage
            ? isResumeNotLocalRecovery
              ? {
                  kind: "resume-unavailable",
                  message: recoveryMessage,
                  followupAction: "continue-in-new-conversation" as const
                }
              : {
                  kind: "failed",
                  message: recoveryMessage,
                  canRetry: false
                }
            : null,
      rawState: sessionChromeRawState
    };
  })();
  const hasNonRetryableRecoveryFailure =
    (sessionChrome.recovery?.kind === "failed" &&
      sessionChrome.recovery.canRetry === false) ||
    sessionChrome.recovery?.kind === "resume-unavailable";
  const authBlocked = sessionChrome.auth !== null;
  const isCollaboratorConversation = isDifferentKnownConversationOwner({
    conversationUserId: input.activeConversation?.userId,
    currentUserId: input.currentUserId
  });
  const hasPendingApproval = pendingApproval !== null;
  const hasPendingInteractivePrompt = pendingInteractivePrompt !== null;
  const settingsUpdateStatus = input.activeEngineSettingsUpdate?.status;
  const settingsUpdatePending =
    settingsUpdateStatus === "inFlight" ||
    settingsUpdateStatus === "waitingForRuntime" ||
    settingsUpdateStatus === "unknown" ||
    settingsUpdateStatus === "failed";
  const composerGate = (() =>
    resolveAgentGUIComposerGate({
      activeConversationBusy,
      activeConversationId: input.activeConversationId,
      activeEngineHasPendingInteractions:
        input.activeEngineHasPendingInteractions,
      activeLiveState: input.activeLiveState,
      activeConversationResumeUnavailable,
      agentTargetsLoading: input.agentTargetsLoading,
      authBlocked,
      hasNonRetryableRecoveryFailure,
      isCollaboratorConversation,
      isCreatingConversation: input.isCreatingConversation,
      isInterrupting: input.isInterrupting,
      isAwaitingTurnStart,
      isSubmitting: input.isSubmitting,
      pendingApproval: hasPendingApproval,
      pendingInteractivePrompt: hasPendingInteractivePrompt,
      providerReadinessGate: input.providerReadinessGate,
      selectedAgentTargetUnavailable: input.selectedAgentTargetUnavailable,
      settingsUpdatePending,
      sessionRuntimeBlockedReason,
      targetConnectionBlocked:
        targetConnection.blocked ||
        observationGap !== null ||
        interactionReadinessBlock !== null
    }))();
  const isRespondingApproval =
    approvalResponsePending ||
    (!approvalReadiness.ownsInteraction &&
      composerGate.runtime.status === "blocked");
  const isRespondingInteractivePrompt =
    interactivePromptResponsePending ||
    (!interactiveReadiness.ownsInteraction &&
      composerGate.runtime.status === "blocked");
  const canSubmit = composerGate.submission.status === "ready";
  const canQueueWhileBusy = composerGate.submission.status === "queue";
  const hasSentUserMessage =
    input.hasSentUserMessage ??
    input.activeTimelineItems.some((item) => item.role === "user");

  return {
    approvalDisabledReason: approvalReadiness.block
      ? interactionReadinessReasonMessage(approvalReadiness.block.reason)
      : null,
    activeConversationBusy,
    composerGate,
    hasPendingSubmitStopTarget: input.activeHasPendingSubmitStopTarget,
    hasSentUserMessage,
    interactivePromptDisabledReason: interactiveReadiness.block
      ? interactionReadinessReasonMessage(interactiveReadiness.block.reason)
      : null,
    isRespondingApproval,
    isRespondingInteractivePrompt,
    isAwaitingTurnStart,
    pendingApproval,
    pendingInteractivePrompt,
    sessionChrome
  };
}
export function reportSessionPresentation(
  input: SessionPresentationInput,
  state: ReturnType<typeof buildSessionPresentation>
) {
  const {
    activeConversationBusy,
    isAwaitingTurnStart,
    pendingApproval,
    pendingInteractivePrompt,
    composerGate
  } = state;
  const activeSubmitBlocked = input.activeEngineAvailability === "blocked";
  const canSubmit = composerGate.submission.status === "ready";
  const canQueueWhileBusy = composerGate.submission.status === "queue";

  const diagnosticKey = [
    input.activeConversationId ?? "",
    input.activeConversation?.status ?? "",
    input.activityDisplayStatus ?? "",
    input.activeLiveState,
    input.activeEngineActiveTurn?.phase ??
      input.activeEngineLatestTurn?.phase ??
      "",
    input.activeEngineActiveTurn?.outcome ??
      input.activeEngineLatestTurn?.outcome ??
      "",
    input.activeEngineActiveTurn?.turnId ?? "",
    input.activeEngineAvailability,
    input.activeEngineRuntimeActivity,
    activeConversationBusy ? "busy" : "ready",
    isAwaitingTurnStart ? "awaiting-turn-start" : "turn-start-observed",
    activeSubmitBlocked ? "submit-blocked" : "submit-open",
    pendingApproval?.requestId ?? "",
    promptRequestId(pendingInteractivePrompt) ?? "",
    input.isCreatingConversation ? "creating" : "",
    input.isLoadingMessages ? "loading-messages" : "",
    input.isSubmitting ? "submitting" : "",
    canSubmit ? "can-submit" : "cannot-submit",
    canQueueWhileBusy ? "can-queue" : "cannot-queue",
    composerGate.editor.status,
    composerGate.editor.reason ?? "",
    composerGate.submission.reason ?? "",
    composerGate.runtime.reason ?? ""
  ].join(":");
  if (input.lastRenderStateDiagnosticKeyRef.current === diagnosticKey) return;
  input.lastRenderStateDiagnosticKeyRef.current = diagnosticKey;
  reportAgentGUIRenderStateDiagnostic({
    activeActivityDisplayStatus: input.activityDisplayStatus,
    activeConversation: input.activeConversation,
    activeConversationBusy,
    activeConversationId: input.activeConversationId,
    activeEngineActiveTurn: input.activeEngineActiveTurn,
    activeEngineAvailability: input.activeEngineAvailability,
    activeEngineLatestTurn: input.activeEngineLatestTurn,
    activeEngineRuntimeActivity: input.activeEngineRuntimeActivity,
    isAwaitingTurnStart,
    activeLiveState: input.activeLiveState,
    activeRuntimeSession: input.activeCanonicalSession,
    activeSessionState: input.activeSessionState,
    activeSubmitBlocked,
    canQueueWhileBusy,
    canSubmit,
    conversation: null,
    isCreatingConversation: input.isCreatingConversation,
    isLoadingMessages: input.isLoadingMessages,
    isSubmitting: input.isSubmitting,
    pendingApproval,
    pendingInteractivePrompt,
    runtime: input.agentActivityRuntime,
    workspaceId: input.workspaceId
  });
}
