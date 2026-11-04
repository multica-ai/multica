import { useMemo } from "react";
import {
  isPendingActivationViable,
  selectPendingActivations,
  selectRootAgentSessionIdsWithPendingInteractions,
  selectWorkspaceAgentConsumerSessions,
  selectWorkspaceReconcileState,
  type AgentSessionEngine,
  type PendingActivationIntentRecord
} from "@tutti-os/agent-activity-core";
import { useEngineSelector, useModelValue } from "../../../../../shared/engine/useEngineSelector";
import type { AgentGUITranscriptReader } from "../../../../../agentActivityRuntime";
import {
  matchesAgentGUIConversationSummaryFilter,
  normalizeAgentGUIConversationFilter,
  type AgentGUIConversationFilter
} from "../../../../../agent-gui/agentGuiNode/model/agentGuiConversationFilter";
import type { AgentGUIProvider } from "../../../../../types";
import type { AgentGUIAgentTarget } from "../../../../../types";
import type { AgentGUIConversationSummary } from "../../../../../agent-gui/agentGuiNode/model/agentGuiConversationModel";
import {
  isAgentGUIProviderUnresolved,
  resolveAgentGUIConversationBrowserFreeTitle,
  resolveAgentGUIConversationTitleDisplayPrompt,
  resolveAgentGUIConversationTitleLeadingMentionKind,
  resolveAgentGUIConversationTitle,
  resolveAgentGUIProviderIdentity
} from "../../../../../shared/agentConversationTitleProjection.ts";
import { createAgentGUIConversationRailTitlePromptSelector } from "../../../../../shared/agentConversationRailTitlePromptSelector.ts";
import { projectCanonicalAgentGUIConversationSummaries } from "../../../../../shared/agentGUIConversationSummaryProjection.ts";

export interface AgentGUIConversationListQuery {
  conversationFilter?: AgentGUIConversationFilter | null;
  workspaceId: string;
  userId: string;
  provider: AgentGUIProvider;
  sessionOrigin: string;
}

const EMPTY_AGENT_GUI_AGENT_TARGETS: readonly AgentGUIAgentTarget[] = [];

export function useAgentGuiConversationList(
  engine: AgentSessionEngine,
  transcript: AgentGUITranscriptReader,
  query: AgentGUIConversationListQuery | null,
  agentTargets: readonly AgentGUIAgentTarget[] = EMPTY_AGENT_GUI_AGENT_TARGETS
) {
  const workspaceReconcile = useEngineSelector(
    engine,
    selectWorkspaceReconcileState
  );
  const sessions = useEngineSelector(
    engine,
    selectWorkspaceAgentConsumerSessions,
    consumerSessionsEqual
  );
  const rootAgentSessionIdsWithPendingInteractions = useEngineSelector(
    engine,
    selectRootAgentSessionIdsWithPendingInteractions,
    stringArraysEqual
  );
  const pendingActivations = useEngineSelector(
    engine,
    selectPendingActivations,
    pendingActivationsEqual
  );
  const selectRailTitlePrompts = useMemo(
    () => createAgentGUIConversationRailTitlePromptSelector(),
    [transcript]
  );
  const firstUserDisplayPromptsBySessionId = useModelValue(
    () => selectRailTitlePrompts(transcript),
    [selectRailTitlePrompts, transcript]
  );
  return useMemo(() => {
    if (!query) return null;
    const rootSessionIdsAwaitingUserAction = new Set(
      rootAgentSessionIdsWithPendingInteractions
    );
    const canonicalIds = new Set(
      sessions.map((item) => item.session.agentSessionId)
    );
    const latestNewActivationBySessionId = new Map(
      pendingActivations
        .filter(
          (activation) =>
            activation.mode === "new" && isPendingActivationViable(activation)
        )
        .map((activation) => [activation.agentSessionId, activation] as const)
    );
    const pendingConversations = pendingActivations
      .filter(
        (activation) =>
          activation.mode === "new" &&
          isPendingActivationViable(activation) &&
          !canonicalIds.has(activation.agentSessionId)
      )
      .map((activation): AgentGUIConversationSummary => {
        const target = agentTargets.find(
          (candidate) => candidate.agentTargetId === activation.agentTargetId
        );
        const provider = resolveAgentGUIProviderIdentity({
          sessionProvider: target?.provider ?? query.provider
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
          userId: query.userId
        };
      });
    const conversations = [
      ...projectCanonicalAgentGUIConversationSummaries(
        sessions.filter(
          (item) => item.session.workspaceId === query.workspaceId
        ),
        firstUserDisplayPromptsBySessionId,
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
        if (query.conversationFilter) {
          return matchesAgentGUIConversationSummaryFilter(
            conversation,
            normalizeAgentGUIConversationFilter(query.conversationFilter)
          );
        }
        return (
          conversation.provider === query.provider ||
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
      error: workspaceReconcile.errorMessage,
      initialized:
        workspaceReconcile.status === "ready" ||
        workspaceReconcile.status === "failed" ||
        workspaceReconcile.status === "unknown",
      isLoading:
        workspaceReconcile.status === "idle" ||
        workspaceReconcile.status === "loading",
      query,
      queryKey: [
        query.workspaceId,
        query.userId,
        query.provider,
        query.sessionOrigin
      ].join("::")
    };
  }, [
    agentTargets,
    firstUserDisplayPromptsBySessionId,
    pendingActivations,
    query,
    rootAgentSessionIdsWithPendingInteractions,
    sessions,
    workspaceReconcile
  ]);
}

function pendingActivationsEqual(
  left: readonly PendingActivationIntentRecord[],
  right: readonly PendingActivationIntentRecord[]
): boolean {
  return (
    left.length === right.length &&
    left.every((item, index) => item === right[index])
  );
}

function consumerSessionsEqual(
  left: ReturnType<typeof selectWorkspaceAgentConsumerSessions>,
  right: ReturnType<typeof selectWorkspaceAgentConsumerSessions>
): boolean {
  if (left.length !== right.length) return false;
  return left.every((item, index) => {
    const other = right[index];
    return (
      other !== undefined &&
      item.session === other.session &&
      item.activeTurn === other.activeTurn &&
      item.latestTurn === other.latestTurn &&
      item.pendingInteractions.length === other.pendingInteractions.length &&
      item.pendingInteractions.every(
        (interaction, interactionIndex) =>
          interaction === other.pendingInteractions[interactionIndex]
      ) &&
      item.displayStatus === other.displayStatus
    );
  });
}

function stringArraysEqual(
  left: readonly string[],
  right: readonly string[]
): boolean {
  return (
    left.length === right.length &&
    left.every((item, index) => item === right[index])
  );
}
