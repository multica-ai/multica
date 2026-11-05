import { isPendingActivationViable } from "@tutti-os/agent-activity-core";
import { computed, makeObservable } from "mobx";
import {
  matchesAgentGUIConversationSummaryFilter,
  normalizeAgentGUIConversationFilter,
  type AgentGUIConversationFilter
} from "../agent-gui/agentGuiNode/model/agentGuiConversationFilter";
import type { AgentGUIConversationSummary } from "../agent-gui/agentGuiNode/model/agentGuiConversationModel";
import type {
  AgentGUITranscriptReader,
  AgentGUIWorkspaceLoadReader
} from "../conversationServices";
import { createAgentGUIConversationRailTitlePromptSelector } from "../shared/agentConversationRailTitlePromptSelector.ts";
import {
  isAgentGUIProviderUnresolved,
  resolveAgentGUIConversationBrowserFreeTitle,
  resolveAgentGUIConversationTitle,
  resolveAgentGUIConversationTitleDisplayPrompt,
  resolveAgentGUIConversationTitleLeadingMentionKind,
  resolveAgentGUIProviderIdentity
} from "../shared/agentConversationTitleProjection.ts";
import { projectCanonicalAgentGUIConversationSummaries } from "../shared/agentGUIConversationSummaryProjection.ts";
import type { AgentGUIAgentTarget, AgentGUIProvider } from "../types";
import type { AgentConversationServices } from "./conversation-services";
export interface AgentGUIConversationListQuery {
  conversationFilter?: AgentGUIConversationFilter | null;
  workspaceId: string;
  userId: string;
  provider: AgentGUIProvider;
  sessionOrigin: string;
}
export class ConversationListPresentationState {
  constructor(
    private readonly engine: AgentConversationServices,
    private readonly transcript: AgentGUITranscriptReader,
    private readonly workspaceLoad: AgentGUIWorkspaceLoadReader,
    readonly query: AgentGUIConversationListQuery,
    private readonly agentTargets: readonly AgentGUIAgentTarget[]
  ) {
    makeObservable<
      this,
      | "loadStatus"
      | "loadError"
      | "sessions"
      | "rootAgentSessionIdsWithPendingInteractions"
      | "pendingActivations"
      | "selectRailTitlePrompts"
      | "firstUserDisplayPromptsBySessionId"
      | "view"
    >(this, {
      loadStatus: computed.struct,
      loadError: computed.struct,
      sessions: computed.struct,
      rootAgentSessionIdsWithPendingInteractions: computed.struct,
      pendingActivations: computed.struct,
      selectRailTitlePrompts: computed.struct,
      firstUserDisplayPromptsBySessionId: computed.struct,
      view: computed.struct
    });
  }
  private get loadStatus() {
    return this.workspaceLoad.status;
  }
  private get loadError() {
    return this.workspaceLoad.errorMessage;
  }
  private get sessions() {
    return this.engine.sessions.consumerSessions;
  }
  private get rootAgentSessionIdsWithPendingInteractions() {
    return this.engine.sessions.rootSessionIdsWithPendingInteractions;
  }
  private get pendingActivations() {
    return this.engine.activations.pending();
  }
  private get selectRailTitlePrompts() {
    return createAgentGUIConversationRailTitlePromptSelector();
  }
  private get firstUserDisplayPromptsBySessionId() {
    return this.selectRailTitlePrompts(this.transcript);
  }
  private get view() {
    if (!this.query) return null;
    const rootSessionIdsAwaitingUserAction = new Set(
      this.rootAgentSessionIdsWithPendingInteractions
    );
    const canonicalIds = new Set(
      this.sessions.map((item) => item.session.agentSessionId)
    );
    const latestNewActivationBySessionId = new Map(
      this.pendingActivations
        .filter(
          (activation) =>
            activation.mode === "new" && isPendingActivationViable(activation)
        )
        .map((activation) => [activation.agentSessionId, activation] as const)
    );
    const pendingConversations = this.pendingActivations
      .filter(
        (activation) =>
          activation.mode === "new" &&
          isPendingActivationViable(activation) &&
          !canonicalIds.has(activation.agentSessionId)
      )
      .map((activation): AgentGUIConversationSummary => {
        const target = this.agentTargets.find(
          (candidate) => candidate.agentTargetId === activation.agentTargetId
        );
        const provider = resolveAgentGUIProviderIdentity({
          sessionProvider: target?.provider ?? this.query.provider
        });
        const { title: canonicalTitle } = resolveAgentGUIConversationTitle(
          activation.optimisticTitle ?? activation.title ?? ""
        );
        const titleDisplayPrompt =
          resolveAgentGUIConversationTitleDisplayPrompt({
            activation,
            allowEmptyTitle: true,
            title: canonicalTitle
          });
        const { title, titleFallback } = resolveAgentGUIConversationTitle(
          resolveAgentGUIConversationBrowserFreeTitle({
            activation,
            allowEmptyTitle: true,
            title: canonicalTitle
          })
        );
        const titleLeadingMentionKind =
          resolveAgentGUIConversationTitleLeadingMentionKind(
            titleDisplayPrompt
          );
        return {
          agentTargetId: activation.agentTargetId,
          cwd: activation.cwd,
          id: activation.agentSessionId,
          provider,
          sortTimeUnixMs: activation.requestedAtUnixMs,
          status: activation.initialTurnExpected ? "working" : "ready",
          projectionSource: "pending_activation",
          railSectionKey: activation.railSectionKey,
          title,
          titleLeadingMentionKind,
          titleFallback,
          updatedAtUnixMs: activation.requestedAtUnixMs,
          userId: this.query.userId
        };
      });
    const conversations = [
      ...projectCanonicalAgentGUIConversationSummaries(
        this.sessions.filter(
          (item) => item.session.workspaceId === this.query.workspaceId
        ),
        this.firstUserDisplayPromptsBySessionId,
        rootSessionIdsAwaitingUserAction
      ).map((conversation): AgentGUIConversationSummary => {
        const canonicalUpdatedAtUnixMs = conversation.updatedAtUnixMs;
        const activation = latestNewActivationBySessionId.get(conversation.id);
        const activationIsNewer =
          activation !== undefined &&
          activation.requestedAtUnixMs > canonicalUpdatedAtUnixMs;
        const optimisticTitle = activation?.optimisticTitle?.trim() ?? "";
        const shouldUseOptimisticTitle =
          conversation.title.trim().length === 0 && optimisticTitle.length > 0;
        const projectedTitle = shouldUseOptimisticTitle
          ? optimisticTitle
          : activationIsNewer && activation?.title
            ? activation.title
            : conversation.title;
        const { title: projectedCanonicalTitle } =
          resolveAgentGUIConversationTitle(projectedTitle);
        const titleDisplayPrompt =
          resolveAgentGUIConversationTitleDisplayPrompt({
            activation,
            allowEmptyTitle: shouldUseOptimisticTitle,
            title: projectedCanonicalTitle
          });
        const { title, titleFallback } = resolveAgentGUIConversationTitle(
          resolveAgentGUIConversationBrowserFreeTitle({
            activation,
            allowEmptyTitle: shouldUseOptimisticTitle,
            title: projectedCanonicalTitle
          })
        );
        const titleLeadingMentionKind =
          resolveAgentGUIConversationTitleLeadingMentionKind(
            titleDisplayPrompt
          ) ?? conversation.titleLeadingMentionKind;
        return {
          ...conversation,
          sortTimeUnixMs: activationIsNewer
            ? activation.requestedAtUnixMs
            : conversation.sortTimeUnixMs,
          title,
          titleLeadingMentionKind,
          titleFallback,
          updatedAtUnixMs: activationIsNewer
            ? activation.requestedAtUnixMs
            : canonicalUpdatedAtUnixMs
        };
      }),
      ...pendingConversations
    ]
      .filter((conversation) => {
        if (this.query.conversationFilter) {
          return matchesAgentGUIConversationSummaryFilter(
            conversation,
            normalizeAgentGUIConversationFilter(this.query.conversationFilter)
          );
        }
        return (
          conversation.provider === this.query.provider ||
          isAgentGUIProviderUnresolved(conversation.provider)
        );
      })
      .sort(
        (left, right) =>
          (right.sortTimeUnixMs ?? right.updatedAtUnixMs) -
            (left.sortTimeUnixMs ?? left.updatedAtUnixMs) ||
          left.id.localeCompare(right.id)
      );
    return {
      conversations,
      error: this.loadError,
      initialized: this.loadStatus === "ready" || this.loadStatus === "failed",
      isLoading: this.loadStatus === "idle" || this.loadStatus === "loading",
      query: this.query,
      queryKey: [
        this.query.workspaceId,
        this.query.userId,
        this.query.provider,
        this.query.sessionOrigin
      ].join("::")
    };
  }
  get conversations() {
    return this.view?.conversations ?? [];
  }
  get error() {
    return this.view?.error ?? null;
  }
  get initialized() {
    return this.view?.initialized ?? false;
  }
  get isLoading() {
    return this.view?.isLoading ?? false;
  }
  get queryKey() {
    return this.view?.queryKey ?? "";
  }
}
