import { autorun, computed, makeObservable, reaction } from "mobx";
import { AgentGUITargetConnectionController } from "../agent-gui/agentGuiNode/controller/AgentGUITargetConnectionController";
import { resolveAgentGUIInteractionReadinessIdentity } from "../agent-gui/agentGuiNode/controller/agentGuiController.interactionHelpers";
import { targetConnectionForAgentGUIView } from "../agent-gui/agentGuiNode/controller/agentGuiController.providerHelpers";
import { readAgentGUIInteractionReadiness } from "../agent-gui/agentGuiNode/controller/useAgentGUIInteractionReadiness";
import { ObservedHostSource } from "./ObservedHostSource";
import type {
  SessionPresentationState,
  SessionPresentationStateInput
} from "./SessionPresentationState";
import {
  reportSessionPresentation,
  type SessionPresentationInput,
  type SessionPresentationSignals
} from "./sessionPresentation";

/** Owns host subscriptions, delayed connection notices and diagnostic lifetime. */
export class SessionPresentationHostService {
  readonly diagnosticKey = { current: null as string | null };
  private readonly host = new ObservedHostSource(
    () => null,
    (listener) => {
      const stops = [
        this.config.targetConnectionSource,
        this.config.interactionReadinessSource,
        this.config.observationGapSource
      ].map((source) => source?.subscribe(listener));
      return () => stops.forEach((stop) => stop?.());
    }
  );
  private readonly connectionController: AgentGUITargetConnectionController;
  private readonly visibleConnection: ObservedHostSource<
    ReturnType<AgentGUITargetConnectionController["getSnapshot"]>
  >;
  constructor(private readonly config: SessionPresentationStateInput) {
    const binding = {
      getSnapshot: () => {
        const id = this.targetConnection.agentTargetId;
        this.host.value;
        return id
          ? (this.config.targetConnectionSource?.getConnectionState(id) ?? null)
          : null;
      },
      subscribe: (listener: () => void) => {
        const stopHost =
          this.config.targetConnectionSource?.subscribe(listener);
        const stopIdentity = reaction(
          () => this.targetConnection.agentTargetId,
          listener
        );
        return () => {
          stopHost?.();
          stopIdentity();
        };
      }
    };
    this.connectionController = new AgentGUITargetConnectionController({
      source: binding
    });
    this.visibleConnection = new ObservedHostSource(
      this.connectionController.getSnapshot,
      this.connectionController.subscribe
    );

    makeObservable<this, "signals" | "targetConnection">(this, {
      signals: computed.struct,
      targetConnection: computed.struct
    });
  }
  get targetConnection() {
    return targetConnectionForAgentGUIView({
      activeConversation: this.config.conversation(),
      selectedTarget: this.config.selectedTarget,
      targets: this.config.targets
    });
  }
  private readiness(
    prompt:
      | SessionPresentationInput["pendingApproval"]
      | SessionPresentationInput["serverInteractivePrompt"]
  ): SessionPresentationSignals["interactiveReadiness"] {
    this.host.value;
    const source = this.config.interactionReadinessSource;
    const identity = resolveAgentGUIInteractionReadinessIdentity({
      agentSessionId:
        prompt && "agentSessionId" in prompt ? prompt.agentSessionId : null,
      requestId: prompt?.requestId,
      turnId: prompt && "turnId" in prompt ? prompt.turnId : null,
      workspaceId: this.config.workspaceId
    });
    const value = identity
      ? readAgentGUIInteractionReadiness({ identity, source })
      : source && prompt
        ? { status: "blocked" as const, reason: "synchronizing" as const }
        : null;
    return {
      block: value?.status === "blocked" ? value : null,
      ownsInteraction: source != null && prompt !== null
    };
  }
  get signals(): SessionPresentationSignals {
    this.host.value;
    const native = this.config.native;
    const id = this.targetConnection.agentTargetId;
    const raw = id
      ? this.config.targetConnectionSource?.getConnectionState(id)
      : null;
    const turnId = native.activeEngineActiveTurn?.turnId;
    return {
      interactiveReadiness: this.readiness(native.serverInteractivePrompt),
      approvalReadiness: this.readiness(native.pendingApproval),
      targetConnection: {
        blocked: raw?.status === "connecting" || raw?.status === "unavailable",
        visibleState: this.visibleConnection.value
      },
      observationGap:
        native.activeConversationId && turnId
          ? (this.config.observationGapSource?.getObservationGap(
              native.activeConversationId,
              turnId
            ) ?? null)
          : null
    };
  }
  startDiagnostics(state: SessionPresentationState): () => void {
    return autorun(() => {
      const diagnostic = state.diagnostic;
      reportSessionPresentation(diagnostic.input, diagnostic.value);
    });
  }
}
