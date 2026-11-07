import type { AgentHostUserInfo } from "../../shared/contracts/dto";
import { translate } from "../../i18n/index";
import { agentMentionEmptyGroupLabel } from "./AgentMentionLabels";
import {
  resolveWorkspaceAgentActivityTitle,
  resolveWorkspaceAgentActivityStatus
} from "../../shared/workspaceAgentActivityListViewModel";
import { workspaceAgentProviderLabel } from "../../shared/workspaceAgentProviderLabel";
import { resolveDisplayableWorkspaceAgentSessionTitle } from "../../shared/workspaceAgentSessionTitle";
import { extractPlainTextWithoutFilesFromContent } from "@tutti-os/ui-rich-text/core";
import type {
  AgentContextMentionItem,
  AgentMentionScope,
  AgentMentionSessionItem
} from "./agentRichText/agentFileMentionExtension";
import { normalizeAgentSessionMentionTitle } from "./agentRichText/agentFileMentionExtension";
import { createRichTextMentionHref } from "@tutti-os/ui-rich-text/core";
import type {
  AgentMentionFilterId,
  AgentMentionGroup,
  AgentMentionGroupId
} from "./AgentMentionSearchController";
import type { AgentMentionRawGroups } from "./AgentMentionSearchContracts";
import type {
  AgentActivityMessage,
  AgentActivitySession
} from "@tutti-os/agent-activity-core";
import type { WorkspaceAgentActivitySessionSummary } from "../../shared/workspaceAgentSessionSummaryTypes";

export function buildSessionMentionItem(input: {
  workspaceId: string;
  currentUserId: string;
  session: AgentActivitySession;
  summary: WorkspaceAgentActivitySessionSummary | null;
  userProfiles: Record<string, Pick<AgentHostUserInfo, "name" | "avatar">>;
  fallbackTitle?: string | null;
}): AgentMentionSessionItem | null {
  const sessionUserId = input.session.userId?.trim() ?? "";
  const scope: AgentMentionScope =
    sessionUserId && sessionUserId === input.currentUserId
      ? "my_sessions"
      : "collab_sessions";
  const userProfile = sessionUserId
    ? input.userProfiles[sessionUserId]
    : undefined;
  const initiatorName = normalizeSessionInitiatorDisplayName(
    userProfile?.name ||
      sessionUserId ||
      translate("agentHost.agentGui.mentionCollaboratorFallback")
  );
  const sessionProvider = input.session.provider?.trim() ?? "";
  const sessionAgentTargetId = input.session.agentTargetId?.trim() ?? "";
  const agentName = workspaceAgentProviderLabel(sessionProvider || "unknown");
  const inputPreview =
    compactText(input.summary?.latestUserRequirement) ||
    compactText(input.summary?.initialUserRequirement) ||
    firstSummaryItemText(input.summary?.latestTurn?.userItems) ||
    "";
  const summaryPreview =
    compactText(input.summary?.recentAgentReplies?.[0]) ||
    firstSummaryItemText(input.summary?.latestTurn?.agentItems) ||
    "";
  const sessionTitle = resolveDisplayableWorkspaceAgentSessionTitle(
    input.session
  );
  const fallbackTitle = compactText(input.fallbackTitle);
  const title =
    fallbackTitle ||
    sessionTitle ||
    inputPreview ||
    summaryPreview ||
    input.session.agentSessionId;
  if (!title) {
    return null;
  }
  const mentionTitle = normalizeAgentSessionMentionTitle(title);
  const status = resolveSessionDisplayStatus(input.session, input.summary);
  return {
    kind: "session",
    href: createRichTextMentionHref({
      providerId: "agent-session",
      entityId: input.session.agentSessionId,
      label: mentionTitle,
      scope: {
        workspaceId: input.workspaceId,
        ...(sessionAgentTargetId ? { agentTargetId: sessionAgentTargetId } : {})
      }
    }),
    workspaceId: input.workspaceId,
    targetId: input.session.agentSessionId,
    ...(sessionAgentTargetId ? { agentTargetId: sessionAgentTargetId } : {}),
    name: `${initiatorName} & ${agentName} ${mentionTitle}`.trim(),
    title: mentionTitle,
    scope,
    ...(sessionUserId ? { initiatorUserId: sessionUserId } : {}),
    initiatorName,
    ...(userProfile?.avatar ? { initiatorAvatarUrl: userProfile.avatar } : {}),
    agentName,
    status,
    inputPreview,
    summaryPreview,
    updatedAtUnixMs:
      input.session.updatedAtUnixMs ??
      input.session.createdAtUnixMs ??
      Date.now()
  };
}

function normalizeSessionInitiatorDisplayName(value: string): string {
  const trimmed = value.trim();
  return trimmed.toLowerCase() === "local" ? "User" : trimmed;
}

export function resolveSessionMentionMessageTitle(
  session: AgentActivitySession,
  _messages: readonly AgentActivityMessage[]
): string {
  return compactText(resolveWorkspaceAgentActivityTitle(session));
}

function resolveSessionDisplayStatus(
  session: AgentActivitySession,
  summary: WorkspaceAgentActivitySessionSummary | null
): string {
  const sessionStatus = resolveWorkspaceAgentActivityStatus(session);
  if (sessionStatus !== "idle") {
    return sessionStatus;
  }
  const status = (
    summary?.executionStatus?.currentOrFinalStatus ??
    summary?.currentOrFinalStatus ??
    ""
  )
    .trim()
    .toLowerCase();
  if (status === "waiting") {
    return "waiting";
  }
  if (status === "working") {
    return "working";
  }
  if (status === "completed") {
    return "completed";
  }
  if (status === "failed") {
    return "failed";
  }
  return status
    ? resolveWorkspaceAgentActivityStatusFromSummary(status)
    : sessionStatus;
}

function resolveWorkspaceAgentActivityStatusFromSummary(
  status: string
): string {
  switch (status) {
    case "waiting":
      return "waiting";
    case "working":
      return "working";
    case "completed":
      return "completed";
    case "canceled":
      return "canceled";
    case "failed":
      return "failed";
    default:
      return "idle";
  }
}

function firstSummaryItemText(
  items: ReadonlyArray<{ content?: string }> | undefined
): string {
  if (!items) {
    return "";
  }
  for (const item of items) {
    const text = compactText(item.content);
    if (text) {
      return text;
    }
  }
  return "";
}

export function matchesSessionQuery(
  item: AgentContextMentionItem,
  rawQuery: string
): boolean {
  if (item.kind !== "session") {
    return true;
  }
  const query = normalizeQuery(rawQuery);
  if (!query) {
    return true;
  }
  const haystack = [
    item.name,
    item.title,
    item.initiatorName,
    item.agentName,
    item.inputPreview ?? "",
    item.summaryPreview ?? ""
  ]
    .join("\n")
    .toLowerCase();
  return query
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean)
    .every((token) => haystack.includes(token));
}

export const AGENT_MENTION_FILTER_TAB_ORDER = ["session", "issue", "agent", "member"] as const satisfies readonly AgentMentionFilterId[];

export const DEFAULT_AGENT_MENTION_FILTER =
  "session" satisfies AgentMentionFilterId;
export const DEFAULT_MENTION_GROUP_PAGE_SIZE = 10;

export function mentionGroupPageSize(
  _filter: AgentMentionFilterId,
  _groupId: AgentMentionGroupId
): number {
  return DEFAULT_MENTION_GROUP_PAGE_SIZE;
}

export function mentionGroupExpandCount(
  group: AgentMentionGroup,
  filter: AgentMentionFilterId
): number {
  const pageSize = mentionGroupPageSize(
    filter,
    group.id as AgentMentionGroupId
  );
  const remaining = Math.max(0, group.totalCount - group.visibleCount);
  return Math.min(pageSize, remaining);
}

export function groupIdsForFilter(
  filter: AgentMentionFilterId
): AgentMentionGroupId[] {
  switch (filter) {
    case "member":
      return ["members"];
    case "agent":
      return ["agents"];
    case "app":
      return ["apps"];
    case "file":
      return ["opened_files", "agent_generated_files"];
    case "session":
      return ["my_sessions"];
    case "issue":
      return ["issues"];
  }
}

export function shouldShowEmptyGroup(
  groupId: AgentMentionGroupId,
  filter: AgentMentionFilterId,
  query: string
): boolean {
  if (groupId.startsWith("issue-topic:")) {
    return false;
  }
  const hasQuery = query.trim().length > 0;
  if (groupId === "files") {
    return false;
  }
  if (groupId === "opened_files" || groupId === "agent_generated_files") {
    return filter === "file" && !hasQuery;
  }
  if (groupId === "apps") {
    return filter === "app";
  }
  if (groupId === "members") return filter === "member";
  if (groupId === "agents") {
    return filter === "agent";
  }
  if (groupId === "my_sessions") {
    return filter === "session";
  }
  if (groupId === "collab_sessions") {
    return false;
  }
  return filter === "issue";
}

export function buildEmptyGroup(
  groupId: AgentMentionGroupId,
  query: string
): AgentMentionGroup {
  return {
    id: groupId,
    items: [],
    totalCount: 0,
    visibleCount: 0,
    hasMore: false,
    emptyLabel: emptyGroupLabel(groupId, query)
  };
}

function emptyGroupLabel(groupId: AgentMentionGroupId, query: string): string {
  return agentMentionEmptyGroupLabel(groupId, query);
}

export function resolveMentionGroupItems(
  groupId: AgentMentionGroupId,
  rawGroups: AgentMentionRawGroups
): AgentContextMentionItem[] {
  if (groupId.startsWith("issue-topic:")) {
    return [];
  }
  if (groupId === "files") {
    return [...rawGroups.opened_files, ...rawGroups.agent_generated_files];
  }
  if (groupId === "my_sessions" || groupId === "collab_sessions") {
    return rawGroups.sessions.filter(
      (item) => item.kind === "session" && item.scope === groupId
    );
  }
  if (groupId.startsWith("agent:")) {
    return [];
  }
  if (
    groupId === "apps" ||
    groupId === "agents" ||
    groupId === "members" ||
    groupId === "opened_files" ||
    groupId === "agent_generated_files" ||
    groupId === "issues"
  ) {
    return rawGroups[groupId];
  }
  return [];
}

export function resolveMentionGroupTotalCount(
  groupId: AgentMentionGroupId,
  totalCounts: Partial<Record<AgentMentionGroupId, number>>,
  itemCount: number
): number {
  if (groupId.startsWith("issue-topic:")) {
    return itemCount;
  }
  if (groupId === "files") {
    return (
      (totalCounts.opened_files ?? 0) + (totalCounts.agent_generated_files ?? 0)
    );
  }
  if (groupId === "agent_generated_files") {
    return itemCount;
  }
  return totalCounts[groupId] ?? itemCount;
}

export function normalizeQuery(value: string): string {
  return value.replace(/\s+/g, " ").trim();
}

export function compactText(value: string | null | undefined): string {
  return value?.replace(/\s+/g, " ").trim() ?? "";
}

export function issuePreviewText(value: string | null | undefined): string {
  const content = extractPlainTextWithoutFilesFromContent(value);
  return compactText(content);
}

export function unique(values: readonly string[]): string[] {
  return [...new Set(values)];
}

export function getOrCreateSummaryCache<T>(
  map: Map<string, Map<string, T>>,
  workspaceId: string
): Map<string, T> {
  const existing = map.get(workspaceId);
  if (existing) {
    return existing;
  }
  const created = new Map<string, T>();
  map.set(workspaceId, created);
  return created;
}
