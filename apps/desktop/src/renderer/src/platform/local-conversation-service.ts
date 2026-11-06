import {
  createSubmissionStorage,
  createSubmissionAssets,
} from "./local-submission-storage";
import {
  SUBMISSION_LIFECYCLE_CAPABILITY,
  localSubmissionStatusesSchema,
} from "@multica/core/local-conversation/submission";
import { persistedQueuedPromptSchema } from "@multica/core/local-conversation/submission-storage";
import { type ConversationService } from "@multica/conversation-client/services";
import { GLOBAL_EXECUTION_PERMISSION_KEY } from "@multica/conversation-client/models";
import { api } from "@multica/core/api";
import { parseWithFallback } from "@multica/core/api/schema";
import {
  isTaskCreationReference,
  type LocalFolderProject,
  type LocalPromptQueue,
  type LocalProviderId,
  type LocalProviderUsage,
} from "@multica/core/local-conversation/protocol";
import {
  LOCAL_V2_PROMPT_STATUS_CAPABILITY,
  localV2PromptStatusesSchema,
  localV2SummarySchema,
  localV2QueueSchema,
  localSubmissionResultSchema,
  type LocalV2Scope,
  type LocalV2Summary,
  type LocalV2Transport,
} from "@multica/core/local-conversation/v2";
import { defaultStorage } from "@multica/core/platform";
import type { AgentTask,Attachment } from "@multica/core/types";
import {
AGENT_MENTION_SELECTION_KEY,
AGENT_MENTION_USAGE_KEY,
parseAgentMentionUsage
} from "@multica/tutti-agent-gui-host/agent-mention-usage";
import { isAutomaticLocalDirectory } from "@multica/tutti-agent-gui-host/local-projects";
import type {
AgentGuiHostEvent,
AgentGuiHostServices,
AgentGuiSession,
AgentGuiSessionDetail,
AgentGuiWorkspaceState,
LocalConversationSource
} from "@multica/tutti-agent-gui-host/protocol";
import { reaction } from "mobx";
import { z } from "zod";
import {
createAgentGuiWorkspaceService,
type AgentGuiParentService,
type ParentServiceOptions
} from "./agent-gui-parent-service";
import { conversationService } from "./conversation-services";
import { projectForDirectory } from "./local-conversation-projects";
import { blobBase64 } from "./local-preview-file";
import { createLocalV2Uploads } from "./local-v2-uploads";

/** Query keys of a device's conversations; deviceId is "local" or another device's peer ID. */
export const localConversationKey = (scope: LocalV2Scope, deviceId = "local") =>
  ["local-conversations", deviceId, scope.userId, scope.workspaceId] as const;
export const localTargetId = (provider: string) => `local:${provider}`;
export function localProviderId(target: string): LocalProviderId {
  if (target === "local:codex") return "codex";
  if (target === "local:claude") return "claude";
  throw new Error("Select a local Codex or Claude Code provider");
}

/** Translate only at the GUI boundary; these records never enter cloud APIs. */
export function projectLocalSession(
  summary: LocalV2Summary,
  scope: LocalV2Scope
): AgentGuiSession {
  return {
    id: summary.conversation_id,
    workspace_id: scope.workspaceId,
    creator_id: scope.userId,
    agent_id: localTargetId(summary.provider),
    title: summary.title,
    status: summary.archived ? "archived" : "active",
    has_unread: false,
    pinned: summary.pinned,
    project_id: summary.project_id,
    last_message: summary.last_message_preview
      ? {
          content: summary.last_message_preview,
          role: "assistant",
          created_at: summary.updated_at
        }
      : null,
    localDirectory: {
      cwd: summary.cwd,
      projectPath: isAutomaticLocalDirectory(summary.cwd) ? null : summary.cwd
    },
    created_at: summary.created_at ?? summary.updated_at,
    updated_at: summary.updated_at
  };
}

/** The daemon's queue in the shape the GUI's desktop queue engine keeps. */
function projectQueue(raw: unknown): LocalPromptQueue {
  const queue = parseWithFallback<z.infer<typeof localV2QueueSchema>>(
    raw,
    localV2QueueSchema,
    { revision: 0, paused: false, items: [] },
    { endpoint: "local-v2.queue" },
  );
  return {
    revision: queue.revision ?? 0,
    paused: queue.paused ?? false,
    ...(queue.error ? { error: queue.error } : {}),
    ...(queue.dispatching_request_id
      ? { dispatchingRequestId: queue.dispatching_request_id }
      : {}),
    items: queue.items.flatMap((item) => {
      const object = z.record(z.string(), z.unknown()).safeParse(item);
      const display = z
        .record(z.string(), z.unknown())
        .safeParse(object.success ? object.data.display : undefined);
      const compatible = object.success
        ? {
            ...object.data,
            display: {
              id: object.data.requestId,
              createdAtUnixMs: 0,
              content: [
                {
                  type: "text",
                  text:
                    typeof object.data.content === "string"
                      ? object.data.content
                      : "",
                },
              ],
              ...(display.success ? display.data : {}),
            },
          }
        : item;
      const parsed = parseWithFallback<z.infer<
        typeof persistedQueuedPromptSchema
      > | null>(compatible, persistedQueuedPromptSchema, null, {
        endpoint: "local-v2.queue-item",
      });
      return parsed ? [parsed] : [];
    }),
  };
}

export type LocalProviderList = {
  providers: { id: LocalProviderId; name: string; available: boolean }[];
};
export type LocalFolderProjectList = {
  projects: { id: string; title: string; path: string; created_at: string }[];
};

/** One query per device for what the page and the GUI both show, so they read one answer. */
export function localProvidersQuery(
  store: ConversationService,
  deviceId: string
) {
  return {
    queryKey: [...localConversationKey(store.scope, deviceId), "providers"],
    queryFn: () =>
      store.request("GET", "/providers") as Promise<LocalProviderList>
  };
}
export function localFolderProjectsQuery(
  store: ConversationService,
  deviceId: string
) {
  return {
    queryKey: [
      ...localConversationKey(store.scope, deviceId),
      "folder-projects"
    ],
    queryFn: () =>
      store.request(
        "GET",
        "/folder-projects"
      ) as Promise<LocalFolderProjectList>
  };
}

/** Survives a tab's page being rebuilt, so @agent does not start empty each time. */
const lastAgents = new WeakMap<
  ConversationService,
  AgentGuiWorkspaceState["agents"]
>();

export function localConversationSource(
  store: ConversationService,
  scope: LocalV2Scope
): LocalConversationSource {
  return {
    submissionStorage: createSubmissionStorage(
      JSON.stringify([
        api.getBaseUrl(),
        scope.userId,
        scope.workspaceId,
        store.deviceId,
      ]),
    ),
    completions: store.completions,
    submissionContext: () => store.submissionContext(),
    async inspectSubmissions(id, requestIds) {
      if (!(await store.submissionContext()).lifecycle) return null;
      const raw = await store.request(
        "POST",
        `/sessions/${encodeURIComponent(id)}/queue/status`,
        { request_ids: requestIds },
      );
      const parsed = parseWithFallback<z.infer<
        typeof localSubmissionStatusesSchema
      > | null>(raw, localSubmissionStatusesSchema, null, {
        endpoint: "local-v2.submission-status",
      });
      if (!parsed) throw new Error("Invalid submission status response");
      return parsed;
    },
    navigation: (id) => store.navigation(id),
    open: (id) => store.open(id),
    ensure: (id) => store.ensure(id),
    refresh: (id) => store.refresh(id),
    loadOlder: (id) => store.loadOlder(id),
    observe: (id, listener) => store.observe(id, listener),
    conversation: (id) => store.conversation(id),
    detail(id) {
      const conversation = store.conversation(id);
      if (!conversation) return null;
      const settings = conversation.settings;
      // A fork's copied turns started before it was created; the last one is where it branched, once loaded.
      const branched = [...conversation.turns.values()]
        .filter(
          (turn) =>
            Date.parse(turn.startedAt) <= Date.parse(conversation.createdAt)
        )
        .at(-1);
      const forked =
        conversation.forkedFromId && conversation.forkedFromTurnId
          ? {
              forkedFrom: {
                sessionId: conversation.forkedFromId,
                turnId: conversation.forkedFromTurnId,
                targetTurnId: branched?.id ?? "",
                operationId: id,
                createdAt: conversation.createdAt
              }
            }
          : {};
      return {
        session: projectLocalSession(
          store.list.summary(id) ?? conversation.summary,
          scope
        ),
        model: (settings.model ?? null) as string | null,
        reasoningEffort: (settings.reasoning_effort ??
          null) as AgentGuiSessionDetail["reasoningEffort"],
        permissionMode:
          settings.permission_mode as AgentGuiSessionDetail["permissionMode"],
        promptQueue: {
          ...projectQueue({
            ...conversation.queue.toQueue(),
            items: [
              ...conversation.queue.toQueue().items,
              ...[...conversation.submissions.values()]
                .filter(
                  (value) =>
                    value.state === "recovery_required" &&
                    !conversation.queue
                      .toQueue()
                      .items.some(
                        (item) => item.requestId === value.request_id,
                      ),
                )
                .map((value) => value.item),
            ],
          }),
          submissions: [...conversation.submissions.values()],
          storeGeneration: conversation.storeGeneration,
          snapshotSeq: conversation.seq,
        },
        messages: [],
        pending: {},
        tasks: [],
        ...forked
      };
    }
  };
}

export function createLocalConversationParentService(
  options: ParentServiceOptions & {
    onLocalSelect(id: string | null, provider?: LocalProviderId): void;
    getSelectionIntent?: () => string;
    getSelectedSessionId?: () => string | null;
    onOpenSide?: (id: string, sourceId: string) => void;
    onOpenResource?: (sessionId: string, url: string) => void;
    onLocateSource?: (
      sessionId: string,
      turnId?: string,
      quote?: string
    ) => void;
    storagePrefix?: string;
    locale?: string;
  },
  transport: LocalV2Transport
): AgentGuiParentService {
  const storage = options.storage ?? defaultStorage;
  const usageStorageKey = `multica:local-agent-mentions:v1:${JSON.stringify([api.getBaseUrl(), options.userId, options.workspaceId])}`;
  const scope: LocalV2Scope = {
    userId: options.userId,
    workspaceId: options.workspaceId
  };
  const client: ConversationService = conversationService(transport, scope);
  const deviceId = (transport as { deviceId?: string }).deviceId ?? "local";
  const providersQuery = localProvidersQuery(client, deviceId);
  const foldersQuery = localFolderProjectsQuery(client, deviceId);
  const cloud = createAgentGuiWorkspaceService(options);
  const listeners = new Set<(event: AgentGuiHostEvent) => void>();
  let disposed = false;
  let cloudState: AgentGuiWorkspaceState | undefined;
  let cloudDirty = true;
  let cloudLoad: Promise<AgentGuiWorkspaceState> | undefined;
  let selectionSequence = 0;
  let workspaceTimer: ReturnType<typeof setTimeout> | undefined;
  const assertCurrent = () => {
    if (disposed || !options.isCurrent())
      throw new Error("This workspace is no longer active");
  };
  const publish = (event: AgentGuiHostEvent) => {
    if (!disposed && options.isCurrent())
      listeners.forEach((listener) => listener(event));
  };
  const remoteWorkspace = async () => {
    if (!cloudState || cloudDirty) {
      cloudLoad ??= cloud
        .load()
        .then((value) => {
          const agentsChanged =
            JSON.stringify(value.agents) !==
            JSON.stringify(lastAgents.get(client) ?? []);
          lastAgents.set(client, value.agents);
          cloudState = value;
          cloudDirty = false;
          if (agentsChanged)
            publish({ type: "invalidate", scope: "workspace" });
          return value;
        })
        .finally(() => {
          cloudLoad = undefined;
        });
      return cloudLoad;
    }
    return cloudState;
  };
  const uploads = createLocalV2Uploads(
    client,
    () => transport.capabilities?.() ?? Promise.resolve([]),
    (outcome) => publish({ type: "attachment.upload", ...outcome }),
    undefined,
    createSubmissionAssets(
      JSON.stringify([
        api.getBaseUrl(),
        scope.userId,
        scope.workspaceId,
        client.deviceId,
      ]),
    ),
  );
  const conversationPath = (id: string, suffix = "") =>
    `/sessions/${encodeURIComponent(id)}${suffix}`;
  const received = (raw: unknown) => {
    const summary = localV2SummarySchema.parse(raw);
    client.receiveSummary(summary);
    return projectLocalSession(summary, scope);
  };
  const command = async (id: string, body: Record<string, unknown>) => {
    assertCurrent();
    return client.command(id, body);
  };
  const fileLocation = (path: string) => {
    const url = client.fileUrl(path);
    if (!url)
      throw new Error("daemon_offline: The local daemon is not reachable yet");
    return url;
  };
  const fileData = async (path: string) => {
    const response = await fetch(client.fileUrl(path));
    if (!response.ok)
      throw new Error(`Local file request failed (HTTP ${response.status})`);
    const disposition = response.headers.get("content-disposition") ?? "";
    const name =
      /filename="?([^";]+)"?/.exec(disposition)?.[1] ??
      path.split("/").at(-1) ??
      "file";
    return {
      data: await blobBase64(await response.blob()),
      mimeType:
        response.headers.get("content-type") ?? "application/octet-stream",
      name: decodeURIComponent(name)
    };
  };

  /**
   * Answers from what is known and refreshes each source on its own; a source
   * that changes invalidates the workspace. Only a GUI that has never seen them
   * waits, and only for this device's targets and list, which it cannot start
   * without. Selecting a conversation therefore never waits on any source.
   */
  async function load(): Promise<AgentGuiWorkspaceState> {
    assertCurrent();
    const refreshFailed = (source: string) => (error: unknown) => {
      console.warn(`Could not refresh ${source}`, error);
    };
    remoteWorkspace().catch(refreshFailed("cloud agents"));
    const qc = options.queryClient;
    void qc
      .fetchQuery({ ...foldersQuery, staleTime: 0 })
      .catch(refreshFailed("folder projects"));
    const providersRead = qc.fetchQuery({ ...providersQuery, staleTime: 0 });
    providersRead.catch(refreshFailed("providers"));
    const summariesRead = client.loadSummaries();
    summariesRead.catch(refreshFailed("conversations"));
    await Promise.all([
      qc.getQueryData(providersQuery.queryKey) ? null : providersRead,
      client.list.read ? null : summariesRead
    ]);
    assertCurrent();
    const providers = qc.getQueryData<LocalProviderList>(
      providersQuery.queryKey
    )!;
    const folderProjects = qc.getQueryData<LocalFolderProjectList>(
      foldersQuery.queryKey
    );
    const summaries = client.list.summaries;
    return {
      local: true,
      composerDraftScope: JSON.stringify([
        api.getBaseUrl(),
        options.storagePrefix ?? "",
        ...(deviceId === "local" ? [] : [deviceId])
      ]),
      queueOwner: "desktop",
      sideWorkspace: true,
      sidePane: Boolean(options.storagePrefix),
      agents: lastAgents.get(client) ?? [],
      executionTargets: providers.providers.map((entry) => ({
        id: localTargetId(entry.id),
        name: entry.name,
        provider: entry.id,
        iconUrl: "",
        canInvoke: entry.available,
        canFork: true,
        runtimeBound: entry.available,
        runtimeOnline: entry.available,
        unavailableReason: entry.available ? undefined : "未在本机安装"
      })),
      sessions: summaries.map((summary) => projectLocalSession(summary, scope)),
      folderProjects: (folderProjects?.projects ?? []).map(
        (project): LocalFolderProject => ({
          id: project.id,
          title: project.title,
          path: project.path,
          createdAt: project.created_at
        })
      ),
      pendingTasks: summaries.flatMap((summary) =>
        summary.active_turn_id
          ? [
              {
                task_id: summary.active_turn_id,
                status: "running" as const,
                chat_session_id: summary.conversation_id
              }
            ]
          : []
      )
    };
  }

  async function referencedContent(
    content: string,
    references: { kind: string; id: string }[]
  ): Promise<string> {
    const excerpts: string[] = [];
    for (const reference of references) {
      if (isTaskCreationReference(reference)) continue;
      if (reference.kind === "session") {
        // The provider reads what it needs through its context tools; the
        // conversation itself never has to cross the device link to be quoted.
        const title = client.list.summary(reference.id)?.title;
        excerpts.push(
          `Multica local conversation ${title ? `${JSON.stringify(title)} ` : ""}(id: ${reference.id}; workspace: ${scope.workspaceId})\n[Not included here. Read it with read_context(kind="conversation", id=${JSON.stringify(reference.id)}, source="local") when it is relevant.]`
        );
      } else if (reference.kind === "issue") {
        const issue = await api.getIssue(reference.id);
        assertCurrent();
        if (issue.workspace_id !== scope.workspaceId)
          throw new Error("Referenced task is outside this workspace");
        const description = issue.description || "";
        excerpts.push(
          `Multica task ${issue.identifier} (id: ${issue.id}; workspace: ${scope.workspaceId}) ${issue.title}\n${description.slice(0, 6000)}\n[Task snapshot only: comments, activity and attachment contents are not included.${description.length > 6000 ? " Description excerpt truncated." : ""} Query this task with the built-in Multica MCP read_context tool (kind=task) or Multica CLI for current context.]`
        );
      }
    }
    return excerpts.length
      ? `${content}\n\nReferenced context (quoted data):\n${excerpts.join("\n\n---\n\n")}`
      : content;
  }

  /** Mentioned agents, checked against the official workspace. */
  async function mentionedAgents(references: { kind: string; id: string }[]) {
    const ids = [
      ...new Set(
        references
          .filter((reference) => reference.kind === "agent")
          .map((reference) => reference.id)
      )
    ];
    if (!ids.length) return [];
    if (references.some(isTaskCreationReference))
      throw new Error(
        "New task requires the current local provider (Codex / Claude Code). Remove agent invocation mentions first."
      );
    const state = await remoteWorkspace();
    return ids.map((id) => {
      const target = state.agents.find((agent) => agent.agent.id === id);
      if (
        !target?.canInvoke ||
        !target.runtimeBound ||
        target.agent.archived_at
      )
        throw new Error("Mentioned agent is not available");
      return { id, name: target.agent.name };
    });
  }

  const services: AgentGuiHostServices = {
    queue: {
      prepare: async (params) => {
        assertCurrent();
        const agents = await mentionedAgents(params.item.references ?? []);
        assertCurrent();
        return { ...params, item: { ...params.item, agents } };
      },
      enqueue: async (params) => {
        assertCurrent();
        await uploads.ensure(params.item.attachmentIds ?? []);
        assertCurrent();

        // Transfer ownership before any asynchronous view-bound preparation.
        const agents =
          params.item.agents ??
          (await mentionedAgents(params.item.references ?? []));
        const item = { ...params.item, ...(agents.length ? { agents } : {}) };
        assertCurrent();
        if (
          !params.paused &&
          (await transport.capabilities?.())?.includes(
            SUBMISSION_LIFECYCLE_CAPABILITY,
          )
        ) {
          const raw = await command(params.sessionId, { type: "submit", item });
          const parsed = parseWithFallback<z.infer<
            typeof localSubmissionResultSchema
          > | null>(raw, localSubmissionResultSchema, null, {
            endpoint: "local-v2.submit",
          });
          if (!parsed) throw new Error("Invalid submission response");
          client
            .conversation(params.sessionId)
            ?.mergeSubmissions([parsed.submission]);
          return {
            ...projectQueue(parsed.queue),
            submissions: [parsed.submission],
            storeGeneration: parsed.store_generation,
            snapshotSeq: parsed.snapshot_seq,
          };
        }
        return projectQueue(
          await command(params.sessionId, {
            type: "queue_enqueue",
            item,
            paused: params.paused === true
          })
        );
      },
      statuses: async (params) => {
        assertCurrent();

        if (
          !(await transport.capabilities?.())?.includes(
            LOCAL_V2_PROMPT_STATUS_CAPABILITY
          )
        )
          return {};
        const raw = await client.request(
          "POST",
          `/sessions/${encodeURIComponent(params.sessionId)}/queue/status`,
          { request_ids: params.requestIds }
        );
        return parseWithFallback(
          raw,
          localV2PromptStatusesSchema,
          { statuses: {} },
          { endpoint: "local-v2.queue-status" }
        ).statuses;
      },
      update: async (params) => {
        assertCurrent();
        return projectQueue(
          await command(params.sessionId, {
            type: "queue_update",
            operation_id: crypto.randomUUID(),
            action: params.action === "sendNow" ? "send_now" : params.action,
            request_id: params.requestId,
            ...(params.targetTurnId?{turn_id:params.targetTurnId}:{}),
          }),
        );
      }
    },
    interaction: {
      respond: async (params) => {
        assertCurrent();
        await command(params.sessionId, {
          type: "answer",
          interaction_id: params.requestId,
          answer: params.answers
        });
        return undefined;
      }
    },
    workspace: {
      load: async () => {
        assertCurrent();
        return load();
      }
    },
    session: {
      load: cloud.session.load,
      create: async (params) => {
        assertCurrent();

        let directory: {
          folder_project_id?: string;
          directory_source_id?: string;
        } = {};
        if (params.cwd) {
          const { projects } = (await client.request(
            "GET",
            "/folder-projects"
          )) as { projects: LocalFolderProject[] };
          const folder = projectForDirectory(projects, params.cwd);
          if (folder) directory = { folder_project_id: folder.id };
          else {
            const source = !isAutomaticLocalDirectory(params.cwd)
              ? ((await client.request(
                  "GET",
                  `/directory-source?cwd=${encodeURIComponent(params.cwd)}`
                )) as { id: string | null })
              : { id: null };
            if (!source.id)
              throw new Error(
                "Local project directory not found in this workspace"
              );
            directory = { directory_source_id: source.id };
          }
        }
        assertCurrent();
        return received(
          await client.request("POST", "/sessions", {
            id: params.sessionId ?? crypto.randomUUID(),
            provider: localProviderId(params.agentId),
            title: params.title ?? "",
            ...directory
          })
        );
      },
      send: async (params) => {
        assertCurrent();

        const references: { kind: string; id: string }[] = Array.isArray(
          params.references
        )
          ? params.references
          : [];
        const agents = await mentionedAgents(references);
        const prompt = await referencedContent(params.content, references);
        assertCurrent();
        if (
          !agents.length &&
          (params.model !== undefined ||
            params.reasoningEffort !== undefined ||
            params.permissionMode !== undefined)
        ) {
          await command(params.sessionId, {
            type: "update_settings",
            ...(params.model !== undefined
              ? { model: params.model ?? null }
              : {}),
            ...(params.reasoningEffort !== undefined
              ? { reasoning_effort: params.reasoningEffort }
              : {}),
            ...(params.permissionMode !== undefined
              ? { permission_mode: params.permissionMode }
              : {})
          });
        }
        const requestId: string = params.requestId ?? crypto.randomUUID();
        const accepted = (await command(params.sessionId, {
          type: "send",
          request_id: requestId,
          text: params.content,
          prompt,
          references,
          attachment_ids: Array.isArray(params.attachmentIds)
            ? params.attachmentIds
            : [],
          ...(agents.length ? { agents } : {})
        })) as { turn_id?: string; turn_ids?: string[] };
        return {
          message_id: `msg-${requestId}`,
          task_id: accepted.turn_id ?? accepted.turn_ids?.[0] ?? "",
          created_at: new Date().toISOString()
        };
      },
      rename: async (params) => {
        assertCurrent();
        return received(
          await command(params.sessionId, {
            type: "update",
            title: params.title
          })
        );
      },
      fork: async (params) => {
        assertCurrent();
        return received(
          await command(params.sessionId, {
            type: "fork",
            turn_id: params.turnId,
            request_id: params.requestId
          })
        );
      },
      pin: async (params) => {
        assertCurrent();
        return received(
          await command(params.sessionId, {
            type: "update",
            pinned: params.pinned
          })
        );
      },
      delete: async () => {
        assertCurrent();
        throw new Error("Archive local conversations from the session list");
      },
      cancel: async (params) => {
        assertCurrent();

        await command(params.sessionId, { type: "interrupt" });
        return {
          id: params.taskId,
          agent_id: "",
          runtime_id: "",
          issue_id: "",
          chat_session_id: params.sessionId,
          kind: "chat",
          status: "running",
          priority: 0,
          created_at: new Date().toISOString(),
          started_at: null,
          dispatched_at: null,
          completed_at: null,
          result: null,
          error: null
        } as AgentTask;
      },
      compact: async (params) => {
        assertCurrent();

        const result = await command(params.sessionId, {
          type: "compact",
          request_id: params.requestId
        });
        const parsed = parseWithFallback<{ turn_id: string } | null>(
          result,
          z.object({ turn_id: z.string().min(1) }),
          null,
          { endpoint: "local-v2.compact" }
        );
        if (!parsed)
          throw new Error(
            "Invalid context compaction response. Reload and retry."
          );
        return { turnId: parsed.turn_id };
      }
    },
    models: {
      list: async (params) => {
        assertCurrent();

        const result = (await client.request(
          "GET",
          `/models?provider=${localProviderId(params.agentId)}${params.force ? "&force=true" : ""}`
        )) as Record<string, unknown>;
        return {
          ...result,
          models: Array.isArray(result.models) ? result.models : [],
          supported: result.supported !== false,
          id: "local-models",
          runtime_id: params.agentId,
          status: "completed"
        } as import("@multica/core/types").RuntimeModelsResult;
      }
    },
    usage: {
      read: async (params) => {
        assertCurrent();

        const usage = (await client.request(
          "GET",
          `/usage?provider=${localProviderId(params.agentId)}${params.sessionId ? `&conversation_id=${encodeURIComponent(params.sessionId)}` : ""}${params.force ? "&force=true" : ""}`
        )) as {
          provider: LocalProviderId;
          context: {
            used_tokens: number | null;
            limit_tokens: number | null;
            model: string | null;
            updated_at: string | null;
          };
          account: {
            status: LocalProviderUsage["account"]["status"];
            plan: string | null;
            windows: {
              id: string;
              label: string;
              used_percent: number;
              resets_at: string | null;
            }[];
            updated_at: string | null;
            stale?: boolean;
          };
        };
        return {
          provider: usage.provider,
          context: {
            usedTokens: usage.context.used_tokens,
            limitTokens: usage.context.limit_tokens,
            model: usage.context.model,
            updatedAt: usage.context.updated_at
          },
          account: {
            status: usage.account.status,
            plan: usage.account.plan,
            updatedAt: usage.account.updated_at,
            ...(usage.account.stale ? { stale: true } : {}),
            windows: usage.account.windows.map((window) => ({
              id: window.id,
              label: window.label,
              usedPercent: window.used_percent,
              resetsAt: window.resets_at
            }))
          }
        } satisfies LocalProviderUsage;
      }
    },
    context: {
      tasks: cloud.context.tasks
    },
    attachment: {
      upload: async (params) => {
        assertCurrent();

        if (
          !(params.file instanceof File) ||
          params.file.size > 25 * 1024 * 1024
        )
          throw new Error("Invalid file, or file exceeds 25 MB");
        const uploaded = params.background
          ? await uploads.begin(params.file)
          : { ...(await client.upload(params.file)), uploading: false };
        const url = client.fileUrl(`/attachments/${uploaded.id}`);
        return {
          id: uploaded.id,
          workspace_id: scope.workspaceId,
          issue_id: null,
          comment_id: null,
          chat_session_id: null,
          chat_message_id: null,
          uploader_type: "user",
          uploader_id: scope.userId,
          filename: uploaded.name,
          content_type: uploaded.mime_type,
          size_bytes: uploaded.size,
          url,
          download_url: url,
          markdown_url: url,
          created_at: new Date().toISOString(),
          uploading: uploaded.uploading
        } satisfies Attachment & { uploading: boolean };
      },
      cancel: async (params) => {
        assertCurrent();
        await uploads.cancel(params.attachmentId);
        return undefined;
      },
      retry: async (params) => {
        assertCurrent();
        uploads.retry(params.attachmentId);
        return undefined;
      },
      forget: async (params) => {
        assertCurrent();
        uploads.forget(params.attachmentId);
        return undefined;
      },
      url: async (params) => {
        assertCurrent();
        return {
          url: fileLocation(
            `/attachments/${encodeURIComponent(params.attachmentId)}`
          )
        };
      },
      read: async (params) => {
        assertCurrent();
        return fileData(
          `/attachments/${encodeURIComponent(params.attachmentId)}`
        );
      }
    },
    image: {
      url: async (params) => {
        assertCurrent();
        return {
          url: fileLocation(
            `${conversationPath(params.sessionId, "/image")}?path=${encodeURIComponent(params.path)}`
          )
        };
      },
      read: async (params) => {
        assertCurrent();
        return fileData(
          `${conversationPath(params.sessionId, "/image")}?path=${encodeURIComponent(params.path)}`
        );
      }
    },
    storage: {
      subscribe: (params, listener) => cloud.storage.subscribe?.({
        key: params.key === GLOBAL_EXECUTION_PERMISSION_KEY ? params.key : `local-provider:${options.storagePrefix ?? ""}${params.key}`
      }, listener) ?? (() => {}),
      read: async ({ key }) => {
        assertCurrent();
        if (key === AGENT_MENTION_USAGE_KEY)
          return storage.getItem(usageStorageKey);
        return cloud.storage.read({
          key: key === GLOBAL_EXECUTION_PERMISSION_KEY ? key : `local-provider:${options.storagePrefix ?? ""}${key}`
        });
      },
      write: async ({ key, value }) => {
        assertCurrent();
        if (key === AGENT_MENTION_SELECTION_KEY) {
          if (typeof value !== "string" || !value.trim())
            throw new Error("Missing selected agent ID");
          const agents = parseAgentMentionUsage(
            storage.getItem(usageStorageKey)
          );
          agents[value] = {
            count: (agents[value]?.count ?? 0) + 1,
            lastUsedAt: Date.now()
          };
          storage.setItem(
            usageStorageKey,
            JSON.stringify({ version: 1, agents })
          );
          return;
        }
        return cloud.storage.write({
          key: key === GLOBAL_EXECUTION_PERMISSION_KEY ? key : `local-provider:${options.storagePrefix ?? ""}${key}`,
          value
        });
      }
    },
    hostApi: {
      openTask: cloud.hostApi.openTask,
      openInternalResource: cloud.hostApi.openInternalResource,
      openSide: async (params) => {
        assertCurrent();

        const raw = params.queuedRequestId
          ? await command(params.sessionId, {
              type: "queue_side",
              request_id: params.queuedRequestId
            })
          : await command(params.sessionId, {
              type: "create_side",
              request_id: params.requestId,
              turn_id: params.turnId,
              quote: params.quote
            });
        const side = received(raw);
        assertCurrent();
        options.onOpenSide?.(side.id, params.sessionId);
        return side;
      },
      openResource: async (params) => {
        assertCurrent();
        options.onOpenResource?.(params.sessionId, params.url);
        return undefined;
      },
      showFileContextMenu: async (params) => {
        assertCurrent();
        await window.desktopAPI.localFileContextMenu.show(
          scope,
          {
            sessionId: params.sessionId,
            url: params.url,
            hasSelection: params.hasSelection
          },
          (transport as { deviceId?: string }).deviceId ?? "local"
        );
        return undefined;
      },
      locateSource: async (params) => {
        assertCurrent();
        options.onLocateSource?.(params.sessionId, params.turnId, params.quote);
        return undefined;
      },
      selectSession: async (params) => {
        assertCurrent();

        const sequence = ++selectionSequence;
        const intent = options.getSelectionIntent?.();
        const summary = params.sessionId
          ? (client.list.summary(params.sessionId) ??
            (await client.snapshot(params.sessionId)).conversation)
          : null;
        assertCurrent();
        if (
          sequence !== selectionSequence ||
          intent !== options.getSelectionIntent?.()
        )
          return undefined;
        if (summary)
          options.onLocalSelect(
            summary.conversation_id,
            summary.provider as LocalProviderId
          );
        else
          options.onLocalSelect(
            null,
            params.agentId ? localProviderId(params.agentId) : undefined
          );
        return undefined;
      },
      showMessage: cloud.hostApi.showMessage,
      promptApplied: cloud.hostApi.promptApplied
    },
    subscribe: (listener) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    }
  };

  // List changes arrive as summaries; coalesce them into one workspace reload.
  const unsubscribeSummaries = reaction(
    () => client.list.summaries,
    () => {
      if (workspaceTimer) return;
      workspaceTimer = setTimeout(() => {
        workspaceTimer = undefined;
        publish({ type: "invalidate", scope: "workspace" });
      }, 50);
    }
  );
  // The page and this service share these queries; either reading new data refreshes the GUI.
  const known = new Map<string, unknown>();
  const unsubscribeKnown = options.queryClient
    .getQueryCache()
    .subscribe((event) => {
      if (event.type !== "updated" || event.action.type !== "success") return;
      const hash = event.query.queryHash;
      if (
        hash !== JSON.stringify(providersQuery.queryKey) &&
        hash !== JSON.stringify(foldersQuery.queryKey)
      )
        return;
      const changed = known.get(hash) !== event.query.state.data;
      known.set(hash, event.query.state.data);
      if (changed) publish({ type: "invalidate", scope: "workspace" });
    });
  const unsubscribeCloud = cloud.subscribe((event) => {
    if (event.type === "invalidate" && event.scope === "workspace") {
      cloudDirty = true;
      publish(event);
    }
  });
  const unsubscribeConnection = transport.onConnection((value, connected) => {
    if (
      disposed ||
      !options.isCurrent() ||
      value.userId !== scope.userId ||
      value.workspaceId !== scope.workspaceId
    )
      return;
    publish({ type: "connection", connected });
    if (connected) publish({ type: "invalidate", scope: "workspace" });
  });
  return {
    localConversations: localConversationSource(client, scope),
    load,
    ...services,
    publish,
    subscribe: (listener) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    reconnect: () => {
      cloudDirty = true;
      cloud.reconnect();
      publish({ type: "invalidate", scope: "workspace" });
    },
    dispose: () => {
      disposed = true;
      uploads.dispose();
      if (workspaceTimer) clearTimeout(workspaceTimer);
      unsubscribeSummaries();
      unsubscribeKnown();
      unsubscribeCloud();
      unsubscribeConnection();
      cloud.dispose();
      listeners.clear();
    }
  };
}
