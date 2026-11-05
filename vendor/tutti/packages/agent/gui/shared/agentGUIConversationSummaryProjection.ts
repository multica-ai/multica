import { type AgentActivitySession } from "@tutti-os/agent-activity-core";
import type { AgentConversationServices } from "../state/conversation-services";
import type { AgentGUIConversationRailTitlePromptsBySessionId } from "./agentConversationRailTitlePromptSelector.ts";
import {
  isAgentGUIProviderUnresolved,
  resolveAgentGUIConversationBrowserFreeTitle,
  resolveAgentGUIConversationTitle,
  resolveAgentGUIConversationTitleDisplayPrompt,
  resolveAgentGUIConversationTitleLeadingMentionKind,
  resolveAgentGUIProviderIdentity,
  type AgentGUIConversationTitleFallback,
  type AgentGUIConversationTitleLeadingMentionKind,
  type AgentGUIResolvedProvider
} from "./agentConversationTitleProjection.ts";
import { resolveWorkspaceAgentSessionSortTimeUnixMs } from "./workspaceAgentSessionSortTime.ts";

export type AgentGUIConsumerSessions =
  AgentConversationServices["sessions"]["consumerSessions"];

export type AgentConversationRailStatus =
  | "working"
  | "waiting"
  | "ready"
  | "completed"
  | "failed"
  | "canceled";

export interface AgentConversationRailSummary {
  agentTargetId?: string | null;
  cwd: string;
  id: string;
  isolation?: AgentActivitySession["isolation"];
  needsUserAction?: boolean;
  pinnedAtUnixMs?: number | null;
  provider: AgentGUIResolvedProvider;
  railSectionKey?: string;
  resumable?: boolean;
  sortTimeUnixMs?: number;
  status: AgentConversationRailStatus;
  title: string;
  titleFallback?: AgentGUIConversationTitleFallback;
  titleLeadingMentionKind?: AgentGUIConversationTitleLeadingMentionKind | null;
  updatedAtUnixMs: number;
  userId?: string;
}

export function projectCanonicalAgentGUIConversationSummaries(
  sessions: AgentGUIConsumerSessions,
  firstUserDisplayPromptsBySessionId: AgentGUIConversationRailTitlePromptsBySessionId = {},
  rootSessionIdsAwaitingUserAction?: ReadonlySet<string>
): AgentConversationRailSummary[] {
  return sessions.map((item): AgentConversationRailSummary => {
    const provider = resolveAgentGUIProviderIdentity({
      sessionProvider: item.session.provider
    });
    const { title: canonicalTitle } = resolveAgentGUIConversationTitle(
      item.session.title
    );
    const firstUserDisplayPrompt =
      firstUserDisplayPromptsBySessionId[item.session.agentSessionId];
    const titleDisplayPrompt = resolveAgentGUIConversationTitleDisplayPrompt({
      firstUserDisplayPrompt,
      title: canonicalTitle
    });
    const { title, titleFallback } = resolveAgentGUIConversationTitle(
      resolveAgentGUIConversationBrowserFreeTitle({
        firstUserDisplayPrompt,
        title: canonicalTitle
      })
    );
    const canonicalUpdatedAtUnixMs =
      item.session.updatedAtUnixMs ?? item.session.createdAtUnixMs ?? 0;
    const titleLeadingMentionKind =
      resolveAgentGUIConversationTitleLeadingMentionKind(titleDisplayPrompt);
    return {
      agentTargetId: item.session.agentTargetId ?? null,
      cwd: item.session.cwd,
      id: item.session.agentSessionId,
      isolation: item.session.isolation,
      needsUserAction:
        rootSessionIdsAwaitingUserAction?.has(item.session.agentSessionId) ??
        item.pendingInteractions.length > 0,
      pinnedAtUnixMs: item.session.pinnedAtUnixMs ?? null,
      provider,
      railSectionKey: item.session.railSectionKey,
      resumable: item.session.resumable,
      sortTimeUnixMs: resolveWorkspaceAgentSessionSortTimeUnixMs({
        createdAtUnixMs: item.session.createdAtUnixMs,
        latestTurn: item.latestTurn
      }),
      status: item.displayStatus === "idle" ? "ready" : item.displayStatus,
      title,
      titleLeadingMentionKind,
      titleFallback,
      updatedAtUnixMs: canonicalUpdatedAtUnixMs,
      userId: item.session.userId?.trim() ?? ""
    };
  });
}

export function projectCanonicalAgentGUIConversationSummariesFromState(
  state: AgentConversationServices,
  input: {
    firstUserDisplayPromptsBySessionId?: AgentGUIConversationRailTitlePromptsBySessionId;
    provider?: string | null;
    rootSessionIdsAwaitingUserAction?: ReadonlySet<string>;
    workspaceId: string;
  }
): AgentConversationRailSummary[] {
  const provider = input.provider?.trim().toLowerCase() ?? "";
  return projectCanonicalAgentGUIConversationSummaries(
    state.sessions.consumerSessions.filter(
      (item) => item.session.workspaceId === input.workspaceId
    ),
    input.firstUserDisplayPromptsBySessionId,
    input.rootSessionIdsAwaitingUserAction
  )
    .filter(
      (conversation) =>
        !provider ||
        conversation.provider === provider ||
        isAgentGUIProviderUnresolved(conversation.provider)
    )
    .sort(
      (left, right) =>
        (right.sortTimeUnixMs ?? right.updatedAtUnixMs) -
          (left.sortTimeUnixMs ?? left.updatedAtUnixMs) ||
        left.id.localeCompare(right.id)
    );
}
