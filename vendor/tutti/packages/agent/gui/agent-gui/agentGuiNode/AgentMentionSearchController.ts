import type {
  ReferenceProvenanceCatalog,
  ReferenceProvenanceFilter
} from "@tutti-os/workspace-file-reference/contracts";
import type { AgentContextMentionProvider } from "./agentContextMentionProvider";
import { AgentMentionDirectoryRequestLifecycle } from "./AgentMentionDirectoryRequestLifecycle";
import { AgentMentionProvenanceFilterState } from "./AgentMentionProvenanceFilterState";
import { resolveAgentMentionQueryUpdate } from "./AgentMentionQueryUpdate";
import {
  MAX_BROWSE_CACHE_ENTRIES,
  mergeAgentMentionBrowseIssueGroupPage,
  resetAgentMentionSearchBrowseCacheForTests,
  scheduleAgentMentionIdleTask
} from "./AgentMentionSearchCache";
import {
  buildBrowseCategories,
  FILE_PROVIDER_ID,
  WORKSPACE_ISSUE_PROVIDER_ID,
  type AgentMentionFilterId,
  type AgentMentionGroupId,
  type AgentMentionSearchListener
} from "./AgentMentionSearchContracts";
import { AgentMentionSearchControllerBase } from "./AgentMentionSearchControllerBase";
import {
  queryAgentMentionProviderWithDiagnostics,
  type AgentMentionProviderQueryDiagnostic
} from "./agentMentionSearchDiagnostics";
import {
  DEFAULT_AGENT_MENTION_FILTER,
  DEFAULT_MENTION_GROUP_PAGE_SIZE,
  mentionGroupPageSize
} from "./agentMentionSearchHelpers";
import {
  queryAgentMentionProviderDirectoryItems,
  queryAgentMentionProviderGroupPage
} from "./AgentMentionSearchIndex";
import { diagnosticErrorKind } from "./AgentMentionSearchModel";
import { presentWorkspaceFileDirectoryMentionItems } from "./agentMentionWorkspaceFilesPresentation";
import type { AgentContextMentionItem } from "./agentRichText/agentFileMentionExtension";
export type {
  AgentMentionBrowseCategory,
  AgentMentionFilterId,
  AgentMentionGroup,
  AgentMentionGroupId,
  AgentMentionSearchState
} from "./AgentMentionSearchContracts";
export { MAX_BROWSE_CACHE_ENTRIES, resetAgentMentionSearchBrowseCacheForTests };
export class AgentMentionSearchController extends AgentMentionSearchControllerBase {
  private readonly issueLoadMoreRequests = new Map<
    string,
    { abortController: AbortController; token: symbol }
  >();
  private readonly directoryRequestLifecycle =
    new AgentMentionDirectoryRequestLifecycle();
  private readonly provenanceFilters = new AgentMentionProvenanceFilterState();
  setProvenanceCatalog(catalog: ReferenceProvenanceCatalog | null): void {
    if (this.currentProvenanceCatalog === catalog) return;
    this.currentProvenanceCatalog = catalog;
    if (
      this.currentFilter !== "session" ||
      (this.state.status !== "ready" && this.state.status !== "loading")
    ) {
      return;
    }
    this.setState({
      ...this.state,
      groups: this.groupsFromRawGroups()
    });
  }
  setProvenanceFilters(
    filters: Record<AgentMentionFilterId, ReferenceProvenanceFilter | null>
  ): void {
    const next = this.provenanceFilters.replace(filters, this.currentFilter);
    if (!next.changed) return;
    this.cancelPendingPreload();
    this.currentProvenanceFilter = next.value;
    this.resetWorkspaceFileBrowsePaths();
    this.updateQuery({
      workspaceId: this.activeWorkspaceId,
      currentUserId: this.currentUserId,
      query: this.currentQuery,
      sectionKey: this.currentSectionKey,
      sessionCwd: this.currentSessionCwd
    });
  }
  subscribe(listener: AgentMentionSearchListener): () => void {
    this.listeners.add(listener);
    listener(this.state);
    return () => {
      this.listeners.delete(listener);
    };
  }
  updateQuery(input: {
    workspaceId: string;
    currentUserId?: string | null;
    query: string;
    sectionKey?: string | null;
    sessionCwd?: string | null;
  }): void {
    if (this.disposed) return;
    const next = resolveAgentMentionQueryUpdate(input, {
      agentGeneratedBrowsePath: this.agentGeneratedBrowsePath,
      workspaceId: this.activeWorkspaceId,
      currentUserId: this.currentUserId,
      sectionKey: this.currentSectionKey,
      sessionCwd: this.currentSessionCwd,
      query: this.currentQuery,
      filter: this.currentFilter,
      directoryDepth: this.workspaceFileBrowsePaths.length
    });
    if (next.ignore) return;
    this.activeWorkspaceId = next.workspaceId;
    this.currentUserId = next.currentUserId;
    this.currentSectionKey = next.sectionKey;
    this.currentSessionCwd = next.sessionCwd;
    this.currentQuery = next.query;
    this.clearTimer();
    this.abortActiveRequest();
    this.directoryRequestLifecycle.cancel();
    this.cancelIssueLoadMoreRequests();
    const requestId = ++this.requestId;
    this.resetAgentGeneratedBrowsePath();
    this.resetWorkspaceFileBrowsePaths();
    this.resetExpandedCounts();
    this.resetSearchLimits();
    this.resetRawGroups();
    if (!this.activeWorkspaceId) {
      this.setState({
        status: "idle",
        query: this.currentQuery,
        mode: "browse",
        filter: this.currentFilter,
        categories: buildBrowseCategories(this.hiddenFilterIds),
        groups: [],
        error: null
      });
      return;
    }
    if (!this.currentQuery) {
      this.startBrowseModeFetch(this.currentFilter);
      return;
    }
    this.setState({
      status: "loading",
      query: this.currentQuery,
      mode: "results",
      filter: this.currentFilter,
      categories: buildBrowseCategories(this.hiddenFilterIds),
      groups: this.groupsFromRawGroups(),
      error: null
    });
    const abortSignal = this.beginActiveRequest();
    this.timer = this.scheduler.schedule(this.debounceMs, () => {
      void this.runSearch({
        workspaceId: this.activeWorkspaceId,
        currentUserId: this.currentUserId,
        query: this.currentQuery,
        requestId,
        filter: this.currentFilter,
        provenanceFilter: this.currentProvenanceFilter,
        sectionKey: this.currentSectionKey,
        sessionCwd: this.currentSessionCwd,
        abortSignal
      });
    });
  }
  setFilter(filter: AgentMentionFilterId): void {
    if (filter === this.currentFilter) return;
    if (this.disposed) {
      return;
    }
    this.currentFilter = filter;
    this.currentProvenanceFilter = this.provenanceFilters.value(filter);
    this.clearTimer();
    this.abortActiveRequest();
    this.directoryRequestLifecycle.cancel();
    this.cancelIssueLoadMoreRequests();
    const requestId = ++this.requestId;
    this.resetAgentGeneratedBrowsePath();
    this.resetWorkspaceFileBrowsePaths();
    this.resetExpandedCounts();
    this.resetSearchLimits();
    this.resetRawGroups();
    if (!this.currentQuery) {
      this.startBrowseModeFetch(filter);
      return;
    }
    if (!this.activeWorkspaceId) {
      this.setState({
        status: "ready",
        query: this.currentQuery,
        mode: "results",
        filter: this.currentFilter,
        categories: buildBrowseCategories(this.hiddenFilterIds),
        groups: [],
        error: null
      });
      return;
    }
    this.setState({
      status: "loading",
      query: this.currentQuery,
      mode: "results",
      filter: this.currentFilter,
      categories: buildBrowseCategories(this.hiddenFilterIds),
      groups: this.groupsFromRawGroups(),
      error: null
    });
    const abortSignal = this.beginActiveRequest();
    void this.runSearch({
      workspaceId: this.activeWorkspaceId,
      currentUserId: this.currentUserId,
      query: this.currentQuery,
      requestId,
      filter,
      provenanceFilter: this.currentProvenanceFilter,
      sectionKey: this.currentSectionKey,
      sessionCwd: this.currentSessionCwd,
      abortSignal
    });
  }

  preloadBrowse(input: {
    workspaceId: string;
    currentUserId?: string | null;
    sectionKey?: string | null;
    sessionCwd?: string | null;
    filter?: AgentMentionFilterId;
  }): void {
    if (this.disposed) {
      return;
    }
    const workspaceId = input.workspaceId.trim();
    if (!workspaceId) {
      return;
    }
    const filter = input.filter ?? DEFAULT_AGENT_MENTION_FILTER;
    const currentUserId = input.currentUserId?.trim() ?? "";
    const sectionKey = input.sectionKey?.trim() ?? "";
    const sessionCwd = input.sessionCwd?.trim() ?? "";
    const provenanceFilter = this.currentProvenanceFilter;
    const cacheKey = this.browseCacheKey(
      { currentUserId, filter, sectionKey, sessionCwd, workspaceId },
      provenanceFilter
    );
    if (this.readBrowseCache(cacheKey).isFresh) {
      return;
    }
    if (this.pendingPreloadKey === cacheKey) {
      return;
    }
    this.cancelPendingPreload();
    this.pendingPreloadKey = cacheKey;
    this.preloadCancel = scheduleAgentMentionIdleTask(() => {
      this.preloadCancel = null;
      this.pendingPreloadKey = null;
      if (this.disposed) {
        return;
      }
      this.runBrowsePreload({
        cacheKey,
        currentUserId,
        filter,
        provenanceFilter,
        sectionKey,
        sessionCwd,
        workspaceId
      });
    });
  }

  private runBrowsePreload(input: {
    cacheKey: string;
    currentUserId: string;
    filter: AgentMentionFilterId;
    provenanceFilter: ReferenceProvenanceFilter | null;
    sectionKey: string;
    sessionCwd: string;
    workspaceId: string;
  }): void {
    const {
      cacheKey,
      currentUserId,
      filter,
      provenanceFilter,
      sectionKey,
      sessionCwd,
      workspaceId
    } = input;
    this.logLifecycle("browse.preload", {
      filter,
      providerIds: this.providerIdsForDiagnostics(),
      sessionCwdPresent: Boolean(sessionCwd),
      workspaceId
    });
    const cached = this.readBrowseCache(cacheKey);
    this.logBrowseCacheState({
      cacheKey,
      cached,
      filter,
      reason: "preload",
      workspaceId
    });
    if (cached.isFresh) {
      return;
    }
    void this.loadBrowseFetchResult(
      {
        workspaceId,
        currentUserId,
        filter,
        sectionKey,
        sessionCwd
      },
      cacheKey,
      "preload",
      provenanceFilter
    ).catch((error) => {
      this.logLifecycle("browse.fetch.error", {
        errorKind: diagnosticErrorKind(error),
        filter,
        reason: "preload",
        workspaceId
      });
    });
  }

  protected cancelPendingPreload(): void {
    if (this.preloadCancel) {
      this.preloadCancel();
      this.preloadCancel = null;
    }
    this.pendingPreloadKey = null;
  }

  enterCategory(category: AgentMentionFilterId): void {
    this.setFilter(category);
  }

  selectFileMentionNavigationItem(item: AgentContextMentionItem): boolean {
    if (item.kind !== "file" || !item.mentionNavigation) {
      return false;
    }
    if (item.mentionNavigation === "agent-generated-folder") {
      this.agentGeneratedBrowsePath = item.path;
      this.expandedCounts.agent_generated_files = mentionGroupPageSize(
        this.currentFilter,
        "agent_generated_files"
      );
      this.setState({
        status: this.state.status === "loading" ? "loading" : "ready",
        query: this.currentQuery,
        mode: this.currentQuery ? "results" : "browse",
        filter: this.currentFilter,
        categories: buildBrowseCategories(this.hiddenFilterIds),
        groups: this.groupsFromRawGroups(),
        error: null
      });
      return true;
    }
    if (item.mentionNavigation === "agent-generated-folder-back") {
      this.resetAgentGeneratedBrowsePath();
      this.setState({
        status: this.state.status === "loading" ? "loading" : "ready",
        query: this.currentQuery,
        mode: this.currentQuery ? "results" : "browse",
        filter: this.currentFilter,
        categories: buildBrowseCategories(this.hiddenFilterIds),
        groups: this.groupsFromRawGroups(),
        error: null
      });
      return true;
    }
    if (item.mentionNavigation === "workspace-folder") {
      const path = item.path.trim();
      if (!path || this.currentFilter !== "file" || this.currentQuery) {
        return false;
      }
      this.workspaceFileBrowsePaths.push(path);
      this.loadWorkspaceFileDirectory(path);
      return true;
    }
    if (item.mentionNavigation === "workspace-folder-back") {
      return this.exitWorkspaceFileBrowse();
    }
    return false;
  }

  exitFileMentionBrowse(): boolean {
    if (this.workspaceFileBrowsePaths.length > 0) {
      return this.exitWorkspaceFileBrowse();
    }
    if (!this.agentGeneratedBrowsePath) return false;
    this.resetAgentGeneratedBrowsePath();
    this.setState({
      status: this.state.status === "loading" ? "loading" : "ready",
      query: this.currentQuery,
      mode: this.currentQuery ? "results" : "browse",
      filter: this.currentFilter,
      categories: buildBrowseCategories(this.hiddenFilterIds),
      groups: this.groupsFromRawGroups(),
      error: null
    });
    return true;
  }

  private exitWorkspaceFileBrowse(): boolean {
    if (this.workspaceFileBrowsePaths.length === 0) {
      return false;
    }
    this.workspaceFileBrowsePaths.pop();
    const parentPath = this.workspaceFileBrowsePaths.at(-1);
    if (parentPath) {
      this.loadWorkspaceFileDirectory(parentPath);
      return true;
    }
    this.directoryRequestLifecycle.cancel();
    this.startBrowseModeFetch("file");
    return true;
  }

  private loadWorkspaceFileDirectory(directoryPath: string): void {
    this.clearTimer();
    this.abortActiveRequest();
    this.cancelIssueLoadMoreRequests();
    this.requestId += 1;
    const workspaceId = this.activeWorkspaceId;
    const provider = this.contextMentionProviders.get(FILE_PROVIDER_ID);
    const directoryRequest = this.directoryRequestLifecycle.start();
    this.setState({
      status: "loading",
      query: "",
      mode: "browse",
      filter: this.currentFilter,
      categories: buildBrowseCategories(this.hiddenFilterIds),
      groups: this.groupsFromRawGroups(),
      error: null
    });
    void queryAgentMentionProviderDirectoryItems({
      abortSignal: directoryRequest.abortSignal,
      provider,
      directoryPath,
      workspaceId,
      currentUserId: this.currentUserId,
      sectionKey: this.currentSectionKey,
      sessionCwd: this.currentSessionCwd,
      provenanceFilter: this.currentProvenanceFilter
    })
      .then((items) => {
        if (
          !this.directoryRequestLifecycle.canApply({
            activeDirectoryPath: this.workspaceFileBrowsePaths.at(-1),
            activeWorkspaceId: this.activeWorkspaceId,
            directoryPath,
            disposed: this.disposed,
            filter: this.currentFilter,
            query: this.currentQuery,
            requestId: directoryRequest.requestId,
            workspaceId
          })
        ) {
          return;
        }
        this.directoryRequestLifecycle.finish(directoryRequest.requestId);
        this.rawGroups.opened_files = presentWorkspaceFileDirectoryMentionItems(
          {
            browsePath: directoryPath,
            items
          }
        );
        this.totalCounts.opened_files = this.rawGroups.opened_files.length;
        this.setState({
          status: "ready",
          query: "",
          mode: "browse",
          filter: "file",
          categories: buildBrowseCategories(this.hiddenFilterIds),
          groups: this.groupsFromRawGroups(),
          error: null
        });
      })
      .catch((error) => {
        if (
          !this.directoryRequestLifecycle.canApply({
            activeDirectoryPath: this.workspaceFileBrowsePaths.at(-1),
            activeWorkspaceId: this.activeWorkspaceId,
            directoryPath,
            disposed: this.disposed,
            filter: this.currentFilter,
            query: this.currentQuery,
            requestId: directoryRequest.requestId,
            workspaceId
          })
        ) {
          return;
        }
        this.directoryRequestLifecycle.finish(directoryRequest.requestId);
        this.setState({
          status: "error",
          query: "",
          mode: "browse",
          filter: "file",
          categories: buildBrowseCategories(this.hiddenFilterIds),
          groups: this.groupsFromRawGroups(),
          error: error instanceof Error ? error.message : String(error)
        });
      });
  }

  expandGroup(groupId: AgentMentionGroupId): void {
    if (groupId.startsWith("issue-topic:")) {
      this.loadMoreIssueTopic(groupId as `issue-topic:${string}`);
      return;
    }
    const pageSize = mentionGroupPageSize(this.currentFilter, groupId);
    const current = this.expandedCounts[groupId] ?? pageSize;
    this.expandedCounts[groupId] = current + pageSize;
    if (!this.currentQuery) {
      this.setState({
        status: "ready",
        query: "",
        mode: "browse",
        filter: this.currentFilter,
        categories: buildBrowseCategories(this.hiddenFilterIds),
        groups: this.groupsFromRawGroups(),
        error: null
      });
      return;
    }
    const needsMoreFiles =
      (groupId === "opened_files" ||
        groupId === "files" ||
        groupId === "agent_generated_files") &&
      (groupId === "agent_generated_files"
        ? this.rawGroups.agent_generated_files.length >=
          this.currentFileSearchLimit
        : this.rawGroups.opened_files.length >= this.currentFileSearchLimit);
    const needsMoreIssues =
      groupId === "issues" &&
      (this.totalCounts.issues ?? this.rawGroups.issues.length) >
        this.rawGroups.issues.length;
    if (needsMoreFiles) {
      this.currentFileSearchLimit += DEFAULT_MENTION_GROUP_PAGE_SIZE;
    }
    if (needsMoreIssues) {
      this.currentIssueSearchLimit += DEFAULT_MENTION_GROUP_PAGE_SIZE;
    }
    if (needsMoreFiles || needsMoreIssues) {
      this.clearTimer();
      this.abortActiveRequest();
      const requestId = ++this.requestId;
      this.setState({
        status: "loading",
        query: this.currentQuery,
        mode: "results",
        filter: this.currentFilter,
        categories: buildBrowseCategories(this.hiddenFilterIds),
        groups: this.groupsFromRawGroups(),
        error: null
      });
      const abortSignal = this.beginActiveRequest();
      void this.runSearch({
        workspaceId: this.activeWorkspaceId,
        currentUserId: this.currentUserId,
        query: this.currentQuery,
        requestId,
        filter: this.currentFilter,
        provenanceFilter: this.currentProvenanceFilter,
        sectionKey: this.currentSectionKey,
        sessionCwd: this.currentSessionCwd,
        abortSignal
      });
      return;
    }
    this.setState({
      status: "ready",
      query: this.currentQuery,
      mode: "results",
      filter: this.currentFilter,
      categories: buildBrowseCategories(this.hiddenFilterIds),
      groups: this.groupsFromRawGroups(),
      error: null
    });
  }

  close(): void {
    this.clearTimer();
    this.abortActiveRequest();
    this.directoryRequestLifecycle.cancel();
    this.cancelIssueLoadMoreRequests();
    this.requestId += 1;
    // Retain the last reference category within this composer lifetime.
    this.resetAgentGeneratedBrowsePath();
    this.resetWorkspaceFileBrowsePaths();
    this.resetExpandedCounts();
    this.resetSearchLimits();
    this.resetRawGroups();
    this.currentQuery = "";
    this.setState({
      status: "idle",
      query: "",
      mode: "browse",
      filter: this.currentFilter,
      categories: buildBrowseCategories(this.hiddenFilterIds),
      groups: [],
      error: null
    });
  }

  dispose(): void {
    this.disposed = true;
    this.clearTimer();
    this.abortActiveRequest();
    this.directoryRequestLifecycle.cancel();
    this.cancelIssueLoadMoreRequests();
    this.cancelPendingPreload();
    this.listeners.clear();
    this.requestId += 1;
  }

  private loadMoreIssueTopic(groupId: `issue-topic:${string}`): void {
    const group = this.issueTopicGroups?.find(
      (candidate) => candidate.id === groupId
    );
    const cursor = group?.nextPageToken;
    const provider = this.contextMentionProviders.get(
      WORKSPACE_ISSUE_PROVIDER_ID
    );
    if (!group || !cursor || !provider?.queryGroupPage) {
      return;
    }
    const requestKey = JSON.stringify({
      workspaceId: this.activeWorkspaceId,
      query: this.currentQuery,
      providerGroupId: group.providerGroupId,
      cursor
    });
    if (this.issueLoadMoreRequests.has(requestKey)) {
      return;
    }
    const requestId = this.requestId;
    const workspaceId = this.activeWorkspaceId;
    const currentUserId = this.currentUserId;
    const sectionKey = this.currentSectionKey;
    const sessionCwd = this.currentSessionCwd;
    const provenanceFilter = this.currentProvenanceFilter;
    const query = this.currentQuery;
    const filter = this.currentFilter;
    const abortController = new AbortController();
    const requestToken = Symbol(requestKey);
    group.loadMoreStatus = "loading";
    group.loadMoreError = null;
    this.emitIssueTopicGroupsState();

    void (async () => {
      try {
        const diagnostics: AgentMentionProviderQueryDiagnostic[] = [];
        const page = await queryAgentMentionProviderWithDiagnostics({
          abortSignal: abortController.signal,
          diagnosticNow: this.diagnosticNow,
          diagnostics,
          fallback: null,
          providerId: WORKSPACE_ISSUE_PROVIDER_ID,
          providerTimeoutMs: this.providerTimeoutMs,
          throwOnTimeout: true,
          query: (abortSignal) =>
            queryAgentMentionProviderGroupPage({
              provider,
              providerGroupId: group.providerGroupId,
              workspaceId,
              currentUserId,
              query,
              cursor,
              pageSize: DEFAULT_MENTION_GROUP_PAGE_SIZE,
              sectionKey,
              sessionCwd,
              abortSignal,
              provenanceFilter
            }),
          resultCount: (result) => result?.items.length ?? 0
        });
        if (
          !page ||
          !this.canApply(requestId, workspaceId, query, filter) ||
          abortController.signal.aborted
        ) {
          return;
        }
        const current = this.issueTopicGroups?.find(
          (candidate) => candidate.id === groupId
        );
        if (!current || current.nextPageToken !== cursor) {
          return;
        }
        const seen = new Set(
          current.items
            .filter((item) => item.kind === "workspace-issue")
            .map((item) => item.targetId)
        );
        const appended = page.items.filter((item) => {
          if (item.kind !== "workspace-issue") {
            return true;
          }
          if (seen.has(item.targetId)) {
            return false;
          }
          seen.add(item.targetId);
          return true;
        });
        current.items = [...current.items, ...appended];
        current.totalCount = Math.max(page.totalCount, current.items.length);
        current.nextPageToken = page.nextPageToken;
        current.loadMoreStatus = "idle";
        current.loadMoreError = null;
        if (!query) {
          mergeAgentMentionBrowseIssueGroupPage({
            cacheKey: this.browseCacheKey(
              {
                workspaceId,
                currentUserId,
                filter,
                sectionKey,
                sessionCwd
              },
              provenanceFilter
            ),
            group: page,
            cachedAt: this.diagnosticNow()
          });
        }
        this.emitIssueTopicGroupsState();
      } catch (error) {
        if (
          !this.canApply(requestId, workspaceId, query, filter) ||
          abortController.signal.aborted
        ) {
          return;
        }
        const current = this.issueTopicGroups?.find(
          (candidate) => candidate.id === groupId
        );
        if (current?.nextPageToken === cursor) {
          current.loadMoreStatus = "error";
          current.loadMoreError =
            error instanceof Error ? error.message : String(error);
          this.emitIssueTopicGroupsState();
        }
      } finally {
        const active = this.issueLoadMoreRequests.get(requestKey);
        if (active?.token === requestToken) {
          this.issueLoadMoreRequests.delete(requestKey);
        }
      }
    })();
    this.issueLoadMoreRequests.set(requestKey, {
      abortController,
      token: requestToken
    });
  }

  private emitIssueTopicGroupsState(): void {
    this.setState({
      status: "ready",
      query: this.currentQuery,
      mode: this.currentQuery ? "results" : "browse",
      filter: this.currentFilter,
      categories: buildBrowseCategories(this.hiddenFilterIds),
      groups: this.groupsFromRawGroups(),
      error: null
    });
  }

  private cancelIssueLoadMoreRequests(): void {
    for (const request of this.issueLoadMoreRequests.values()) {
      request.abortController.abort();
    }
    this.issueLoadMoreRequests.clear();
  }
}

// A transient controller warms the shared cache and leaves no timer behind.
export function preloadAgentMentionBrowse(input: {
  workspaceId: string;
  currentUserId?: string | null;
  sectionKey?: string | null;
  sessionCwd?: string | null;
  contextMentionProviders?: readonly AgentContextMentionProvider[];
  filter?: AgentMentionFilterId;
}): void {
  const controller = new AgentMentionSearchController({
    contextMentionProviders: input.contextMentionProviders
  });
  controller.preloadBrowse({
    workspaceId: input.workspaceId,
    currentUserId: input.currentUserId,
    sectionKey: input.sectionKey,
    sessionCwd: input.sessionCwd,
    filter: input.filter
  });
}
