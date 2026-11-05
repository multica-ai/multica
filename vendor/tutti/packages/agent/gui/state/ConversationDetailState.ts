import { computed, makeObservable } from "mobx";
import type { ConversationServices } from "../conversationServices";
import type { ActiveConversationState } from "./ActiveConversationState";
import type { ComposerCapabilitiesState } from "./ComposerCapabilitiesState";
import type { SessionPresentationState } from "./SessionPresentationState";

export class ConversationDetailState {
  constructor(
    private readonly input: {
      native: ActiveConversationState;
      services: ConversationServices;
      capabilities: ComposerCapabilitiesState;
      session: SessionPresentationState;
      isLoadingMessages: boolean;
      isLoadingOlderMessages: boolean;
      detailError: string | null;
      avoidGroupingEdits: boolean;
    }
  ) {
    makeObservable(this, {
      availability: computed,
      isLoadingMessages: computed,
      hasOlderMessages: computed,
      usage: computed,
      hasSentUserMessage: computed
    });
  }
  get isLoadingMessages() {
    return (
      this.input.isLoadingMessages ||
      this.input.native.activeSessionDetailLoading
    );
  }
  get availability(): "ready" | "loading" | "not_found" | "error" {
    const native = this.input.native;
    return native.activeConversationId === null
      ? "ready"
      : native.activeCanonicalSessionDeleted
        ? "not_found"
        : this.isLoadingMessages
          ? "loading"
          : native.activeSessionReconcileError || this.input.detailError
            ? "error"
            : "ready";
  }
  get hasOlderMessages() {
    const id = this.input.native.activeConversationId;
    return id ? this.input.services.transcript.hasOlderMessages(id) : false;
  }
  get isLoadingOlderMessages() {
    return this.input.isLoadingOlderMessages;
  }
  get usage() {
    return this.input.capabilities.usage;
  }
  get hasSentUserMessage() {
    return this.input.session.hasSentUserMessage;
  }
  get avoidGroupingEdits() {
    return this.input.avoidGroupingEdits;
  }
}
