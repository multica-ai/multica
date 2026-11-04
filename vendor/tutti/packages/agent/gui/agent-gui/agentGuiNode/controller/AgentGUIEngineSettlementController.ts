import {
  selectEngineInteraction,
  selectEngineHasVisibleQueuedSubmit,
  selectPendingActivations,
  selectPendingSubmitsForSession,
  type AgentSessionEngine,
  type AgentSessionEngineState,
  type PendingSubmitIntentRecord
} from "@tutti-os/agent-activity-core";
import { agentComposerDraftPreservingConnectors } from "../model/agentComposerDraft";
import { resolveAgentComposerDraftScopeKey } from "../model/agentComposerDraftScope";
import type {
  AgentComposerDraft,
  SubmittedDraftSnapshot
} from "../model/agentGuiNodeTypes";
import { clearSubmittedDraftIfUnchanged } from "./agentGuiController.draftMessageHelpers";
import type { AgentGUIPromptOperations } from "./agentGuiController.promptOperations";
import { restoreFailedAgentGUIHomeDraft } from "./agentGuiController.homeDraftHelpers";

export interface AgentGUIInteractionDraftPendingSettlement {
  agentSessionId: string;
  requestId: string;
  submittedDraftSnapshot: SubmittedDraftSnapshot;
  turnId: string;
}

export function agentGUIInteractionDraftSettlementKey(input: {
  agentSessionId: string;
  requestId: string;
  turnId: string;
}): string {
  return JSON.stringify([
    input.agentSessionId.trim(),
    input.turnId.trim(),
    input.requestId.trim()
  ]);
}

interface AgentGUIEngineSettlementControllerInput {
  applyDraftUpdate(
    update: (
      current: Record<string, AgentComposerDraft>
    ) => Record<string, AgentComposerDraft>
  ): void;
  engine: AgentSessionEngine;
  prompts: Pick<
    AgentGUIPromptOperations,
    "interactionResponse" | "subscribeInteractionResponses"
  >;
  interactionDraftSettlements?: Record<
    string,
    AgentGUIInteractionDraftPendingSettlement
  >;
  isCurrentConversation?(agentSessionId: string): boolean;
  onSubmitFailed?(submit: PendingSubmitIntentRecord): void;
  snapshots: Record<string, SubmittedDraftSnapshot>;
}

export class AgentGUIEngineSettlementController {
  private readonly applyDraftUpdate: AgentGUIEngineSettlementControllerInput["applyDraftUpdate"];
  private readonly engine: AgentSessionEngine;
  private readonly prompts: AgentGUIEngineSettlementControllerInput["prompts"];
  private readonly isCurrentConversation: NonNullable<
    AgentGUIEngineSettlementControllerInput["isCurrentConversation"]
  >;
  private readonly interactionDraftSettlements: Record<
    string,
    AgentGUIInteractionDraftPendingSettlement
  >;
  private readonly onSubmitFailed: NonNullable<
    AgentGUIEngineSettlementControllerInput["onSubmitFailed"]
  >;
  private readonly snapshots: Record<string, SubmittedDraftSnapshot>;
  private unsubscribe: (() => void) | null = null;

  constructor(input: AgentGUIEngineSettlementControllerInput) {
    this.applyDraftUpdate = input.applyDraftUpdate;
    this.engine = input.engine;
    this.prompts = input.prompts;
    this.interactionDraftSettlements = input.interactionDraftSettlements ?? {};
    this.isCurrentConversation = input.isCurrentConversation ?? (() => false);
    this.onSubmitFailed = input.onSubmitFailed ?? (() => undefined);
    this.snapshots = input.snapshots;
  }

  attach(): () => void {
    if (this.unsubscribe) return this.unsubscribe;
    this.settle(this.engine.getSnapshot());
    const unsubscribe = this.engine.subscribe((state) => this.settle(state));
    // A failed answer changes nothing in the engine.
    const unsubscribeResponses = this.prompts.subscribeInteractionResponses(() =>
      this.settle(this.engine.getSnapshot())
    );
    this.unsubscribe = () => {
      unsubscribe();
      unsubscribeResponses();
      this.unsubscribe = null;
    };
    return this.unsubscribe;
  }

  private settle(state: AgentSessionEngineState): void {
    const activationsByClientSubmitId = new Map(
      selectPendingActivations(state).flatMap((record) => {
        const clientSubmitId = record.clientSubmitId?.trim() ?? "";
        return record.mode === "new" && clientSubmitId
          ? [[clientSubmitId, record] as const]
          : [];
      })
    );
    for (const [clientSubmitId, snapshot] of Object.entries(this.snapshots)) {
      const activation = activationsByClientSubmitId.get(clientSubmitId);
      if (
        activation?.status === "confirmed" ||
        activation?.status === "failed" ||
        activation?.status === "canceled"
      ) {
        this.applyDraftUpdate((drafts) =>
          activation.status === "confirmed"
            ? seedActivatedSessionDraft(
                clearSubmittedDraftIfUnchanged({ drafts, snapshot }),
                activation.agentSessionId,
                snapshot
              )
            : restoreFailedAgentGUIHomeDraft({
                draftKey: snapshot.sourceScopeKey,
                drafts,
                submittedDraft: snapshot.content
              })
        );
        delete this.snapshots[clientSubmitId];
        continue;
      }

      const targetAgentSessionId =
        snapshot.targetAgentSessionId ??
        (snapshot.sourceScopeKey.startsWith("session:")
          ? snapshot.sourceScopeKey.slice("session:".length)
          : "");
      if (!targetAgentSessionId) continue;
      const submit = selectPendingSubmitsForSession(
        state,
        targetAgentSessionId
      ).find((record) => record.clientSubmitId === clientSubmitId);
      if (
        submit?.status !== "accepted" &&
        submit?.status !== "confirmed" &&
        submit?.status !== "failed"
      ) {
        continue;
      }
      if (
        submit.status === "failed" &&
        selectEngineHasVisibleQueuedSubmit(
          state,
          submit.agentSessionId,
          clientSubmitId
        )
      ) {
        continue;
      }
      if (
        submit.status === "failed" &&
        this.isCurrentConversation(submit.agentSessionId)
      ) {
        this.onSubmitFailed(submit);
      }
      this.applyDraftUpdate((drafts) =>
        submit.status === "failed"
          ? restoreFailedAgentGUIHomeDraft({
              draftKey: snapshot.sourceScopeKey,
              drafts,
              submittedDraft: snapshot.content
            })
          : clearSubmittedDraftIfUnchanged({ drafts, snapshot })
      );
      delete this.snapshots[clientSubmitId];
    }
    this.settleInteractionDrafts(state);
  }

  private settleInteractionDrafts(state: AgentSessionEngineState): void {
    for (const [key, pending] of Object.entries(
      this.interactionDraftSettlements
    )) {
      const interaction = selectEngineInteraction(
        state,
        pending.agentSessionId,
        pending.turnId,
        pending.requestId
      );
      const response = this.prompts.interactionResponse(
        pending.agentSessionId,
        pending.turnId,
        pending.requestId
      );
      if (interaction?.status === "pending" && response?.status !== "failed") {
        continue;
      }
      delete this.interactionDraftSettlements[key];
      if (interaction?.status === "answered") {
        continue;
      }
      const snapshot = pending.submittedDraftSnapshot;
      this.applyDraftUpdate((drafts) =>
        restoreFailedAgentGUIHomeDraft({
          draftKey: snapshot.sourceScopeKey,
          drafts,
          submittedDraft: snapshot.content
        })
      );
    }
  }

}

function seedActivatedSessionDraft(
  drafts: Record<string, AgentComposerDraft>,
  agentSessionId: string,
  snapshot: SubmittedDraftSnapshot
): Record<string, AgentComposerDraft> {
  const sessionScopeKey = resolveAgentComposerDraftScopeKey({
    agentSessionId
  });
  if (!agentSessionId.trim() || snapshot.sourceScopeKey === sessionScopeKey) {
    return drafts;
  }
  return {
    ...drafts,
    [sessionScopeKey]: agentComposerDraftPreservingConnectors(snapshot.content)
  };
}
