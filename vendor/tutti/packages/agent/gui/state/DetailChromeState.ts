import { computed, makeObservable } from "mobx";
import type { AgentComposerSlashStatusLimit } from "../agent-gui/agentGuiNode/AgentComposer";
import type { AgentGUIViewLabels } from "../agent-gui/agentGuiNode/AgentGUINodeView";
import type { AgentSessionChromeSource } from "../agent-gui/agentGuiNode/AgentSessionChrome";
import type {
  AgentGUIComposerViewModel,
  AgentGUIDetailViewModel,
  AgentGUIInteractionViewModel,
  AgentGUIRailViewModel,
  AgentGUIReadinessViewModel,
  AgentGUISessionChrome
} from "../agent-gui/agentGuiNode/model/agentGuiNodeTypes";
import {
  isAgentGUIHomeStatusNoticeVisible,
  isContextCanceledMessage,
  resolveAgentGUIHomeNoticeChrome,
  resolveAgentGUIStopControl,
  resolveSlashStatus
} from "../agent-gui/agentGuiNode/view/agentGUIDetailModelHelpers";
import type { ActiveConversationState } from "./ActiveConversationState";
interface Input {
  labels: AgentGUIViewLabels;
  slashStatusLimits: readonly AgentComposerSlashStatusLimit[];
  slashStatusLimitsLoading: boolean;
  slashStatusLimitsUnavailable: boolean;
  rail: AgentGUIRailViewModel;
  detail: AgentGUIDetailViewModel;
  composer: AgentGUIComposerViewModel;
  interaction: AgentGUIInteractionViewModel;
  readiness: AgentGUIReadinessViewModel;
  nativeState: ActiveConversationState;
}
export class DetailChromeState {
  constructor(private readonly input: Input) {
    makeObservable(this, {
      hasActiveConversation: computed.struct,
      activePrompt: computed.struct,
      activePromptResponsePending: computed.struct,
      activePromptRequestId: computed.struct,
      sessionChrome: computed.struct,
      homeStatusNoticeVisible: computed.struct,
      rawSlashStatus: computed.struct,
      displayedInlineNotice: computed.struct,
      inlineNoticeChrome: computed.struct,
      homeNoticeChrome: computed.struct,
      activeConversationTurnBusy: computed.struct,
      isComposerSending: computed.struct,
      isCollaboratorConversation: computed.struct,
      composerDisabledReason: computed.struct,
      stopControl: computed.struct,
      showStopButton: computed.struct,
      stopDisabled: computed.struct
    });
  }
  get hasActiveConversation() {
    return this.input.rail.activeConversationId !== null;
  }
  get activePrompt() {
    return (
      this.input.interaction.pendingInteractivePrompt ??
      this.input.interaction.pendingApproval
    );
  }
  get activePromptResponsePending() {
    return this.activePrompt?.kind === "approval"
      ? this.input.interaction.isRespondingApproval
      : this.input.interaction.isRespondingInteractivePrompt;
  }
  get activePromptRequestId() {
    return this.activePrompt?.requestId ?? null;
  }
  get sessionChrome(): AgentGUISessionChrome {
    return { ...this.input.interaction.sessionChrome, approval: null };
  }
  get homeStatusNoticeVisible() {
    return isAgentGUIHomeStatusNoticeVisible(this.sessionChrome.recovery);
  }
  get rawSlashStatus() {
    return resolveSlashStatus({
      rawState: this.input.interaction.sessionChrome.rawState,
      limits: this.input.slashStatusLimits,
      limitsLoading: this.input.slashStatusLimitsLoading,
      limitsUnavailable: this.input.slashStatusLimitsUnavailable,
      usage: this.input.detail.usage
    });
  }
  get displayedInlineNotice() {
    if (this.homeStatusNoticeVisible) {
      return null;
    }
    const inlineNotice = this.input.interaction.inlineNotice;
    const inlineNoticeMessage = inlineNotice?.message.trim() ?? "";
    if (!inlineNotice || inlineNoticeMessage === "") {
      return null;
    }
    if (
      isContextCanceledMessage(inlineNoticeMessage) &&
      this.input.rail.activeConversation?.status === "completed" &&
      this.input.readiness.activeLiveState !== "failed"
    ) {
      return null;
    }
    const chromeMessages = [
      this.sessionChrome.auth?.message,
      this.sessionChrome.recovery?.message
    ].flatMap((message) => {
      const normalizedMessage = message?.trim() ?? "";
      return normalizedMessage === "" ? [] : [normalizedMessage];
    });
    return chromeMessages.includes(inlineNoticeMessage)
      ? null
      : { ...inlineNotice, message: inlineNoticeMessage };
  }
  get inlineNoticeChrome(): AgentGUISessionChrome | null {
    if (!this.displayedInlineNotice) {
      return null;
    }
    return {
      auth: null,
      approval: null,
      recovery: {
        kind:
          this.displayedInlineNotice.tone === "warning" ? "warning" : "failed",
        message: this.displayedInlineNotice.message,
        canRetry: this.displayedInlineNotice.id.startsWith(
          "agent-gui-tutti-mode-update-failed"
        )
      },
      rawState: null
    };
  }
  get homeNoticeChrome() {
    return resolveAgentGUIHomeNoticeChrome({
      inlineNoticeChrome: this.inlineNoticeChrome,
      sessionChrome: this.sessionChrome
    });
  }
  get activeConversationTurnBusy() {
    return this.input.composer.state.gate.conversationBusy;
  }
  get isComposerSending() {
    return (
      this.activeConversationTurnBusy ||
      this.input.composer.state.gate.isAwaitingTurnStart === true ||
      (!this.hasActiveConversation &&
        this.input.composer.state.gate.submission.status === "blocked" &&
        this.input.composer.state.gate.submission.reason ===
          "creating_conversation")
    );
  }
  get isCollaboratorConversation() {
    return (
      this.input.composer.state.gate.editor.status === "blocked" &&
      this.input.composer.state.gate.editor.reason === "collaborator_read_only"
    );
  }
  get composerDisabledReason() {
    return this.isCollaboratorConversation
      ? this.input.labels.collaboratorSessionReadOnlyPlaceholder
      : null;
  }
  get stopControl() {
    return resolveAgentGUIStopControl({
      hasPendingApproval: this.input.interaction.pendingApproval !== null,
      hasPendingInteractivePrompt:
        this.input.interaction.pendingInteractivePrompt !== null,
      isAuthBlocked: this.sessionChrome.auth !== null,
      isCancelPending: this.input.nativeState.isCancelPending,
      isConversationBusy: this.activeConversationTurnBusy,
      isCreatingConversation: this.input.nativeState.isCreatingConversation,
      hasPendingSubmitStopTarget:
        this.input.nativeState.hasPendingSubmitStopTarget === true,
      isInterrupting: this.input.nativeState.isInterrupting,
      isSubmitting: this.input.nativeState.isSubmitting,
      isUnavailable: this.input.readiness.activeLiveState === "failed"
    });
  }
  get showStopButton() {
    return this.stopControl.visible;
  }
  get stopDisabled() {
    return this.stopControl.disabled;
  }
  private source(
    read: () => AgentGUISessionChrome | null
  ): AgentSessionChromeSource {
    const state = this;
    return {
      get chrome() {
        return read();
      },
      get isRespondingApproval() {
        return state.input.interaction.isRespondingApproval;
      },
      get approvalDisabledReason() {
        return state.input.interaction.approvalDisabledReason;
      }
    };
  }
  readonly statusSource = this.source(() => this.sessionChrome);
  readonly inlineSource = this.source(() => this.inlineNoticeChrome);
  readonly homeSource = this.source(() => this.homeNoticeChrome);
}
