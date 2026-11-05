import { computed, makeObservable } from "mobx";
import { providerSkillsFromComposerOptions } from "../agent-gui/agentGuiNode/controller/agentGuiController.composerHelpers";
import type { AgentGUIAgentTarget } from "../types";
import type { ActiveConversationState } from "./ActiveConversationState";
import type { ComposerCapabilitiesState } from "./ComposerCapabilitiesState";
import type { ComposerSettingsState } from "./ComposerSettingsState";
import type { SessionPresentationState } from "./SessionPresentationState";

/** Drafts stay in their existing client store; native-derived choices and gates are computed here. */
export class ComposerFeatureState {
  constructor(
    private readonly input: {
      native: ActiveConversationState;
      capabilities: ComposerCapabilitiesState;
      settings: ComposerSettingsState;
      session: SessionPresentationState;
      handoffAgentTargets: readonly AgentGUIAgentTarget[];
    }
  ) {
    makeObservable(this, {
      availableCommands: computed.struct,
      availableSkills: computed.struct,
      promptImagesSupported: computed,
      compactSupported: computed,
      gate: computed,
      composerSettings: computed,
      usage: computed,
      hasCompactableContext: computed,
      isSendingTurn: computed,
      isSubmittingPrompt: computed,
      activeTurnId: computed
    });
  }
  get handoffAgentTargets() {
    return this.input.handoffAgentTargets;
  }
  get availableCommands() {
    return (
      this.input.capabilities.providerComposerOptions?.commands?.map(
        (command) => ({ ...command })
      ) ?? []
    );
  }
  get availableSkills() {
    return providerSkillsFromComposerOptions(
      this.input.capabilities.providerComposerOptions
    );
  }
  get promptImagesSupported() {
    return this.input.capabilities.promptImagesSupported;
  }
  get compactSupported() {
    return this.input.capabilities.compactSupported;
  }
  get gate() {
    return this.input.session.composerGate;
  }
  get usage() {
    return this.input.capabilities.usage;
  }
  get hasCompactableContext() {
    return this.input.session.hasSentUserMessage;
  }
  get isSendingTurn() {
    return (
      this.gate.conversationBusy ||
      this.gate.isAwaitingTurnStart === true ||
      (this.input.native.activeConversationId === null &&
        this.gate.submission.status === "blocked" &&
        this.gate.submission.reason === "creating_conversation")
    );
  }
  get isSubmittingPrompt() {
    return this.input.session.pendingInteractivePrompt
      ? this.input.session.isRespondingInteractivePrompt
      : this.input.session.pendingApproval
        ? this.input.session.isRespondingApproval
        : this.input.session.isRespondingInteractivePrompt;
  }
  get activeTurnId() {
    return this.input.native.activeEngineActiveTurn?.turnId ?? null;
  }
  get composerSettings() {
    return this.input.settings.composerSettings;
  }
}
