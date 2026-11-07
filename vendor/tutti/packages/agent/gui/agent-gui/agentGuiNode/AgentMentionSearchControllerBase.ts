import type { RuntimeDiagnosticsDetailValue } from "../../shared/contracts/dto/debug";
import {
  emitAgentMentionSearchDiagnostic,
  logAgentMentionSearchInfo,
  queryAgentMentionProviderWithDiagnostics,
  type AgentMentionProviderQueryDiagnostic,
  type AgentMentionSearchDiagnosticLog
} from "./agentMentionSearchDiagnostics";
import {
  DEFAULT_AGENT_MENTION_FILTER,
  mentionGroupPageSize
} from "./agentMentionSearchHelpers";
import type { AgentContextMentionItem } from "./agentRichText/agentFileMentionExtension";
import type { AgentContextMentionProvider } from "./agentContextMentionProvider";
import {
  buildBrowseCategories,
  DEFAULT_BROWSE_CACHE_TTL_MS,
  DEFAULT_DEBOUNCE_MS,
  DEFAULT_DIAGNOSTIC_SLOW_THRESHOLD_MS,
  DEFAULT_FILE_LIMIT,
  DEFAULT_ISSUE_LIMIT,
  DEFAULT_PROVIDER_TIMEOUT_MS,
  type AgentMentionFilterId,
  type AgentMentionGroup,
  type AgentMentionGroupId,
  type AgentMentionIssueTopicGroup,
  type AgentMentionLifecycleDiagnosticLog,
  type AgentMentionRawGroups,
  type AgentMentionSearchControllerOptions,
  type AgentMentionSearchListener,
  type AgentMentionSearchState,
  type AgentMentionTotalCounts
} from "./AgentMentionSearchContracts";
import {
  buildAgentMentionBrowseCacheKey,
  loadAgentMentionBrowseFetchResult,
  mergeAgentMentionBrowseIssueGroupPage,
  readAgentMentionBrowseCache,
  type AgentMentionBrowseCacheEntry,
  type AgentMentionBrowseFetchResult,
  type AgentMentionBrowseLoadReason
} from "./AgentMentionSearchCache";
import {
  buildAgentMentionGroups,
  cloneAgentMentionIssueTopicGroups,
  cloneAgentMentionRawGroups,
  elapsedDiagnosticMs,
  emptyAgentMentionRawGroups,
  issueTopicPaginationChanges,
  logAgentMentionLifecycleDiagnostic,
  rawGroupItemCount
} from "./AgentMentionSearchModel";
import {
  fetchAgentMentionFilterResult,
  queryAgentMentionProviderGroups,
  queryAgentMentionProviderItems
} from "./AgentMentionSearchIndex";
import type {
  ReferenceProvenanceCatalog,
  ReferenceProvenanceFilter
} from "@tutti-os/workspace-file-reference/contracts";
import { referenceProvenanceFilterCacheKey } from "@tutti-os/workspace-file-reference/core";
import {
  agentGuiScheduler,
  type AgentGuiScheduledTask
} from "./agentGuiScheduler";

export class AgentMentionSearchControllerBase {
  protected readonly contextMentionProviders: ReadonlyMap<
    string,
    AgentContextMentionProvider
  >;
  protected readonly debounceMs: number;
  protected readonly fileLimit: number;
  protected readonly issueLimit: number;
  protected readonly browseCacheTtlMs: number;
  protected readonly providerTimeoutMs: number;
  protected readonly diagnosticInfoLogger: (
    payload: AgentMentionSearchDiagnosticLog
  ) => void;
  protected readonly diagnosticNow: () => number;
  protected readonly diagnosticSlowThresholdMs: number;
  protected readonly hiddenFilterIds: readonly string[];
  protected readonly listeners = new Set<AgentMentionSearchListener>();
  protected readonly expandedCounts: Partial<
    Record<AgentMentionGroupId, number>
  > = {};
  protected readonly totalCounts: AgentMentionTotalCounts = {};
  protected readonly scheduler = agentGuiScheduler;
  protected timer: AgentGuiScheduledTask | null = null;
  protected preloadCancel: (() => void) | null = null;
  protected pendingPreloadKey: string | null = null;
  protected requestId = 0;
  protected activeRequestAbortController: AbortController | null = null;
  protected disposed = false;
  protected activeWorkspaceId = "";
  protected currentUserId = "";
  protected currentFilter: AgentMentionFilterId = DEFAULT_AGENT_MENTION_FILTER;
  protected currentQuery = "";
  protected currentSessionCwd = "";
  protected currentSectionKey = "";
  protected currentProvenanceFilter: ReferenceProvenanceFilter | null = null;
  protected currentProvenanceCatalog: ReferenceProvenanceCatalog | null = null;
  protected currentFileSearchLimit: number;
  protected currentIssueSearchLimit: number;
  protected agentGeneratedBrowsePath: string | null = null;
  protected workspaceFileBrowsePaths: string[] = [];
  protected rawGroups: AgentMentionRawGroups = emptyAgentMentionRawGroups();
  protected issueTopicGroups: AgentMentionIssueTopicGroup[] | null = null;
  protected state: AgentMentionSearchState = {
    status: "idle",
    query: "",
    mode: "browse",
    filter: DEFAULT_AGENT_MENTION_FILTER,
    categories: [],
    groups: [],
    error: null
  };
  constructor(options: AgentMentionSearchControllerOptions) {
    this.contextMentionProviders = new Map(
      (options.contextMentionProviders ?? []).map((provider) => [
        provider.id,
        provider
      ])
    );
    this.debounceMs = options.debounceMs ?? DEFAULT_DEBOUNCE_MS;
    this.fileLimit = options.fileLimit ?? DEFAULT_FILE_LIMIT;
    this.issueLimit = options.issueLimit ?? DEFAULT_ISSUE_LIMIT;
    this.browseCacheTtlMs =
      options.browseCacheTtlMs ?? DEFAULT_BROWSE_CACHE_TTL_MS;
    this.providerTimeoutMs =
      options.providerTimeoutMs ?? DEFAULT_PROVIDER_TIMEOUT_MS;
    this.diagnosticInfoLogger =
      options.diagnosticInfoLogger ?? logAgentMentionSearchInfo;
    this.diagnosticNow = options.diagnosticNow ?? Date.now;
    this.diagnosticSlowThresholdMs =
      options.diagnosticSlowThresholdMs ?? DEFAULT_DIAGNOSTIC_SLOW_THRESHOLD_MS;
    this.hiddenFilterIds = [...(options.hiddenFilterIds ?? []),
      ...(!options.contextMentionProviders?.some((provider) => provider.id === "workspace-member") ? ["member"] : [])];
    this.currentFileSearchLimit = this.fileLimit;
    this.currentIssueSearchLimit = this.issueLimit;
    this.state.categories = buildBrowseCategories(this.hiddenFilterIds);
  }

  protected startBrowseModeFetch(filter: AgentMentionFilterId): void {
    if (!this.activeWorkspaceId) {
      this.resetRawGroups();
      this.emitBrowseState("ready");
      return;
    }
    this.clearTimer();
    const requestId = ++this.requestId;
    const cacheKey = this.browseCacheKey({
      currentUserId: this.currentUserId,
      filter,
      sectionKey: this.currentSectionKey,
      sessionCwd: this.currentSessionCwd,
      workspaceId: this.activeWorkspaceId
    });
    this.logLifecycle("browse.open", {
      filter,
      providerIds: this.providerIdsForDiagnostics(),
      requestId,
      sessionCwdPresent: Boolean(this.currentSessionCwd),
      workspaceId: this.activeWorkspaceId
    });
    const cached = this.readBrowseCache(cacheKey);
    this.logBrowseCacheState({
      cacheKey,
      cached,
      filter,
      reason: "open",
      workspaceId: this.activeWorkspaceId
    });
    if (cached.entry) {
      this.applyBrowseFetchResult(cached.entry);
      this.setState({
        status: "ready",
        query: "",
        mode: "browse",
        filter: this.currentFilter,
        categories: buildBrowseCategories(this.hiddenFilterIds),
        groups: this.groupsFromRawGroups(),
        error: null
      });
      // A user-opened palette always revalidates. Cache freshness is used only
      // to suppress redundant speculative preloads; here the cached entry is a
      // zero-latency presentation while the provider query refreshes it.
    } else {
      this.rawGroups = emptyAgentMentionRawGroups();
      this.resetTotalCounts();
      this.emitBrowseState("loading");
    }
    const abortSignal = this.beginActiveRequest();
    void this.runBrowseSearch({
      workspaceId: this.activeWorkspaceId,
      currentUserId: this.currentUserId,
      requestId,
      filter,
      sectionKey: this.currentSectionKey,
      sessionCwd: this.currentSessionCwd,
      abortSignal
    });
  }

  protected async runSearch(input: {
    workspaceId: string;
    currentUserId: string;
    query: string;
    requestId: number;
    filter: AgentMentionFilterId;
    provenanceFilter: ReferenceProvenanceFilter | null;
    sectionKey: string;
    sessionCwd: string;
    abortSignal?: AbortSignal;
  }): Promise<void> {
    const startedAt = this.diagnosticNow();
    let providerDiagnostics: AgentMentionProviderQueryDiagnostic[] = [];
    try {
      const result = await this.fetchFilterResult(
        {
          workspaceId: input.workspaceId,
          currentUserId: input.currentUserId,
          query: input.query,
          filter: input.filter,
          sectionKey: input.sectionKey,
          sessionCwd: input.sessionCwd,
          includeAgentGeneratedFiles: false
        },
        input.provenanceFilter,
        input.abortSignal
      );
      providerDiagnostics = result.providerDiagnostics;

      if (
        !this.canApply(
          input.requestId,
          input.workspaceId,
          input.query,
          input.filter
        )
      ) {
        return;
      }

      this.applyBrowseFetchResult(result);
      const groups = this.groupsFromRawGroups();

      this.setState({
        status: "ready",
        query: input.query,
        mode: "results",
        filter: this.currentFilter,
        categories: buildBrowseCategories(this.hiddenFilterIds),
        groups,
        error: null
      });
      this.logSearchDiagnostic({
        filter: input.filter,
        groups,
        mode: "results",
        providerDiagnostics,
        query: input.query,
        requestId: input.requestId,
        startedAt,
        status: "ready",
        workspaceId: input.workspaceId
      });
    } catch (error) {
      if (
        !this.canApply(
          input.requestId,
          input.workspaceId,
          input.query,
          input.filter
        )
      ) {
        return;
      }
      this.setState({
        status: "error",
        query: input.query,
        mode: "results",
        filter: this.currentFilter,
        categories: buildBrowseCategories(this.hiddenFilterIds),
        groups: [],
        error: error instanceof Error ? error.message : String(error)
      });
      this.logSearchDiagnostic({
        error,
        filter: input.filter,
        groups: [],
        mode: "results",
        providerDiagnostics,
        query: input.query,
        requestId: input.requestId,
        startedAt,
        status: "error",
        workspaceId: input.workspaceId
      });
    }
  }

  protected async runBrowseSearch(input: {
    workspaceId: string;
    currentUserId: string;
    requestId: number;
    filter: AgentMentionFilterId;
    sectionKey: string;
    sessionCwd: string;
    abortSignal: AbortSignal;
  }): Promise<void> {
    const startedAt = this.diagnosticNow();
    let providerDiagnostics: AgentMentionProviderQueryDiagnostic[] = [];
    const provenanceFilter = this.currentProvenanceFilter;
    const cacheKey = this.browseCacheKey(input, provenanceFilter);
    const issueTopicGroupsAtStart = cloneAgentMentionIssueTopicGroups(
      this.issueTopicGroups
    );
    try {
      const result = await this.loadBrowseFetchResult(
        input,
        cacheKey,
        "open",
        provenanceFilter,
        input.abortSignal
      );
      providerDiagnostics = result.providerDiagnostics;
      if (
        !this.canApply(input.requestId, input.workspaceId, "", input.filter)
      ) {
        this.logLifecycle("browse.apply.skipped", {
          filter: input.filter,
          requestId: input.requestId,
          reason: "open",
          workspaceId: input.workspaceId
        });
        return;
      }
      const paginationChanges = issueTopicPaginationChanges(
        issueTopicGroupsAtStart,
        this.issueTopicGroups
      );
      for (const group of paginationChanges) {
        mergeAgentMentionBrowseIssueGroupPage({
          cacheKey,
          group,
          cachedAt: this.diagnosticNow()
        });
      }
      const resultToApply =
        paginationChanges.length > 0
          ? (this.readBrowseCache(cacheKey).entry ?? result)
          : result;
      this.applyBrowseFetchResult(resultToApply);
      const groups = this.groupsFromRawGroups();

      this.setState({
        status: "ready",
        query: "",
        mode: "browse",
        filter: this.currentFilter,
        categories: buildBrowseCategories(this.hiddenFilterIds),
        groups,
        error: null
      });
      this.logSearchDiagnostic({
        filter: input.filter,
        groups,
        mode: "browse",
        providerDiagnostics,
        query: "",
        requestId: input.requestId,
        startedAt,
        status: "ready",
        workspaceId: input.workspaceId
      });
    } catch (error) {
      if (
        !this.canApply(input.requestId, input.workspaceId, "", input.filter)
      ) {
        return;
      }
      this.setState({
        status: "error",
        query: "",
        mode: "browse",
        filter: this.currentFilter,
        categories: buildBrowseCategories(this.hiddenFilterIds),
        groups: [],
        error: error instanceof Error ? error.message : String(error)
      });
      this.logSearchDiagnostic({
        error,
        filter: input.filter,
        groups: [],
        mode: "browse",
        providerDiagnostics,
        query: "",
        requestId: input.requestId,
        startedAt,
        status: "error",
        workspaceId: input.workspaceId
      });
    }
  }

  protected async fetchBrowseResult(
    input: {
      workspaceId: string;
      currentUserId: string;
      filter: AgentMentionFilterId;
      sectionKey: string;
      sessionCwd: string;
    },
    provenanceFilter: ReferenceProvenanceFilter | null = this
      .currentProvenanceFilter,
    abortSignal?: AbortSignal
  ): Promise<AgentMentionBrowseFetchResult> {
    return this.fetchFilterResult(
      {
        ...input,
        query: "",
        includeAgentGeneratedFiles: input.filter === "file"
      },
      provenanceFilter,
      abortSignal
    );
  }

  protected applyBrowseFetchResult(
    result: AgentMentionBrowseFetchResult
  ): void {
    this.rawGroups = cloneAgentMentionRawGroups(result.rawGroups);
    this.issueTopicGroups = cloneAgentMentionIssueTopicGroups(
      result.issueTopicGroups
    );
    this.resetTotalCounts();
    for (const [groupId, count] of Object.entries(result.totalCounts) as [
      AgentMentionGroupId,
      number
    ][]) {
      this.totalCounts[groupId] = count;
    }
  }

  protected logBrowseCacheState(input: {
    cacheKey: string;
    cached: { entry: AgentMentionBrowseCacheEntry | null; isFresh: boolean };
    filter: AgentMentionFilterId;
    reason: AgentMentionBrowseLoadReason;
    workspaceId: string;
  }): void {
    this.logLifecycle("browse.cache", {
      ageMs: input.cached.entry
        ? elapsedDiagnosticMs(this.diagnosticNow(), input.cached.entry.cachedAt)
        : null,
      cacheKeyLength: input.cacheKey.length,
      cacheState: input.cached.entry
        ? input.cached.isFresh
          ? "fresh"
          : "stale"
        : "miss",
      filter: input.filter,
      itemCount: input.cached.entry
        ? rawGroupItemCount(input.cached.entry.rawGroups)
        : 0,
      reason: input.reason,
      workspaceId: input.workspaceId
    });
  }

  protected logLifecycle(
    event: AgentMentionLifecycleDiagnosticLog["event"],
    details: Record<string, RuntimeDiagnosticsDetailValue>
  ): void {
    logAgentMentionLifecycleDiagnostic({ event, details });
  }

  protected providerIdsForDiagnostics(): string {
    return [...this.contextMentionProviders.keys()].sort().join(",");
  }

  protected async queryProviderMentionItemsById(input: {
    diagnostics: AgentMentionProviderQueryDiagnostic[];
    providerId: string;
    workspaceId: string;
    currentUserId: string;
    query: string;
    limit?: number;
    sessionCwd?: string;
    sectionKey?: string;
    provenanceFilter: ReferenceProvenanceFilter | null;
    abortSignal?: AbortSignal;
  }): Promise<AgentContextMentionItem[]> {
    const provider = this.contextMentionProviders.get(input.providerId);
    return queryAgentMentionProviderWithDiagnostics({
      diagnosticNow: this.diagnosticNow,
      abortSignal: input.abortSignal,
      diagnostics: input.diagnostics,
      fallback: [] as AgentContextMentionItem[],
      providerId: input.providerId,
      providerTimeoutMs: this.providerTimeoutMs,
      query: provider
        ? (abortSignal) =>
            queryAgentMentionProviderItems({
              provider,
              workspaceId: input.workspaceId,
              currentUserId: input.currentUserId,
              query: input.query,
              limit: input.limit,
              sectionKey: input.sectionKey ?? this.currentSectionKey,
              sessionCwd: input.sessionCwd ?? this.currentSessionCwd,
              abortSignal,
              provenanceFilter: input.provenanceFilter
            })
        : null,
      resultCount: (result) => result.length
    });
  }

  protected async queryProviderMentionGroupsById(input: {
    diagnostics: AgentMentionProviderQueryDiagnostic[];
    providerId: string;
    workspaceId: string;
    currentUserId: string;
    query: string;
    limit?: number;
    sessionCwd?: string;
    sectionKey?: string;
    provenanceFilter: ReferenceProvenanceFilter | null;
    abortSignal?: AbortSignal;
  }): Promise<AgentMentionIssueTopicGroup[] | null> {
    const provider = this.contextMentionProviders.get(input.providerId);
    if (!provider?.queryGroups) {
      return null;
    }
    return queryAgentMentionProviderWithDiagnostics({
      abortSignal: input.abortSignal,
      diagnosticNow: this.diagnosticNow,
      diagnostics: input.diagnostics,
      fallback: [] as AgentMentionIssueTopicGroup[],
      providerId: input.providerId,
      providerTimeoutMs: this.providerTimeoutMs,
      throwOnTimeout: true,
      query: (abortSignal) =>
        queryAgentMentionProviderGroups({
          provider,
          workspaceId: input.workspaceId,
          currentUserId: input.currentUserId,
          query: input.query,
          limit: input.limit,
          sectionKey: input.sectionKey ?? this.currentSectionKey,
          sessionCwd: input.sessionCwd ?? this.currentSessionCwd,
          abortSignal,
          provenanceFilter: input.provenanceFilter
        }).then((groups) => groups ?? []),
      resultCount: (groups) =>
        groups.reduce((count, group) => count + group.items.length, 0)
    });
  }

  protected logSearchDiagnostic(input: {
    error?: unknown;
    filter: AgentMentionFilterId;
    groups: readonly AgentMentionGroup[];
    mode: "browse" | "results";
    providerDiagnostics: readonly AgentMentionProviderQueryDiagnostic[];
    query: string;
    requestId: number;
    startedAt: number;
    status: "ready" | "error";
    workspaceId: string;
  }): void {
    emitAgentMentionSearchDiagnostic({
      debounceMs: this.debounceMs,
      diagnosticInfoLogger: this.diagnosticInfoLogger,
      diagnosticNow: this.diagnosticNow,
      diagnosticSlowThresholdMs: this.diagnosticSlowThresholdMs,
      error: input.error,
      filter: input.filter,
      groups: input.groups,
      mode: input.mode,
      providerDiagnostics: input.providerDiagnostics,
      providerTimeoutMs: this.providerTimeoutMs,
      query: input.query,
      requestId: input.requestId,
      startedAt: input.startedAt,
      status: input.status,
      workspaceId: input.workspaceId
    });
  }

  protected emitBrowseState(status: "ready" | "loading"): void {
    this.setState({
      status,
      query: "",
      mode: "browse",
      filter: this.currentFilter,
      categories: buildBrowseCategories(this.hiddenFilterIds),
      groups: [],
      error: null
    });
  }

  protected canApply(
    requestId: number,
    workspaceId: string,
    query: string,
    filter: AgentMentionFilterId
  ): boolean {
    return (
      !this.disposed &&
      requestId === this.requestId &&
      workspaceId === this.activeWorkspaceId &&
      query === this.currentQuery &&
      filter === this.currentFilter
    );
  }

  protected clearTimer(): void {
    if (this.timer !== null) {
      this.timer.cancel();
      this.timer = null;
    }
  }

  protected resetExpandedCounts(): void {
    for (const groupId of Object.keys(this.expandedCounts)) {
      delete this.expandedCounts[groupId as AgentMentionGroupId];
    }
    for (const groupId of [
      "files",
      "opened_files",
      "agent_generated_files",
      "my_sessions",
      "collab_sessions",
      "agents",
      "apps",
      "issues"
    ] as const) {
      this.expandedCounts[groupId] = mentionGroupPageSize(
        this.currentFilter,
        groupId
      );
    }
  }

  protected resetSearchLimits(): void {
    this.currentFileSearchLimit = this.fileLimit;
    this.currentIssueSearchLimit = this.issueLimit;
  }

  protected resetRawGroups(): void {
    this.rawGroups = emptyAgentMentionRawGroups();
    this.issueTopicGroups = null;
    this.resetTotalCounts();
  }

  protected resetAgentGeneratedBrowsePath(): void {
    this.agentGeneratedBrowsePath = null;
  }

  protected resetWorkspaceFileBrowsePaths(): void {
    this.workspaceFileBrowsePaths = [];
  }

  protected resetTotalCounts(): void {
    for (const groupId of [
      "files",
      "opened_files",
      "agent_generated_files",
      "my_sessions",
      "collab_sessions",
      "agents",
      "apps",
      "issues"
    ] as const) {
      delete this.totalCounts[groupId];
    }
  }

  protected setState(state: AgentMentionSearchState): void {
    this.state = state;
    for (const listener of this.listeners) {
      listener(state);
    }
  }

  protected groupsFromRawGroups(): AgentMentionGroup[] {
    return buildAgentMentionGroups({
      agentGeneratedBrowsePath: this.agentGeneratedBrowsePath,
      currentFileSearchLimit: this.currentFileSearchLimit,
      currentFilter: this.currentFilter,
      currentQuery: this.currentQuery,
      expandedCounts: this.expandedCounts,
      rawGroups: this.rawGroups,
      totalCounts: this.totalCounts,
      issueTopicGroups: this.issueTopicGroups,
      provenanceCatalog: this.currentProvenanceCatalog,
      provenanceFilter: this.currentProvenanceFilter
    });
  }

  protected browseCacheKey(
    input: {
      workspaceId: string;
      currentUserId: string;
      filter: AgentMentionFilterId;
      sectionKey: string;
      sessionCwd: string;
    },
    provenanceFilter: ReferenceProvenanceFilter | null = this
      .currentProvenanceFilter
  ): string {
    return buildAgentMentionBrowseCacheKey({
      ...input,
      fileLimit: this.fileLimit,
      issueLimit: this.currentIssueSearchLimit,
      providerIds: [...this.contextMentionProviders.keys()].sort(),
      provenanceFilterKey: provenanceFilter
        ? referenceProvenanceFilterCacheKey(provenanceFilter)
        : "disabled"
    });
  }

  protected readBrowseCache(cacheKey: string) {
    return readAgentMentionBrowseCache({
      cacheKey,
      browseCacheTtlMs: this.browseCacheTtlMs,
      diagnosticNow: this.diagnosticNow
    });
  }

  protected loadBrowseFetchResult(
    input: {
      workspaceId: string;
      currentUserId: string;
      filter: AgentMentionFilterId;
      sectionKey: string;
      sessionCwd: string;
    },
    cacheKey: string,
    reason: AgentMentionBrowseLoadReason,
    provenanceFilter: ReferenceProvenanceFilter | null = this
      .currentProvenanceFilter,
    abortSignal?: AbortSignal
  ): Promise<AgentMentionBrowseFetchResult> {
    return loadAgentMentionBrowseFetchResult({
      input,
      cacheKey,
      reason,
      abortSignal,
      diagnosticNow: this.diagnosticNow,
      providerIds: this.providerIdsForDiagnostics(),
      fetchBrowseResult: (sharedAbortSignal) =>
        this.fetchBrowseResult(input, provenanceFilter, sharedAbortSignal),
      logLifecycle: (event, details) => this.logLifecycle(event, details)
    });
  }

  protected fetchFilterResult(
    input: {
      workspaceId: string;
      currentUserId: string;
      query: string;
      filter: AgentMentionFilterId;
      sectionKey: string;
      sessionCwd: string;
      includeAgentGeneratedFiles: boolean;
    },
    provenanceFilter: ReferenceProvenanceFilter | null = this
      .currentProvenanceFilter,
    abortSignal?: AbortSignal
  ): Promise<AgentMentionBrowseFetchResult> {
    return fetchAgentMentionFilterResult({
      ...input,
      fileLimit: this.fileLimit,
      currentFileSearchLimit: this.currentFileSearchLimit,
      currentIssueSearchLimit: this.currentIssueSearchLimit,
      provenanceCatalog: this.currentProvenanceCatalog,
      provenanceFilter,
      queryProviderMentionGroupsById: (queryInput) =>
        this.queryProviderMentionGroupsById({ ...queryInput, abortSignal }),
      queryProviderMentionItemsById: (queryInput) =>
        this.queryProviderMentionItemsById({ ...queryInput, abortSignal })
    });
  }

  protected beginActiveRequest(): AbortSignal {
    this.abortActiveRequest();
    const controller = new AbortController();
    this.activeRequestAbortController = controller;
    return controller.signal;
  }

  protected abortActiveRequest(): void {
    this.activeRequestAbortController?.abort();
    this.activeRequestAbortController = null;
  }
}
