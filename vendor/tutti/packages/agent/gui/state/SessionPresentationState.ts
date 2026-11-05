import { computed, makeObservable } from "mobx";
import type { AgentGUIAgentTarget } from "../types";
import type { ActiveConversationState } from "./ActiveConversationState";
import type { ComposerCapabilitiesState } from "./ComposerCapabilitiesState";
import { conversationTranscriptStateFor } from "./ConversationTranscriptState";
import type { SessionPresentationHostService } from "./SessionPresentationHostService";
import {
  buildSessionPresentation,
  type SessionPresentationInput
} from "./sessionPresentation";

export type SessionPresentationStateInput = Omit<
  SessionPresentationInput,
  | "activeConversationId"
  | "activeEngineActiveTurn"
  | "activeEngineAvailability"
  | "activeEngineHasPendingInteractions"
  | "activeHasPendingSubmitStopTarget"
  | "activeEngineLatestTurn"
  | "activeEngineRuntimeAvailability"
  | "activeEngineRuntimeActivity"
  | "activeCanonicalSession"
  | "activeEngineSettingsUpdate"
  | "activeLatestPendingSubmitTurnId"
  | "activeLiveState"
  | "activePendingActivation"
  | "activeSessionState"
  | "activationError"
  | "activationErrorCode"
  | "activationState"
  | "activityDisplayStatus"
  | "hasUnconfirmedSubmit"
  | "isCreatingConversation"
  | "isInterrupting"
  | "isRespondingToInteraction"
  | "isSubmitting"
  | "pendingApproval"
  | "serverInteractivePrompt"
  | "activeMessages"
  | "activeTimelineItems"
  | "composerSupport"
  | "activeConversation"
  | "conversationServices"
  | "targetConnectionAgentTargetId"
  | "ownerDeviceLabel"
  | "lastRenderStateDiagnosticKeyRef"
> & {
  native: ActiveConversationState;
  capabilities: ComposerCapabilitiesState;
  conversation(): SessionPresentationInput["activeConversation"];
  selectedTarget: AgentGUIAgentTarget;
  targets: readonly AgentGUIAgentTarget[];
  detailError: string | null;
  workspacePath: string;
  avoidGroupingEdits: boolean;
  locale?: string;
};

/** Native conversation state and host readiness remain live until the consuming observer reads them. */
export class SessionPresentationState {
  constructor(
    private readonly config: SessionPresentationStateInput,
    private readonly host: SessionPresentationHostService
  ) {
    makeObservable<this, "value">(this, {
      value: computed.struct,
      activeLiveState: computed,
      activationError: computed,
      providerReadinessGate: computed,
      inlineNotice: computed.struct,
      canAnswerPendingInteractivePromptFromComposer: computed,
      approvalDisabledReason: computed.struct,
      activeConversationBusy: computed.struct,
      composerGate: computed.struct,
      hasPendingSubmitStopTarget: computed.struct,
      hasSentUserMessage: computed.struct,
      interactivePromptDisabledReason: computed.struct,
      isRespondingApproval: computed.struct,
      isRespondingInteractivePrompt: computed.struct,
      isAwaitingTurnStart: computed.struct,
      pendingApproval: computed.struct,
      pendingInteractivePrompt: computed.struct,
      sessionChrome: computed.struct
    });
  }

  private get input(): SessionPresentationInput & {
    hasSentUserMessage: boolean;
  } {
    const native = this.config.native;
    return {
      ...this.config,
      activeConversationId: native.activeConversationId,
      activeEngineActiveTurn: native.activeEngineActiveTurn,
      activeEngineAvailability: native.activeEngineAvailability,
      activeEngineHasPendingInteractions: native.pendingInteractions.length > 0,
      activeHasPendingSubmitStopTarget: native.activeHasPendingSubmitStopTarget,
      activeEngineLatestTurn: native.activeEngineLatestTurn,
      activeEngineRuntimeAvailability: native.activeEngineRuntimeAvailability,
      activeEngineRuntimeActivity: native.activeEngineRuntimeActivity,
      activeCanonicalSession: native.activeCanonicalSession,
      activeEngineSettingsUpdate: native.activeEngineSettingsUpdate,
      activeLatestPendingSubmitTurnId:
        native.activeLatestPendingSubmit?.turnId ?? null,
      activeLiveState: native.activeLiveState,
      activePendingActivation: native.activePendingActivation,
      activeSessionState: native.activeSessionState,
      activationError: native.activationError,
      activationErrorCode: native.activationErrorCode,
      activationState: native.activationPresentation.status,
      activityDisplayStatus: native.activityDisplayStatus,
      hasUnconfirmedSubmit: native.hasUnconfirmedSubmit,
      isCreatingConversation: native.isCreatingConversation,
      isInterrupting: native.isInterrupting,
      isRespondingToInteraction: native.relatedIsRespondingToInteraction,
      isSubmitting: native.isSubmitting,
      pendingApproval: native.pendingApproval,
      serverInteractivePrompt: native.serverInteractivePrompt,
      activeConversation: this.config.conversation(),
      activeMessages: [],
      activeTimelineItems: [],
      hasSentUserMessage: this.hasCommittedUserMessage,
      composerSupport: this.config.capabilities.composerSupport,
      conversationServices: this.config.agentActivityRuntime,
      targetConnectionAgentTargetId: this.host.targetConnection.agentTargetId,
      ownerDeviceLabel: this.host.targetConnection.ownerDeviceLabel,
      isLoadingMessages:
        this.config.isLoadingMessages || native.activeSessionDetailLoading,
      lastRenderStateDiagnosticKeyRef: this.host.diagnosticKey
    };
  }
  private get hasCommittedUserMessage() {
    const id = this.config.native.activeConversationId;
    return id
      ? conversationTranscriptStateFor(
          this.config.agentActivityRuntime.transcript,
          id
        ).hasUserMessage
      : false;
  }

  private get value() {
    return buildSessionPresentation(this.input, this.host.signals);
  }
  get activeLiveState() {
    return this.config.native.activeLiveState;
  }
  get activationError() {
    return this.config.native.activationError;
  }
  get providerReadinessGate() {
    return this.config.providerReadinessGate;
  }
  get inlineNotice() {
    const native = this.config.native;
    const message =
      this.config.detailError ??
      native.activeSessionReconcileError ??
      (native.activeConversationId !== null ? native.activeSessionError : null);
    return message
      ? {
          id: `agent-gui-detail-error:${native.activeConversationId ?? "current"}`,
          message,
          tone: "error" as const,
          autoDismissMs: null
        }
      : null;
  }
  get canAnswerPendingInteractivePromptFromComposer() {
    const prompt = this.pendingInteractivePrompt,
      target = this.config.native.pendingQuestionComposerTarget;
    return (
      prompt?.kind === "ask-user" &&
      target !== null &&
      prompt.agentSessionId?.trim() === target.agentSessionId &&
      prompt.turnId?.trim() === target.turnId &&
      prompt.requestId === target.requestId
    );
  }
  get diagnostic() {
    return { input: this.input, value: this.value };
  }
  get approvalDisabledReason() {
    return this.value.approvalDisabledReason;
  }
  get activeConversationBusy() {
    return this.value.activeConversationBusy;
  }
  get composerGate() {
    return this.value.composerGate;
  }
  get hasPendingSubmitStopTarget() {
    return this.value.hasPendingSubmitStopTarget;
  }
  get hasSentUserMessage() {
    return this.value.hasSentUserMessage;
  }
  get interactivePromptDisabledReason() {
    return this.value.interactivePromptDisabledReason;
  }
  get isRespondingApproval() {
    return this.value.isRespondingApproval;
  }
  get isRespondingInteractivePrompt() {
    return this.value.isRespondingInteractivePrompt;
  }
  get isAwaitingTurnStart() {
    return this.value.isAwaitingTurnStart;
  }
  get pendingApproval() {
    return this.value.pendingApproval;
  }
  get pendingInteractivePrompt() {
    return this.value.pendingInteractivePrompt;
  }
  get sessionChrome() {
    return this.value.sessionChrome;
  }
}
