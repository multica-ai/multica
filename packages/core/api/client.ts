import type {
  Issue,
  CreateIssueRequest,
  UpdateIssueRequest,
  ListIssuesResponse,
  SearchIssuesResponse,
  SearchProjectsResponse,
  UpdateMeRequest,
  CreateMemberRequest,
  UpdateMemberRequest,
  ListIssuesParams,
  Agent,
  CreateAgentRequest,
  UpdateAgentRequest,
  AgentTask,
  AgentActivityBucket,
  AgentRunCount,
  AgentRuntime,
  InboxItem,
  IssueSubscriber,
  Comment,
  Reaction,
  IssueReaction,
  Workspace,
  WorkspaceRepo,
  MemberWithUser,
  MemberUsage,
  User,
  Skill,
  SkillSummary,
  CreateSkillRequest,
  UpdateSkillRequest,
  SetAgentSkillsRequest,
  SkillVersion,
  SkillChangeRequest,
  SkillFork,
  UpdateSkillOwnershipRequest,
  CreateSkillChangeRequestRequest,
  ReviewSkillChangeRequestRequest,
  ForkSkillRequest,
  PersonalAccessToken,
  CreatePersonalAccessTokenRequest,
  CreatePersonalAccessTokenResponse,
  CreateRuntimeSetupTokenRequest,
  CreateRuntimeSetupTokenResponse,
  RuntimeUsage,
  IssueUsageSummary,
  RuntimeHourlyActivity,
  RuntimeUsageByAgent,
  RuntimeUsageByHour,
  RuntimeUpdate,
  RuntimeModelListRequest,
  RuntimeLocalSkillListRequest,
  CreateRuntimeLocalSkillImportRequest,
  RuntimeLocalSkillImportRequest,
  TimelineEntry,
  AssigneeFrequencyEntry,
  TaskMessagePayload,
  Attachment,
  Artifact,
  ArtifactFolder,
  ArtifactUploadResponse,
  CreateArtifactRequest,
  UpdateArtifactRequest,
  UpdateArtifactScopeRequest,
  MoveArtifactToFolderRequest,
  CreateArtifactFolderRequest,
  UpdateArtifactFolderRequest,
  ListArtifactsParams,
  ChatSession,
  ChatMessage,
  ChatPendingTask,
  ChatSessionUsage,
  PendingChatTasksResponse,
  SendChatMessageResponse,
  Channel,
  ChannelAgentListenMode,
  ChannelAgentSetting,
  ChannelAgentSettingsResponse,
  CreateChannelRequest,
  Project,
  ProjectMember,
  CreateProjectRequest,
  UpdateProjectRequest,
  ListProjectsResponse,
  // CEREBRO-PATCH(nested-projects): project tree response types for fork endpoints.
  ListProjectTreeResponse,
  ProjectTreeItem,
  ProjectRollupStats,
  ProjectResource,
  CreateProjectResourceRequest,
  ListProjectResourcesResponse,
  Label,
  CreateLabelRequest,
  UpdateLabelRequest,
  ListLabelsResponse,
  IssueLabelsResponse,
  PinnedItem,
  CreatePinRequest,
  PinnedItemType,
  ReorderPinsRequest,
  Invitation,
  Autopilot,
  AutopilotTrigger,
  AutopilotRun,
  CreateAutopilotRequest,
  UpdateAutopilotRequest,
  CreateAutopilotTriggerRequest,
  UpdateAutopilotTriggerRequest,
  ListAutopilotsResponse,
  GetAutopilotResponse,
  ListAutopilotRunsResponse,
  WorkSession,
  UserProfileResponse,
  UserProfileRequest,
  PushSubscriptionResponse,
  NotificationPreferenceResponse,
  NotificationPreferences,
} from "../types";
import type { OnboardingCompletionPath } from "../onboarding/types";
import { type Logger, noopLogger } from "../logger";
import { createRequestId } from "../utils";
import { getCurrentSlug } from "../platform/workspace-storage";
import { parseWithFallback } from "./schema";
import {
  AgentToolsListSchema,
  ChildIssuesResponseSchema,
  CommentsListSchema,
  EMPTY_LIST_ISSUES_RESPONSE,
  EMPTY_TIMELINE_ENTRIES,
  ListIssuesResponseSchema,
  SubscribersListSchema,
  TimelineEntriesSchema,
} from "./schemas";

/** Identifies the calling client to the server.
 *  Sent on every HTTP request as X-Client-Platform / X-Client-Version /
 *  X-Client-OS so the backend can log, gate, or split metrics by client.
 *  See server/internal/middleware/client.go for the receiving end. */
export interface ApiClientIdentity {
  /** Logical client kind. Server expects: "web" | "desktop" | "cli" | "daemon". */
  platform?: string;
  /** Client/app version string (e.g. "0.1.0", git tag, commit). */
  version?: string;
  /** Operating system the client is running on: "macos" | "windows" | "linux". */
  os?: string;
}

export interface ApiClientOptions {
  logger?: Logger;
  onUnauthorized?: () => void;
  /** Identifies the client to the server. Sent as X-Client-* headers. */
  identity?: ApiClientIdentity;
}

export interface LoginResponse {
  token: string;
  user: User;
}

// --- Starter content (post-onboarding import) -----------------------------
// Shape mirrors the Go request/response in handler/onboarding.go.
//
// The client sends both branches of sub-issues and an unbound welcome
// issue template (title + description, no `agent_id`). The SERVER picks
// the branch by inspecting the workspace's agent list inside the
// import transaction. This removes the client as a trusted decider —
// even if the client has a stale agent cache or lies, the server uses
// the DB as source of truth.

export interface ImportStarterIssuePayload {
  title: string;
  description: string;
  status: string;
  priority: string;
  /** Server uses `user_id` (per app-wide AssigneePicker convention)
   *  as assignee when true. No member_id is threaded through. */
  assign_to_self: boolean;
}

export interface ImportStarterWelcomeIssueTemplate {
  title: string;
  description: string;
  /** Defaults to "high" on server when empty. */
  priority: string;
}

export interface ImportStarterContentPayload {
  workspace_id: string;
  project: { title: string; description: string; icon: string };
  /** Always sent. Server creates it only when an agent exists in the
   *  workspace; ignored otherwise. Agent id is picked by the server. */
  welcome_issue_template: ImportStarterWelcomeIssueTemplate;
  /** Used when the workspace has at least one agent. */
  agent_guided_sub_issues: ImportStarterIssuePayload[];
  /** Used when the workspace has zero agents. */
  self_serve_sub_issues: ImportStarterIssuePayload[];
}

export interface ImportStarterContentResponse {
  user: User;
  project_id: string;
  /** Non-null when server took the agent-guided branch. */
  welcome_issue_id: string | null;
}

// CEREBRO-PATCH(cerebro-account-client): JEH-921 workspace account types. JEH-881 adds availability fields. JEH-998 adds usage fields.
export interface CerebroAccount {
  id: string;
  workspace_id: string;
  provider: string;
  login_identity: string;
  usage_window_pct: number | null;
  throttled_until: string | null;
  extra_spend_on: boolean;
  paused_manual: boolean;
  created_at: string;
  updated_at: string;
  // CEREBRO-PATCH(cerebro-account-availability): JEH-881 runtime availability fields.
  runtime_count: number;
  available_runtime_count: number;
  nearest_unpause_at: string | null;
  status: "available" | "throttled" | "paused" | "no_runtime";
}
export interface CreateCerebroAccountRequest {
  provider: string;
  login_identity: string;
}
export interface UpdateCerebroAccountControlsRequest {
  extra_spend_on?: boolean;
  paused_manual?: boolean;
}

export class ApiError extends Error {
  readonly status: number;
  readonly statusText: string;
  // Raw decoded JSON body (when the server returned one). Carries structured
  // error fields like `code` so callers can branch on machine-readable
  // identifiers instead of pattern-matching the human-readable message.
  readonly body?: unknown;

  constructor(message: string, status: number, statusText: string, body?: unknown) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.statusText = statusText;
    this.body = body;
  }
}

export class ApiClient {
  private baseUrl: string;
  private token: string | null = null;
  private logger: Logger;
  private options: ApiClientOptions;

  constructor(baseUrl: string, options?: ApiClientOptions) {
    this.baseUrl = baseUrl;
    this.options = options ?? {};
    this.logger = options?.logger ?? noopLogger;
  }

  getBaseUrl(): string {
    return this.baseUrl;
  }

  setToken(token: string | null) {
    this.token = token;
  }

  private readCsrfToken(): string | null {
    if (typeof document === "undefined") return null;
    const match = document.cookie
      .split("; ")
      .find((c) => c.startsWith("multica_csrf="));
    return match ? match.split("=")[1] ?? null : null;
  }

  private authHeaders(): Record<string, string> {
    const headers: Record<string, string> = {};
    if (this.token) headers["Authorization"] = `Bearer ${this.token}`;
    const slug = getCurrentSlug();
    if (slug) headers["X-Workspace-Slug"] = slug;
    const csrf = this.readCsrfToken();
    if (csrf) headers["X-CSRF-Token"] = csrf;
    const id = this.options.identity;
    if (id?.platform) headers["X-Client-Platform"] = id.platform;
    if (id?.version) headers["X-Client-Version"] = id.version;
    if (id?.os) headers["X-Client-OS"] = id.os;
    return headers;
  }

  private handleUnauthorized() {
    this.token = null;
    // Workspace id is owned by the URL-driven workspace-storage singleton
    // (set by [workspaceSlug]/layout.tsx). On 401, the auth flow navigates
    // to /login which leaves the workspace route, and the next workspace
    // entry will overwrite the id. No clear needed here.
    this.options.onUnauthorized?.();
  }

  private async parseErrorMessage(res: Response, fallback: string): Promise<string> {
    try {
      const data = await res.json() as { error?: string };
      if (typeof data.error === "string" && data.error) return data.error;
    } catch {
      // Ignore non-JSON error bodies.
    }
    return fallback;
  }

  // Reads the response body once for both human-readable error message and
  // structured fields. The Response stream can only be consumed once, so
  // both pieces have to come from a single read.
  private async parseErrorBody(res: Response, fallback: string): Promise<{ message: string; body: unknown }> {
    try {
      const data = await res.json() as { error?: string };
      const message = typeof data.error === "string" && data.error ? data.error : fallback;
      return { message, body: data };
    } catch {
      return { message: fallback, body: undefined };
    }
  }

  private async fetch<T>(path: string, init?: RequestInit): Promise<T> {
    const rid = createRequestId();
    const start = Date.now();
    const method = init?.method ?? "GET";

    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      "X-Request-ID": rid,
      ...this.authHeaders(),
      ...((init?.headers as Record<string, string>) ?? {}),
    };

    this.logger.info(`→ ${method} ${path}`, { rid });

    const res = await fetch(`${this.baseUrl}${path}`, {
      ...init,
      headers,
      credentials: "include",
    });

    if (!res.ok) {
      if (res.status === 401) this.handleUnauthorized();
      const { message, body } = await this.parseErrorBody(res, `API error: ${res.status} ${res.statusText}`);
      // CEREBRO-PATCH(api-client-401-warn): treat 401 as warn (like 404). Both
      // are expected client states — initial cookie-auth probe before login,
      // and resource-not-found — so they should not fire Next.js dev's
      // console-error overlay during normal usage.
      const logLevel = res.status === 404 || res.status === 401 ? "warn" : "error";
      this.logger[logLevel](`← ${res.status} ${path}`, { rid, duration: `${Date.now() - start}ms`, error: message });
      throw new ApiError(message, res.status, res.statusText, body);
    }

    this.logger.info(`← ${res.status} ${path}`, { rid, duration: `${Date.now() - start}ms` });

    // Handle 204 No Content
    if (res.status === 204) {
      return undefined as T;
    }

    return res.json() as Promise<T>;
  }

  // Auth
  async sendCode(email: string): Promise<void> {
    await this.fetch("/auth/send-code", {
      method: "POST",
      body: JSON.stringify({ email }),
    });
  }

  async verifyCode(email: string, code: string): Promise<LoginResponse> {
    return this.fetch("/auth/verify-code", {
      method: "POST",
      body: JSON.stringify({ email, code }),
    });
  }

  async googleLogin(code: string, redirectUri: string): Promise<LoginResponse> {
    return this.fetch("/auth/google", {
      method: "POST",
      body: JSON.stringify({ code, redirect_uri: redirectUri }),
    });
  }

  async logout(): Promise<void> {
    await this.fetch("/auth/logout", { method: "POST" });
  }

  async issueCliToken(): Promise<{ token: string }> {
    return this.fetch("/api/cli-token", { method: "POST" });
  }

  async getMe(): Promise<User> {
    return this.fetch("/api/me");
  }

  async markOnboardingComplete(payload?: {
    completion_path?: OnboardingCompletionPath;
    workspace_id?: string;
  }): Promise<User> {
    return this.fetch("/api/me/onboarding/complete", {
      method: "POST",
      body: payload ? JSON.stringify(payload) : undefined,
    });
  }

  async joinCloudWaitlist(payload: {
    email: string;
    reason?: string;
  }): Promise<User> {
    return this.fetch("/api/me/onboarding/cloud-waitlist", {
      method: "POST",
      body: JSON.stringify(payload),
    });
  }

  async patchOnboarding(payload: {
    questionnaire?: Record<string, unknown>;
  }): Promise<User> {
    return this.fetch("/api/me/onboarding", {
      method: "PATCH",
      body: JSON.stringify(payload),
    });
  }

  /**
   * Imports the Getting Started project + optional welcome issue + sub-issues
   * in a single server-side transaction. Gated by an atomic
   * starter_content_state: NULL → 'imported' claim — a second call returns
   * 409 (already decided) and creates nothing new.
   *
   * The content templates live in TypeScript (see
   * @multica/views/onboarding/utils/starter-content-templates) and are
   * rendered from the user's questionnaire answers before being sent.
   */
  async importStarterContent(
    payload: ImportStarterContentPayload,
  ): Promise<ImportStarterContentResponse> {
    return this.fetch("/api/me/starter-content/import", {
      method: "POST",
      body: JSON.stringify(payload),
    });
  }

  async dismissStarterContent(payload?: {
    workspace_id?: string;
  }): Promise<User> {
    return this.fetch("/api/me/starter-content/dismiss", {
      method: "POST",
      body: payload ? JSON.stringify(payload) : undefined,
    });
  }

  async updateMe(data: UpdateMeRequest): Promise<User> {
    return this.fetch("/api/me", {
      method: "PATCH",
      body: JSON.stringify(data),
    });
  }

  // User communication profile (JEH-304). 404 from GET means "no profile set"
  // — callers should fall back to the default profile.
  async getMyProfile(): Promise<UserProfileResponse | null> {
    try {
      return await this.fetch<UserProfileResponse>("/api/me/profile");
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) return null;
      throw err;
    }
  }

  async upsertMyProfile(data: UserProfileRequest): Promise<UserProfileResponse> {
    return this.fetch("/api/me/profile", {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  async deleteMyProfile(): Promise<void> {
    await this.fetch("/api/me/profile", { method: "DELETE" });
  }

  // Fork-specific: per-user preferences (composer keybinding, etc.)
  async updateMyPreferences(patch: Record<string, unknown>): Promise<User> {
    return this.fetch("/api/me/preferences", {
      method: "PATCH",
      body: JSON.stringify(patch),
    });
  }

  // CEREBRO-PATCH(feature-flags-client): per-(workspace, user) feature flag overrides.
  // Server returns ONLY the overrides — defaults are applied client-side
  // from the cerebro-feature-flags registry.
  async listFeatureFlags(wsId: string): Promise<{ overrides: Record<string, boolean> }> {
    return this.fetch(`/api/workspaces/${wsId}/feature-flags`);
  }

  async setFeatureFlag(wsId: string, key: string, enabled: boolean): Promise<void> {
    await this.fetch(`/api/workspaces/${wsId}/feature-flags/${key}`, {
      method: "PUT",
      body: JSON.stringify({ enabled }),
    });
  }

  // CEREBRO-PATCH(cerebro-account-client): JEH-921 workspace accounts CRUD.
  async listCerebroAccounts(wsId: string): Promise<CerebroAccount[]> {
    return this.fetch(`/api/workspaces/${wsId}/accounts`);
  }

  async createCerebroAccount(
    wsId: string,
    body: CreateCerebroAccountRequest,
  ): Promise<CerebroAccount> {
    return this.fetch(`/api/workspaces/${wsId}/accounts`, {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  async deleteCerebroAccount(wsId: string, id: string): Promise<void> {
    await this.fetch(`/api/workspaces/${wsId}/accounts/${id}`, {
      method: "DELETE",
    });
  }

  // CEREBRO-PATCH(cerebro-account-client): JEH-998 UI-driven control toggles.
  async updateCerebroAccountControls(
    wsId: string,
    id: string,
    body: UpdateCerebroAccountControlsRequest,
  ): Promise<CerebroAccount> {
    return this.fetch(`/api/workspaces/${wsId}/accounts/${id}/controls`, {
      method: "PATCH",
      body: JSON.stringify(body),
    });
  }

  // CEREBRO-PATCH(cerebro-credentials-client): JEH-1199 read-only credential
  // registry methods. Bodies are `unknown` so the cerebro-credentials package
  // owns the schema via parseWithFallback (the API Response Compatibility
  // rule in CLAUDE.md). Mutating endpoints (create/reveal/rotate/delete)
  // are not exposed here yet — the admin UI today only reads.
  async listCerebroCredentials<T = unknown>(wsId: string): Promise<T> {
    return this.fetch<T>(`/api/workspaces/${wsId}/credentials`);
  }
  async listCerebroCredentialAudit<T = unknown>(
    wsId: string,
    credId: string,
    limit = 100,
  ): Promise<T> {
    return this.fetch<T>(
      `/api/workspaces/${wsId}/credentials/${credId}/audit?limit=${limit}`,
    );
  }
  async listCerebroCredentialBindings<T = unknown>(
    wsId: string,
    credId: string,
  ): Promise<T> {
    return this.fetch<T>(`/api/workspaces/${wsId}/credentials/${credId}/bindings`);
  }

  // CEREBRO-PATCH(cerebro-groups-client): JEH-1006 workspace groups CRUD + member management.
  // Endpoints are mounted by `cerebro-groups-routes` in server/cmd/server/router.go.
  // Server paths are NOT under /api/cerebro/* — they live on the generic
  // /api/workspaces/{id}/groups (workspace-scoped) and /api/groups/{id}
  // (workspace-membership-gated) routes; matching them here is what makes
  // the GroupsTab actually reach the handler.
  async listCerebroGroups<T = unknown>(wsId: string): Promise<T> {
    return this.fetch<T>(`/api/workspaces/${wsId}/groups`);
  }

  async createCerebroGroup<T = unknown>(
    wsId: string,
    body: { name: string; description?: string | null },
  ): Promise<T> {
    return this.fetch<T>(`/api/workspaces/${wsId}/groups`, {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  async updateCerebroGroup<T = unknown>(
    groupId: string,
    body: { name?: string; description?: string | null },
  ): Promise<T> {
    return this.fetch<T>(`/api/groups/${groupId}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    });
  }

  async deleteCerebroGroup(groupId: string): Promise<void> {
    await this.fetch(`/api/groups/${groupId}`, { method: "DELETE" });
  }

  async listCerebroGroupMembers<T = unknown>(groupId: string): Promise<T> {
    return this.fetch<T>(`/api/groups/${groupId}/members`);
  }

  async addCerebroGroupMember<T = unknown>(
    groupId: string,
    userId: string,
  ): Promise<T> {
    return this.fetch<T>(`/api/groups/${groupId}/members`, {
      method: "POST",
      body: JSON.stringify({ user_id: userId }),
    });
  }

  async removeCerebroGroupMember(
    groupId: string,
    userId: string,
  ): Promise<void> {
    await this.fetch(`/api/groups/${groupId}/members/${userId}`, {
      method: "DELETE",
    });
  }

  // CEREBRO-PATCH(cerebro-group-permissions-client): JEH-1009 wrappers for
  // capability / runtime-allowlist / agent-allowlist / project-group-access
  // endpoints. Server routes are registered in router.go under
  // `cerebro-group-permissions-routes` (JEH-1008).
  async listCerebroGroupCapabilities<T = unknown>(groupId: string): Promise<T> {
    return this.fetch<T>(`/api/groups/${groupId}/capabilities`);
  }

  async setCerebroGroupCapability<T = unknown>(
    groupId: string,
    capability: string,
  ): Promise<T> {
    return this.fetch<T>(`/api/groups/${groupId}/capabilities`, {
      method: "POST",
      body: JSON.stringify({ capability }),
    });
  }

  async removeCerebroGroupCapability(
    groupId: string,
    capability: string,
  ): Promise<void> {
    await this.fetch(
      `/api/groups/${groupId}/capabilities/${capability}`,
      { method: "DELETE" },
    );
  }

  async listCerebroGroupRuntimes<T = unknown>(groupId: string): Promise<T> {
    return this.fetch<T>(`/api/groups/${groupId}/runtimes`);
  }

  async addCerebroGroupRuntime<T = unknown>(
    groupId: string,
    runtimeId: string,
  ): Promise<T> {
    return this.fetch<T>(`/api/groups/${groupId}/runtimes`, {
      method: "POST",
      body: JSON.stringify({ runtime_id: runtimeId }),
    });
  }

  async removeCerebroGroupRuntime(
    groupId: string,
    runtimeId: string,
  ): Promise<void> {
    await this.fetch(
      `/api/groups/${groupId}/runtimes/${runtimeId}`,
      { method: "DELETE" },
    );
  }

  async listCerebroGroupAgents<T = unknown>(groupId: string): Promise<T> {
    return this.fetch<T>(`/api/groups/${groupId}/agents`);
  }

  async addCerebroGroupAgent<T = unknown>(
    groupId: string,
    agentId: string,
  ): Promise<T> {
    return this.fetch<T>(`/api/groups/${groupId}/agents`, {
      method: "POST",
      body: JSON.stringify({ agent_id: agentId }),
    });
  }

  async removeCerebroGroupAgent(
    groupId: string,
    agentId: string,
  ): Promise<void> {
    await this.fetch(
      `/api/groups/${groupId}/agents/${agentId}`,
      { method: "DELETE" },
    );
  }

  async listCerebroProjectGroups<T = unknown>(projectId: string): Promise<T> {
    return this.fetch<T>(`/api/projects/${projectId}/group-access`);
  }

  async addCerebroProjectGroup<T = unknown>(
    projectId: string,
    groupId: string,
  ): Promise<T> {
    return this.fetch<T>(`/api/projects/${projectId}/group-access`, {
      method: "POST",
      body: JSON.stringify({ group_id: groupId }),
    });
  }

  async removeCerebroProjectGroup(
    projectId: string,
    groupId: string,
  ): Promise<void> {
    await this.fetch(
      `/api/projects/${projectId}/group-access/${groupId}`,
      { method: "DELETE" },
    );
  }

  // CEREBRO-PATCH(cerebro-dashboard-client): JEH-684 dashboard overview endpoint
  async getCerebroDashboardOverview<T = unknown>(
    range: "24h" | "7d" | "30d",
    // CEREBRO-PATCH(cerebro-dashboard-client): actor filtering for JEH-684 dashboard drilldown
    filter?: { actor_type?: "member" | "agent"; actor_id?: string | null },
  ): Promise<T> {
    const params = new URLSearchParams({ range });
    if (filter?.actor_type) params.set("actor_type", filter.actor_type);
    if (filter?.actor_id) params.set("actor_id", filter.actor_id);
    return this.fetch<T>(`/api/cerebro/dashboard?${params.toString()}`);
  }

  // CEREBRO-PATCH(cerebro-tasks-client): JEH-900 cross-agent tasks list endpoint
  async getCerebroTasks<T = unknown>(filter: {
    agent_id?: string | null;
    issue_id?: string | null; // CEREBRO-PATCH(cerebro-tasks-issue-filter): JEH-1297
    project_id?: string | null; // CEREBRO-PATCH(cerebro-tasks-project-filter): JEH-1297
    status?: string | null;
    type?: "issue" | "chat" | null;
    since?: string | null;
    until?: string | null; // CEREBRO-PATCH(cerebro-tasks-until): JEH-1297 custom date range end
    limit?: number;
    offset?: number;
    // CEREBRO-PATCH(cerebro-tasks-search): JEH-1237 server-side full-page search param
    q?: string | null;
  }): Promise<T> {
    const params = new URLSearchParams();
    if (filter.agent_id) params.set("agent_id", filter.agent_id);
    if (filter.issue_id) params.set("issue_id", filter.issue_id);
    if (filter.project_id) params.set("project_id", filter.project_id);
    if (filter.status) params.set("status", filter.status);
    if (filter.type) params.set("type", filter.type);
    if (filter.since) params.set("since", filter.since);
    if (filter.until) params.set("until", filter.until);
    if (filter.limit !== undefined) params.set("limit", String(filter.limit));
    if (filter.offset !== undefined) params.set("offset", String(filter.offset));
    if (filter.q) params.set("q", filter.q);
    const qs = params.toString();
    return this.fetch<T>(qs ? `/api/cerebro/tasks?${qs}` : `/api/cerebro/tasks`);
  }

  // CEREBRO-PATCH(cerebro-workflows-client): JEH-1047 workflow engine REST surface (PR 2/3).
  async listCerebroWorkflows<T = unknown>(): Promise<T> {
    return this.fetch<T>("/api/cerebro/workflows");
  }
  async getCerebroWorkflow<T = unknown>(id: string): Promise<T> {
    return this.fetch<T>(`/api/cerebro/workflows/${id}`);
  }
  async createCerebroWorkflow<T = unknown>(payload: unknown): Promise<T> {
    return this.fetch<T>("/api/cerebro/workflows", {
      method: "POST",
      body: JSON.stringify(payload),
    });
  }
  async updateCerebroWorkflow<T = unknown>(id: string, payload: unknown): Promise<T> {
    return this.fetch<T>(`/api/cerebro/workflows/${id}`, {
      method: "PUT",
      body: JSON.stringify(payload),
    });
  }
  async toggleCerebroWorkflow<T = unknown>(id: string, enabled: boolean): Promise<T> {
    return this.fetch<T>(`/api/cerebro/workflows/${id}/toggle`, {
      method: "POST",
      body: JSON.stringify({ enabled }),
    });
  }
  async deleteCerebroWorkflow(id: string): Promise<void> {
    await this.fetch<void>(`/api/cerebro/workflows/${id}`, { method: "DELETE" });
  }
  async listCerebroWorkflowRuns<T = unknown>(filter: {
    workflowId?: string | null;
    limit?: number;
    offset?: number;
  }): Promise<T> {
    const params = new URLSearchParams();
    if (filter.limit !== undefined) params.set("limit", String(filter.limit));
    if (filter.offset !== undefined) params.set("offset", String(filter.offset));
    const qs = params.toString();
    const base = filter.workflowId
      ? `/api/cerebro/workflows/${filter.workflowId}/runs`
      : `/api/cerebro/workflows/runs`;
    return this.fetch<T>(qs ? `${base}?${qs}` : base);
  }
  // CEREBRO-PATCH(cerebro-workflows-client): JEH-1108 phase-3 regenerate endpoints.
  async regenerateCerebroInboundToken<T = unknown>(id: string): Promise<T> {
    return this.fetch<T>(`/api/cerebro/workflows/${id}/regenerate-token`, { method: "POST" });
  }
  async regenerateCerebroInboundSigningSecret<T = unknown>(id: string): Promise<T> {
    return this.fetch<T>(`/api/cerebro/workflows/${id}/regenerate-signing-secret`, {
      method: "POST",
    });
  }
  async regenerateCerebroOutboundSecret<T = unknown>(id: string): Promise<T> {
    return this.fetch<T>(`/api/cerebro/workflows/${id}/regenerate-outbound-secret`, {
      method: "POST",
    });
  }

  // CEREBRO-PATCH(cerebro-persona-grants-client): JEH-1180 Persona grant
  // control plane UI. Endpoints mirror the JEH-1179 description:
  //   GET    /api/workspaces/{id}/grants[?subject_type=…&subject_id=…&resource_type=…&status=…&classification=…]
  //   POST   /api/workspaces/{id}/grants
  //   GET    /api/workspaces/{id}/grants/{grantId}
  //   PATCH  /api/workspaces/{id}/grants/{grantId}
  //   DELETE /api/workspaces/{id}/grants/{grantId}
  //   GET    /api/workspaces/{id}/grants/audit[?subject_id=…&grant_id=…&since=…&limit=…]
  // Bodies are `unknown` so the cerebro-permissions package owns the
  // schema; parseWithFallback handles drift if Fætta's final API-shape
  // lands different.
  async listPersonaGrants<T = unknown>(
    wsId: string,
    filter: {
      subject_type?: string | null;
      subject_id?: string | null;
      resource_type?: string | null;
      status?: string | null;
      classification?: string | null;
      limit?: number;
      offset?: number;
    } = {},
  ): Promise<T> {
    const params = new URLSearchParams();
    if (filter.subject_type) params.set("subject_type", filter.subject_type);
    if (filter.subject_id) params.set("subject_id", filter.subject_id);
    if (filter.resource_type) params.set("resource_type", filter.resource_type);
    if (filter.status) params.set("status", filter.status);
    if (filter.classification) params.set("classification", filter.classification);
    if (filter.limit !== undefined) params.set("limit", String(filter.limit));
    if (filter.offset !== undefined) params.set("offset", String(filter.offset));
    const qs = params.toString();
    return this.fetch<T>(
      qs
        ? `/api/workspaces/${wsId}/grants?${qs}`
        : `/api/workspaces/${wsId}/grants`,
    );
  }

  async getPersonaGrant<T = unknown>(
    wsId: string,
    grantId: string,
  ): Promise<T> {
    return this.fetch<T>(`/api/workspaces/${wsId}/grants/${grantId}`);
  }

  async createPersonaGrant<T = unknown>(
    wsId: string,
    body: unknown,
  ): Promise<T> {
    return this.fetch<T>(`/api/workspaces/${wsId}/grants`, {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  async updatePersonaGrant<T = unknown>(
    wsId: string,
    grantId: string,
    body: unknown,
  ): Promise<T> {
    return this.fetch<T>(`/api/workspaces/${wsId}/grants/${grantId}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    });
  }

  async deletePersonaGrant(wsId: string, grantId: string): Promise<void> {
    await this.fetch<void>(`/api/workspaces/${wsId}/grants/${grantId}`, {
      method: "DELETE",
    });
  }

  async listPersonaGrantAudit<T = unknown>(
    wsId: string,
    filter: {
      subject_id?: string | null;
      grant_id?: string | null;
      since?: string | null;
      limit?: number;
      offset?: number;
    } = {},
  ): Promise<T> {
    const params = new URLSearchParams();
    if (filter.subject_id) params.set("subject_id", filter.subject_id);
    if (filter.grant_id) params.set("grant_id", filter.grant_id);
    if (filter.since) params.set("since", filter.since);
    if (filter.limit !== undefined) params.set("limit", String(filter.limit));
    if (filter.offset !== undefined) params.set("offset", String(filter.offset));
    const qs = params.toString();
    return this.fetch<T>(
      qs
        ? `/api/workspaces/${wsId}/grants/audit?${qs}`
        : `/api/workspaces/${wsId}/grants/audit`,
    );
  }

  // Web Push (per-device subscriptions). The server returns enabled=false
  // when VAPID keys aren't configured — callers should hide the subscribe UI
  // in that case.
  async getPushPublicKey(): Promise<{ enabled: boolean; publicKey?: string }> {
    return this.fetch("/api/push/public-key");
  }

  async listPushSubscriptions(): Promise<PushSubscriptionResponse[]> {
    return this.fetch("/api/push/subscriptions");
  }

  async subscribePush(subscription: {
    endpoint: string;
    keys: { p256dh: string; auth: string };
    userAgent?: string;
  }): Promise<PushSubscriptionResponse> {
    return this.fetch("/api/push/subscribe", {
      method: "POST",
      body: JSON.stringify(subscription),
    });
  }

  async unsubscribePush(endpoint: string): Promise<void> {
    await this.fetch("/api/push/unsubscribe", {
      method: "POST",
      body: JSON.stringify({ endpoint }),
    });
  }


  // Issues
  async listIssues(params?: ListIssuesParams): Promise<ListIssuesResponse> {
    const search = new URLSearchParams();
    if (params?.limit) search.set("limit", String(params.limit));
    if (params?.offset) search.set("offset", String(params.offset));
    if (params?.workspace_id) search.set("workspace_id", params.workspace_id);
    if (params?.status) search.set("status", params.status);
    if (params?.priority) search.set("priority", params.priority);
    if (params?.assignee_id) search.set("assignee_id", params.assignee_id);
    if (params?.assignee_ids?.length) search.set("assignee_ids", params.assignee_ids.join(","));
    if (params?.creator_id) search.set("creator_id", params.creator_id);
    if (params?.project_id) search.set("project_id", params.project_id);
    if (params?.open_only) search.set("open_only", "true");
    const path = `/api/issues?${search}`;
    const raw = await this.fetch<unknown>(path);
    return parseWithFallback(raw, ListIssuesResponseSchema, EMPTY_LIST_ISSUES_RESPONSE, {
      endpoint: "GET /api/issues",
    });
  }

  async searchIssues(params: { q: string; limit?: number; offset?: number; include_closed?: boolean; signal?: AbortSignal }): Promise<SearchIssuesResponse> {
    const search = new URLSearchParams({ q: params.q });
    if (params.limit !== undefined) search.set("limit", String(params.limit));
    if (params.offset !== undefined) search.set("offset", String(params.offset));
    if (params.include_closed) search.set("include_closed", "true");
    return this.fetch(`/api/issues/search?${search}`, params.signal ? { signal: params.signal } : undefined);
  }

  async searchProjects(params: { q: string; limit?: number; offset?: number; include_closed?: boolean; signal?: AbortSignal }): Promise<SearchProjectsResponse> {
    const search = new URLSearchParams({ q: params.q });
    if (params.limit !== undefined) search.set("limit", String(params.limit));
    if (params.offset !== undefined) search.set("offset", String(params.offset));
    if (params.include_closed) search.set("include_closed", "true");
    return this.fetch(`/api/projects/search?${search}`, params.signal ? { signal: params.signal } : undefined);
  }

  async getIssue(id: string): Promise<Issue> {
    return this.fetch(`/api/issues/${id}`);
  }

  async createIssue(data: CreateIssueRequest): Promise<Issue> {
    return this.fetch("/api/issues", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async quickCreateIssue(data: { agent_id: string; prompt: string; project_id?: string | null }): Promise<{ task_id: string }> {
    return this.fetch("/api/issues/quick-create", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async createFeedback(data: {
    message: string;
    url?: string;
    workspace_id?: string;
  }): Promise<{ id: string; created_at: string }> {
    return this.fetch("/api/feedback", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async updateIssue(id: string, data: UpdateIssueRequest): Promise<Issue> {
    return this.fetch(`/api/issues/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  async listChildIssues(id: string): Promise<{ issues: Issue[] }> {
    const raw = await this.fetch<unknown>(`/api/issues/${id}/children`);
    return parseWithFallback(raw, ChildIssuesResponseSchema, { issues: [] }, {
      endpoint: "GET /api/issues/:id/children",
    });
  }

  async getChildIssueProgress(): Promise<{ progress: { parent_issue_id: string; total: number; done: number }[] }> {
    return this.fetch("/api/issues/child-progress");
  }

  async deleteIssue(id: string): Promise<void> {
    await this.fetch(`/api/issues/${id}`, { method: "DELETE" });
  }

  async batchUpdateIssues(issueIds: string[], updates: UpdateIssueRequest): Promise<{ updated: number }> {
    return this.fetch("/api/issues/batch-update", {
      method: "POST",
      body: JSON.stringify({ issue_ids: issueIds, updates }),
    });
  }

  async batchDeleteIssues(issueIds: string[]): Promise<{ deleted: number }> {
    return this.fetch("/api/issues/batch-delete", {
      method: "POST",
      body: JSON.stringify({ issue_ids: issueIds }),
    });
  }

  // Comments
  async listComments(issueId: string): Promise<Comment[]> {
    const raw = await this.fetch<unknown>(`/api/issues/${issueId}/comments`);
    return parseWithFallback(raw, CommentsListSchema, [], {
      endpoint: "GET /api/issues/:id/comments",
    });
  }

  async createComment(issueId: string, content: string, type?: string, parentId?: string, attachmentIds?: string[]): Promise<Comment> {
    return this.fetch(`/api/issues/${issueId}/comments`, {
      method: "POST",
      body: JSON.stringify({
        content,
        type: type ?? "comment",
        ...(parentId ? { parent_id: parentId } : {}),
        ...(attachmentIds?.length ? { attachment_ids: attachmentIds } : {}),
      }),
    });
  }

  async listTimeline(issueId: string): Promise<TimelineEntry[]> {
    const raw = await this.fetch<unknown>(
      `/api/issues/${issueId}/timeline`,
    );
    return parseWithFallback(raw, TimelineEntriesSchema, EMPTY_TIMELINE_ENTRIES, {
      endpoint: "GET /api/issues/:id/timeline",
    });
  }

  async getAssigneeFrequency(): Promise<AssigneeFrequencyEntry[]> {
    return this.fetch("/api/assignee-frequency");
  }

  async updateComment(commentId: string, content: string): Promise<Comment> {
    return this.fetch(`/api/comments/${commentId}`, {
      method: "PUT",
      body: JSON.stringify({ content }),
    });
  }

  async deleteComment(commentId: string): Promise<void> {
    await this.fetch(`/api/comments/${commentId}`, { method: "DELETE" });
  }

  async resolveComment(commentId: string): Promise<Comment> {
    return this.fetch(`/api/comments/${commentId}/resolve`, { method: "POST" });
  }

  async unresolveComment(commentId: string): Promise<Comment> {
    return this.fetch(`/api/comments/${commentId}/resolve`, { method: "DELETE" });
  }

  async addReaction(commentId: string, emoji: string): Promise<Reaction> {
    return this.fetch(`/api/comments/${commentId}/reactions`, {
      method: "POST",
      body: JSON.stringify({ emoji }),
    });
  }

  async removeReaction(commentId: string, emoji: string): Promise<void> {
    await this.fetch(`/api/comments/${commentId}/reactions`, {
      method: "DELETE",
      body: JSON.stringify({ emoji }),
    });
  }

  async addIssueReaction(issueId: string, emoji: string): Promise<IssueReaction> {
    return this.fetch(`/api/issues/${issueId}/reactions`, {
      method: "POST",
      body: JSON.stringify({ emoji }),
    });
  }

  async removeIssueReaction(issueId: string, emoji: string): Promise<void> {
    await this.fetch(`/api/issues/${issueId}/reactions`, {
      method: "DELETE",
      body: JSON.stringify({ emoji }),
    });
  }

  // Subscribers
  async listIssueSubscribers(issueId: string): Promise<IssueSubscriber[]> {
    const raw = await this.fetch<unknown>(`/api/issues/${issueId}/subscribers`);
    return parseWithFallback(raw, SubscribersListSchema, [], {
      endpoint: "GET /api/issues/:id/subscribers",
    });
  }

  async subscribeToIssue(issueId: string, userId?: string, userType?: string): Promise<void> {
    const body: Record<string, string> = {};
    if (userId) body.user_id = userId;
    if (userType) body.user_type = userType;
    await this.fetch(`/api/issues/${issueId}/subscribe`, {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  async unsubscribeFromIssue(issueId: string, userId?: string, userType?: string): Promise<void> {
    const body: Record<string, string> = {};
    if (userId) body.user_id = userId;
    if (userType) body.user_type = userType;
    await this.fetch(`/api/issues/${issueId}/unsubscribe`, {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  // Agents
  async listAgents(params?: { workspace_id?: string; include_archived?: boolean }): Promise<Agent[]> {
    const search = new URLSearchParams();
    if (params?.workspace_id) search.set("workspace_id", params.workspace_id);
    if (params?.include_archived) search.set("include_archived", "true");
    return this.fetch(`/api/agents?${search}`);
  }

  async getAgent(id: string): Promise<Agent> {
    return this.fetch(`/api/agents/${id}`);
  }

  async createAgent(data: CreateAgentRequest): Promise<Agent> {
    return this.fetch("/api/agents", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async updateAgent(id: string, data: UpdateAgentRequest): Promise<Agent> {
    return this.fetch(`/api/agents/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  // Persona pass-through: lists sandboxes the operator can attach to an
  // agent. Returns an empty list when persona is not configured server-side.
  async listPersonaSandboxes(): Promise<{ name: string; description: string; system_owned: boolean }[]> {
    const res = await this.fetch<{ sandboxes?: { name: string; description: string; system_owned: boolean }[] }>("/api/persona/sandboxes");
    return res.sandboxes ?? [];
  }

  async archiveAgent(id: string): Promise<Agent> {
    return this.fetch(`/api/agents/${id}/archive`, { method: "POST" });
  }

  async restoreAgent(id: string): Promise<Agent> {
    return this.fetch(`/api/agents/${id}/restore`, { method: "POST" });
  }

  // Bulk-cancel every active task (queued/dispatched/running) for the agent.
  // Permission: agent owner or workspace admin/owner. Server returns the
  // count of cancelled rows; broadcasts task:cancelled for each so other
  // surfaces can clear their live cards.
  async cancelAgentTasks(id: string): Promise<{ cancelled: number }> {
    return this.fetch(`/api/agents/${id}/cancel-tasks`, { method: "POST" });
  }

  async listRuntimes(params?: { workspace_id?: string; owner?: "me" }): Promise<AgentRuntime[]> {
    const search = new URLSearchParams();
    if (params?.workspace_id) search.set("workspace_id", params.workspace_id);
    if (params?.owner) search.set("owner", params.owner);
    return this.fetch(`/api/runtimes?${search}`);
  }

  async deleteRuntime(runtimeId: string): Promise<void> {
    await this.fetch(`/api/runtimes/${runtimeId}`, { method: "DELETE" });
  }

  // CEREBRO-PATCH(api-client-runtime-pause): cerebro pause/unpause endpoints.
  async pauseRuntime(runtimeId: string, body: { unpause_at?: string; reason?: string } = {}): Promise<AgentRuntime> {
    return this.fetch(`/api/runtimes/${runtimeId}/pause`, { method: "POST", body: JSON.stringify(body) });
  }

  // CEREBRO-PATCH(api-client-runtime-pause): cerebro pause/unpause endpoints.
  async unpauseRuntime(runtimeId: string): Promise<AgentRuntime> {
    return this.fetch(`/api/runtimes/${runtimeId}/unpause`, { method: "POST" });
  }

  async updateRuntimeSandbox(
    runtimeId: string,
    sandboxEnabled: boolean | null,
  ): Promise<AgentRuntime> {
    return this.fetch(`/api/runtimes/${runtimeId}/sandbox`, {
      method: "PATCH",
      body: JSON.stringify({ sandbox_enabled: sandboxEnabled }),
    });
  }

  // updateRuntimePersonaSandbox sets (or clears, via empty string) the
// CEREBRO-PATCH(client): persona integration additions.
  // runtime-level persona sandbox cap (E1). Server-side the field is gated to
  // workspace owner/admin; non-admins get a 403 the UI surfaces inline.
  async updateRuntimePersonaSandbox(
    runtimeId: string,
    personaSandbox: string,
  ): Promise<AgentRuntime> {
    return this.fetch(`/api/runtimes/${runtimeId}/persona-sandbox`, {
      method: "PATCH",
      body: JSON.stringify({ persona_sandbox: personaSandbox }),
    });
  }

  async getRuntimeUsage(runtimeId: string, params?: { days?: number }): Promise<RuntimeUsage[]> {
    const search = new URLSearchParams();
    if (params?.days) search.set("days", String(params.days));
    return this.fetch(`/api/runtimes/${runtimeId}/usage?${search}`);
  }

  async getRuntimeTaskActivity(runtimeId: string): Promise<RuntimeHourlyActivity[]> {
    return this.fetch(`/api/runtimes/${runtimeId}/activity`);
  }

  async getRuntimeUsageByAgent(
    runtimeId: string,
    params?: { days?: number },
  ): Promise<RuntimeUsageByAgent[]> {
    const search = new URLSearchParams();
    if (params?.days) search.set("days", String(params.days));
    return this.fetch(`/api/runtimes/${runtimeId}/usage/by-agent?${search}`);
  }

  async getRuntimeUsageByHour(
    runtimeId: string,
    params?: { days?: number },
  ): Promise<RuntimeUsageByHour[]> {
    const search = new URLSearchParams();
    if (params?.days) search.set("days", String(params.days));
    return this.fetch(`/api/runtimes/${runtimeId}/usage/by-hour?${search}`);
  }

  async initiateUpdate(
    runtimeId: string,
    targetVersion: string,
  ): Promise<RuntimeUpdate> {
    return this.fetch(`/api/runtimes/${runtimeId}/update`, {
      method: "POST",
      body: JSON.stringify({ target_version: targetVersion }),
    });
  }

  async getUpdateResult(
    runtimeId: string,
    updateId: string,
  ): Promise<RuntimeUpdate> {
    return this.fetch(`/api/runtimes/${runtimeId}/update/${updateId}`);
  }

  async initiateListModels(runtimeId: string): Promise<RuntimeModelListRequest> {
    return this.fetch(`/api/runtimes/${runtimeId}/models`, { method: "POST" });
  }

  async getListModelsResult(
    runtimeId: string,
    requestId: string,
  ): Promise<RuntimeModelListRequest> {
    return this.fetch(`/api/runtimes/${runtimeId}/models/${requestId}`);
  }

  async initiateListLocalSkills(
    runtimeId: string,
  ): Promise<RuntimeLocalSkillListRequest> {
    return this.fetch(`/api/runtimes/${runtimeId}/local-skills`, {
      method: "POST",
    });
  }

  async getListLocalSkillsResult(
    runtimeId: string,
    requestId: string,
  ): Promise<RuntimeLocalSkillListRequest> {
    return this.fetch(`/api/runtimes/${runtimeId}/local-skills/${requestId}`);
  }

  async initiateImportLocalSkill(
    runtimeId: string,
    data: CreateRuntimeLocalSkillImportRequest,
  ): Promise<RuntimeLocalSkillImportRequest> {
    return this.fetch(`/api/runtimes/${runtimeId}/local-skills/import`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async getImportLocalSkillResult(
    runtimeId: string,
    requestId: string,
  ): Promise<RuntimeLocalSkillImportRequest> {
    return this.fetch(`/api/runtimes/${runtimeId}/local-skills/import/${requestId}`);
  }

  async listAgentTasks(agentId: string): Promise<AgentTask[]> {
    return this.fetch(`/api/agents/${agentId}/tasks`);
  }

  // Workspace-scoped agent task snapshot: every active task
  // (queued/dispatched/running) plus each agent's most recent terminal task.
  // Powers the front-end's "active wins, else latest terminal" presence
  // derivation; one fetch backs every per-agent presence read in the app.
  // Workspace is resolved server-side from the X-Workspace-Slug header.
  async getAgentTaskSnapshot(): Promise<AgentTask[]> {
    return this.fetch(`/api/agent-task-snapshot`);
  }

  // Per-agent daily activity for the last 30 days, anchored on
  // completed_at. One workspace-wide fetch backs both the Agents-list
  // sparkline (uses trailing 7 buckets) and the agent detail "Last 30
  // days" panel (uses all 30).
  async getWorkspaceAgentActivity30d(): Promise<AgentActivityBucket[]> {
    return this.fetch(`/api/agent-activity-30d`);
  }

  // Per-agent 30-day total run count for the Agents-list RUNS column.
  async getWorkspaceAgentRunCounts(): Promise<AgentRunCount[]> {
    return this.fetch(`/api/agent-run-counts`);
  }

  async getActiveTasksForIssue(issueId: string): Promise<{ tasks: AgentTask[] }> {
    return this.fetch(`/api/issues/${issueId}/active-task`);
  }

  async listTaskMessages(taskId: string): Promise<TaskMessagePayload[]> {
    return this.fetch(`/api/tasks/${taskId}/messages`);
  }

  async listTasksByIssue(issueId: string): Promise<AgentTask[]> {
    return this.fetch(`/api/issues/${issueId}/task-runs`);
  }

  async listWorkSessions(issueId: string): Promise<WorkSession[]> {
    return this.fetch(`/api/issues/${issueId}/work-sessions`);
  }

  async getWorkSessionMessages(sessionId: string): Promise<TaskMessagePayload[]> {
    return this.fetch(`/api/work-sessions/${sessionId}/messages`);
  }

  async completeWorkSession(sessionId: string, summary?: string): Promise<{ id: string; status: string }> {
    return this.fetch(`/api/work-sessions/${sessionId}/complete`, {
      method: "PUT",
      body: JSON.stringify({ summary: summary ?? "" }),
    });
  }

  async resumeWorkSession(sessionId: string): Promise<{ id: string; issue_id: string; status: string }> {
    return this.fetch(`/api/work-sessions/${sessionId}/resume`, { method: "POST" });
  }

  async forkWorkSession(sessionId: string): Promise<{ id: string; issue_id: string; forked_from: string; status: string }> {
    return this.fetch(`/api/work-sessions/${sessionId}/fork`, { method: "POST" });
  }

  async getIssueUsage(issueId: string): Promise<IssueUsageSummary> {
    return this.fetch(`/api/issues/${issueId}/usage`);
  }

  async cancelTask(issueId: string, taskId: string): Promise<AgentTask> {
    return this.fetch(`/api/issues/${issueId}/tasks/${taskId}/cancel`, {
      method: "POST",
    });
  }

  // Channels (multi-party chat — issues with kind in channel,dm)
  async listChannels(): Promise<Channel[]> {
    return this.fetch("/api/channels");
  }

  async getChannel(id: string): Promise<Channel> {
    return this.fetch(`/api/channels/${id}`);
  }

  async createChannel(data: CreateChannelRequest): Promise<Channel> {
    return this.fetch("/api/channels", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async markChannelRead(id: string): Promise<{ count: number }> {
    return this.fetch(`/api/channels/${id}/read`, { method: "POST" });
  }

  // CEREBRO-PATCH(channel-archive-client): JEH-851 — per-user channel archive
  // endpoints. Archive hides the channel from the user's channel list until
  // a new inbox_item lands for it (server clears the row in re-surface
  // listener), at which point the channel reappears.
  async archiveChannel(id: string): Promise<{ archived_at: string }> {
    return this.fetch(`/api/channels/${id}/archive`, { method: "POST" });
  }

  async unarchiveChannel(id: string): Promise<void> {
    await this.fetch(`/api/channels/${id}/archive`, { method: "DELETE" });
  }

  // CEREBRO-PATCH(channel-listen-client): JEH-699 — per (channel × agent)
  // listen-mode endpoints. Default 'always' applies when no explicit row
  // exists for an agent; the response only contains overrides, so the
  // consumer fills the default itself.
  async listChannelAgentSettings(
    channelId: string,
  ): Promise<ChannelAgentSettingsResponse> {
    return this.fetch(`/api/channels/${channelId}/agent-settings`);
  }

  async setChannelAgentListenMode(
    channelId: string,
    agentId: string,
    listenMode: ChannelAgentListenMode,
  ): Promise<ChannelAgentSetting> {
    return this.fetch(
      `/api/channels/${channelId}/agents/${agentId}/listen-mode`,
      {
        method: "PUT",
        body: JSON.stringify({ listen_mode: listenMode }),
      },
    );
  }

  async rerunIssue(issueId: string): Promise<AgentTask> {
    return this.fetch(`/api/issues/${issueId}/rerun`, {
      method: "POST",
    });
  }

  // Inbox
  async listInbox(params?: { archived?: boolean }): Promise<InboxItem[]> {
    const qs = new URLSearchParams();
    if (params?.archived) qs.set("archived", "1");
    const query = qs.toString();
    return this.fetch(`/api/inbox${query ? `?${query}` : ""}`);
  }

  // CEREBRO-PATCH(active-issue-tasks-status): extended response with per-task status for run-state pip (JEH-1332)
  async listActiveIssueTasks(): Promise<{ issue_ids: string[]; tasks?: { issue_id: string; status: string }[] }> {
    return this.fetch("/api/inbox/active-issue-tasks");
  }

  async markInboxRead(id: string): Promise<InboxItem> {
    return this.fetch(`/api/inbox/${id}/read`, { method: "POST" });
  }

  async archiveInbox(id: string): Promise<InboxItem> {
    return this.fetch(`/api/inbox/${id}/archive`, { method: "POST" });
  }

  // CEREBRO-PATCH(cerebro-inbox-unarchive): JEH-1166 — unarchive from archived view.
  async unarchiveInbox(id: string): Promise<InboxItem> {
    return this.fetch(`/api/inbox/${id}/unarchive`, { method: "POST" });
  }

  // CEREBRO-PATCH(cerebro-inbox-actions): per-item mute / unmute / mark-unread.
  // Server returns the partial item (id + flags) — clients merge it into the
  // cached row optimistically and re-fetch on settle.
  async muteInbox(id: string, mutedUntil: Date): Promise<InboxItem> {
    return this.fetch(`/api/inbox/${id}/mute`, {
      method: "POST",
      body: JSON.stringify({ muted_until: mutedUntil.toISOString() }),
    });
  }

  async unmuteInbox(id: string): Promise<InboxItem> {
    return this.fetch(`/api/inbox/${id}/mute`, { method: "DELETE" });
  }

  async markInboxUnread(id: string): Promise<InboxItem> {
    return this.fetch(`/api/inbox/${id}/unread`, { method: "POST" });
  }

  async getUnreadInboxCount(): Promise<{ count: number }> {
    return this.fetch("/api/inbox/unread-count");
  }

  // Cross-workspace unread inbox count for the OS app-icon badge. Sent
  // outside the workspace-scoped tree so it works without an active slug.
  async getMyTotalUnreadInboxCount(): Promise<{ count: number }> {
    return this.fetch("/api/me/inbox/unread-count");
  }

  async markAllInboxRead(): Promise<{ count: number }> {
    return this.fetch("/api/inbox/mark-all-read", { method: "POST" });
  }

  async archiveAllInbox(): Promise<{ count: number }> {
    return this.fetch("/api/inbox/archive-all", { method: "POST" });
  }

  async archiveAllReadInbox(): Promise<{ count: number }> {
    return this.fetch("/api/inbox/archive-all-read", { method: "POST" });
  }

  async archiveCompletedInbox(): Promise<{ count: number }> {
    return this.fetch("/api/inbox/archive-completed", { method: "POST" });
  }

  // Notifications (route='notifications'). Single-item operations
  // (mark-read, archive) reuse the inbox endpoints since the route flag is
  // set server-side at insert time and doesn't change after that.
  async listNotifications(): Promise<InboxItem[]> {
    return this.fetch("/api/inbox/notifications");
  }

  async getUnreadNotificationsCount(): Promise<{ count: number }> {
    return this.fetch("/api/inbox/notifications/unread-count");
  }

  async markAllNotificationsRead(): Promise<{ count: number }> {
    return this.fetch("/api/inbox/notifications/mark-all-read", { method: "POST" });
  }

  async archiveAllNotifications(): Promise<{ count: number }> {
    return this.fetch("/api/inbox/notifications/archive-all", { method: "POST" });
  }

  // CEREBRO-PATCH(notification-preferences-api): cerebro feature, not in upstream.
  async getNotificationPreferences(): Promise<NotificationPreferenceResponse> {
    return this.fetch("/api/notification-preferences");
  }

  async updateNotificationPreferences(preferences: NotificationPreferences): Promise<NotificationPreferenceResponse> {
    return this.fetch("/api/notification-preferences", {
      method: "PUT",
      body: JSON.stringify({ preferences }),
    });
  }

  // App Config
  async getConfig(): Promise<{
    cdn_domain: string;
    allow_signup: boolean;
    google_client_id?: string;
    posthog_key?: string;
    posthog_host?: string;
    analytics_environment?: string;
  }> {
    return this.fetch("/api/config");
  }

  // Workspaces
  async listWorkspaces(): Promise<Workspace[]> {
    return this.fetch("/api/workspaces");
  }

  async getWorkspace(id: string): Promise<Workspace> {
    return this.fetch(`/api/workspaces/${id}`);
  }

  async createWorkspace(data: { name: string; slug: string; description?: string; context?: string }): Promise<Workspace> {
    return this.fetch("/api/workspaces", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async updateWorkspace(id: string, data: { name?: string; description?: string; context?: string; settings?: Record<string, unknown>; repos?: WorkspaceRepo[] }): Promise<Workspace> {
    return this.fetch(`/api/workspaces/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    });
  }

  // Fork-specific: workspace kill switch.
  async pauseWorkspaceTasks(id: string, paused: boolean): Promise<{ workspace: Workspace; cancelled_count: number }> {
    return this.fetch(`/api/workspaces/${id}/pause-tasks`, {
      method: "POST",
      body: JSON.stringify({ paused }),
    });
  }

  // Members
  async listMembers(workspaceId: string): Promise<MemberWithUser[]> {
    return this.fetch(`/api/workspaces/${workspaceId}/members`);
  }

  async createMember(workspaceId: string, data: CreateMemberRequest): Promise<Invitation> {
    return this.fetch(`/api/workspaces/${workspaceId}/members`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async updateMember(workspaceId: string, memberId: string, data: UpdateMemberRequest): Promise<MemberWithUser> {
    return this.fetch(`/api/workspaces/${workspaceId}/members/${memberId}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    });
  }

  async deleteMember(workspaceId: string, memberId: string): Promise<void> {
    await this.fetch(`/api/workspaces/${workspaceId}/members/${memberId}`, {
      method: "DELETE",
    });
  }

  async setMemberBudgetEnforcement(
    workspaceId: string,
    memberId: string,
    enabled: boolean,
  ): Promise<MemberWithUser> {
    return this.fetch(
      `/api/workspaces/${workspaceId}/members/${memberId}/budget-enforcement`,
      { method: "PATCH", body: JSON.stringify({ enabled }) },
    );
  }

  async getMemberUsage(workspaceId: string, memberId: string): Promise<MemberUsage> {
    return this.fetch(
      `/api/workspaces/${workspaceId}/members/${memberId}/usage`,
    );
  }

  async leaveWorkspace(workspaceId: string): Promise<void> {
    await this.fetch(`/api/workspaces/${workspaceId}/leave`, {
      method: "POST",
    });
  }

  // Invitations
  async listWorkspaceInvitations(workspaceId: string): Promise<Invitation[]> {
    return this.fetch(`/api/workspaces/${workspaceId}/invitations`);
  }

  async revokeInvitation(workspaceId: string, invitationId: string): Promise<void> {
    await this.fetch(`/api/workspaces/${workspaceId}/invitations/${invitationId}`, {
      method: "DELETE",
    });
  }

  async listMyInvitations(): Promise<Invitation[]> {
    return this.fetch("/api/invitations");
  }

  async getInvitation(invitationId: string): Promise<Invitation> {
    return this.fetch(`/api/invitations/${invitationId}`);
  }

  async acceptInvitation(invitationId: string): Promise<MemberWithUser> {
    return this.fetch(`/api/invitations/${invitationId}/accept`, {
      method: "POST",
    });
  }

  async declineInvitation(invitationId: string): Promise<void> {
    await this.fetch(`/api/invitations/${invitationId}/decline`, {
      method: "POST",
    });
  }

  async deleteWorkspace(workspaceId: string): Promise<void> {
    await this.fetch(`/api/workspaces/${workspaceId}`, {
      method: "DELETE",
    });
  }

  // Skills
  async listSkills(): Promise<SkillSummary[]> {
    return this.fetch("/api/skills");
  }

  async getSkill(id: string): Promise<Skill> {
    return this.fetch(`/api/skills/${id}`);
  }

  async createSkill(data: CreateSkillRequest): Promise<Skill> {
    return this.fetch("/api/skills", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async updateSkill(id: string, data: UpdateSkillRequest): Promise<Skill> {
    return this.fetch(`/api/skills/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  async deleteSkill(id: string): Promise<void> {
    await this.fetch(`/api/skills/${id}`, { method: "DELETE" });
  }

  async importSkill(data: { url: string }): Promise<Skill> {
    return this.fetch("/api/skills/import", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async listAgentSkills(agentId: string): Promise<SkillSummary[]> {
    return this.fetch(`/api/agents/${agentId}/skills`);
  }

  async setAgentSkills(agentId: string, data: SetAgentSkillsRequest): Promise<void> {
    await this.fetch(`/api/agents/${agentId}/skills`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  // CEREBRO-PATCH(agent-tools-api): JEH-1284/1290/1353/1359 — tool grant list +
  // toggle via W3 admin API. Schema-validated per API Response Compatibility.
  async getAgentTools(agentId: string, params?: { workspace_id?: string }): Promise<import("@multica/cerebro-types").AgentTool[]> {
    const search = new URLSearchParams();
    if (params?.workspace_id) search.set("workspace_id", params.workspace_id);
    const query = search.toString();
    const path = `/api/agents/${agentId}/tools${query ? `?${query}` : ""}`;
    const raw = await this.fetch(path);
    return parseWithFallback(raw, AgentToolsListSchema, [], {
      endpoint: path,
    }) as import("@multica/cerebro-types").AgentTool[];
  }

  async updateAgentTool(agentId: string, toolName: string, data: { enabled: boolean; config?: Record<string, unknown> }, params?: { workspace_id?: string }): Promise<void> {
    const search = new URLSearchParams();
    if (params?.workspace_id) search.set("workspace_id", params.workspace_id);
    const query = search.toString();
    await this.fetch(`/api/agents/${agentId}/tools/${encodeURIComponent(toolName)}${query ? `?${query}` : ""}`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  // Skill ownership / versioning / change requests / forks (JEH-216)
  async updateSkillOwnership(id: string, data: UpdateSkillOwnershipRequest): Promise<Skill> {
    return this.fetch(`/api/skills/${id}/ownership`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  async listSkillVersions(id: string): Promise<SkillVersion[]> {
    return this.fetch(`/api/skills/${id}/versions`);
  }

  async listSkillChangeRequests(id: string): Promise<SkillChangeRequest[]> {
    return this.fetch(`/api/skills/${id}/change-requests`);
  }

  async listPendingSkillChangeRequests(): Promise<SkillChangeRequest[]> {
    return this.fetch("/api/skills/change-requests");
  }

  async createSkillChangeRequest(
    id: string,
    data: CreateSkillChangeRequestRequest,
  ): Promise<SkillChangeRequest> {
    return this.fetch(`/api/skills/${id}/change-requests`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async reviewSkillChangeRequest(
    crId: string,
    data: ReviewSkillChangeRequestRequest,
  ): Promise<SkillChangeRequest> {
    return this.fetch(`/api/skills/change-requests/${crId}/review`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async listSkillForks(id: string): Promise<SkillFork[]> {
    return this.fetch(`/api/skills/${id}/forks`);
  }

  async createSkillFork(id: string, data: ForkSkillRequest): Promise<Skill> {
    return this.fetch(`/api/skills/${id}/forks`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  // Personal Access Tokens
  async listPersonalAccessTokens(): Promise<PersonalAccessToken[]> {
    return this.fetch("/api/tokens");
  }

  async createPersonalAccessToken(data: CreatePersonalAccessTokenRequest): Promise<CreatePersonalAccessTokenResponse> {
    return this.fetch("/api/tokens", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async revokePersonalAccessToken(id: string): Promise<void> {
    await this.fetch(`/api/tokens/${id}`, { method: "DELETE" });
  }

  // Runtime setup tokens — bootstraps a new daemon via one-line installer.
  async createRuntimeSetupToken(
    data: CreateRuntimeSetupTokenRequest = {},
  ): Promise<CreateRuntimeSetupTokenResponse> {
    return this.fetch("/api/runtime-setup/tokens", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  // File Upload & Attachments
  async uploadFile(file: File, opts?: { issueId?: string; commentId?: string }): Promise<Attachment> {
    const formData = new FormData();
    formData.append("file", file);
    if (opts?.issueId) formData.append("issue_id", opts.issueId);
    if (opts?.commentId) formData.append("comment_id", opts.commentId);

    const rid = createRequestId();
    const start = Date.now();
    this.logger.info("→ POST /api/upload-file", { rid });

    const res = await fetch(`${this.baseUrl}/api/upload-file`, {
      method: "POST",
      headers: this.authHeaders(),
      body: formData,
      credentials: "include",
    });

    if (!res.ok) {
      if (res.status === 401) this.handleUnauthorized();
      const message = await this.parseErrorMessage(res, `Upload failed: ${res.status}`);
      this.logger.error(`← ${res.status} /api/upload-file`, { rid, duration: `${Date.now() - start}ms`, error: message });
      throw new Error(message);
    }

    this.logger.info(`← ${res.status} /api/upload-file`, { rid, duration: `${Date.now() - start}ms` });
    return res.json() as Promise<Attachment>;
  }

  // Chat Sessions
  async listChatSessions(params?: { status?: string }): Promise<ChatSession[]> {
    const qs = new URLSearchParams();
    if (params?.status) qs.set("status", params.status);
    const query = qs.toString() ? `?${qs.toString()}` : "";
    return this.fetch(`/api/chat/sessions${query}`);
  }

  async getChatSession(id: string): Promise<ChatSession> {
    return this.fetch(`/api/chat/sessions/${id}`);
  }

  async createChatSession(data: { agent_id: string; title?: string }): Promise<ChatSession> {
    return this.fetch("/api/chat/sessions", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async deleteChatSession(id: string): Promise<void> {
    await this.fetch(`/api/chat/sessions/${id}`, { method: "DELETE" });
  }

  // CEREBRO-PATCH(api-chat-session-actions): JEH-799 chat-session header actions.
  async updateChatSession(
    id: string,
    data: { title?: string; status?: "active" | "archived" },
  ): Promise<ChatSession> {
    return this.fetch(`/api/chat/sessions/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    });
  }

  // CEREBRO-PATCH(api-chat-session-actions): JEH-799 chat-session header actions.
  async convertChatSessionToIssue(
    id: string,
  ): Promise<{ issue_id: string; identifier: string; number: number }> {
    return this.fetch(`/api/chat/sessions/${id}/convert-to-issue`, {
      method: "POST",
    });
  }

  async listChatMessages(sessionId: string): Promise<ChatMessage[]> {
    return this.fetch(`/api/chat/sessions/${sessionId}/messages`);
  }

  async sendChatMessage(sessionId: string, content: string): Promise<SendChatMessageResponse> {
    return this.fetch(`/api/chat/sessions/${sessionId}/messages`, {
      method: "POST",
      body: JSON.stringify({ content }),
    });
  }

  async getChatSessionUsage(sessionId: string): Promise<ChatSessionUsage> {
    return this.fetch(`/api/chat/sessions/${sessionId}/usage`);
  }

  async getPendingChatTask(sessionId: string): Promise<ChatPendingTask> {
    return this.fetch(`/api/chat/sessions/${sessionId}/pending-task`);
  }

  async listPendingChatTasks(): Promise<PendingChatTasksResponse> {
    return this.fetch(`/api/chat/pending-tasks`);
  }

  async markChatSessionRead(sessionId: string): Promise<void> {
    await this.fetch(`/api/chat/sessions/${sessionId}/read`, { method: "POST" });
  }

  async cancelTaskById(taskId: string): Promise<void> {
    await this.fetch(`/api/tasks/${taskId}/cancel`, { method: "POST" });
  }

  async listAttachments(issueId: string): Promise<Attachment[]> {
    return this.fetch(`/api/issues/${issueId}/attachments`);
  }

  async getAttachment(id: string): Promise<Attachment> {
    return this.fetch(`/api/attachments/${id}`);
  }

  async deleteAttachment(id: string): Promise<void> {
    await this.fetch(`/api/attachments/${id}`, { method: "DELETE" });
  }

  // Projects
  async listProjects(params?: { status?: string }): Promise<ListProjectsResponse> {
    const search = new URLSearchParams();
    if (params?.status) search.set("status", params.status);
    return this.fetch(`/api/projects?${search}`);
  }

  async getProject(id: string): Promise<Project> {
    return this.fetch(`/api/projects/${id}`);
  }

  // CEREBRO-PATCH(nested-projects): fork-only nested project endpoints.
  async getProjectTree(): Promise<ListProjectTreeResponse> {
    return this.fetch("/api/projects/tree");
  }

  async createProject(data: CreateProjectRequest): Promise<Project> {
    return this.fetch("/api/projects", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async updateProject(id: string, data: UpdateProjectRequest): Promise<Project> {
    return this.fetch(`/api/projects/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  async setProjectParent(
    id: string,
    parentProjectId: string | null,
  ): Promise<ProjectTreeItem> {
    return this.fetch(`/api/projects/${id}/parent`, {
      method: "PUT",
      body: JSON.stringify({ parent_project_id: parentProjectId }),
    });
  }

  async setProjectShowDescendants(
    id: string,
    showDescendants: boolean,
  ): Promise<ProjectTreeItem> {
    return this.fetch(`/api/projects/${id}/show-descendants`, {
      method: "PUT",
      body: JSON.stringify({ show_descendants: showDescendants }),
    });
  }

  async getProjectRollupStats(id: string): Promise<ProjectRollupStats> {
    return this.fetch(`/api/projects/${id}/rollup-stats`);
  }

  async deleteProject(id: string): Promise<void> {
    await this.fetch(`/api/projects/${id}`, { method: "DELETE" });
  }

  async updateProjectAccess(
    id: string,
    access: "workspace" | "restricted",
  ): Promise<Project> {
    return this.fetch(`/api/projects/${id}/access`, {
      method: "PATCH",
      body: JSON.stringify({ access }),
    });
  }

  async listProjectMembers(
    id: string,
  ): Promise<{ members: ProjectMember[] }> {
    return this.fetch(`/api/projects/${id}/members`);
  }

  async addProjectMember(
    id: string,
    userId: string,
  ): Promise<{ member: unknown }> {
    return this.fetch(`/api/projects/${id}/members`, {
      method: "POST",
      body: JSON.stringify({ user_id: userId }),
    });
  }

  async removeProjectMember(id: string, userId: string): Promise<void> {
    await this.fetch(`/api/projects/${id}/members/${userId}`, {
      method: "DELETE",
    });
  }

  // Project resources
  async listProjectResources(
    projectId: string,
  ): Promise<ListProjectResourcesResponse> {
    return this.fetch(`/api/projects/${projectId}/resources`);
  }

  async createProjectResource(
    projectId: string,
    data: CreateProjectResourceRequest,
  ): Promise<ProjectResource> {
    return this.fetch(`/api/projects/${projectId}/resources`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async deleteProjectResource(
    projectId: string,
    resourceId: string,
  ): Promise<void> {
    await this.fetch(`/api/projects/${projectId}/resources/${resourceId}`, {
      method: "DELETE",
    });
  }

  async listMemberProjects(
    workspaceId: string,
    memberId: string,
  ): Promise<{
    projects: Array<{
      id: string;
      title: string;
      icon: string | null;
      color: string | null;
      added_at: string;
    }>;
  }> {
    return this.fetch(
      `/api/workspaces/${workspaceId}/members/${memberId}/projects`,
    );
  }

  // Labels
  async listLabels(): Promise<ListLabelsResponse> {
    return this.fetch(`/api/labels`);
  }

  async getLabel(id: string): Promise<Label> {
    return this.fetch(`/api/labels/${id}`);
  }

  async createLabel(data: CreateLabelRequest): Promise<Label> {
    return this.fetch(`/api/labels`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async updateLabel(id: string, data: UpdateLabelRequest): Promise<Label> {
    return this.fetch(`/api/labels/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  async deleteLabel(id: string): Promise<void> {
    await this.fetch(`/api/labels/${id}`, { method: "DELETE" });
  }

  async listLabelsForIssue(issueId: string): Promise<IssueLabelsResponse> {
    return this.fetch(`/api/issues/${issueId}/labels`);
  }

  async attachLabel(issueId: string, labelId: string): Promise<IssueLabelsResponse> {
    return this.fetch(`/api/issues/${issueId}/labels`, {
      method: "POST",
      body: JSON.stringify({ label_id: labelId }),
    });
  }

  async detachLabel(issueId: string, labelId: string): Promise<IssueLabelsResponse> {
    return this.fetch(`/api/issues/${issueId}/labels/${labelId}`, {
      method: "DELETE",
    });
  }

  // Pins
  async listPins(): Promise<PinnedItem[]> {
    return this.fetch("/api/pins");
  }

  async createPin(data: CreatePinRequest): Promise<PinnedItem> {
    return this.fetch("/api/pins", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async deletePin(itemType: PinnedItemType, itemId: string): Promise<void> {
    await this.fetch(`/api/pins/${itemType}/${itemId}`, { method: "DELETE" });
  }

  async reorderPins(data: ReorderPinsRequest): Promise<void> {
    await this.fetch("/api/pins/reorder", {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  // Autopilots
  async listAutopilots(params?: { status?: string }): Promise<ListAutopilotsResponse> {
    const search = new URLSearchParams();
    if (params?.status) search.set("status", params.status);
    return this.fetch(`/api/autopilots?${search}`);
  }

  async getAutopilot(id: string): Promise<GetAutopilotResponse> {
    return this.fetch(`/api/autopilots/${id}`);
  }

  async createAutopilot(data: CreateAutopilotRequest): Promise<Autopilot> {
    return this.fetch("/api/autopilots", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async updateAutopilot(id: string, data: UpdateAutopilotRequest): Promise<Autopilot> {
    return this.fetch(`/api/autopilots/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    });
  }

  async deleteAutopilot(id: string): Promise<void> {
    await this.fetch(`/api/autopilots/${id}`, { method: "DELETE" });
  }

  async triggerAutopilot(id: string): Promise<AutopilotRun> {
    return this.fetch(`/api/autopilots/${id}/trigger`, { method: "POST" });
  }

  async listAutopilotRuns(id: string, params?: { limit?: number; offset?: number }): Promise<ListAutopilotRunsResponse> {
    const search = new URLSearchParams();
    if (params?.limit) search.set("limit", params.limit.toString());
    if (params?.offset) search.set("offset", params.offset.toString());
    return this.fetch(`/api/autopilots/${id}/runs?${search}`);
  }

  async createAutopilotTrigger(autopilotId: string, data: CreateAutopilotTriggerRequest): Promise<AutopilotTrigger> {
    return this.fetch(`/api/autopilots/${autopilotId}/triggers`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async updateAutopilotTrigger(autopilotId: string, triggerId: string, data: UpdateAutopilotTriggerRequest): Promise<AutopilotTrigger> {
    return this.fetch(`/api/autopilots/${autopilotId}/triggers/${triggerId}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    });
  }

  async deleteAutopilotTrigger(autopilotId: string, triggerId: string): Promise<void> {
    await this.fetch(`/api/autopilots/${autopilotId}/triggers/${triggerId}`, { method: "DELETE" });
  }

  // Artifacts
  async createArtifact(data: CreateArtifactRequest): Promise<Artifact> {
    return this.fetch("/api/artifacts", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async getArtifact(id: string): Promise<Artifact> {
    return this.fetch(`/api/artifacts/${id}`);
  }

  async listArtifactsByIssue(issueId: string): Promise<Artifact[]> {
    return this.fetch(`/api/issues/${issueId}/artifacts`);
  }

  async listArtifactsByProject(projectId: string): Promise<Artifact[]> {
    return this.fetch(`/api/projects/${projectId}/artifacts`);
  }

  async updateArtifact(id: string, data: UpdateArtifactRequest): Promise<Artifact> {
    return this.fetch(`/api/artifacts/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  async deleteArtifact(id: string): Promise<void> {
    await this.fetch(`/api/artifacts/${id}`, { method: "DELETE" });
  }

  async searchArtifacts(params?: ListArtifactsParams): Promise<Artifact[]> {
    const search = new URLSearchParams();
    if (params?.kind) search.set("kind", params.kind);
    if (params?.scope) search.set("scope", params.scope);
    if (params?.author_type) search.set("author_type", params.author_type);
    if (params?.author_id) search.set("author_id", params.author_id);
    if (params?.origin_issue_id)
      search.set("origin_issue_id", params.origin_issue_id);
    if (params?.q) search.set("q", params.q);
    if (params?.limit != null) search.set("limit", String(params.limit));
    if (params?.offset != null) search.set("offset", String(params.offset));
    const qs = search.toString();
    return this.fetch(`/api/artifacts${qs ? `?${qs}` : ""}`);
  }

  async updateArtifactScope(
    id: string,
    data: UpdateArtifactScopeRequest,
  ): Promise<Artifact> {
    return this.fetch(`/api/artifacts/${id}/scope`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  async moveArtifactToFolder(
    id: string,
    data: MoveArtifactToFolderRequest,
  ): Promise<Artifact> {
    return this.fetch(`/api/artifacts/${id}/folder`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  // Artifact folders
  async listArtifactFolders(): Promise<ArtifactFolder[]> {
    return this.fetch("/api/artifact-folders");
  }

  async createArtifactFolder(
    data: CreateArtifactFolderRequest,
  ): Promise<ArtifactFolder> {
    return this.fetch("/api/artifact-folders", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async updateArtifactFolder(
    id: string,
    data: UpdateArtifactFolderRequest,
  ): Promise<ArtifactFolder> {
    return this.fetch(`/api/artifact-folders/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  async deleteArtifactFolder(id: string): Promise<void> {
    await this.fetch(`/api/artifact-folders/${id}`, { method: "DELETE" });
  }

  async uploadArtifactFile(file: File): Promise<ArtifactUploadResponse> {
    const formData = new FormData();
    formData.append("file", file);

    const rid = createRequestId();
    const start = Date.now();
    this.logger.info("→ POST /api/artifact-uploads", { rid });

    const res = await fetch(`${this.baseUrl}/api/artifact-uploads`, {
      method: "POST",
      headers: this.authHeaders(),
      body: formData,
      credentials: "include",
    });

    if (!res.ok) {
      if (res.status === 401) this.handleUnauthorized();
      const message = await this.parseErrorMessage(res, `Upload failed: ${res.status}`);
      this.logger.error(`← ${res.status} /api/artifact-uploads`, { rid, duration: `${Date.now() - start}ms`, error: message });
      throw new ApiError(message, res.status, res.statusText);
    }

    this.logger.info(`← ${res.status} /api/artifact-uploads`, { rid, duration: `${Date.now() - start}ms` });
    return res.json() as Promise<ArtifactUploadResponse>;
  }
}
