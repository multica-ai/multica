import { resolveAgentMentionFileThumbnailUrl } from "../shared/mentionFilePresentation";
import type { AgentContextMentionItem } from "./agentRichText/agentFileMentionExtension";
import type {
  AgentContextMentionProvider,
  AgentContextMentionQueryInput
} from "./agentContextMentionProvider";
import type { AgentMentionProviderQueryDiagnostic } from "./agentMentionSearchDiagnostics";
import {
  emptyAgentMentionRawGroups,
  normalizeSessionMentionItems,
  providerItemToAgentMentionItem,
  totalCountsFromRawGroups
} from "./AgentMentionSearchModel";
import type { AgentMentionFilterId } from "./AgentMentionSearchContracts";
import type { AgentMentionIssueTopicGroup } from "./AgentMentionSearchContracts";
import {
  AGENT_GENERATED_FILE_PROVIDER_ID,
  AGENT_SESSION_PROVIDER_ID,
  AGENT_TARGET_PROVIDER_ID,
  DEFAULT_SESSION_LIMIT,
  FILE_PROVIDER_ID,
  WORKSPACE_APP_PROVIDER_ID,
  WORKSPACE_ISSUE_PROVIDER_ID
} from "./AgentMentionSearchContracts";
import type { AgentMentionBrowseFetchResult } from "./AgentMentionSearchCache";
import type {
  ReferenceProvenanceCatalog,
  ReferenceProvenanceFilter
} from "@tutti-os/workspace-file-reference/contracts";
import { referenceProvenanceFilterIsActive } from "@tutti-os/workspace-file-reference/core";

export interface AgentMentionProviderQueryInput {
  abortSignal?: AbortSignal;
  diagnostics: AgentMentionProviderQueryDiagnostic[];
  providerId: string;
  workspaceId: string;
  currentUserId: string;
  query: string;
  limit?: number;
  sessionCwd?: string;
  sectionKey?: string;
  provenanceFilter: ReferenceProvenanceFilter | null;
}

export interface AgentMentionProviderGroupedQueryInput extends AgentMentionProviderQueryInput {}

export async function fetchAgentMentionFilterResult(input: {
  workspaceId: string;
  currentUserId: string;
  query: string;
  filter: AgentMentionFilterId;
  sessionCwd: string;
  sectionKey: string;
  includeAgentGeneratedFiles: boolean;
  fileLimit: number;
  currentFileSearchLimit: number;
  currentIssueSearchLimit: number;
  provenanceCatalog?: ReferenceProvenanceCatalog | null;
  provenanceFilter: ReferenceProvenanceFilter | null;
  queryProviderMentionItemsById: (
    input: AgentMentionProviderQueryInput
  ) => Promise<AgentContextMentionItem[]>;
  queryProviderMentionGroupsById?: (
    input: AgentMentionProviderGroupedQueryInput
  ) => Promise<AgentMentionIssueTopicGroup[] | null>;
}): Promise<AgentMentionBrowseFetchResult> {
  const providerDiagnostics: AgentMentionProviderQueryDiagnostic[] = [];
  const provenanceFilterActive = referenceProvenanceFilterIsActive(
    input.provenanceFilter
  );
  switch (input.filter) {
    case "file": {
      // Opened/local files have no durable provenance. Never silently return
      // them while the UI says any provenance filter is active.
      const fileQuery = provenanceFilterActive
        ? Promise.resolve([] as AgentContextMentionItem[])
        : input.queryProviderMentionItemsById({
            providerId: FILE_PROVIDER_ID,
            workspaceId: input.workspaceId,
            currentUserId: input.currentUserId,
            query: input.query,
            limit: input.query ? input.currentFileSearchLimit : input.fileLimit,
            sessionCwd: input.sessionCwd,
            diagnostics: providerDiagnostics,
            provenanceFilter: input.provenanceFilter
          });
      const agentGeneratedFileQuery =
        input.includeAgentGeneratedFiles || provenanceFilterActive
          ? input.queryProviderMentionItemsById({
              providerId: AGENT_GENERATED_FILE_PROVIDER_ID,
              workspaceId: input.workspaceId,
              currentUserId: input.currentUserId,
              query: input.query,
              limit: input.fileLimit,
              sessionCwd: input.sessionCwd,
              sectionKey: input.sectionKey,
              diagnostics: providerDiagnostics,
              provenanceFilter: input.provenanceFilter
            })
          : Promise.resolve([] as AgentContextMentionItem[]);
      const [fileItems, agentGeneratedFileItems] = await Promise.all([
        fileQuery,
        agentGeneratedFileQuery
      ]);
      const rawGroups = emptyAgentMentionRawGroups();
      rawGroups.opened_files = fileItems.filter((item) => item.kind === "file");
      rawGroups.agent_generated_files = agentGeneratedFileItems.filter(
        (item) => item.kind === "file"
      );
      return {
        providerDiagnostics,
        rawGroups,
        totalCounts: totalCountsFromRawGroups(rawGroups),
        issueTopicGroups: null
      };
    }
    case "session": {
      const sessionItems = await input.queryProviderMentionItemsById({
        providerId: AGENT_SESSION_PROVIDER_ID,
        workspaceId: input.workspaceId,
        currentUserId: input.currentUserId,
        query: input.query,
        // A provenance-aware host owns a room-scoped directory snapshot and
        // grouping must see that complete snapshot. Legacy providers retain
        // the bounded query contract.
        limit: input.provenanceCatalog?.enabledDimensions.includes("agent")
          ? undefined
          : DEFAULT_SESSION_LIMIT,
        sessionCwd: input.sessionCwd,
        diagnostics: providerDiagnostics,
        provenanceFilter: input.provenanceFilter
      });
      const rawGroups = emptyAgentMentionRawGroups();
      rawGroups.sessions = normalizeSessionMentionItems({
        items: sessionItems
      });
      return {
        providerDiagnostics,
        rawGroups,
        totalCounts: totalCountsFromRawGroups(rawGroups),
        issueTopicGroups: null
      };
    }
    case "issue": {
      // Issue summaries do not yet carry durable provenance. Fail closed
      // rather than displaying unfiltered issues under an active filter.
      const queryInput = {
        providerId: WORKSPACE_ISSUE_PROVIDER_ID,
        workspaceId: input.workspaceId,
        currentUserId: input.currentUserId,
        query: input.query,
        limit: input.currentIssueSearchLimit,
        sessionCwd: input.sessionCwd,
        diagnostics: providerDiagnostics,
        provenanceFilter: input.provenanceFilter
      };
      const issueTopicGroups = provenanceFilterActive
        ? []
        : input.queryProviderMentionGroupsById
          ? await input.queryProviderMentionGroupsById(queryInput)
          : null;
      const issueItems = provenanceFilterActive
        ? []
        : issueTopicGroups === null
          ? await input.queryProviderMentionItemsById(queryInput)
          : [];
      const rawGroups = emptyAgentMentionRawGroups();
      rawGroups.issues = issueItems.filter(
        (item) => item.kind === "workspace-issue"
      );
      return {
        providerDiagnostics,
        rawGroups,
        totalCounts: totalCountsFromRawGroups(rawGroups),
        issueTopicGroups
      };
    }
    case "agent": {
      const agentItems = await input.queryProviderMentionItemsById({
        providerId: AGENT_TARGET_PROVIDER_ID,
        workspaceId: input.workspaceId,
        currentUserId: input.currentUserId,
        query: input.query,
        sessionCwd: input.sessionCwd,
        diagnostics: providerDiagnostics,
        provenanceFilter: input.provenanceFilter
      });
      const rawGroups = emptyAgentMentionRawGroups();
      rawGroups.agents = agentItems.filter(
        (item) => item.kind === "agent-target"
      );
      return {
        providerDiagnostics,
        rawGroups,
        totalCounts: totalCountsFromRawGroups(rawGroups),
        issueTopicGroups: null
      };
    }
    case "member": {
      const items = await input.queryProviderMentionItemsById({
        providerId: "workspace-member", workspaceId: input.workspaceId, currentUserId: input.currentUserId,
        query: input.query, sessionCwd: input.sessionCwd, diagnostics: providerDiagnostics, provenanceFilter: null
      });
      const rawGroups = emptyAgentMentionRawGroups();
      rawGroups.members = items.filter((item) => item.kind === "workspace-member");
      return { providerDiagnostics, rawGroups, totalCounts: totalCountsFromRawGroups(rawGroups), issueTopicGroups: null };
    }
    case "app": {
      const appItems = await input.queryProviderMentionItemsById({
        providerId: WORKSPACE_APP_PROVIDER_ID,
        workspaceId: input.workspaceId,
        currentUserId: input.currentUserId,
        query: input.query,
        sessionCwd: input.sessionCwd,
        diagnostics: providerDiagnostics,
        provenanceFilter: input.provenanceFilter
      });
      const rawGroups = emptyAgentMentionRawGroups();
      rawGroups.apps = appItems.filter((item) => item.kind === "workspace-app");
      return {
        providerDiagnostics,
        rawGroups,
        totalCounts: totalCountsFromRawGroups(rawGroups),
        issueTopicGroups: null
      };
    }
  }
}

export async function queryAgentMentionProviderItems(input: {
  provider: AgentContextMentionProvider;
  workspaceId: string;
  currentUserId: string;
  query: string;
  limit?: number;
  sessionCwd: string;
  sectionKey: string;
  abortSignal: AbortSignal;
  provenanceFilter: ReferenceProvenanceFilter | null;
}): Promise<AgentContextMentionItem[]> {
  const queryInput: AgentContextMentionQueryInput = {
    keyword: input.query,
    maxResults: input.limit,
    abortSignal: input.abortSignal,
    trigger: "@",
    context: {
      metadata: {
        currentUserId: input.currentUserId,
        sectionKey: input.sectionKey || undefined,
        sessionCwd: input.sessionCwd || undefined,
        target: "agent-gui",
        workspaceId: input.workspaceId,
        referenceProvenanceFilter: input.provenanceFilter ?? undefined
      }
    }
  };
  const items = await input.provider.query(queryInput);
  if (input.abortSignal.aborted) {
    return [];
  }
  return mapProviderItemsToAgentMentionItems({
    ...input,
    items
  });
}

export async function queryAgentMentionProviderDirectoryItems(input: {
  provider: AgentContextMentionProvider | undefined;
  workspaceId: string;
  currentUserId: string;
  directoryPath: string;
  sessionCwd: string;
  sectionKey: string;
  abortSignal: AbortSignal;
  provenanceFilter: ReferenceProvenanceFilter | null;
}): Promise<AgentContextMentionItem[]> {
  const provider = input.provider;
  if (!provider?.queryDirectory) {
    throw new Error("Mention provider does not support directory browsing.");
  }
  const items = await provider.queryDirectory({
    keyword: "",
    abortSignal: input.abortSignal,
    trigger: "@",
    directoryPath: input.directoryPath,
    context: {
      metadata: {
        currentUserId: input.currentUserId,
        sectionKey: input.sectionKey || undefined,
        sessionCwd: input.sessionCwd || undefined,
        target: "agent-gui",
        workspaceId: input.workspaceId,
        referenceProvenanceFilter: input.provenanceFilter ?? undefined
      }
    }
  });
  if (input.abortSignal.aborted) {
    return [];
  }
  return mapProviderItemsToAgentMentionItems({
    ...input,
    provider,
    items
  });
}

export async function queryAgentMentionProviderGroups(input: {
  provider: AgentContextMentionProvider;
  workspaceId: string;
  currentUserId: string;
  query: string;
  limit?: number;
  sessionCwd: string;
  sectionKey: string;
  abortSignal: AbortSignal;
  provenanceFilter: ReferenceProvenanceFilter | null;
}): Promise<AgentMentionIssueTopicGroup[] | null> {
  if (!input.provider.queryGroups) {
    return null;
  }
  const result = await input.provider.queryGroups({
    keyword: input.query,
    maxResults: input.limit,
    abortSignal: input.abortSignal,
    trigger: "@",
    context: {
      metadata: {
        currentUserId: input.currentUserId,
        sectionKey: input.sectionKey || undefined,
        sessionCwd: input.sessionCwd || undefined,
        target: "agent-gui",
        workspaceId: input.workspaceId,
        referenceProvenanceFilter: input.provenanceFilter ?? undefined
      }
    }
  });
  if (input.abortSignal.aborted) {
    return [];
  }
  return Promise.all(
    result.groups.map(async (group): Promise<AgentMentionIssueTopicGroup> => {
      const items = await mapProviderItemsToAgentMentionItems({
        ...input,
        items: group.items
      });
      const uniqueItems = dedupeWorkspaceIssueMentionItems(items);
      return {
        id: agentMentionIssueTopicGroupId(group.id),
        providerGroupId: group.id,
        label: group.label,
        items: uniqueItems,
        totalCount: Math.max(group.totalCount, uniqueItems.length),
        nextPageToken: group.nextCursor ?? null,
        loadMoreStatus: "idle",
        loadMoreError: null
      };
    })
  );
}

export async function queryAgentMentionProviderGroupPage(input: {
  provider: AgentContextMentionProvider;
  providerGroupId: string;
  workspaceId: string;
  currentUserId: string;
  query: string;
  cursor: string;
  pageSize: number;
  sessionCwd: string;
  sectionKey: string;
  abortSignal: AbortSignal;
  provenanceFilter: ReferenceProvenanceFilter | null;
}): Promise<AgentMentionIssueTopicGroup> {
  if (!input.provider.queryGroupPage) {
    throw new Error("Mention provider does not support grouped pagination.");
  }
  const group = await input.provider.queryGroupPage({
    groupId: input.providerGroupId,
    cursor: input.cursor,
    pageSize: input.pageSize,
    keyword: input.query,
    maxResults: input.pageSize,
    abortSignal: input.abortSignal,
    trigger: "@",
    context: {
      metadata: {
        currentUserId: input.currentUserId,
        sectionKey: input.sectionKey || undefined,
        sessionCwd: input.sessionCwd || undefined,
        target: "agent-gui",
        workspaceId: input.workspaceId,
        referenceProvenanceFilter: input.provenanceFilter ?? undefined
      }
    }
  });
  const items = await mapProviderItemsToAgentMentionItems({
    ...input,
    provider: input.provider,
    items: group.items
  });
  const uniqueItems = dedupeWorkspaceIssueMentionItems(items);
  return {
    id: agentMentionIssueTopicGroupId(input.providerGroupId),
    providerGroupId: input.providerGroupId,
    label: group.label,
    items: uniqueItems,
    totalCount: Math.max(group.totalCount, uniqueItems.length),
    nextPageToken: group.nextCursor ?? null,
    loadMoreStatus: "idle",
    loadMoreError: null
  };
}

export function agentMentionIssueTopicGroupId(
  topicId: string
): `issue-topic:${string}` {
  return `issue-topic:${encodeURIComponent(topicId)}`;
}

function dedupeWorkspaceIssueMentionItems(
  items: readonly AgentContextMentionItem[]
): AgentContextMentionItem[] {
  const issueIds = new Set<string>();
  return items.filter((item) => {
    if (item.kind !== "workspace-issue") {
      return true;
    }
    if (issueIds.has(item.targetId)) {
      return false;
    }
    issueIds.add(item.targetId);
    return true;
  });
}

async function mapProviderItemsToAgentMentionItems(input: {
  provider: AgentContextMentionProvider;
  workspaceId: string;
  currentUserId: string;
  items: readonly any[];
}): Promise<AgentContextMentionItem[]> {
  const mentionItems = await Promise.all(
    input.items.map(async (item) => {
      const mentionItem = providerItemToAgentMentionItem({
        currentUserId: input.currentUserId,
        insertResult: input.provider.toInsertResult(item),
        label: input.provider.getItemLabel(item),
        providerId: input.provider.id,
        directory: input.provider.getItemDirectory?.(item) ?? null,
        subtitle: input.provider.getItemSubtitle?.(item) ?? "",
        workspaceId: input.workspaceId
      });
      if (!mentionItem || mentionItem.kind !== "file") {
        return mentionItem;
      }
      const iconUrl = await Promise.resolve(
        input.provider.getItemIconUrl?.(item) ?? null
      ).catch(() => null);
      const resolvedThumbnailUrl = resolveAgentMentionFileThumbnailUrl({
        ...mentionItem,
        thumbnailUrl: iconUrl
      });
      if (!resolvedThumbnailUrl) {
        return mentionItem;
      }
      return {
        ...mentionItem,
        thumbnailUrl: resolvedThumbnailUrl
      };
    })
  );
  return mentionItems.filter(
    (item): item is AgentContextMentionItem => item !== null
  );
}
