import { computed, makeObservable } from "mobx";
import type { AgentGUIRailViewModel } from "../agent-gui/agentGuiNode/model/agentGuiNodeTypes";
import type { SessionPresentationState } from "./SessionPresentationState";

export class RailPresentationState {
  constructor(
    private readonly input: Omit<
      AgentGUIRailViewModel,
      "activeConversation" | "conversations"
    > & {
      overview: {
        readonly activeConversation: AgentGUIRailViewModel["activeConversation"];
        readonly visibleConversations: AgentGUIRailViewModel["conversations"];
      };
      session: SessionPresentationState;
    }
  ) {
    makeObservable(this, {
      activeConversation: computed.struct,
      conversations: computed.struct
    });
  }
  get selectedAgentTarget() {
    return this.input.selectedAgentTarget;
  }
  get agentTargets() {
    return this.input.agentTargets;
  }
  get agentTargetsLoading() {
    return this.input.agentTargetsLoading;
  }
  get providerRailMode() {
    return this.input.providerRailMode;
  }
  get comingSoonProviders() {
    return this.input.comingSoonProviders;
  }
  get conversationFilter() {
    return this.input.conversationFilter;
  }
  get userProjects() {
    return this.input.userProjects;
  }
  get activeConversationId() {
    return this.input.activeConversationId;
  }
  get revealRequest() {
    return this.input.revealRequest;
  }
  get isLoadingConversations() {
    return this.input.isLoadingConversations;
  }
  get listError() {
    return this.input.listError;
  }
  get activeConversation() {
    const value = this.input.overview.activeConversation;
    return value &&
      this.input.session.pendingInteractivePrompt?.kind ===
        "plan-implementation" &&
      !value.needsUserAction
      ? { ...value, needsUserAction: true }
      : value;
  }
  get conversations() {
    const values = this.input.overview.visibleConversations;
    return this.input.session.pendingInteractivePrompt?.kind ===
      "plan-implementation"
      ? values.map((value) =>
          value.id === this.activeConversationId && !value.needsUserAction
            ? { ...value, needsUserAction: true }
            : value
        )
      : values;
  }
}
