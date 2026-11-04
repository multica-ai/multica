import { QueryObserver, type QueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { deriveAgentPresenceDetail, isAgentRuntimeBound } from "@multica/core/agents";
import { canAssignAgentToIssue } from "@multica/core/permissions";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { defaultStorage } from "@multica/core/platform";
import type { StorageAdapter } from "@multica/core/types";
import type {
  AgentGuiAgentDescriptor,
  AgentGuiHostEvent,
  AgentGuiRpcMethod,
  AgentGuiRpcMethods,
  AgentGuiWorkspaceState,
  LocalConversationSource,
} from "@multica/tutti-agent-gui-host/protocol";
import { redactSecrets } from "@multica/views/common/task-transcript";
import { issueKeys } from "@multica/core/issues/queries";
import { buildIssueStatusCatalog, issueStatusListOptions } from "@multica/core/issue-statuses";
import { resolveResourceReference } from "@multica/tutti-agent-gui-host/resource-reference";
import { agentGuiAvatarUrl } from "./agent-gui-avatar";

export class AgentGuiBridgeError extends Error {
  constructor(message: string, readonly code: string, readonly status?: number) {
    super(message);
  }
}

export interface ParentServiceOptions {
  queryClient: QueryClient;
  workspaceId: string;
  workspaceSlug: string;
  userId: string;
  isCurrent: () => boolean;
  resolveAvatar: (url: string) => string;
  onOpenTask: (issueId: string) => void;
  getAppOrigin?: () => string | null;
  onOpenInternalResource?: (path: string, background: boolean) => void;
  onShowMessage: (message: string, tone?: "success" | "info" | "warning" | "error") => void;
  onPromptApplied?: (sequence: number, restoreRequestId?: string) => void;
  storage?: StorageAdapter;
}

function record(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new AgentGuiBridgeError("Invalid GUI request", "invalid_request", 400);
  }
  return value as Record<string, unknown>;
}

function stringField(value: Record<string, unknown>, key: string): string {
  const result = value[key];
  if (typeof result !== "string" || !result.trim()) {
    throw new AgentGuiBridgeError(`Missing ${key}`, "invalid_request", 400);
  }
  return result;
}

/** What a mounted GUI talks to. Auth and server connections stay in the Multica document. */
export interface AgentGuiParentService extends AgentGuiWorkspaceService {
  localConversations: LocalConversationSource;
}

/** The workspace-level part of the host: official agents, task search, navigation and storage. */
export interface AgentGuiWorkspaceService {
  load(): Promise<AgentGuiWorkspaceState>;
  request<M extends AgentGuiRpcMethod>(method: M, params: AgentGuiRpcMethods[M]["params"]): Promise<AgentGuiRpcMethods[M]["result"]>;
  publish(event: AgentGuiHostEvent): void;
  subscribe(listener: (event: AgentGuiHostEvent) => void): () => void;
  reconnect(): void;
  dispose(): void;
}

export function createAgentGuiWorkspaceService(options: ParentServiceOptions): AgentGuiWorkspaceService {
  const { queryClient: qc, workspaceId, userId } = options;
  const storage = options.storage ?? defaultStorage;
  const listeners = new Set<(event: AgentGuiHostEvent) => void>();
  const cleanup: Array<() => void> = [];
  let disposed = false;
  let workspaceNotificationPending = false;

  function assertCurrent() {
    if (disposed || !options.isCurrent()) {
      throw new AgentGuiBridgeError("This workspace is no longer active", "stale_workspace", 409);
    }
  }

  function publish(event: AgentGuiHostEvent) {
    if (disposed || !options.isCurrent()) return;
    listeners.forEach((listener) => listener(event));
  }

  function notifyWorkspace() {
    if (workspaceNotificationPending) return;
    workspaceNotificationPending = true;
    queueMicrotask(() => {
      workspaceNotificationPending = false;
      publish({ type: "invalidate", scope: "workspace" });
    });
  }

  async function workspaceLoad(): Promise<AgentGuiWorkspaceState> {
    assertCurrent();
    const [agents, members, runtimes] = await Promise.all([
      qc.fetchQuery({ ...agentListOptions(workspaceId), staleTime: 0 }),
      qc.fetchQuery(memberListOptions(workspaceId)),
      api.listRuntimes({ workspace_id: workspaceId }),
    ]);
    assertCurrent();
    const role = members.find((member) => member.user_id === userId)?.role;
    const descriptors: AgentGuiAgentDescriptor[] = agents.map((agent) => {
      const runtime = runtimes.find((entry) => entry.id === agent.runtime_id);
      const provider = runtime?.provider || (
        typeof agent.runtime_config.provider === "string"
          ? agent.runtime_config.provider : "unknown"
      );
      const runtimeBound = isAgentRuntimeBound(agent);
      const canInvoke = !agent.archived_at && canAssignAgentToIssue(agent, {
        userId,
        role: role === "owner" || role === "admin" || role === "member" ? role : null,
      }).allowed;
      const { availability } = deriveAgentPresenceDetail({ agent, runtime: runtime ?? null, tasks: [], now: Date.now() });
      return {
        agent,
        provider,
        iconUrl: agentGuiAvatarUrl(agent.avatar_url, options.resolveAvatar),
        canInvoke,
        runtimeBound,
        runtimeOnline: availability === "online" || availability === "unstable",
      };
    });
    return { agents: descriptors, sessions: [] };
  }

  async function request(method: AgentGuiRpcMethod, rawParams: unknown): Promise<unknown> {
    assertCurrent();
    const params = record(rawParams);
    let result: unknown;
    switch (method) {
      case "context.tasks": {
        if (typeof params.query !== "string" || params.query.length > 200 || typeof params.limit !== "number" || !Number.isInteger(params.limit) || params.limit < 1 || params.limit > 50) {
          throw new AgentGuiBridgeError("Invalid task search", "invalid_request", 400);
        }
        const query = params.query.trim();
        const limit = params.limit;
        const [page, statuses] = await Promise.all([qc.fetchQuery({
          queryKey: [...issueKeys.all(workspaceId), "gui-context", query, limit],
          queryFn: () => api.listIssues({ workspace_id: workspaceId, q: query, limit, sort_by: "updated_at", sort_direction: "desc" }),
          staleTime: 0,
        }), qc.fetchQuery({ ...issueStatusListOptions(workspaceId), retry: false }).catch(() => undefined)]);
        assertCurrent();
        const catalog = buildIssueStatusCatalog(statuses?.statuses);
        result = page.issues.filter((issue) => issue.workspace_id === workspaceId).map((issue) => ({
          id: issue.id, label: `${issue.identifier} ${issue.title}`, description: issue.description ? redactSecrets(issue.description).slice(0, 300) : undefined, status: issue.status,
          statusLabel: catalog.entryOf(issue.status)?.is_system === false ? catalog.labelOf(issue.status) : undefined,
          statusCategory: catalog.entryOf(issue.status)?.category ?? issue.status_category,
        }));
        break;
      }
      case "storage.read":
      case "storage.write": {
        const key = `multica:tutti-gui:${userId}:${workspaceId}:${encodeURIComponent(stringField(params, "key"))}`;
        if (method === "storage.read") result = storage.getItem(key);
        else {
          if (typeof params.value !== "string") throw new AgentGuiBridgeError("Invalid stored value", "invalid_request", 400);
          storage.setItem(key, params.value);
        }
        break;
      }
      case "host.openTask": options.onOpenTask(stringField(params, "issueId")); break;
      case "host.openInternalResource": {
        const resource = resolveResourceReference(stringField(params, "href"), { workspaceId, workspaceSlug: options.workspaceSlug, appOrigin: options.getAppOrigin?.() });
        if (!resource || params.background !== undefined && typeof params.background !== "boolean") throw new AgentGuiBridgeError("Invalid internal resource link", "invalid_request", 400);
        if (!options.onOpenInternalResource) throw new AgentGuiBridgeError("Internal navigation is unavailable", "navigation_unavailable", 409);
        options.onOpenInternalResource(resource.path, params.background === true); break;
      }
      case "host.showMessage":
        options.onShowMessage(stringField(params, "message"), params.tone === "success" || params.tone === "warning" || params.tone === "error" ? params.tone : "info");
        break;
      case "host.promptApplied":
        if (typeof params.sequence === "number") options.onPromptApplied?.(params.sequence, typeof params.restoreRequestId === "string" ? params.restoreRequestId : undefined);
        break;
      default: throw new AgentGuiBridgeError("Unsupported GUI command", "unsupported");
    }
    assertCurrent();
    return result;
  }

  cleanup.push(qc.getQueryCache().subscribe((event) => {
    if (event.type !== "updated") return;
    if (event.action.type !== "invalidate" && !(event.action.type === "success" && event.action.manual)) return;
    const key = event.query.queryKey;
    if ((key[0] === "workspaces" && key[1] === workspaceId) || (key[0] === "runtimes" && key[1] === workspaceId)) notifyWorkspace();
  }));

  const agentListObserver = new QueryObserver(qc, agentListOptions(workspaceId));
  cleanup.push(agentListObserver.subscribe(() => {}), () => agentListObserver.destroy());

  return {
    request: request as <M extends AgentGuiRpcMethod>(method: M, params: AgentGuiRpcMethods[M]["params"]) => Promise<AgentGuiRpcMethods[M]["result"]>,
    load: workspaceLoad,
    subscribe(listener: (event: AgentGuiHostEvent) => void) { listeners.add(listener); return () => { listeners.delete(listener); }; },
    publish,
    reconnect: notifyWorkspace,
    dispose() {
      disposed = true;
      cleanup.forEach((fn) => fn());
      listeners.clear();
    },
  };
}
