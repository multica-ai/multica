import { afterEach, describe, expect, it, vi } from "vitest";
import { setAgentGuiI18nTestLocale } from "../../i18n/testUtils";
import type { AgentContextMentionProvider } from "./agentContextMentionProvider";
import { AGENT_CONTEXT_MENTION_PROVIDER_IDS } from "./agentContextMentionProvider";
import {
  AgentMentionSearchController as BaseAgentMentionSearchController,
  MAX_BROWSE_CACHE_ENTRIES,
  preloadAgentMentionBrowse,
  resetAgentMentionSearchBrowseCacheForTests,
  type AgentMentionSearchState
} from "./AgentMentionSearchController";
import { issuePreviewText } from "./agentMentionSearchHelpers";

// Controller work settles through timers or microtasks. Vitest's 50 ms polling
// default adds seconds of idle time across this state-heavy suite.
function waitForFast<T>(assertion: () => T | Promise<T>): Promise<T> {
  return vi.waitFor(assertion, { interval: 1 });
}

interface TestFileMentionItem {
  childCount?: number;
  label: string;
  href: string;
  kind?: "directory" | "file";
}

interface TestIssueMentionItem {
  issueId: string;
  title: string;
  status: string;
}

interface TestWorkspaceAppMentionItem {
  appId: string;
  description?: string;
  name: string;
  workspaceId: string;
}

interface TestAgentTargetMentionItem {
  description?: string;
  iconUrl?: string;
  name: string;
  provider: string;
  status?: string;
  targetId: string;
  workspaceId: string;
}

interface TestSessionMentionItem {
  agentTargetId?: string;
  agentName: string;
  id: string;
  initiatorName: string;
  provider: string;
  scope?: "my_sessions" | "collab_sessions";
  status: string;
  title: string;
  updatedAtUnixMs: number;
  userId: string;
  workspaceId: string;
}

const {
  agentGeneratedFile: AGENT_GENERATED_FILE_PROVIDER_ID,
  agentSession: AGENT_SESSION_PROVIDER_ID,
  agentTarget: AGENT_TARGET_PROVIDER_ID,
  file: FILE_PROVIDER_ID,
  workspaceApp: WORKSPACE_APP_PROVIDER_ID,
  workspaceIssue: WORKSPACE_ISSUE_PROVIDER_ID
} = AGENT_CONTEXT_MENTION_PROVIDER_IDS;

interface TestContextMentionProviderOptions {
  queryAgentGeneratedFiles?: (input: any) => Promise<any>;
  queryFiles?: (input: any) => Promise<any>;
  queryIssues?: (input: any) => Promise<any>;
  queryAgentTargets?: (input: any) => Promise<any>;
  queryWorkspaceApps?: (input: any) => Promise<any>;
  querySessions?: (input: any) => Promise<any>;
  loadSessionSummary?: (input: any) => Promise<any>;
  loadUserProfiles?: (input: any) => Promise<any>;
  loadSessionMessages?: (input: any) => Promise<any>;
  contextMentionProviders?: readonly AgentContextMentionProvider[];
  debounceMs?: number;
  diagnosticInfoLogger?: (payload: any) => void;
  diagnosticNow?: () => number;
  diagnosticSlowThresholdMs?: number;
  fileLimit?: number;
  browseCacheTtlMs?: number;
  issueLimit?: number;
  providerTimeoutMs?: number;
  hiddenFilterIds?: readonly string[];
}

class AgentMentionSearchController extends BaseAgentMentionSearchController {
  constructor(options: TestContextMentionProviderOptions) {
    super({
      debounceMs: options.debounceMs,
      diagnosticInfoLogger: options.diagnosticInfoLogger,
      diagnosticNow: options.diagnosticNow,
      diagnosticSlowThresholdMs: options.diagnosticSlowThresholdMs,
      fileLimit: options.fileLimit,
      browseCacheTtlMs: options.browseCacheTtlMs,
      issueLimit: options.issueLimit,
      providerTimeoutMs: options.providerTimeoutMs,
      hiddenFilterIds: options.hiddenFilterIds,
      contextMentionProviders:
        options.contextMentionProviders ??
        createTestContextMentionProviders(options)
    });
  }
}

function createTestContextMentionProviders(
  options: TestContextMentionProviderOptions
): readonly AgentContextMentionProvider[] {
  return [
    createTestFileProvider(options),
    createTestAgentGeneratedFileProvider(options),
    createTestAgentTargetProvider(options),
    createTestWorkspaceAppProvider(options),
    createTestIssueProvider(options),
    createTestSessionProvider(options)
  ];
}

function createTestAgentGeneratedFileProvider(
  options: TestContextMentionProviderOptions
): AgentContextMentionProvider<{ label: string; href: string }> {
  return {
    id: AGENT_GENERATED_FILE_PROVIDER_ID,
    trigger: "@",
    async query({ context, keyword, maxResults }) {
      if (!options.queryAgentGeneratedFiles) {
        return [];
      }
      const result = await options.queryAgentGeneratedFiles({
        workspaceId: context?.metadata?.workspaceId,
        query: keyword,
        limit: maxResults,
        sectionKey: context?.metadata?.sectionKey,
        provenanceFilter: context?.metadata?.referenceProvenanceFilter
      });
      return (result.entries ?? []).map((entry: any) => ({
        label: entry.name,
        href: entry.path
      }));
    },
    getItemKey: (item) => item.href,
    getItemLabel: (item) => item.label,
    toInsertResult: (item) => ({
      kind: "markdown-link",
      label: item.label,
      href: item.href
    })
  };
}

function createTestFileProvider(
  options: TestContextMentionProviderOptions
): AgentContextMentionProvider<TestFileMentionItem> {
  const queryFiles = async (input: {
    context: { metadata?: Readonly<Record<string, unknown>> };
    directoryPath?: string;
    keyword: string;
    maxResults?: number;
  }) => {
    if (!options.queryFiles) {
      return [];
    }
    const result = await options.queryFiles({
      workspaceId: input.context?.metadata?.workspaceId,
      query: input.keyword,
      limit: input.maxResults,
      includeKinds: ["file", "directory"],
      directoryPath: input.directoryPath
    });
    return (result.entries ?? []).map((entry: any) => ({
      childCount: entry.childCount,
      label: entry.name,
      href: entry.path,
      kind: entry.kind
    }));
  };
  return {
    id: FILE_PROVIDER_ID,
    trigger: "@",
    query: queryFiles,
    queryDirectory: queryFiles,
    getItemDirectory: (item) =>
      item.kind === "directory"
        ? { childCount: item.childCount, path: item.href }
        : null,
    getItemKey: (item) => item.href,
    getItemLabel: (item) => item.label,
    toInsertResult: (item) => ({
      kind: "markdown-link",
      label: item.label,
      href: item.href
    })
  };
}

function createTestIssueProvider(
  options: TestContextMentionProviderOptions
): AgentContextMentionProvider<any> {
  return {
    id: WORKSPACE_ISSUE_PROVIDER_ID,
    trigger: "@",
    async query({ context, keyword, maxResults }) {
      if (!options.queryIssues) {
        return [];
      }
      const result = await options.queryIssues({
        workspaceId: context?.metadata?.workspaceId,
        pageSize: maxResults,
        searchQuery: keyword
      });
      return result.issues ?? [];
    },
    getItemKey: (item) => item.issueId,
    getItemLabel: (item) => item.title,
    getItemSubtitle: (item) => issuePreviewText(item.content),
    toInsertResult: (item) => ({
      kind: "mention",
      mention: {
        entityId: item.issueId,
        label: item.title,
        scope: { workspaceId: item.workspaceId },
        presentation: {
          description: issuePreviewText(item.content),
          status: item.status
        }
      }
    })
  };
}

function createTestAgentTargetProvider(
  options: TestContextMentionProviderOptions
): AgentContextMentionProvider<TestAgentTargetMentionItem> {
  return {
    id: AGENT_TARGET_PROVIDER_ID,
    trigger: "@",
    async query({ context, keyword, maxResults }) {
      if (!options.queryAgentTargets) {
        return [];
      }
      const result = await options.queryAgentTargets({
        workspaceId: context?.metadata?.workspaceId,
        query: keyword,
        limit: maxResults
      });
      return result.targets ?? [];
    },
    getItemKey: (item) => item.targetId,
    getItemLabel: (item) => item.name,
    getItemSubtitle: (item) => item.description,
    getItemIconUrl: (item) => item.iconUrl,
    toInsertResult: (item) => ({
      kind: "mention",
      mention: {
        entityId: item.targetId,
        label: item.name,
        scope: { workspaceId: item.workspaceId },
        presentation: {
          agentProviderId: item.provider,
          description: item.description,
          iconUrl: item.iconUrl,
          status: item.status,
          subtitle: item.description
        }
      }
    })
  };
}

function createTestWorkspaceAppProvider(
  options: TestContextMentionProviderOptions
): AgentContextMentionProvider<TestWorkspaceAppMentionItem> {
  return {
    id: WORKSPACE_APP_PROVIDER_ID,
    trigger: "@",
    async query({ context, keyword, maxResults }) {
      if (!options.queryWorkspaceApps) {
        return [];
      }
      const result = await options.queryWorkspaceApps({
        workspaceId: context?.metadata?.workspaceId,
        query: keyword,
        limit: maxResults
      });
      return result.apps ?? [];
    },
    getItemKey: (item) => item.appId,
    getItemLabel: (item) => item.name,
    getItemSubtitle: (item) => item.description,
    toInsertResult: (item) => ({
      kind: "mention",
      mention: {
        entityId: item.appId,
        label: item.name,
        scope: { workspaceId: item.workspaceId },
        presentation: {
          description: item.description
        }
      }
    })
  };
}

function createTestSessionProvider(
  options: TestContextMentionProviderOptions
): AgentContextMentionProvider<any> {
  return {
    id: AGENT_SESSION_PROVIDER_ID,
    trigger: "@",
    async query({ context, keyword, maxResults }) {
      if (!options.querySessions) {
        return [];
      }
      const workspaceId = String(context?.metadata?.workspaceId ?? "");
      const currentUserId = String(context?.metadata?.currentUserId ?? "");
      const snapshot = await options.querySessions({
        workspaceId,
        sessionOrigin: "WORKSPACE_AGENT_SESSION_ORIGIN_RUNTIME"
      });
      const allSessions = snapshot.sessions ?? [];
      const sessions =
        maxResults === undefined
          ? allSessions
          : allSessions.slice(0, maxResults);
      const userIds = [
        ...new Set(
          sessions
            .map((session: any) => String(session.userId ?? "").trim())
            .filter(Boolean)
        )
      ];
      const profilesResult =
        userIds.length > 0 && options.loadUserProfiles
          ? await options.loadUserProfiles({ userIds })
          : { users: [] };
      const profiles = new Map(
        (profilesResult.users ?? []).map((user: any) => [user.userId, user])
      );
      const items = await Promise.all(
        sessions.map(async (session: any) => {
          const summary = options.loadSessionSummary
            ? await Promise.resolve(
                options.loadSessionSummary({
                  workspaceId,
                  agentSessionId: session.agentSessionId,
                  agentReplyLimit: 1,
                  recentTurnLimit: 1
                })
              ).catch(() => null)
            : null;
          const userId = String(session.userId ?? "");
          const profile = profiles.get(userId) as any;
          const title =
            compactSessionText(session.title) ||
            compactSessionText(summary?.latestUserRequirement) ||
            compactSessionText(summary?.initialUserRequirement) ||
            (await testSessionFallbackTitle({
              loadSessionMessages: options.loadSessionMessages,
              session,
              workspaceId
            })) ||
            session.agentSessionId;
          return {
            agentTargetId: session.agentTargetId,
            agentName: testProviderLabel(session.provider),
            id: session.agentSessionId,
            initiatorAvatarUrl: profile?.avatar ?? "",
            initiatorName: profile?.name || userId,
            provider: session.provider,
            scope:
              userId && userId === currentUserId
                ? "my_sessions"
                : "collab_sessions",
            status: testSessionStatus(summary, session),
            title,
            inputPreview:
              compactSessionText(summary?.latestUserRequirement) ||
              compactSessionText(summary?.initialUserRequirement),
            summaryPreview: compactSessionText(
              summary?.recentAgentReplies?.[0]
            ),
            updatedAtUnixMs:
              session.updatedAtUnixMs ?? session.createdAtUnixMs ?? 0,
            userId,
            workspaceId
          };
        })
      );
      const normalizedKeyword = keyword.trim().toLowerCase();
      return normalizedKeyword
        ? items.filter((item) =>
            [
              item.agentName,
              item.initiatorName,
              item.provider,
              item.title,
              item.inputPreview,
              item.summaryPreview
            ]
              .join("\n")
              .toLowerCase()
              .includes(normalizedKeyword)
          )
        : items;
    },
    getItemKey: (item) => item.id,
    getItemLabel: (item) => item.title,
    getItemSubtitle: (item) => item.inputPreview || item.status,
    toInsertResult: (item) => ({
      kind: "mention",
      mention: {
        entityId: item.id,
        label: item.title,
        scope: {
          ...(item.agentTargetId ? { agentTargetId: item.agentTargetId } : {}),
          scope: item.scope ?? "",
          userId: item.userId,
          workspaceId: item.workspaceId
        },
        presentation: {
          description: item.inputPreview || item.summaryPreview,
          status: item.status,
          subtitle: item.agentName
        }
      }
    })
  };
}

function compactSessionText(value: unknown): string {
  return typeof value === "string" ? value.replace(/\s+/g, " ").trim() : "";
}

function testProviderLabel(provider: string): string {
  if (provider === "codex") {
    return "Codex";
  }
  if (provider === "nexight") {
    return "Nexight";
  }
  if (provider === "claude-code") {
    return "Claude Code";
  }
  return provider;
}

function testSessionStatus(summary: any, session: any): string {
  const raw = String(
    (typeof summary?.executionStatus === "string"
      ? summary.executionStatus
      : summary?.executionStatus?.currentOrFinalStatus) ??
      summary?.currentOrFinalStatus ??
      session.lifecycleStatus ??
      session.status ??
      "idle"
  ).toLowerCase();
  return raw === "running" ? "working" : raw;
}

async function testSessionFallbackTitle({
  loadSessionMessages,
  session,
  workspaceId
}: {
  loadSessionMessages?: (input: any) => Promise<any>;
  session: any;
  workspaceId: string;
}): Promise<string> {
  if (!loadSessionMessages) {
    return "";
  }
  const result = await loadSessionMessages({
    workspaceId,
    agentSessionId: session.agentSessionId,
    ...(session.sessionOrigin ? { sessionOrigin: session.sessionOrigin } : {}),
    afterVersion: 0,
    limit: 20
  }).catch(() => null);
  const messages = result?.messages ?? [];
  const firstUserMessage = messages.find((message: any) => {
    return message.role === "user" || message.kind === "user";
  });
  return compactSessionText(
    firstUserMessage?.payload?.text ?? firstUserMessage?.body ?? ""
  );
}

describe("AgentMentionSearchController", () => {
  afterEach(() => {
    vi.useRealTimers();
    void setAgentGuiI18nTestLocale("en");
    resetAgentMentionSearchBrowseCacheForTests();
  });

  it("localizes browse filter categories using the active locale at emit time", () => {
    // Regression: browse category labels used to be frozen to the default ("en")
    // runtime because they were computed once at module load. They must reflect
    // the active agent GUI locale when a state is emitted.
    setAgentGuiI18nTestLocale("zh-CN");
    const controller = new AgentMentionSearchController({});
    const states: { categories: readonly { id: string; label: string }[] }[] =
      [];
    controller.subscribe((state) => states.push(state));

    const categories = states.at(-1)?.categories ?? [];
    const labelById = new Map(categories.map((c) => [c.id, c.label]));

    expect(categories.map((category) => category.id)).toEqual([
      "session",
      "issue",
      "agent"
    ]);
    expect(labelById.get("session")).toBe("会话");
    expect(labelById.get("issue")).toBe("任务");
    expect(labelById.get("agent")).toBe("智能体");
  });

  it("omits host-disabled mention filters from browse categories", () => {
    const controller = new AgentMentionSearchController({
      hiddenFilterIds: ["issue", " file "]
    });
    const states: AgentMentionSearchState[] = [];
    controller.subscribe((state) => states.push(state));

    expect(states.at(-1)?.categories.map((category) => category.id)).toEqual([
      "session",
      "agent"
    ]);
  });

  it("uses Tasks for the English issue browse category label", () => {
    setAgentGuiI18nTestLocale("en");
    const controller = new AgentMentionSearchController({});
    const states: { categories: readonly { id: string; label: string }[] }[] =
      [];
    controller.subscribe((state) => states.push(state));

    const categories = states.at(-1)?.categories ?? [];
    const labelById = new Map(categories.map((c) => [c.id, c.label]));

    expect(labelById.get("issue")).toBe("Tasks");
  });

  it("prefetches the default session tab for blank queries", async () => {
    const queryFiles = vi.fn().mockResolvedValue({
      workspaceId: "room-1",
      root: "/workspace",
      entries: []
    });
    const queryIssues = vi.fn().mockResolvedValue({
      issues: [],
      totalCount: 0,
      statusCounts: undefined
    });
    const querySessions = vi
      .fn()
      .mockResolvedValue({ presences: [], sessions: [] });
    const controller = new AgentMentionSearchController({
      queryFiles,
      queryIssues,
      querySessions,
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] })
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));

    controller.updateQuery({ workspaceId: "room-1", query: "   " });

    expect(states.at(-1)).toMatchObject({
      status: "loading",
      mode: "browse",
      query: ""
    });
    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        mode: "browse",
        filter: "session"
      })
    );
    expect(querySessions).toHaveBeenCalledWith({
      workspaceId: "room-1",
      sessionOrigin: "WORKSPACE_AGENT_SESSION_ORIGIN_RUNTIME"
    });
    expect(queryFiles).not.toHaveBeenCalled();
    expect(queryIssues).not.toHaveBeenCalled();
  });

  it("groups session mentions by initiator instead of Agent owner", async () => {
    const controller = new AgentMentionSearchController({
      querySessions: vi.fn().mockResolvedValue({
        sessions: [
          {
            agentSessionId: "session-local-claude",
            agentTargetId: "claude-main",
            provider: "claude-code",
            title: "Local Claude build",
            userId: "user-1"
          },
          ...Array.from({ length: 31 }, (_, index) => ({
            agentSessionId: `session-local-${index}`,
            agentTargetId: "codex-main",
            provider: "codex",
            title: `Local build ${index}`,
            userId: "user-1"
          })),
          {
            agentSessionId: "session-shared",
            agentTargetId: "shared-agent:shared-codex",
            provider: "codex",
            title: "Shared build initiated by me",
            userId: "user-1"
          },
          {
            agentSessionId: "session-shared-by-owner",
            agentTargetId: "shared-agent:shared-codex",
            provider: "codex",
            title: "Shared build initiated by owner",
            userId: "user-2"
          },
          {
            agentSessionId: "session-uncatalogued",
            agentTargetId: "legacy-gemini",
            provider: "gemini",
            title: "Legacy build",
            userId: "user-3"
          }
        ]
      })
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));
    controller.setProvenanceCatalog({
      enabledDimensions: ["agent"],
      agentOptions: [
        {
          id: "codex-main",
          label: "Me · Codex",
          parentMemberId: "user-1"
        },
        {
          id: "claude-main",
          label: "Me · Claude Code",
          parentMemberId: "user-1"
        },
        {
          id: "shared-agent:shared-codex",
          label: "Lin · Codex",
          parentMemberId: "user-2"
        }
      ],
      memberOptions: [
        { id: "user-1", label: "Me" },
        {
          id: "user-2",
          label: "Lin",
          iconUrl: "https://cdn.example.com/lin.png"
        }
      ]
    });

    controller.updateQuery({
      workspaceId: "room-1",
      currentUserId: "user-1",
      query: ""
    });

    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        filter: "session",
        groups: [
          {
            id: "member:user-1",
            label: "Me",
            items: expect.arrayContaining([
              expect.objectContaining({ targetId: "session-local-0" }),
              expect.objectContaining({ targetId: "session-local-claude" })
            ]),
            totalCount: 33,
            hasMore: true
          },
          {
            id: "member:user-2",
            label: "Lin",
            items: [
              expect.objectContaining({
                initiatorAvatarUrl: "https://cdn.example.com/lin.png",
                initiatorName: "Lin",
                initiatorUserId: "user-2",
                targetId: "session-shared-by-owner"
              })
            ]
          },
          {
            id: "agent:uncatalogued%3Auser-3",
            label: "user-3",
            items: [
              expect.objectContaining({ targetId: "session-uncatalogued" })
            ]
          }
        ]
      })
    );
    controller.expandGroup("member:user-1");
    expect(
      (
        states.at(-1) as { groups: Array<{ id: string; items: unknown[] }> }
      ).groups.find((group) => group.id === "member:user-1")?.items
    ).toHaveLength(20);
    controller.setProvenanceFilters({
      session: {
        agentTargetIds: ["shared-agent:shared-codex"],
        memberIds: null
      },
      file: null,
      issue: null,
      agent: null,
      app: null
    });
    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        groups: [
          {
            id: "member:user-1",
            items: [expect.objectContaining({ targetId: "session-shared" })]
          },
          {
            id: "member:user-2",
            items: [
              expect.objectContaining({ targetId: "session-shared-by-owner" })
            ]
          }
        ]
      })
    );
    controller.dispose();
  });

  it("loads the app provider when switching to the app tab for blank queries", async () => {
    const queryWorkspaceApps = vi.fn().mockResolvedValue({
      apps: [
        {
          appId: "vibe-design",
          description: "Design prototypes in Tutti.",
          name: "Vibe Design",
          workspaceId: "room-1"
        }
      ]
    });
    const queryAgentTargets = vi.fn().mockResolvedValue({
      targets: [
        {
          name: "Codex",
          provider: "codex",
          targetId: "local:codex",
          workspaceId: "room-1"
        }
      ]
    });
    const queryFiles = vi.fn().mockResolvedValue({
      workspaceId: "room-1",
      root: "/workspace",
      entries: []
    });
    const queryIssues = vi.fn().mockResolvedValue({
      issues: [],
      totalCount: 0,
      statusCounts: undefined
    });
    const querySessions = vi
      .fn()
      .mockResolvedValue({ presences: [], sessions: [] });
    const controller = new AgentMentionSearchController({
      queryFiles,
      queryIssues,
      querySessions,
      queryAgentTargets,
      queryWorkspaceApps,
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] })
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));

    controller.updateQuery({ workspaceId: "room-1", query: "" });
    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        mode: "browse",
        filter: "session"
      })
    );
    controller.setFilter("app");

    expect(states.at(-1)).toMatchObject({
      status: "loading",
      mode: "browse",
      filter: "app"
    });
    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        mode: "browse",
        filter: "app",
        groups: [
          expect.objectContaining({
            id: "apps",
            items: [
              expect.objectContaining({
                kind: "workspace-app",
                appId: "vibe-design"
              })
            ]
          })
        ]
      })
    );
    expect(queryWorkspaceApps).toHaveBeenCalledWith({
      workspaceId: "room-1",
      query: "",
      limit: undefined
    });
    expect(queryFiles).not.toHaveBeenCalled();
    expect(queryIssues).not.toHaveBeenCalled();
    expect(queryAgentTargets).not.toHaveBeenCalled();
    expect(querySessions).toHaveBeenCalledTimes(1);
  });

  it("keeps provenance filters independent across mention tabs", async () => {
    const querySessions = vi.fn().mockResolvedValue([]);
    const queryFiles = vi.fn().mockResolvedValue([]);
    const sessionProvider: AgentContextMentionProvider<{ id: string }> = {
      id: AGENT_SESSION_PROVIDER_ID,
      trigger: "@",
      query: querySessions,
      getItemKey: (item) => item.id,
      getItemLabel: (item) => item.id,
      toInsertResult: (item) => ({
        kind: "markdown-link",
        href: `mention://agent-session/${item.id}`,
        label: item.id
      })
    };
    const fileProvider: AgentContextMentionProvider<{
      href: string;
      label: string;
    }> = {
      id: FILE_PROVIDER_ID,
      trigger: "@",
      query: queryFiles,
      getItemKey: (item) => item.href,
      getItemLabel: (item) => item.label,
      toInsertResult: (item) => ({
        kind: "markdown-link",
        href: item.href,
        label: item.label
      })
    };
    const controller = new AgentMentionSearchController({
      contextMentionProviders: [sessionProvider, fileProvider],
      debounceMs: 0
    });
    controller.setProvenanceFilters({
      session: {
        agentTargetIds: null,
        memberIds: ["zhou-man"]
      },
      file: {
        agentTargetIds: null,
        memberIds: null
      },
      issue: null,
      agent: null,
      app: null
    });
    controller.updateQuery({
      workspaceId: "room-1",
      currentUserId: "user-1",
      query: "report"
    });

    await waitForFast(() => expect(querySessions).toHaveBeenCalledTimes(1));
    expect(querySessions).toHaveBeenLastCalledWith(
      expect.objectContaining({
        context: {
          metadata: expect.objectContaining({
            referenceProvenanceFilter: {
              agentTargetIds: null,
              memberIds: ["zhou-man"]
            }
          })
        }
      })
    );

    controller.setFilter("file");

    await waitForFast(() => expect(queryFiles).toHaveBeenCalledTimes(1));
    expect(queryFiles).toHaveBeenLastCalledWith(
      expect.objectContaining({
        context: {
          metadata: expect.objectContaining({
            referenceProvenanceFilter: {
              agentTargetIds: null,
              memberIds: null
            }
          })
        }
      })
    );

    controller.setFilter("session");

    await waitForFast(() => expect(querySessions).toHaveBeenCalledTimes(2));
    expect(querySessions).toHaveBeenLastCalledWith(
      expect.objectContaining({
        context: {
          metadata: expect.objectContaining({
            referenceProvenanceFilter: {
              agentTargetIds: null,
              memberIds: ["zhou-man"]
            }
          })
        }
      })
    );
    controller.dispose();
  });

  it("loads agent targets from the agent tab and inserts agent-target mentions", async () => {
    const queryAgentTargets = vi.fn().mockResolvedValue({
      targets: [
        {
          description: "Run Codex locally",
          iconUrl: "tutti://agent/codex.svg",
          name: "Codex",
          provider: "codex",
          targetId: "local:codex",
          workspaceId: "room-1"
        },
        {
          description: "Run Claude Code locally",
          iconUrl: "tutti://agent/claude-code.svg",
          name: "Claude Code",
          provider: "claude-code",
          targetId: "local:claude-code",
          workspaceId: "room-1"
        },
        {
          description: "Run a shared Codex agent",
          iconUrl: "tutti://agent/codex.svg",
          name: "Lin's Codex",
          provider: "codex",
          status: "unavailable",
          targetId: "shared-agent:shared-codex",
          workspaceId: "room-1"
        },
        {
          description: "Run an uncatalogued agent",
          name: "Legacy Gemini",
          provider: "gemini",
          targetId: "legacy:gemini",
          workspaceId: "room-1"
        }
      ]
    });
    const queryWorkspaceApps = vi.fn().mockResolvedValue({
      apps: [
        {
          appId: "vibe-design",
          name: "Vibe Design",
          workspaceId: "room-1"
        }
      ]
    });
    const controller = new AgentMentionSearchController({
      queryAgentTargets,
      queryWorkspaceApps,
      queryFiles: vi.fn().mockResolvedValue({
        workspaceId: "room-1",
        root: "/workspace",
        entries: []
      }),
      queryIssues: vi.fn().mockResolvedValue({
        issues: [],
        totalCount: 0,
        statusCounts: undefined
      }),
      querySessions: vi.fn().mockResolvedValue({ presences: [], sessions: [] }),
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] })
    });
    const states: any[] = [];
    controller.subscribe((state) => states.push(state));
    controller.setProvenanceCatalog({
      enabledDimensions: ["agent"],
      agentOptions: [
        {
          id: "shared-agent:shared-codex",
          label: "Lin · Codex",
          parentMemberId: "user-lin"
        },
        {
          id: "local:codex",
          label: "Me · Codex",
          parentMemberId: "user-current"
        },
        {
          id: "local:claude-code",
          label: "Me · Claude Code",
          parentMemberId: "user-current"
        }
      ],
      memberOptions: [
        { id: "user-current", label: "Me" },
        { id: "user-lin", label: "Lin" }
      ]
    });

    controller.setFilter("agent");
    controller.updateQuery({ workspaceId: "room-1", query: "" });

    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        mode: "browse",
        filter: "agent",
        groups: [
          {
            id: "member:user-current",
            label: "Me",
            items: [
              expect.objectContaining({
                kind: "agent-target",
                targetId: "local:codex",
                agentProviderId: "codex"
              }),
              expect.objectContaining({
                kind: "agent-target",
                targetId: "local:claude-code"
              })
            ]
          },
          {
            id: "member:user-lin",
            label: "Lin",
            items: [
              expect.objectContaining({
                kind: "agent-target",
                targetId: "shared-agent:shared-codex",
                href: "mention://agent-target/shared-agent:shared-codex?workspaceId=room-1",
                agentProviderId: "codex",
                availabilityStatus: "unavailable"
              })
            ]
          },
          {
            id: "agent:uncatalogued%3Alegacy%3Agemini",
            label: "Legacy Gemini",
            items: [
              expect.objectContaining({
                kind: "agent-target",
                targetId: "legacy:gemini"
              })
            ]
          }
        ]
      })
    );
    expect(queryAgentTargets).toHaveBeenCalledWith({
      workspaceId: "room-1",
      query: "",
      limit: undefined
    });
    expect(queryWorkspaceApps).not.toHaveBeenCalled();
    controller.setProvenanceFilters({
      session: null,
      file: null,
      issue: null,
      agent: {
        agentTargetIds: ["shared-agent:shared-codex"],
        memberIds: null
      },
      app: null
    });
    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        groups: [
          {
            id: "member:user-lin",
            items: [
              expect.objectContaining({
                targetId: "shared-agent:shared-codex"
              })
            ]
          }
        ]
      })
    );
  });

  it("renders all workspace apps without mention pagination", async () => {
    const apps = Array.from({ length: 12 }, (_, index) => ({
      appId: `app-${index + 1}`,
      name: `App ${index + 1}`,
      description: `Workspace app ${index + 1}`,
      workspaceId: "room-1"
    }));
    const queryWorkspaceApps = vi.fn().mockResolvedValue({ apps });
    const controller = new AgentMentionSearchController({
      queryWorkspaceApps,
      queryFiles: vi.fn().mockResolvedValue({
        workspaceId: "room-1",
        root: "/workspace",
        entries: []
      }),
      queryIssues: vi.fn().mockResolvedValue({
        issues: [],
        totalCount: 0,
        statusCounts: undefined
      }),
      querySessions: vi.fn().mockResolvedValue({ presences: [], sessions: [] }),
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] })
    });
    const states: any[] = [];
    controller.subscribe((state) => states.push(state));

    controller.setFilter("app");
    controller.updateQuery({ workspaceId: "room-1", query: "" });

    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        mode: "browse",
        filter: "app",
        groups: [
          expect.objectContaining({
            id: "apps",
            totalCount: 12,
            visibleCount: 12,
            hasMore: false
          })
        ]
      })
    );
    expect(states.at(-1).groups[0].items).toHaveLength(12);
    expect(queryWorkspaceApps).toHaveBeenCalledWith({
      workspaceId: "room-1",
      query: "",
      limit: undefined
    });
  });

  it("shows fresh cached browse results immediately and revalidates when the mention palette reopens", async () => {
    let now = 1_000;
    let entries = [
      {
        path: "/workspace/README.md",
        name: "README.md",
        kind: "file"
      }
    ];
    const queryFiles = vi.fn(async () => ({
      workspaceId: "room-1",
      root: "/workspace",
      entries
    }));
    const controller = new AgentMentionSearchController({
      queryFiles,
      queryIssues: vi.fn().mockResolvedValue({
        issues: [],
        totalCount: 0,
        statusCounts: undefined
      }),
      querySessions: vi.fn().mockResolvedValue({ presences: [], sessions: [] }),
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] }),
      diagnosticNow: () => now
    });
    const states: any[] = [];
    controller.subscribe((state) => states.push(state));

    controller.updateQuery({ workspaceId: "room-1", query: "" });
    controller.setFilter("file");
    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        mode: "browse",
        groups: expect.arrayContaining([
          expect.objectContaining({
            id: "opened_files",
            items: [
              expect.objectContaining({
                kind: "file",
                path: "/workspace/README.md"
              })
            ]
          })
        ])
      })
    );
    expect(queryFiles).toHaveBeenCalledTimes(1);

    controller.close();
    now += 1_000;
    entries = [
      {
        path: "/workspace/package.json",
        name: "package.json",
        kind: "file"
      }
    ];
    controller.updateQuery({ workspaceId: "room-1", query: "" });
    controller.setFilter("file");

    // Cache is presentation-only: it paints synchronously on reopen.
    expect(states.at(-1)).toMatchObject({
      status: "ready",
      mode: "browse",
      groups: expect.arrayContaining([
        expect.objectContaining({
          id: "opened_files",
          items: [
            expect.objectContaining({
              kind: "file",
              path: "/workspace/README.md"
            })
          ]
        })
      ])
    });
    await waitForFast(() => {
      expect(queryFiles).toHaveBeenCalledTimes(2);
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        mode: "browse",
        groups: expect.arrayContaining([
          expect.objectContaining({
            id: "opened_files",
            items: [
              expect.objectContaining({
                kind: "file",
                path: "/workspace/package.json"
              })
            ]
          })
        ])
      });
    });
  });

  it("revalidates cached Agent availability when the Agent tab reopens", async () => {
    let status = "available";
    const queryAgentTargets = vi.fn(async () => ({
      targets: [
        {
          name: "Claude Code",
          provider: "claude-code",
          status,
          targetId: "local:claude-code",
          workspaceId: "room-1"
        }
      ]
    }));
    const controller = new AgentMentionSearchController({ queryAgentTargets });
    const states: any[] = [];
    controller.subscribe((state) => states.push(state));
    const latestClaudeAvailability = () =>
      states
        .at(-1)
        ?.groups.flatMap((group: { items: any[] }) => group.items)
        .find(
          (item: { targetId?: string }) => item.targetId === "local:claude-code"
        )?.availabilityStatus;

    controller.updateQuery({ workspaceId: "room-1", query: "" });
    controller.setFilter("agent");
    await waitForFast(() => {
      expect(queryAgentTargets).toHaveBeenCalledTimes(1);
      expect(latestClaudeAvailability()).toBe("available");
    });

    controller.close();
    status = "unavailable";
    controller.updateQuery({ workspaceId: "room-1", query: "" });
    controller.setFilter("agent");

    expect(latestClaudeAvailability()).toBe("available");
    await waitForFast(() => {
      expect(queryAgentTargets).toHaveBeenCalledTimes(2);
      expect(latestClaudeAvailability()).toBe("unavailable");
    });
    controller.dispose();
  });

  it("shows fresh cached browse results and revalidates after the controller is recreated", async () => {
    let now = 2_000;
    const queryFiles = vi.fn().mockResolvedValue({
      workspaceId: "room-1",
      root: "/workspace",
      entries: [
        {
          path: "/workspace/package.json",
          name: "package.json",
          kind: "file"
        }
      ]
    });
    const providerOptions: TestContextMentionProviderOptions = {
      queryFiles,
      queryIssues: vi.fn().mockResolvedValue({
        issues: [],
        totalCount: 0,
        statusCounts: undefined
      }),
      querySessions: vi.fn().mockResolvedValue({ presences: [], sessions: [] }),
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] }),
      diagnosticNow: () => now
    };
    const options: TestContextMentionProviderOptions = {
      ...providerOptions,
      contextMentionProviders:
        createTestContextMentionProviders(providerOptions)
    };
    const firstController = new AgentMentionSearchController(options);
    const firstStates: any[] = [];
    firstController.subscribe((state) => firstStates.push(state));

    firstController.setFilter("file");
    firstController.updateQuery({ workspaceId: "room-1", query: "" });
    await waitForFast(() =>
      expect(firstStates.at(-1)).toMatchObject({
        status: "ready",
        mode: "browse",
        groups: expect.arrayContaining([
          expect.objectContaining({
            id: "opened_files",
            items: [
              expect.objectContaining({
                kind: "file",
                path: "/workspace/package.json"
              })
            ]
          })
        ])
      })
    );
    expect(queryFiles).toHaveBeenCalledTimes(1);

    firstController.dispose();
    now += 1_000;

    const secondController = new AgentMentionSearchController(options);
    const secondStates: any[] = [];
    secondController.subscribe((state) => secondStates.push(state));
    secondController.setFilter("file");
    secondController.updateQuery({ workspaceId: "room-1", query: "" });

    expect(secondStates.at(-1)).toMatchObject({
      status: "ready",
      mode: "browse",
      groups: expect.arrayContaining([
        expect.objectContaining({
          id: "opened_files",
          items: [
            expect.objectContaining({
              kind: "file",
              path: "/workspace/package.json"
            })
          ]
        })
      ])
    });
    expect(queryFiles).toHaveBeenCalledTimes(1);
    await waitForFast(() => expect(queryFiles).toHaveBeenCalledTimes(2));
  });

  it("evicts the oldest browse cache entry once the shared cap is exceeded", async () => {
    const now = 5_000;
    const queryFiles = vi.fn().mockResolvedValue({
      workspaceId: "room",
      root: "/workspace",
      entries: [{ path: "/workspace/a.md", name: "a.md", kind: "file" }]
    });
    const providerOptions: TestContextMentionProviderOptions = {
      queryFiles,
      queryIssues: vi.fn().mockResolvedValue({
        issues: [],
        totalCount: 0,
        statusCounts: undefined
      }),
      querySessions: vi.fn().mockResolvedValue({ presences: [], sessions: [] }),
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] }),
      diagnosticNow: () => now
    };
    const controller = new AgentMentionSearchController({
      ...providerOptions,
      contextMentionProviders:
        createTestContextMentionProviders(providerOptions)
    });
    const states: any[] = [];
    controller.subscribe((state) => states.push(state));
    controller.setFilter("file");

    // Warm one more distinct workspace than the cache can hold. Each distinct
    // workspace is a cache miss, so this drives MAX + 1 fetches and evicts the
    // oldest (room-0) on the final insert.
    const warmCount = MAX_BROWSE_CACHE_ENTRIES + 1;
    for (let index = 0; index < warmCount; index += 1) {
      controller.updateQuery({ workspaceId: `room-${index}`, query: "" });
      // eslint-disable-next-line no-await-in-loop
      await waitForFast(() =>
        expect(queryFiles).toHaveBeenCalledTimes(index + 1)
      );
    }

    // The most recently warmed workspace paints from cache synchronously, then
    // still revalidates because this is a user-opened browse.
    controller.updateQuery({
      workspaceId: `room-${warmCount - 1}`,
      query: ""
    });
    expect(states.at(-1)).toMatchObject({ status: "ready", mode: "browse" });
    await waitForFast(() =>
      expect(queryFiles).toHaveBeenCalledTimes(warmCount + 1)
    );

    // The oldest workspace was evicted by the cap, so it has no synchronous
    // cached presentation before its required realtime fetch.
    controller.updateQuery({ workspaceId: "room-0", query: "" });
    expect(states.at(-1)).toMatchObject({ status: "loading", mode: "browse" });
    await waitForFast(() =>
      expect(queryFiles).toHaveBeenCalledTimes(warmCount + 2)
    );

    controller.dispose();
  });

  it("preloadAgentMentionBrowse warms the shared cache for a later controller", async () => {
    const queryFiles = vi.fn().mockResolvedValue({
      workspaceId: "room-1",
      root: "/workspace",
      entries: [
        {
          path: "/workspace/preloaded.md",
          name: "preloaded.md",
          kind: "file"
        }
      ]
    });
    const providerOptions: TestContextMentionProviderOptions = {
      queryFiles,
      queryIssues: vi.fn().mockResolvedValue({
        issues: [],
        totalCount: 0,
        statusCounts: undefined
      }),
      querySessions: vi.fn().mockResolvedValue({ presences: [], sessions: [] }),
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] })
    };

    // Warm the shared cache without a mounted controller (startup-style).
    preloadAgentMentionBrowse({
      workspaceId: "room-1",
      filter: "file",
      contextMentionProviders:
        createTestContextMentionProviders(providerOptions)
    });
    await waitForFast(() => expect(queryFiles).toHaveBeenCalledTimes(1));

    // A later controller built with the same providers paints the warmed cache.
    const controller = new AgentMentionSearchController({
      contextMentionProviders:
        createTestContextMentionProviders(providerOptions)
    });
    const states: any[] = [];
    controller.subscribe((state) => states.push(state));
    controller.setFilter("file");
    controller.updateQuery({ workspaceId: "room-1", query: "" });

    expect(states.at(-1)).toMatchObject({
      status: "ready",
      mode: "browse",
      groups: expect.arrayContaining([
        expect.objectContaining({
          id: "opened_files",
          items: [
            expect.objectContaining({
              kind: "file",
              path: "/workspace/preloaded.md"
            })
          ]
        })
      ])
    });
    expect(queryFiles).toHaveBeenCalledTimes(1);
    await waitForFast(() => expect(queryFiles).toHaveBeenCalledTimes(2));
    controller.dispose();
  });

  it("isolates generated-file browse cache entries by persisted section key", async () => {
    const queryAgentGeneratedFiles = vi.fn().mockResolvedValue({ entries: [] });
    const controller = new AgentMentionSearchController({
      queryAgentGeneratedFiles,
      queryFiles: vi.fn().mockResolvedValue({ entries: [] })
    });
    controller.setFilter("file");

    controller.updateQuery({
      workspaceId: "room-1",
      query: "",
      sectionKey: "project:/workspace/one"
    });
    await waitForFast(() =>
      expect(queryAgentGeneratedFiles).toHaveBeenCalledTimes(1)
    );

    controller.updateQuery({
      workspaceId: "room-1",
      query: "",
      sectionKey: "project:/workspace/two"
    });
    await waitForFast(() =>
      expect(queryAgentGeneratedFiles).toHaveBeenCalledTimes(2)
    );
    expect(queryAgentGeneratedFiles).toHaveBeenNthCalledWith(
      1,
      expect.objectContaining({ sectionKey: "project:/workspace/one" })
    );
    expect(queryAgentGeneratedFiles).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({ sectionKey: "project:/workspace/two" })
    );
    controller.dispose();
  });

  it("uses preloaded browse results when the mention palette first opens", async () => {
    let now = 3_000;
    const queryFiles = vi.fn().mockResolvedValue({
      workspaceId: "room-1",
      root: "/workspace",
      entries: [
        {
          path: "/workspace/preloaded.md",
          name: "preloaded.md",
          kind: "file"
        }
      ]
    });
    const providerOptions: TestContextMentionProviderOptions = {
      queryFiles,
      queryIssues: vi.fn().mockResolvedValue({
        issues: [],
        totalCount: 0,
        statusCounts: undefined
      }),
      querySessions: vi.fn().mockResolvedValue({ presences: [], sessions: [] }),
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] }),
      diagnosticNow: () => now
    };
    const controller = new AgentMentionSearchController({
      ...providerOptions,
      contextMentionProviders:
        createTestContextMentionProviders(providerOptions)
    });
    const states: any[] = [];
    controller.subscribe((state) => states.push(state));

    controller.preloadBrowse({ workspaceId: "room-1", filter: "file" });
    await waitForFast(() => expect(queryFiles).toHaveBeenCalledTimes(1));

    now += 1_000;
    controller.setFilter("file");
    controller.updateQuery({ workspaceId: "room-1", query: "" });

    expect(states.at(-1)).toMatchObject({
      status: "ready",
      mode: "browse",
      groups: expect.arrayContaining([
        expect.objectContaining({
          id: "opened_files",
          items: [
            expect.objectContaining({
              kind: "file",
              path: "/workspace/preloaded.md"
            })
          ]
        })
      ])
    });
    expect(queryFiles).toHaveBeenCalledTimes(1);
    await waitForFast(() => expect(queryFiles).toHaveBeenCalledTimes(2));
  });

  it("reuses preloaded app results when the app category first opens", async () => {
    let now = 4_000;
    const queryWorkspaceApps = vi.fn().mockResolvedValue({
      apps: [
        {
          appId: "app-1",
          name: "Task Manager",
          description: "Manage workspace issues",
          workspaceId: "room-1"
        }
      ]
    });
    const providerOptions: TestContextMentionProviderOptions = {
      queryFiles: vi.fn().mockResolvedValue({
        workspaceId: "room-1",
        root: "/workspace",
        entries: []
      }),
      queryIssues: vi.fn().mockResolvedValue({
        issues: [],
        totalCount: 0,
        statusCounts: undefined
      }),
      queryWorkspaceApps,
      querySessions: vi.fn().mockResolvedValue({ presences: [], sessions: [] }),
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] }),
      diagnosticNow: () => now
    };
    const controller = new AgentMentionSearchController({
      ...providerOptions,
      contextMentionProviders:
        createTestContextMentionProviders(providerOptions)
    });
    const states: any[] = [];
    controller.subscribe((state) => states.push(state));

    controller.preloadBrowse({ workspaceId: "room-1", filter: "app" });
    await waitForFast(() =>
      expect(queryWorkspaceApps).toHaveBeenCalledTimes(1)
    );

    now += 1_000;
    controller.updateQuery({ workspaceId: "room-1", query: "" });
    controller.enterCategory("app");

    expect(states.at(-1)).toMatchObject({
      status: "ready",
      mode: "browse",
      filter: "app",
      groups: [
        expect.objectContaining({
          id: "apps",
          items: [
            expect.objectContaining({
              kind: "workspace-app",
              appId: "app-1",
              name: "Task Manager"
            })
          ]
        })
      ]
    });
    expect(queryWorkspaceApps).toHaveBeenCalledTimes(1);
    await waitForFast(() =>
      expect(queryWorkspaceApps).toHaveBeenCalledTimes(2)
    );
  });

  it("cancels an orphaned browse load and refetches after reopen", async () => {
    const resolveFiles: Array<
      (value: {
        workspaceId: string;
        root: string;
        entries: { path: string; name: string; kind: string }[];
      }) => void
    > = [];
    const queryFiles = vi.fn(
      () =>
        new Promise((resolve) => {
          resolveFiles.push(resolve);
        })
    );
    const controller = new AgentMentionSearchController({
      queryFiles,
      queryIssues: vi.fn().mockResolvedValue({
        issues: [],
        totalCount: 0,
        statusCounts: undefined
      }),
      querySessions: vi.fn().mockResolvedValue({ presences: [], sessions: [] }),
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] })
    });
    const states: any[] = [];
    controller.subscribe((state) => states.push(state));

    controller.setFilter("file");
    controller.updateQuery({ workspaceId: "room-1", query: "" });
    await waitForFast(() => expect(queryFiles).toHaveBeenCalledTimes(1));
    controller.close();
    controller.updateQuery({ workspaceId: "room-1", query: "" });
    controller.setFilter("file");
    await waitForFast(() => expect(queryFiles).toHaveBeenCalledTimes(2));

    resolveFiles[1]?.({
      workspaceId: "room-1",
      root: "/workspace",
      entries: [
        {
          path: "/workspace/src/App.tsx",
          name: "App.tsx",
          kind: "file"
        }
      ]
    });

    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        mode: "browse",
        groups: expect.arrayContaining([
          expect.objectContaining({
            id: "opened_files",
            items: [
              expect.objectContaining({
                kind: "file",
                path: "/workspace/src/App.tsx"
              })
            ]
          })
        ])
      })
    );
    expect(queryFiles).toHaveBeenCalledTimes(2);
  });

  it("cancels an orphaned browse load and refetches after controller recreation", async () => {
    const resolveFiles: Array<
      (value: {
        workspaceId: string;
        root: string;
        entries: { path: string; name: string; kind: string }[];
      }) => void
    > = [];
    const queryFiles = vi.fn(
      () =>
        new Promise((resolve) => {
          resolveFiles.push(resolve);
        })
    );
    const providerOptions: TestContextMentionProviderOptions = {
      queryFiles,
      queryIssues: vi.fn().mockResolvedValue({
        issues: [],
        totalCount: 0,
        statusCounts: undefined
      }),
      querySessions: vi.fn().mockResolvedValue({ presences: [], sessions: [] }),
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] })
    };
    const options: TestContextMentionProviderOptions = {
      ...providerOptions,
      contextMentionProviders:
        createTestContextMentionProviders(providerOptions)
    };
    const firstController = new AgentMentionSearchController(options);
    firstController.setFilter("file");
    firstController.updateQuery({ workspaceId: "room-1", query: "" });
    await waitForFast(() => expect(queryFiles).toHaveBeenCalledTimes(1));
    firstController.dispose();

    const secondController = new AgentMentionSearchController(options);
    const secondStates: any[] = [];
    secondController.subscribe((state) => secondStates.push(state));
    secondController.setFilter("file");
    secondController.updateQuery({ workspaceId: "room-1", query: "" });
    await waitForFast(() => expect(queryFiles).toHaveBeenCalledTimes(2));

    resolveFiles[1]?.({
      workspaceId: "room-1",
      root: "/workspace",
      entries: [
        {
          path: "/workspace/pnpm-lock.yaml",
          name: "pnpm-lock.yaml",
          kind: "file"
        }
      ]
    });

    await waitForFast(() =>
      expect(secondStates.at(-1)).toMatchObject({
        status: "ready",
        mode: "browse",
        groups: expect.arrayContaining([
          expect.objectContaining({
            id: "opened_files",
            items: [
              expect.objectContaining({
                kind: "file",
                path: "/workspace/pnpm-lock.yaml"
              })
            ]
          })
        ])
      })
    );
    expect(queryFiles).toHaveBeenCalledTimes(2);
  });

  it("debounces grouped searches and returns file results", async () => {
    vi.useFakeTimers();
    const queryFiles = vi.fn().mockResolvedValue({
      workspaceId: "room-1",
      root: "/workspace",
      entries: [
        {
          path: "/workspace/src/App.tsx",
          name: "App.tsx",
          kind: "file",
          directoryPath: "/workspace/src",
          score: 10
        }
      ]
    });
    const controller = new AgentMentionSearchController({
      queryFiles,
      queryIssues: vi.fn().mockResolvedValue({
        issues: [],
        totalCount: 0,
        statusCounts: undefined
      }),
      querySessions: vi.fn().mockResolvedValue({ presences: [], sessions: [] }),
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] }),
      debounceMs: 20
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));

    controller.setFilter("file");
    controller.updateQuery({
      workspaceId: " room-1 ",
      currentUserId: "user-1",
      query: " app "
    });
    expect(states.at(-1)).toMatchObject({
      status: "loading",
      query: "app",
      mode: "results"
    });

    await vi.advanceTimersByTimeAsync(20);
    await waitForFast(() => expect(queryFiles).toHaveBeenCalledTimes(1));

    expect(queryFiles).toHaveBeenCalledWith({
      workspaceId: "room-1",
      query: "app",
      limit: 30,
      includeKinds: ["file", "directory"]
    });
    expect(states.at(-1)).toMatchObject({
      status: "ready",
      query: "app",
      mode: "results",
      groups: expect.arrayContaining([
        expect.objectContaining({
          id: "opened_files",
          items: [
            expect.objectContaining({
              kind: "file",
              path: "/workspace/src/App.tsx",
              name: "App.tsx"
            })
          ]
        })
      ])
    });
  });

  it("logs completed mention search diagnostics without the raw query", async () => {
    vi.useFakeTimers();
    const diagnosticLogs: any[] = [];
    const rawQuery = "secret-file";
    const controller = new AgentMentionSearchController({
      queryFiles: vi.fn().mockResolvedValue({
        workspaceId: "room-1",
        root: "/workspace",
        entries: [
          {
            path: "/workspace/src/App.tsx",
            name: "App.tsx",
            kind: "file",
            directoryPath: "/workspace/src",
            score: 10
          }
        ]
      }),
      queryIssues: vi.fn().mockResolvedValue({
        issues: [],
        totalCount: 0,
        statusCounts: undefined
      }),
      querySessions: vi.fn().mockResolvedValue({ presences: [], sessions: [] }),
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] }),
      debounceMs: 20,
      diagnosticInfoLogger: (payload) => diagnosticLogs.push(payload),
      diagnosticSlowThresholdMs: 0
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));

    controller.setFilter("file");
    controller.updateQuery({
      workspaceId: "room-1",
      currentUserId: "user-1",
      query: rawQuery
    });
    await vi.advanceTimersByTimeAsync(20);

    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        query: rawQuery,
        mode: "results"
      })
    );
    expect(diagnosticLogs).toEqual([
      expect.objectContaining({
        debounceMs: 20,
        event: "agent_gui.mention_search",
        mode: "results",
        queryLength: rawQuery.length,
        status: "ready",
        workspaceId: "room-1",
        providerResults: expect.arrayContaining([
          expect.objectContaining({
            providerId: FILE_PROVIDER_ID,
            resultCount: 1,
            status: "success"
          })
        ])
      })
    ]);
    expect(JSON.stringify(diagnosticLogs)).not.toContain(rawQuery);
  });

  it("keeps mention search ready when diagnostic logging fails", async () => {
    vi.useFakeTimers();
    const diagnosticInfoLogger = vi.fn(() => {
      throw new Error("diagnostic sink failed");
    });
    const controller = new AgentMentionSearchController({
      queryFiles: vi.fn().mockResolvedValue({
        workspaceId: "room-1",
        root: "/workspace",
        entries: [
          {
            path: "/workspace/src/App.tsx",
            name: "App.tsx",
            kind: "file",
            directoryPath: "/workspace/src",
            score: 10
          }
        ]
      }),
      queryIssues: vi.fn().mockResolvedValue({
        issues: [],
        totalCount: 0,
        statusCounts: undefined
      }),
      querySessions: vi.fn().mockResolvedValue({ presences: [], sessions: [] }),
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] }),
      debounceMs: 20,
      diagnosticInfoLogger,
      diagnosticSlowThresholdMs: 0
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));

    controller.updateQuery({
      workspaceId: "room-1",
      currentUserId: "user-1",
      query: "app"
    });
    await vi.advanceTimersByTimeAsync(20);

    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        query: "app",
        mode: "results"
      })
    );
    expect(diagnosticInfoLogger).toHaveBeenCalledTimes(1);
  });

  it("times out the selected stalled result provider", async () => {
    vi.useFakeTimers();
    const diagnosticLogs: any[] = [];
    const rawQuery = "secret-token";
    const queryIssues = vi.fn(
      () =>
        new Promise(() => {
          // Simulates a host provider that neither resolves nor rejects.
        })
    );
    const controller = new AgentMentionSearchController({
      queryFiles: vi.fn().mockResolvedValue({
        workspaceId: "room-1",
        root: "/workspace",
        entries: [
          {
            path: "/workspace/src/App.tsx",
            name: "App.tsx",
            kind: "file",
            directoryPath: "/workspace/src",
            score: 10
          }
        ]
      }),
      queryIssues,
      querySessions: vi.fn().mockResolvedValue({ presences: [], sessions: [] }),
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] }),
      debounceMs: 20,
      diagnosticInfoLogger: (payload) => diagnosticLogs.push(payload),
      providerTimeoutMs: 20
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));

    controller.setFilter("issue");
    controller.updateQuery({
      workspaceId: "room-1",
      currentUserId: "user-1",
      query: rawQuery
    });
    await vi.advanceTimersByTimeAsync(40);

    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        query: rawQuery,
        mode: "results",
        filter: "issue",
        groups: [
          expect.objectContaining({
            id: "issues",
            items: []
          })
        ]
      })
    );
    expect(queryIssues).toHaveBeenCalledTimes(1);
    expect(diagnosticLogs).toEqual([
      expect.objectContaining({
        event: "agent_gui.mention_search",
        mode: "results",
        providerTimeoutMs: 20,
        queryLength: rawQuery.length,
        status: "ready",
        providerResults: expect.arrayContaining([
          expect.objectContaining({
            providerId: WORKSPACE_ISSUE_PROVIDER_ID,
            resultCount: 0,
            status: "timeout"
          })
        ])
      })
    ]);
    expect(JSON.stringify(diagnosticLogs)).not.toContain(rawQuery);
  });

  it("times out stalled browse providers and keeps partial results", async () => {
    vi.useFakeTimers();
    const queryIssues = vi.fn(
      () =>
        new Promise(() => {
          // Simulates a host provider that neither resolves nor rejects.
        })
    );
    const controller = new AgentMentionSearchController({
      queryFiles: vi.fn().mockResolvedValue({
        workspaceId: "room-1",
        root: "/workspace",
        entries: [
          {
            path: "/workspace/src/App.tsx",
            name: "App.tsx",
            kind: "file",
            directoryPath: "/workspace/src",
            score: 10
          }
        ]
      }),
      queryAgentGeneratedFiles: vi.fn().mockResolvedValue({
        workspaceId: "room-1",
        root: "/workspace",
        entries: []
      }),
      queryIssues,
      querySessions: vi.fn().mockResolvedValue({ presences: [], sessions: [] }),
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] }),
      providerTimeoutMs: 20
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));

    controller.setFilter("file");
    controller.updateQuery({ workspaceId: "room-1", query: "" });
    expect(states.at(-1)).toMatchObject({
      status: "loading",
      mode: "browse",
      query: ""
    });

    await vi.advanceTimersByTimeAsync(20);

    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        query: "",
        mode: "browse",
        groups: expect.arrayContaining([
          expect.objectContaining({
            id: "opened_files",
            items: [
              expect.objectContaining({
                kind: "file",
                path: "/workspace/src/App.tsx"
              })
            ]
          })
        ])
      })
    );
    expect(queryIssues).not.toHaveBeenCalled();
  });

  it("uses rich text @ providers for workspace files, issues, and sessions when available", async () => {
    vi.useFakeTimers();
    const queryFiles = vi.fn();
    const queryIssues = vi.fn();
    const querySessions = vi.fn();
    const fileProviderQuery = vi.fn().mockResolvedValue([
      {
        label: "App.tsx",
        href: "/Users/test/project/tutti/src/App.tsx"
      }
    ]);
    const issueProviderQuery = vi.fn().mockResolvedValue([
      {
        issueId: "issue-1",
        title: "Fix mention search",
        status: "running"
      }
    ]);
    const sessionProviderQuery = vi.fn().mockResolvedValue([
      {
        agentName: "Codex",
        id: "session-1",
        initiatorName: "local",
        provider: "codex",
        status: "working",
        title: "Fix mention session provider",
        updatedAtUnixMs: 30,
        userId: "local",
        workspaceId: "room-1"
      }
    ]);
    const controller = new AgentMentionSearchController({
      queryFiles,
      queryIssues,
      querySessions,
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] }),
      contextMentionProviders: [
        {
          id: FILE_PROVIDER_ID,
          trigger: "@",
          query: fileProviderQuery,
          getItemKey: (item) => item.href,
          getItemLabel: (item) => item.label,
          toInsertResult: (item) => ({
            kind: "markdown-link",
            label: item.label,
            href: item.href
          })
        },
        {
          id: WORKSPACE_ISSUE_PROVIDER_ID,
          trigger: "@",
          query: issueProviderQuery,
          getItemKey: (item) => item.issueId,
          getItemLabel: (item) => item.title,
          getItemSubtitle: (item) => item.status,
          toInsertResult: (item) => ({
            kind: "mention",
            mention: {
              entityId: item.issueId,
              label: item.title,
              scope: { workspaceId: "room-1" },
              presentation: { status: item.status }
            }
          })
        },
        {
          id: AGENT_SESSION_PROVIDER_ID,
          trigger: "@",
          query: sessionProviderQuery,
          getItemKey: (item) => item.id,
          getItemLabel: (item) => item.title,
          getItemSubtitle: (item) => `${item.provider} · ${item.status}`,
          toInsertResult: (item) => ({
            kind: "mention",
            mention: {
              entityId: item.id,
              label: item.title,
              scope: {
                userId: item.userId,
                workspaceId: item.workspaceId
              },
              presentation: {
                agentIconUrl: "data:image/svg+xml;base64,gemini",
                status: item.status,
                subtitle: item.agentName
              }
            }
          })
        }
      ] satisfies [
        AgentContextMentionProvider<TestFileMentionItem>,
        AgentContextMentionProvider<TestIssueMentionItem>,
        AgentContextMentionProvider<TestSessionMentionItem>
      ],
      debounceMs: 20
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));

    controller.updateQuery({
      workspaceId: "room-1",
      currentUserId: "local",
      query: "mention"
    });
    await vi.advanceTimersByTimeAsync(20);

    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        groups: [
          expect.objectContaining({
            id: "my_sessions",
            items: [
              expect.objectContaining({
                kind: "session",
                agentName: "Codex",
                agentIconUrl: "data:image/svg+xml;base64,gemini",
                initiatorName: "",
                name: "Fix mention session provider",
                scope: "my_sessions",
                targetId: "session-1"
              })
            ]
          })
        ]
      })
    );
    expect(queryFiles).not.toHaveBeenCalled();
    expect(queryIssues).not.toHaveBeenCalled();
    expect(querySessions).not.toHaveBeenCalled();
    expect(fileProviderQuery).not.toHaveBeenCalled();
    expect(issueProviderQuery).not.toHaveBeenCalled();
    expect(sessionProviderQuery).toHaveBeenCalledWith(
      expect.objectContaining({
        keyword: "mention",
        maxResults: 30
      })
    );
  });

  it("builds grouped issue results with parsed previews", async () => {
    vi.useFakeTimers();
    const controller = new AgentMentionSearchController({
      queryFiles: vi.fn().mockResolvedValue({
        workspaceId: "room-1",
        root: "/workspace",
        entries: []
      }),
      queryIssues: vi.fn().mockResolvedValue({
        issues: [
          {
            issueId: "issue-1",
            workspaceId: "room-1",
            title: "修复 room status",
            content: JSON.stringify({
              type: "doc",
              content: [
                {
                  type: "paragraph",
                  content: [
                    { type: "text", text: "补齐 statusBatch 的错误处理" }
                  ]
                }
              ]
            }),
            status: "running",
            creatorUserId: "user-2",
            creatorDisplayName: "Alice",
            issueCount: 0,
            notStartedCount: 0,
            runningCount: 0,
            pendingAcceptanceCount: 0,
            completedCount: 0,
            failedCount: 0,
            canceledCount: 0,
            updatedAtUnix: 20
          }
        ],
        totalCount: 1,
        statusCounts: undefined
      }),
      querySessions: vi.fn().mockResolvedValue({
        presences: [],
        sessions: [
          {
            ...{
              activeTurnId: null,
              latestTurnInteractions: [],
              pendingInteractions: []
            },
            agentSessionId: "session-1",
            workspaceId: "room-1",
            userId: "user-1",
            provider: "codex",
            title: "看看项目有什么文件",
            effectiveStatus: "working",
            createdAtUnixMs: 1,
            updatedAtUnixMs: 10
          },
          {
            agentSessionId: "session-2",
            workspaceId: "room-1",
            userId: "user-2",
            provider: "nexight",
            title: "room status 接口整理",
            lifecycleStatus: "completed",
            createdAtUnixMs: 2,
            updatedAtUnixMs: 9
          }
        ]
      }),
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn().mockImplementation(({ agentSessionId }) =>
        Promise.resolve({
          workspaceId: "room-1",
          agentSessionId,
          executionStatus:
            agentSessionId === "session-1" ? "RUNNING" : "COMPLETED",
          latestUserRequirement:
            agentSessionId === "session-1"
              ? "看看项目有什么文件"
              : "整理 room status",
          recentAgentReplies:
            agentSessionId === "session-1"
              ? ["已读取 workspace 结构"]
              : ["输出了 statusBatch 调用链"],
          initialUserRequirement: "",
          initialTurn: null,
          latestTurn: null,
          recentTurns: []
        })
      ),
      loadUserProfiles: vi.fn().mockResolvedValue({
        users: [
          {
            userId: "user-1",
            name: "Wang",
            avatar: "https://cdn.example.com/wang.png"
          },
          {
            userId: "user-2",
            name: "Alice",
            avatar: "https://cdn.example.com/alice.png"
          }
        ]
      }),
      debounceMs: 20
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));

    controller.setFilter("issue");
    controller.updateQuery({
      workspaceId: "room-1",
      currentUserId: "user-1",
      query: "status"
    });

    await vi.advanceTimersByTimeAsync(20);
    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        filter: "issue",
        groups: [
          expect.objectContaining({
            id: "issues",
            items: [
              expect.objectContaining({
                kind: "workspace-issue",
                targetId: "issue-1",
                contentPreview: "补齐 statusBatch 的错误处理"
              })
            ]
          })
        ]
      })
    );
  });

  it("retains the active filter when the picker closes", () => {
    const controller = new AgentMentionSearchController({
      queryFiles: vi.fn().mockResolvedValue({
        workspaceId: "room-1",
        root: "/workspace",
        entries: []
      }),
      queryIssues: vi.fn(),
      querySessions: vi.fn(),
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn()
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));

    controller.updateQuery({ workspaceId: "room-1", query: "" });
    controller.setFilter("issue");
    controller.close();

    expect(states.at(-1)).toMatchObject({
      status: "idle",
      mode: "browse",
      filter: "issue"
    });
  });

  it("loads dock-backed file items when switching to the file tab without a keyword", async () => {
    const queryFiles = vi.fn().mockResolvedValue({
      workspaceId: "room-1",
      root: "/workspace",
      entries: [
        {
          path: "/workspace/README.md",
          name: "README.md",
          kind: "file"
        }
      ]
    });
    const queryAgentGeneratedFiles = vi.fn().mockResolvedValue({
      entries: [
        {
          path: "/workspace/output/report.md",
          name: "report.md"
        }
      ]
    });
    const controller = new AgentMentionSearchController({
      queryAgentGeneratedFiles,
      queryFiles,
      queryIssues: vi.fn().mockResolvedValue({
        issues: [],
        totalCount: 0,
        statusCounts: undefined
      }),
      querySessions: vi.fn().mockResolvedValue({ presences: [], sessions: [] }),
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] })
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));

    controller.updateQuery({
      workspaceId: "room-1",
      currentUserId: "user-1",
      query: ""
    });
    controller.setFilter("file");

    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        mode: "browse",
        query: "",
        filter: "file",
        groups: [
          {
            id: "opened_files",
            items: [
              expect.objectContaining({
                kind: "file",
                name: "README.md",
                path: "/workspace/README.md"
              })
            ]
          },
          {
            id: "agent_generated_files",
            items: [
              expect.objectContaining({
                kind: "file",
                name: "report.md",
                path: "/workspace/output/report.md"
              })
            ]
          }
        ]
      })
    );
    expect(queryFiles).toHaveBeenCalledWith(
      expect.objectContaining({
        workspaceId: "room-1",
        query: "",
        limit: 30
      })
    );
    expect(queryAgentGeneratedFiles).toHaveBeenCalledWith(
      expect.objectContaining({
        workspaceId: "room-1",
        query: "",
        limit: 30
      })
    );
  });

  it("searches agent-generated files for typed file queries under an active provenance filter", async () => {
    const queryFiles = vi.fn().mockResolvedValue({ entries: [] });
    const queryAgentGeneratedFiles = vi.fn().mockResolvedValue({
      entries: [
        {
          path: "/workspace/output/report.md",
          name: "report.md"
        }
      ]
    });
    const controller = new AgentMentionSearchController({
      debounceMs: 0,
      queryAgentGeneratedFiles,
      queryFiles
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));
    controller.setFilter("file");
    controller.setProvenanceFilters({
      session: null,
      file: {
        agentTargetIds: ["agent-codex"],
        memberIds: null
      },
      issue: null,
      agent: null,
      app: null
    });
    controller.updateQuery({
      workspaceId: "room-1",
      currentUserId: "user-1",
      query: "report"
    });

    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        mode: "results",
        filter: "file",
        groups: [
          {
            id: "agent_generated_files",
            items: [
              expect.objectContaining({
                kind: "file",
                path: "/workspace/output/report.md"
              })
            ]
          }
        ]
      })
    );
    expect(queryFiles).not.toHaveBeenCalled();
    expect(queryAgentGeneratedFiles).toHaveBeenCalledWith(
      expect.objectContaining({
        query: "report",
        provenanceFilter: {
          agentTargetIds: ["agent-codex"],
          memberIds: null
        }
      })
    );
    controller.dispose();
  });

  it("groups agent-generated files by folder and supports folder drill-down", async () => {
    setAgentGuiI18nTestLocale("zh-CN");
    const queryAgentGeneratedFiles = vi.fn().mockResolvedValue({
      entries: [
        {
          path: "/workspace/demo/apps/11.md",
          name: "11.md"
        },
        {
          path: "/workspace/demo/static/app.js",
          name: "app.js"
        },
        {
          path: "/workspace/demo/static/index.html",
          name: "index.html"
        },
        {
          path: "/workspace/demo/static/styles.css",
          name: "styles.css"
        }
      ]
    });
    const controller = new AgentMentionSearchController({
      queryAgentGeneratedFiles,
      queryFiles: vi.fn().mockResolvedValue({
        workspaceId: "room-1",
        root: "/workspace",
        entries: []
      }),
      queryIssues: vi.fn().mockResolvedValue({
        issues: [],
        totalCount: 0,
        statusCounts: undefined
      }),
      querySessions: vi.fn().mockResolvedValue({ presences: [], sessions: [] }),
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] })
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));

    controller.updateQuery({
      workspaceId: "room-1",
      currentUserId: "user-1",
      query: ""
    });
    controller.setFilter("file");

    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        groups: [
          expect.objectContaining({ id: "opened_files" }),
          {
            id: "agent_generated_files",
            items: [
              expect.objectContaining({
                kind: "file",
                name: "11.md",
                path: "/workspace/demo/apps/11.md"
              }),
              expect.objectContaining({
                kind: "file",
                name: "static",
                path: "/workspace/demo/static",
                entryKind: "directory",
                mentionNavigation: "agent-generated-folder",
                childCount: 3
              })
            ],
            totalCount: 2,
            visibleCount: 2,
            hasMore: false
          }
        ]
      })
    );

    const latestState = states.at(-1) as {
      groups: Array<{
        id: string;
        items: Array<{ mentionNavigation?: string; path: string }>;
      }>;
    };
    const folderItem = latestState.groups
      .find((group) => group.id === "agent_generated_files")
      ?.items.find(
        (item) => item.mentionNavigation === "agent-generated-folder"
      );
    expect(folderItem).toBeDefined();
    controller.selectFileMentionNavigationItem(
      folderItem as Parameters<
        BaseAgentMentionSearchController["selectFileMentionNavigationItem"]
      >[0]
    );

    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        groups: [
          expect.objectContaining({ id: "opened_files" }),
          {
            id: "agent_generated_files",
            items: [
              expect.objectContaining({
                mentionNavigation: "agent-generated-folder-back",
                name: "返回"
              }),
              expect.objectContaining({
                kind: "file",
                name: "app.js",
                path: "/workspace/demo/static/app.js"
              }),
              expect.objectContaining({
                kind: "file",
                name: "index.html",
                path: "/workspace/demo/static/index.html"
              }),
              expect.objectContaining({
                kind: "file",
                name: "styles.css",
                path: "/workspace/demo/static/styles.css"
              })
            ]
          }
        ]
      })
    );
    setAgentGuiI18nTestLocale("en");
  });

  it("loads nested workspace folders with child counts and returns one level at a time", async () => {
    setAgentGuiI18nTestLocale("zh-CN");
    const queryFiles = vi.fn(
      async ({ directoryPath }: { directoryPath?: string }) => {
        if (directoryPath === "/workspace/src/components") {
          return {
            entries: [
              {
                path: "/workspace/src/components/Button.tsx",
                name: "Button.tsx",
                kind: "file"
              }
            ]
          };
        }
        if (directoryPath === "/workspace/src") {
          return {
            entries: [
              {
                path: "/workspace/src/components",
                name: "components",
                kind: "directory",
                childCount: 1
              },
              {
                path: "/workspace/src/index.ts",
                name: "index.ts",
                kind: "file"
              }
            ]
          };
        }
        return {
          entries: [
            {
              path: "/workspace/src",
              name: "src",
              kind: "directory",
              childCount: 2
            }
          ]
        };
      }
    );
    const controller = new AgentMentionSearchController({ queryFiles });
    const states: AgentMentionSearchState[] = [];
    controller.subscribe((state) => states.push(state));

    controller.updateQuery({ workspaceId: "room-1", query: "" });
    controller.setFilter("file");

    await waitForFast(() =>
      expect(states.at(-1)?.groups).toEqual([
        expect.objectContaining({
          id: "opened_files",
          items: [
            expect.objectContaining({
              childCount: 2,
              entryKind: "directory",
              mentionNavigation: "workspace-folder",
              path: "/workspace/src"
            })
          ]
        }),
        expect.objectContaining({ id: "agent_generated_files" })
      ])
    );

    const rootFolder = states
      .at(-1)
      ?.groups.find((group) => group.id === "opened_files")
      ?.items.at(0);
    expect(rootFolder).toBeDefined();
    expect(controller.selectFileMentionNavigationItem(rootFolder!)).toBe(true);

    await waitForFast(() =>
      expect(states.at(-1)?.groups[0]).toMatchObject({
        id: "opened_files",
        items: [
          expect.objectContaining({
            mentionNavigation: "workspace-folder-back",
            name: "返回"
          }),
          expect.objectContaining({
            childCount: 1,
            mentionNavigation: "workspace-folder",
            path: "/workspace/src/components"
          }),
          expect.objectContaining({ path: "/workspace/src/index.ts" })
        ]
      })
    );

    const nestedFolder = states
      .at(-1)
      ?.groups.find((group) => group.id === "opened_files")
      ?.items.find(
        (item) =>
          item.kind === "file" && item.path === "/workspace/src/components"
      );
    expect(nestedFolder).toBeDefined();
    expect(controller.selectFileMentionNavigationItem(nestedFolder!)).toBe(
      true
    );

    await waitForFast(() =>
      expect(states.at(-1)?.groups[0]?.items).toEqual([
        expect.objectContaining({
          mentionNavigation: "workspace-folder-back",
          path: "/workspace/src/components"
        }),
        expect.objectContaining({
          path: "/workspace/src/components/Button.tsx"
        })
      ])
    );

    expect(controller.exitFileMentionBrowse()).toBe(true);
    await waitForFast(() =>
      expect(states.at(-1)?.groups[0]?.items).toEqual([
        expect.objectContaining({
          mentionNavigation: "workspace-folder-back",
          path: "/workspace/src"
        }),
        expect.objectContaining({ path: "/workspace/src/components" }),
        expect.objectContaining({ path: "/workspace/src/index.ts" })
      ])
    );
    expect(queryFiles).toHaveBeenCalledWith(
      expect.objectContaining({ directoryPath: "/workspace/src/components" })
    );
    expect(queryFiles).toHaveBeenCalledWith(
      expect.objectContaining({ directoryPath: "/workspace/src" })
    );

    controller.dispose();
    setAgentGuiI18nTestLocale("en");
  });

  it("keeps an entered workspace folder when the cleared editor query repeats", async () => {
    const queryFiles = vi.fn(
      async ({ directoryPath }: { directoryPath?: string }) => ({
        entries: directoryPath
          ? [
              {
                path: `${directoryPath}/Button.tsx`,
                name: "Button.tsx",
                kind: "file"
              }
            ]
          : [
              {
                path: "/workspace/src",
                name: "src",
                kind: "directory",
                childCount: 1
              }
            ]
      })
    );
    const controller = new AgentMentionSearchController({
      queryFiles,
      debounceMs: 0
    });
    const states: AgentMentionSearchState[] = [];
    controller.subscribe((state) => states.push(state));

    controller.setFilter("file");
    controller.updateQuery({ workspaceId: "room-1", query: "src" });

    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        mode: "results",
        groups: [
          expect.objectContaining({
            items: [
              expect.objectContaining({
                mentionNavigation: "workspace-folder",
                path: "/workspace/src"
              })
            ]
          })
        ]
      })
    );
    const folder = states.at(-1)?.groups[0]?.items[0];
    expect(folder).toBeDefined();
    controller.updateQuery({ workspaceId: "room-1", query: "" });
    expect(controller.selectFileMentionNavigationItem(folder!)).toBe(true);
    controller.updateQuery({ workspaceId: "room-1", query: "" });

    await waitForFast(() => {
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        mode: "browse",
        filter: "file"
      });
      expect(
        states.at(-1)?.groups.find((group) => group.id === "opened_files")
          ?.items
      ).toEqual([
        expect.objectContaining({
          mentionNavigation: "workspace-folder-back"
        }),
        expect.objectContaining({
          path: "/workspace/src/Button.tsx"
        })
      ]);
    });
    expect(queryFiles).toHaveBeenCalledWith(
      expect.objectContaining({ directoryPath: "/workspace/src" })
    );

    controller.setProvenanceFilters({
      session: null,
      file: { agentTargetIds: ["agent-1"], memberIds: null },
      issue: null,
      agent: null,
      app: null
    });
    expect(controller.exitFileMentionBrowse()).toBe(false);

    controller.dispose();
  });

  it("keeps an entered agent-generated folder when the cleared editor query repeats", async () => {
    const queryAgentGeneratedFiles = vi.fn().mockResolvedValue({
      entries: [
        {
          path: "/workspace/demo/static/app.js",
          name: "app.js"
        },
        {
          path: "/workspace/demo/static/index.html",
          name: "index.html"
        },
        {
          path: "/workspace/demo/static/styles.css",
          name: "styles.css"
        }
      ]
    });
    const controller = new AgentMentionSearchController({
      queryAgentGeneratedFiles,
      queryFiles: vi.fn().mockResolvedValue({
        workspaceId: "room-1",
        root: "/workspace",
        entries: []
      }),
      queryIssues: vi.fn().mockResolvedValue({
        issues: [],
        totalCount: 0,
        statusCounts: undefined
      }),
      querySessions: vi.fn().mockResolvedValue({ presences: [], sessions: [] }),
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] })
    });
    const states: AgentMentionSearchState[] = [];
    controller.subscribe((state) => states.push(state));

    controller.updateQuery({ workspaceId: "room-1", query: "" });
    controller.setFilter("file");

    await waitForFast(() => {
      const folder = states
        .at(-1)
        ?.groups.find((group) => group.id === "agent_generated_files")
        ?.items.find(
          (item) =>
            item.kind === "file" &&
            item.mentionNavigation === "agent-generated-folder"
        );
      expect(folder).toEqual(
        expect.objectContaining({
          path: "/workspace/demo/static",
          childCount: 3
        })
      );
    });

    const folder = states
      .at(-1)
      ?.groups.find((group) => group.id === "agent_generated_files")
      ?.items.find(
        (item) =>
          item.kind === "file" &&
          item.mentionNavigation === "agent-generated-folder"
      );
    expect(folder).toBeDefined();
    expect(controller.selectFileMentionNavigationItem(folder!)).toBe(true);

    // The editor emits this duplicate empty query after the click. It must
    // not reset the agent-generated browse path back to the root folder list.
    controller.updateQuery({ workspaceId: "room-1", query: "" });

    await waitForFast(() => {
      expect(states.at(-1)?.groups).toEqual([
        expect.objectContaining({
          id: "opened_files"
        }),
        expect.objectContaining({
          id: "agent_generated_files",
          items: [
            expect.objectContaining({
              mentionNavigation: "agent-generated-folder-back"
            }),
            expect.objectContaining({ path: "/workspace/demo/static/app.js" }),
            expect.objectContaining({
              path: "/workspace/demo/static/index.html"
            }),
            expect.objectContaining({
              path: "/workspace/demo/static/styles.css"
            })
          ]
        })
      ]);
    });

    controller.dispose();
  });

  it("keeps a directory request alive beyond the provider search timeout", async () => {
    let resolveDirectory:
      | ((value: {
          entries: Array<{ path: string; name: string; kind: string }>;
        }) => void)
      | undefined;
    const queryFiles = vi.fn(
      ({ directoryPath }: { directoryPath?: string }) => {
        if (directoryPath === "/workspace/src") {
          return new Promise<{
            entries: Array<{ path: string; name: string; kind: string }>;
          }>((resolve) => {
            resolveDirectory = resolve;
          });
        }
        return Promise.resolve({
          entries: [
            {
              path: "/workspace/src",
              name: "src",
              kind: "directory",
              childCount: 1
            }
          ]
        });
      }
    );
    const controller = new AgentMentionSearchController({
      providerTimeoutMs: 20,
      queryFiles
    });
    const states: AgentMentionSearchState[] = [];
    controller.subscribe((state) => states.push(state));

    controller.setFilter("file");
    controller.updateQuery({ workspaceId: "room-1", query: "" });
    await waitForFast(() =>
      expect(states.at(-1)?.groups[0]?.items[0]).toMatchObject({
        mentionNavigation: "workspace-folder",
        path: "/workspace/src"
      })
    );

    const folder = states.at(-1)?.groups[0]?.items[0];
    expect(folder).toBeDefined();
    expect(controller.selectFileMentionNavigationItem(folder!)).toBe(true);

    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(states.at(-1)).toMatchObject({
      status: "loading",
      error: null
    });

    resolveDirectory?.({
      entries: [
        {
          path: "/workspace/src/index.ts",
          name: "index.ts",
          kind: "file"
        }
      ]
    });
    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        groups: expect.arrayContaining([
          expect.objectContaining({
            id: "opened_files",
            items: [
              expect.objectContaining({
                mentionNavigation: "workspace-folder-back"
              }),
              expect.objectContaining({
                path: "/workspace/src/index.ts"
              })
            ]
          })
        ])
      })
    );
  });

  it("cancels an in-flight directory request when the search scope changes", async () => {
    let resolveDirectory:
      | ((value: readonly TestFileMentionItem[]) => void)
      | undefined;
    let directoryAbortSignal: AbortSignal | undefined;
    const fileProvider = createTestFileProvider({
      queryFiles: vi.fn().mockResolvedValue({
        entries: [
          {
            path: "/workspace/src",
            name: "src",
            kind: "directory",
            childCount: 1
          }
        ]
      })
    });
    fileProvider.queryDirectory = (input) => {
      directoryAbortSignal = input.abortSignal;
      return new Promise<readonly TestFileMentionItem[]>((resolve) => {
        resolveDirectory = resolve;
      });
    };
    const controller = new AgentMentionSearchController({
      contextMentionProviders: [fileProvider]
    });
    const states: AgentMentionSearchState[] = [];
    controller.subscribe((state) => states.push(state));

    controller.setFilter("file");
    controller.updateQuery({ workspaceId: "room-1", query: "" });
    await waitForFast(() =>
      expect(states.at(-1)?.groups[0]?.items[0]).toMatchObject({
        mentionNavigation: "workspace-folder",
        path: "/workspace/src"
      })
    );

    const folder = states.at(-1)?.groups[0]?.items[0];
    expect(folder).toBeDefined();
    expect(controller.selectFileMentionNavigationItem(folder!)).toBe(true);
    expect(directoryAbortSignal?.aborted).toBe(false);

    controller.updateQuery({ workspaceId: "room-2", query: "" });
    expect(directoryAbortSignal?.aborted).toBe(true);
    resolveDirectory?.([
      {
        href: "/workspace/src/stale.ts",
        label: "stale.ts",
        kind: "file"
      }
    ]);

    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        groups: expect.arrayContaining([
          expect.objectContaining({
            id: "opened_files",
            items: [
              expect.objectContaining({
                mentionNavigation: "workspace-folder",
                path: "/workspace/src"
              })
            ]
          })
        ])
      })
    );
    expect(JSON.stringify(states.at(-1))).not.toContain("stale.ts");
  });

  it("surfaces directory provider failures instead of presenting an empty directory", async () => {
    const controller = new AgentMentionSearchController({
      queryFiles: vi.fn(({ directoryPath }: { directoryPath?: string }) => {
        if (directoryPath === "/workspace/src") {
          return Promise.reject(new Error("directory read failed"));
        }
        return Promise.resolve({
          entries: [
            {
              path: "/workspace/src",
              name: "src",
              kind: "directory",
              childCount: 1
            }
          ]
        });
      })
    });
    const states: AgentMentionSearchState[] = [];
    controller.subscribe((state) => states.push(state));

    controller.setFilter("file");
    controller.updateQuery({ workspaceId: "room-1", query: "" });
    await waitForFast(() =>
      expect(states.at(-1)?.groups[0]?.items[0]).toMatchObject({
        mentionNavigation: "workspace-folder",
        path: "/workspace/src"
      })
    );

    const folder = states.at(-1)?.groups[0]?.items[0];
    expect(folder).toBeDefined();
    expect(controller.selectFileMentionNavigationItem(folder!)).toBe(true);
    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "error",
        error: "directory read failed"
      })
    );
  });

  it("fails closed when a file provider does not implement directory queries", async () => {
    const query = vi.fn().mockResolvedValue([
      {
        childCount: 1,
        href: "/workspace/src",
        kind: "directory",
        label: "src"
      }
    ]);
    const fileProvider: AgentContextMentionProvider<TestFileMentionItem> = {
      id: FILE_PROVIDER_ID,
      trigger: "@",
      query,
      getItemDirectory: (item) => ({
        childCount: item.childCount,
        path: item.href
      }),
      getItemKey: (item) => item.href,
      getItemLabel: (item) => item.label,
      toInsertResult: (item) => ({
        kind: "markdown-link",
        label: item.label,
        href: item.href
      })
    };
    const controller = new AgentMentionSearchController({
      contextMentionProviders: [fileProvider]
    });
    const states: AgentMentionSearchState[] = [];
    controller.subscribe((state) => states.push(state));

    controller.setFilter("file");
    controller.updateQuery({ workspaceId: "room-1", query: "" });
    await waitForFast(() =>
      expect(states.at(-1)?.groups[0]?.items[0]).toMatchObject({
        mentionNavigation: "workspace-folder",
        path: "/workspace/src"
      })
    );

    const folder = states.at(-1)?.groups[0]?.items[0];
    expect(folder).toBeDefined();
    expect(controller.selectFileMentionNavigationItem(folder!)).toBe(true);
    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "error",
        error: "Mention provider does not support directory browsing."
      })
    );
    expect(query).toHaveBeenCalledTimes(1);
  });

  it("keeps workspace and agent-generated folder navigation independent", async () => {
    setAgentGuiI18nTestLocale("zh-CN");
    const queryFiles = vi.fn(
      async ({ directoryPath }: { directoryPath?: string }) => ({
        entries:
          directoryPath === "/workspace/src"
            ? [
                {
                  path: "/workspace/src/index.ts",
                  name: "index.ts",
                  kind: "file"
                }
              ]
            : [
                {
                  path: "/workspace/src",
                  name: "src",
                  kind: "directory",
                  childCount: 1
                }
              ]
      })
    );
    const controller = new AgentMentionSearchController({
      queryFiles,
      queryAgentGeneratedFiles: vi.fn().mockResolvedValue({
        entries: [
          {
            path: "/workspace/generated/app.js",
            name: "app.js"
          },
          {
            path: "/workspace/generated/index.html",
            name: "index.html"
          }
        ]
      })
    });
    const states: AgentMentionSearchState[] = [];
    controller.subscribe((state) => states.push(state));

    controller.updateQuery({ workspaceId: "room-1", query: "" });
    controller.setFilter("file");

    await waitForFast(() =>
      expect(states.at(-1)?.groups).toEqual([
        expect.objectContaining({
          id: "opened_files",
          items: [
            expect.objectContaining({
              mentionNavigation: "workspace-folder",
              path: "/workspace/src"
            })
          ]
        }),
        expect.objectContaining({
          id: "agent_generated_files",
          items: [
            expect.objectContaining({
              mentionNavigation: "agent-generated-folder",
              path: "/workspace/generated"
            })
          ]
        })
      ])
    );

    const workspaceFolder = states
      .at(-1)
      ?.groups.find((group) => group.id === "opened_files")
      ?.items.find(
        (item) =>
          item.kind === "file" && item.mentionNavigation === "workspace-folder"
      );
    expect(workspaceFolder).toBeDefined();
    expect(controller.selectFileMentionNavigationItem(workspaceFolder!)).toBe(
      true
    );

    await waitForFast(() =>
      expect(
        states
          .at(-1)
          ?.groups.find((group) => group.id === "opened_files")
          ?.items.at(0)
      ).toMatchObject({ mentionNavigation: "workspace-folder-back" })
    );

    const generatedFolder = states
      .at(-1)
      ?.groups.find((group) => group.id === "agent_generated_files")
      ?.items.find(
        (item) =>
          item.kind === "file" &&
          item.mentionNavigation === "agent-generated-folder"
      );
    expect(generatedFolder).toBeDefined();
    expect(controller.selectFileMentionNavigationItem(generatedFolder!)).toBe(
      true
    );

    const bothNestedState = states.at(-1);
    const workspaceBack = bothNestedState?.groups
      .find((group) => group.id === "opened_files")
      ?.items.find(
        (item) =>
          item.kind === "file" &&
          item.mentionNavigation === "workspace-folder-back"
      );
    const generatedBack = bothNestedState?.groups
      .find((group) => group.id === "agent_generated_files")
      ?.items.find(
        (item) =>
          item.kind === "file" &&
          item.mentionNavigation === "agent-generated-folder-back"
      );
    expect(workspaceBack).toBeDefined();
    expect(generatedBack).toBeDefined();

    expect(controller.selectFileMentionNavigationItem(workspaceBack!)).toBe(
      true
    );
    await waitForFast(() =>
      expect(
        states
          .at(-1)
          ?.groups.find((group) => group.id === "opened_files")
          ?.items.some(
            (item) =>
              item.kind === "file" &&
              item.mentionNavigation === "workspace-folder-back"
          )
      ).toBe(false)
    );
    expect(
      states
        .at(-1)
        ?.groups.find((group) => group.id === "agent_generated_files")
        ?.items.at(0)
    ).toMatchObject({ mentionNavigation: "agent-generated-folder-back" });

    expect(controller.selectFileMentionNavigationItem(generatedBack!)).toBe(
      true
    );
    controller.dispose();
    setAgentGuiI18nTestLocale("en");
  });

  it("loads default session items in browse mode when switching to the session tab", async () => {
    const querySessions = vi.fn().mockResolvedValue({
      presences: [],
      sessions: [
        {
          ...{
            activeTurnId: null,
            latestTurnInteractions: [],
            pendingInteractions: []
          },
          agentSessionId: "session-1",
          workspaceId: "room-1",
          userId: "user-1",
          provider: "codex",
          title: "@README.md 这是什么内容",
          createdAtUnixMs: 1,
          updatedAtUnixMs: 10
        }
      ]
    });
    const loadSessionSummary = vi.fn().mockResolvedValue({
      workspaceId: "room-1",
      agentSessionId: "session-1",
      executionStatus: "RUNNING",
      latestUserRequirement: "看看项目有什么文件",
      recentAgentReplies: ["已读取 workspace 结构"],
      initialUserRequirement: "",
      initialTurn: null,
      latestTurn: null,
      recentTurns: []
    });
    const loadSessionMessages = vi
      .fn()
      .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false });
    const controller = new AgentMentionSearchController({
      queryFiles: vi.fn().mockResolvedValue({
        workspaceId: "room-1",
        root: "/workspace",
        entries: []
      }),
      queryIssues: vi.fn().mockResolvedValue({
        issues: [],
        totalCount: 0,
        statusCounts: undefined
      }),
      querySessions,
      loadSessionMessages,
      loadSessionSummary,
      loadUserProfiles: vi.fn().mockResolvedValue({
        users: [{ userId: "user-1", name: "Wang" }]
      })
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));

    controller.updateQuery({
      workspaceId: "room-1",
      currentUserId: "user-1",
      query: ""
    });
    controller.setFilter("session");

    expect(states.at(-1)).toMatchObject({
      status: "loading",
      mode: "browse",
      filter: "session",
      query: ""
    });

    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        mode: "browse",
        filter: "session",
        groups: [
          {
            id: "my_sessions",
            items: [
              expect.objectContaining({
                kind: "session",
                targetId: "session-1",
                agentName: "Codex",
                name: "@README.md 这是什么内容",
                title: "README.md 这是什么内容"
              })
            ]
          }
        ]
      })
    );
    expect(querySessions).toHaveBeenLastCalledWith({
      workspaceId: "room-1",
      sessionOrigin: "WORKSPACE_AGENT_SESSION_ORIGIN_RUNTIME"
    });
    expect(loadSessionSummary).toHaveBeenCalledWith({
      workspaceId: "room-1",
      agentSessionId: "session-1",
      agentReplyLimit: 1,
      recentTurnLimit: 1
    });
    expect(loadSessionMessages).not.toHaveBeenCalled();
  });

  it("queries runtime-origin sessions in browse mode when switching to the session tab", async () => {
    const querySessions = vi.fn().mockImplementation(async (input) => {
      if (
        typeof input !== "string" &&
        input.sessionOrigin === "WORKSPACE_AGENT_SESSION_ORIGIN_RUNTIME"
      ) {
        return {
          presences: [],
          sessions: [
            {
              ...{
                activeTurnId: null,
                latestTurnInteractions: [],
                pendingInteractions: []
              },
              agentSessionId: "runtime-session-1",
              workspaceId: "room-1",
              userId: "user-1",
              provider: "codex",
              sessionOrigin: "WORKSPACE_AGENT_SESSION_ORIGIN_RUNTIME",
              title: "分析open code 架构设计",
              createdAtUnixMs: 1,
              updatedAtUnixMs: 10
            }
          ]
        };
      }
      return { presences: [], sessions: [] };
    });
    const controller = new AgentMentionSearchController({
      queryFiles: vi.fn().mockResolvedValue({
        workspaceId: "room-1",
        root: "/workspace",
        entries: []
      }),
      queryIssues: vi.fn().mockResolvedValue({
        issues: [],
        totalCount: 0,
        statusCounts: undefined
      }),
      querySessions,
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn().mockResolvedValue({
        workspaceId: "room-1",
        agentSessionId: "runtime-session-1",
        executionStatus: "RUNNING",
        latestUserRequirement: "分析open code 架构设计",
        recentAgentReplies: [],
        initialUserRequirement: "",
        initialTurn: null,
        latestTurn: null,
        recentTurns: []
      }),
      loadUserProfiles: vi.fn().mockResolvedValue({
        users: [{ userId: "user-1", name: "Wang" }]
      })
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));

    controller.updateQuery({
      workspaceId: "room-1",
      currentUserId: "user-1",
      query: ""
    });
    controller.setFilter("session");

    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        mode: "browse",
        filter: "session",
        groups: [
          {
            id: "my_sessions",
            items: [
              expect.objectContaining({
                kind: "session",
                targetId: "runtime-session-1",
                title: "分析open code 架构设计"
              })
            ]
          }
        ]
      })
    );
    expect(querySessions).toHaveBeenLastCalledWith({
      workspaceId: "room-1",
      sessionOrigin: "WORKSPACE_AGENT_SESSION_ORIGIN_RUNTIME"
    });
  });

  it("uses the canonical session title without loading message fallback", async () => {
    const loadSessionMessages = vi.fn().mockResolvedValue({
      messages: [
        {
          id: 1,
          messageId: "message-user-1",
          agentSessionId: "runtime-session-1",
          role: "user",
          kind: "text",
          payload: { text: "这个会话里面做了什么事情" },
          version: 1,
          occurredAtUnixMs: 10
        }
      ],
      latestVersion: 1,
      hasMore: false
    });
    const controller = new AgentMentionSearchController({
      queryFiles: vi.fn().mockResolvedValue({
        workspaceId: "room-1",
        root: "/workspace",
        entries: []
      }),
      queryIssues: vi.fn().mockResolvedValue({
        issues: [],
        totalCount: 0,
        statusCounts: undefined
      }),
      querySessions: vi.fn().mockResolvedValue({
        presences: [],
        sessions: [
          {
            ...{
              activeTurnId: null,
              latestTurnInteractions: [],
              pendingInteractions: []
            },
            agentSessionId: "runtime-session-1",
            workspaceId: "room-1",
            userId: "user-1",
            provider: "codex",
            sessionOrigin: "WORKSPACE_AGENT_SESSION_ORIGIN_RUNTIME",
            title: "Canonical title",
            createdAtUnixMs: 1,
            updatedAtUnixMs: 10
          }
        ]
      }),
      loadSessionMessages,
      loadSessionSummary: vi.fn().mockResolvedValue({
        workspaceId: "room-1",
        agentSessionId: "runtime-session-1",
        executionStatus: "RUNNING",
        latestUserRequirement: "",
        recentAgentReplies: [],
        initialUserRequirement: "",
        initialTurn: null,
        latestTurn: null,
        recentTurns: []
      }),
      loadUserProfiles: vi.fn().mockResolvedValue({
        users: [{ userId: "user-1", name: "Wang" }]
      })
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));

    controller.updateQuery({
      workspaceId: "room-1",
      currentUserId: "user-1",
      query: ""
    });
    controller.setFilter("session");

    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        mode: "browse",
        filter: "session",
        groups: [
          {
            id: "my_sessions",
            items: [
              expect.objectContaining({
                kind: "session",
                targetId: "runtime-session-1",
                title: "Canonical title"
              })
            ]
          }
        ]
      })
    );
    expect(loadSessionMessages).not.toHaveBeenCalled();
  });

  it("loads default issue items in browse mode when switching to the issue tab", async () => {
    const queryIssues = vi.fn().mockResolvedValue({
      issues: [
        {
          issueId: "issue-1",
          workspaceId: "room-1",
          title: "修复 room status",
          content: JSON.stringify({
            type: "doc",
            content: [
              {
                type: "paragraph",
                content: [{ type: "text", text: "补齐 statusBatch 的错误处理" }]
              }
            ]
          }),
          status: "running",
          creatorUserId: "user-2",
          creatorDisplayName: "Alice",
          issueCount: 0,
          notStartedCount: 0,
          runningCount: 0,
          pendingAcceptanceCount: 0,
          completedCount: 0,
          failedCount: 0,
          canceledCount: 0,
          updatedAtUnix: 20
        }
      ],
      totalCount: 1,
      statusCounts: undefined
    });
    const controller = new AgentMentionSearchController({
      queryFiles: vi.fn().mockResolvedValue({
        workspaceId: "room-1",
        root: "/workspace",
        entries: []
      }),
      queryIssues,
      querySessions: vi.fn().mockResolvedValue({ presences: [], sessions: [] }),
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] })
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));

    controller.updateQuery({
      workspaceId: "room-1",
      currentUserId: "user-1",
      query: ""
    });
    controller.setFilter("issue");

    expect(states.at(-1)).toMatchObject({
      status: "loading",
      mode: "browse",
      filter: "issue",
      query: ""
    });

    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        mode: "browse",
        filter: "issue",
        groups: [
          {
            id: "issues",
            items: [
              expect.objectContaining({
                kind: "workspace-issue",
                targetId: "issue-1",
                contentPreview: "补齐 statusBatch 的错误处理"
              })
            ]
          }
        ]
      })
    );
    expect(queryIssues).toHaveBeenLastCalledWith({
      workspaceId: "room-1",
      pageSize: 25,
      searchQuery: ""
    });
  });

  it("loads grouped session browse without querying other tabs", async () => {
    const queryIssues = vi.fn().mockResolvedValue({
      issues: [
        {
          issueId: "issue-1",
          workspaceId: "room-1",
          title: "修复 room status",
          content: JSON.stringify({
            type: "doc",
            content: [
              {
                type: "paragraph",
                content: [{ type: "text", text: "补齐 statusBatch 的错误处理" }]
              }
            ]
          }),
          status: "running",
          creatorUserId: "user-2",
          creatorDisplayName: "Alice",
          issueCount: 0,
          notStartedCount: 0,
          runningCount: 0,
          pendingAcceptanceCount: 0,
          completedCount: 0,
          failedCount: 0,
          canceledCount: 0,
          updatedAtUnix: 20
        }
      ],
      totalCount: 1,
      statusCounts: undefined
    });
    const queryFiles = vi.fn().mockResolvedValue({
      workspaceId: "room-1",
      root: "/workspace",
      entries: []
    });
    const querySessions = vi.fn().mockResolvedValue({
      presences: [],
      sessions: [
        {
          ...{
            activeTurnId: null,
            latestTurnInteractions: [],
            pendingInteractions: []
          },
          agentSessionId: "session-1",
          workspaceId: "room-1",
          userId: "user-1",
          provider: "codex",
          title: "看看项目有什么文件",
          createdAtUnixMs: 1,
          updatedAtUnixMs: 10
        },
        {
          agentSessionId: "session-2",
          workspaceId: "room-1",
          userId: "user-2",
          provider: "nexight",
          title: "room status 接口整理",
          createdAtUnixMs: 2,
          updatedAtUnixMs: 9
        }
      ]
    });
    const controller = new AgentMentionSearchController({
      queryFiles,
      queryIssues,
      querySessions,
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn().mockImplementation(({ agentSessionId }) =>
        Promise.resolve({
          workspaceId: "room-1",
          agentSessionId,
          executionStatus:
            agentSessionId === "session-1" ? "RUNNING" : "COMPLETED",
          latestUserRequirement:
            agentSessionId === "session-1"
              ? "看看项目有什么文件"
              : "整理 room status",
          recentAgentReplies:
            agentSessionId === "session-1"
              ? ["已读取 workspace 结构"]
              : ["输出了 statusBatch 调用链"],
          initialUserRequirement: "",
          initialTurn: null,
          latestTurn: null,
          recentTurns: []
        })
      ),
      loadUserProfiles: vi.fn().mockResolvedValue({
        users: [
          { userId: "user-1", name: "Wang" },
          { userId: "user-2", name: "Alice" }
        ]
      })
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));

    controller.updateQuery({
      workspaceId: "room-1",
      currentUserId: "user-1",
      query: ""
    });

    expect(states.at(-1)).toMatchObject({
      status: "loading",
      mode: "browse",
      filter: "session",
      query: ""
    });

    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        mode: "browse",
        filter: "session",
        groups: [
          expect.objectContaining({
            id: "my_sessions",
            items: [
              expect.objectContaining({
                targetId: "session-1",
                kind: "session"
              })
            ]
          })
        ]
      })
    );

    expect(querySessions).toHaveBeenCalledWith({
      workspaceId: "room-1",
      sessionOrigin: "WORKSPACE_AGENT_SESSION_ORIGIN_RUNTIME"
    });
    expect(queryFiles).not.toHaveBeenCalled();
    expect(queryIssues).not.toHaveBeenCalled();
    expect(
      (
        states.at(-1) as {
          groups: Array<{ id: string }>;
        }
      ).groups.map((group) => group.id)
    ).toEqual(["my_sessions"]);
  });

  it("keeps the default session empty state scoped to sessions", async () => {
    const controller = new AgentMentionSearchController({
      queryFiles: vi.fn().mockResolvedValue({
        workspaceId: "room-1",
        root: "/workspace",
        entries: []
      }),
      queryIssues: vi.fn().mockResolvedValue({
        issues: [],
        totalCount: 0,
        statusCounts: undefined
      }),
      querySessions: vi.fn().mockResolvedValue({ presences: [], sessions: [] }),
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] })
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));

    controller.updateQuery({
      workspaceId: "room-1",
      currentUserId: "user-1",
      query: ""
    });

    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        mode: "browse",
        filter: "session",
        groups: [
          { id: "my_sessions", items: [], emptyLabel: "No sessions yet" }
        ]
      })
    );
  });

  it("omits empty file subgroups while searching on the file tab when another subgroup has matches", async () => {
    vi.useFakeTimers();
    const controller = new AgentMentionSearchController({
      queryFiles: vi.fn().mockResolvedValue({
        workspaceId: "room-1",
        root: "/workspace",
        entries: [
          {
            path: "/workspace/quickPhrases.ts",
            name: "quickPhrases.ts",
            kind: "file"
          }
        ]
      }),
      queryIssues: vi.fn().mockResolvedValue({
        issues: [],
        totalCount: 0,
        statusCounts: undefined
      }),
      querySessions: vi.fn().mockResolvedValue({ presences: [], sessions: [] }),
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] }),
      debounceMs: 20
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));

    controller.updateQuery({
      workspaceId: "room-1",
      currentUserId: "user-1",
      query: ""
    });
    controller.setFilter("file");

    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        mode: "browse",
        filter: "file"
      })
    );

    controller.updateQuery({
      workspaceId: "room-1",
      currentUserId: "user-1",
      query: "quick"
    });

    await vi.advanceTimersByTimeAsync(20);
    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        mode: "results",
        filter: "file",
        groups: [
          {
            id: "opened_files",
            items: [
              expect.objectContaining({
                kind: "file",
                name: "quickPhrases.ts",
                path: "/workspace/quickPhrases.ts"
              })
            ]
          }
        ]
      })
    );
    expect(
      (states.at(-1) as { groups: Array<{ id: string }> }).groups.some(
        (group) => group.id === "agent_generated_files"
      )
    ).toBe(false);
    vi.useRealTimers();
  });

  it("searches only the default session filter for typed queries", async () => {
    vi.useFakeTimers();
    const queryFiles = vi.fn().mockResolvedValue([
      {
        label: "automation.md",
        href: "/workspace/docs/automation.md"
      }
    ]);
    const queryApps = vi.fn().mockResolvedValue([
      {
        appId: "automation",
        name: "Automation"
      }
    ]);
    const querySessions = vi.fn().mockResolvedValue([
      {
        agentName: "Codex",
        id: "session-automation",
        initiatorName: "local",
        provider: "codex",
        status: "completed",
        title: "Automation cleanup",
        updatedAtUnixMs: 30,
        userId: "local",
        workspaceId: "room-1"
      }
    ]);
    const controller = new AgentMentionSearchController({
      contextMentionProviders: [
        {
          id: FILE_PROVIDER_ID,
          trigger: "@",
          query: queryFiles,
          getItemKey: (item) => item.href,
          getItemLabel: (item) => item.label,
          toInsertResult: (item) => ({
            kind: "markdown-link",
            label: item.label,
            href: item.href
          })
        },
        {
          id: WORKSPACE_APP_PROVIDER_ID,
          trigger: "@",
          query: queryApps,
          getItemKey: (item) => item.appId,
          getItemLabel: (item) => item.name,
          toInsertResult: (item) => ({
            kind: "mention",
            mention: {
              entityId: item.appId,
              label: item.name,
              scope: { workspaceId: "room-1" }
            }
          })
        },
        {
          id: AGENT_SESSION_PROVIDER_ID,
          trigger: "@",
          query: querySessions,
          getItemKey: (item) => item.id,
          getItemLabel: (item) => item.title,
          toInsertResult: (item) => ({
            kind: "mention",
            mention: {
              entityId: item.id,
              label: item.title,
              scope: {
                scope: "my_sessions",
                userId: item.userId,
                workspaceId: item.workspaceId
              },
              presentation: {
                subtitle: item.agentName
              }
            }
          })
        }
      ],
      debounceMs: 20
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));

    controller.updateQuery({
      workspaceId: "room-1",
      currentUserId: "local",
      query: "automation"
    });
    await vi.advanceTimersByTimeAsync(20);

    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        mode: "results",
        filter: "session",
        groups: [
          expect.objectContaining({
            id: "my_sessions",
            items: [
              expect.objectContaining({
                kind: "session",
                targetId: "session-automation"
              })
            ]
          })
        ]
      })
    );
    expect(querySessions).toHaveBeenCalledTimes(1);
    expect(queryFiles).not.toHaveBeenCalled();
    expect(queryApps).not.toHaveBeenCalled();
    vi.useRealTimers();
  });

  it("searches the app provider with the existing query after switching to the app tab", async () => {
    vi.useFakeTimers();
    const queryFiles = vi.fn().mockResolvedValue({
      workspaceId: "room-1",
      root: "/workspace",
      entries: []
    });
    const queryIssues = vi.fn().mockResolvedValue({
      issues: [],
      totalCount: 0,
      statusCounts: undefined
    });
    const queryWorkspaceApps = vi.fn().mockResolvedValue({
      apps: [
        {
          appId: "vibe-design",
          description: "Design prototypes in Tutti.",
          name: "Vibe Design",
          workspaceId: "room-1"
        }
      ]
    });
    const querySessions = vi
      .fn()
      .mockResolvedValue({ presences: [], sessions: [] });
    const controller = new AgentMentionSearchController({
      queryFiles,
      queryIssues,
      querySessions,
      queryWorkspaceApps,
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] }),
      debounceMs: 20
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));

    controller.updateQuery({
      workspaceId: "room-1",
      currentUserId: "local",
      query: "design"
    });
    await vi.advanceTimersByTimeAsync(20);
    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        mode: "results",
        filter: "session"
      })
    );
    controller.setFilter("app");

    expect(states.at(-1)).toMatchObject({
      status: "loading",
      mode: "results",
      filter: "app",
      query: "design"
    });
    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        mode: "results",
        filter: "app",
        query: "design",
        groups: [
          expect.objectContaining({
            id: "apps",
            items: [
              expect.objectContaining({
                kind: "workspace-app",
                appId: "vibe-design"
              })
            ]
          })
        ]
      })
    );
    expect(queryWorkspaceApps).toHaveBeenCalledWith({
      workspaceId: "room-1",
      query: "design",
      limit: undefined
    });
    expect(queryFiles).not.toHaveBeenCalled();
    expect(queryIssues).not.toHaveBeenCalled();
    expect(querySessions).toHaveBeenCalledTimes(1);
    vi.useRealTimers();
  });

  it("keeps file query misses scoped to the file tab", async () => {
    vi.useFakeTimers();
    const queryFiles = vi.fn().mockResolvedValue({
      workspaceId: "room-1",
      root: "/workspace",
      entries: []
    });
    const controller = new AgentMentionSearchController({
      queryFiles,
      queryIssues: vi.fn().mockResolvedValue({
        issues: [],
        totalCount: 0,
        statusCounts: undefined
      }),
      querySessions: vi.fn().mockResolvedValue({ presences: [], sessions: [] }),
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] }),
      debounceMs: 20
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));

    controller.setFilter("file");
    controller.updateQuery({
      workspaceId: "room-1",
      currentUserId: "user-1",
      query: "readme"
    });

    await vi.advanceTimersByTimeAsync(20);
    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        mode: "results",
        filter: "file",
        groups: []
      })
    );
    expect(queryFiles).toHaveBeenCalledWith(
      expect.objectContaining({
        workspaceId: "room-1",
        query: "readme",
        limit: 30
      })
    );
  });

  it("expands the issue group without changing the selected tab", async () => {
    const controller = new AgentMentionSearchController({
      queryFiles: vi.fn().mockResolvedValue({
        workspaceId: "room-1",
        root: "/workspace",
        entries: []
      }),
      queryIssues: vi.fn().mockResolvedValue({
        issues: Array.from({ length: 14 }, (_, index) => ({
          issueId: `issue-${index + 1}`,
          workspaceId: "room-1",
          title: `Issue ${index + 1}`,
          content: "",
          status: "running",
          creatorUserId: "user-2",
          creatorDisplayName: "Alice",
          issueCount: 0,
          notStartedCount: 0,
          runningCount: 0,
          pendingAcceptanceCount: 0,
          completedCount: 0,
          failedCount: 0,
          canceledCount: 0,
          updatedAtUnix: 20 + index
        })),
        totalCount: 14,
        statusCounts: undefined
      }),
      querySessions: vi.fn().mockResolvedValue({ presences: [], sessions: [] }),
      loadSessionMessages: vi
        .fn()
        .mockResolvedValue({ messages: [], latestVersion: 0, hasMore: false }),
      loadSessionSummary: vi.fn(),
      loadUserProfiles: vi.fn().mockResolvedValue({ users: [] })
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));

    controller.updateQuery({
      workspaceId: "room-1",
      currentUserId: "user-1",
      query: ""
    });
    controller.setFilter("issue");

    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        mode: "browse",
        filter: "issue",
        groups: [
          {
            id: "issues",
            visibleCount: 10,
            hasMore: true
          }
        ]
      })
    );

    controller.expandGroup("issues");

    expect(states.at(-1)).toMatchObject({
      status: "ready",
      mode: "browse",
      filter: "issue",
      groups: [
        {
          id: "issues",
          visibleCount: 14,
          hasMore: false
        }
      ]
    });
  });

  it("falls back to the first user message when session title and summary are empty", async () => {
    const loadSessionMessages = vi.fn().mockResolvedValue({
      messages: [
        {
          id: 1,
          messageId: "message-user-1",
          agentSessionId: "session-1",
          role: "user",
          kind: "text",
          payload: { text: "hi" },
          version: 1,
          occurredAtUnixMs: 10
        }
      ],
      latestVersion: 1,
      hasMore: false
    });
    const controller = new AgentMentionSearchController({
      queryFiles: vi.fn().mockResolvedValue({
        workspaceId: "room-1",
        root: "/workspace",
        entries: []
      }),
      queryIssues: vi.fn().mockResolvedValue({
        issues: [],
        totalCount: 0,
        statusCounts: undefined
      }),
      querySessions: vi.fn().mockResolvedValue({
        presences: [],
        sessions: [
          {
            ...{
              activeTurnId: null,
              latestTurnInteractions: [],
              pendingInteractions: []
            },
            agentSessionId: "session-1",
            workspaceId: "room-1",
            userId: "user-1",
            provider: "codex",
            title: "",
            createdAtUnixMs: 1,
            updatedAtUnixMs: 10
          }
        ]
      }),
      loadSessionMessages,
      loadSessionSummary: vi.fn().mockResolvedValue({
        workspaceId: "room-1",
        agentSessionId: "session-1",
        executionStatus: "COMPLETED",
        latestUserRequirement: "",
        recentAgentReplies: [],
        initialUserRequirement: "",
        initialTurn: null,
        latestTurn: null,
        recentTurns: []
      }),
      loadUserProfiles: vi.fn().mockResolvedValue({
        users: [{ userId: "user-1", name: "Wang" }]
      })
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));

    controller.updateQuery({
      workspaceId: "room-1",
      currentUserId: "user-1",
      query: ""
    });
    controller.setFilter("session");

    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        groups: [
          {
            id: "my_sessions",
            items: [
              expect.objectContaining({
                kind: "session",
                targetId: "session-1",
                agentName: "Codex",
                name: "hi"
              })
            ]
          }
        ]
      })
    );
    expect(loadSessionMessages).toHaveBeenCalledWith({
      workspaceId: "room-1",

      agentSessionId: "session-1",
      afterVersion: 0,
      limit: 20
    });
  });

  it("falls back to the first user message when the session title is missing", async () => {
    const loadSessionMessages = vi.fn().mockResolvedValue({
      messages: [
        {
          id: 1,
          messageId: "message-user-1",
          agentSessionId: "session-1",
          role: "user",
          kind: "text",
          payload: { text: "如何做excel的数据清理" },
          version: 1,
          occurredAtUnixMs: 10
        }
      ],
      latestVersion: 1,
      hasMore: false
    });
    const controller = new AgentMentionSearchController({
      queryFiles: vi.fn().mockResolvedValue({
        workspaceId: "room-1",
        root: "/workspace",
        entries: []
      }),
      queryIssues: vi.fn().mockResolvedValue({
        issues: [],
        totalCount: 0,
        statusCounts: undefined
      }),
      querySessions: vi.fn().mockResolvedValue({
        presences: [],
        sessions: [
          {
            ...{
              activeTurnId: null,
              latestTurnInteractions: [],
              pendingInteractions: []
            },
            agentSessionId: "session-1",
            workspaceId: "room-1",
            userId: "user-1",
            provider: "codex",
            sessionOrigin: "WORKSPACE_AGENT_SESSION_ORIGIN_RUNTIME",
            title: "",
            createdAtUnixMs: 1,
            updatedAtUnixMs: 10
          }
        ]
      }),
      loadSessionMessages,
      loadSessionSummary: vi.fn().mockResolvedValue({
        workspaceId: "room-1",
        agentSessionId: "session-1",
        executionStatus: "COMPLETED",
        latestUserRequirement: "",
        recentAgentReplies: [],
        initialUserRequirement: "",
        initialTurn: null,
        latestTurn: null,
        recentTurns: []
      }),
      loadUserProfiles: vi.fn().mockResolvedValue({
        users: [{ userId: "user-1", name: "Wang" }]
      })
    });
    const states: unknown[] = [];
    controller.subscribe((state) => states.push(state));

    controller.updateQuery({
      workspaceId: "room-1",
      currentUserId: "user-1",
      query: ""
    });
    controller.setFilter("session");

    await waitForFast(() =>
      expect(states.at(-1)).toMatchObject({
        status: "ready",
        query: "",
        groups: [
          {
            id: "my_sessions",
            items: [
              expect.objectContaining({
                kind: "session",
                targetId: "session-1",
                agentName: "Codex",
                name: "如何做excel的数据清理",
                title: "如何做excel的数据清理"
              })
            ]
          }
        ]
      })
    );
    expect(loadSessionMessages).toHaveBeenCalledWith({
      workspaceId: "room-1",

      agentSessionId: "session-1",
      sessionOrigin: "WORKSPACE_AGENT_SESSION_ORIGIN_RUNTIME",
      afterVersion: 0,
      limit: 20
    });
  });
});
