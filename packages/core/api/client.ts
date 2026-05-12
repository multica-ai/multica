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
  User,
  Skill,
  SkillSummary,
  CreateSkillRequest,
  UpdateSkillRequest,
  SetAgentSkillsRequest,
  PersonalAccessToken,
  CreatePersonalAccessTokenRequest,
  CreatePersonalAccessTokenResponse,
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
  ChatSession,
  ChatMessage,
  ChatPendingTask,
  PendingChatTasksResponse,
  SendChatMessageResponse,
  Project,
  CreateProjectRequest,
  UpdateProjectRequest,
  ListProjectsResponse,
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
  NotificationPreferenceResponse,
  NotificationPreferences,
  Channel,
  ChannelMembership,
  ChannelMessage,
  ChannelReaction,
  ChannelMessageThread,
  ChannelSearchHit,
  CreateChannelRequest,
  UpdateChannelRequest,
  AddChannelMemberRequest,
  CreateChannelMessageRequest,
  MarkChannelReadRequest,
  CreateOrFetchDMRequest,
  MemoryArtifact,
  CreateMemoryArtifactRequest,
  UpdateMemoryArtifactRequest,
  ListMemoryArtifactsParams,
  ListMemoryArtifactsResponse,
  SearchMemoryArtifactsParams,
  MemoryArtifactAnchorType,
  Deploy,
  DeployEnvironment,
  CreateDeployEnvironmentRequest,
  UpdateDeployEnvironmentRequest,
  LogDeployRequest,
  ListDeployAdaptersResponse,
  ConfigureDeployAdapterRequest,
  ConfigureDeployAdapterResponse,
  PollDeployEnvironmentResponse,
  RollbackDeployRequest,
  ListShipProjectsResponse,
  ListPullRequestsResponse,
  SyncPullRequestsResult,
  ListDeployEnvironmentsResponse,
  ListDeploysResponse,
  PullRequestState,
  WebhookSecretResponse,
  ActionResult,
  ListShipCardActionsResponse,
  MergePullRequestRequest,
  CommentPullRequestRequest,
  DismissPullRequestReviewRequest,
  NudgePullRequestAuthorRequest,
  RunSmokeTestsRequest,
  ClosePullRequestAsStaleRequest,
  SubmitPullRequestReviewRequest,
  UpdatePullRequestRequest,
  LinkedIssuesResponse,
  TalkToAgentRequest,
  TalkToAgentResponse,
  ListPullRequestStacksResponse,
  ShipHubSummary,
  DeployPreflight,
  CreatePreflightRequest,
  UpdatePreflightRequest,
  PromoteDeployPreflightResponse,
  ShipSnapshotResponse,
  CreateReleaseRequest,
  CreateReleaseResponse,
  Release,
  ReleaseDetailResponse,
  ListReleasesResponse,
  UpdateReleaseRequest,
  AddPullRequestToReleaseRequest,
  CancelReleaseRequest,
  StartMergeRequest,
  ResumeMergeRequest,
  AbortMergeRequest,
  MergeStateResponse,
  RunReleaseSmokeTestsRequest,
  MarkSmokePassRequest,
  MarkReleaseVerifiedRequest,
  UnverifyReleaseRequest,
  PromoteReleaseRequest,
  RollbackReleaseRequest,
  ReleaseHealth,
  PullRequestDetailsResponse,
} from "../types";
import type { OnboardingCompletionPath } from "../onboarding/types";
import { type Logger, noopLogger } from "../logger";
import { createRequestId } from "../utils";
import { getCurrentSlug } from "../platform/workspace-storage";
import { parseWithFallback } from "./schema";
import {
  AttachmentResponseSchema,
  ChildIssuesResponseSchema,
  CommentsListSchema,
  EMPTY_ATTACHMENT,
  EMPTY_LIST_ISSUES_RESPONSE,
  EMPTY_TIMELINE_ENTRIES,
  ListIssuesResponseSchema,
  SubscribersListSchema,
  TimelineEntriesSchema,
  ListShipProjectsResponseSchema,
  ListPullRequestsResponseSchema,
  ListDeployEnvironmentsResponseSchema,
  ListDeploysResponseSchema,
  EMPTY_LIST_SHIP_PROJECTS_RESPONSE,
  EMPTY_LIST_PULL_REQUESTS_RESPONSE,
  EMPTY_LIST_DEPLOY_ENVIRONMENTS_RESPONSE,
  EMPTY_LIST_DEPLOYS_RESPONSE,
  ListDeployAdaptersResponseSchema,
  EMPTY_LIST_DEPLOY_ADAPTERS_RESPONSE,
  ConfigureDeployAdapterResponseSchema,
  EMPTY_CONFIGURE_DEPLOY_ADAPTER_RESPONSE,
  PollDeployEnvironmentResponseSchema,
  EMPTY_POLL_DEPLOY_ENVIRONMENT_RESPONSE,
  WebhookSecretResponseSchema,
  EMPTY_WEBHOOK_SECRET_RESPONSE,
  ActionResultSchema,
  EMPTY_ACTION_RESULT,
  ListShipCardActionsResponseSchema,
  EMPTY_LIST_SHIP_CARD_ACTIONS_RESPONSE,
  LinkedIssuesResponseSchema,
  EMPTY_LINKED_ISSUES_RESPONSE,
  ListPullRequestStacksResponseSchema,
  EMPTY_LIST_PULL_REQUEST_STACKS_RESPONSE,
  TalkToAgentResponseSchema,
  EMPTY_TALK_TO_AGENT_RESPONSE,
  ShipHubSummarySchema,
  EMPTY_SHIP_HUB_SUMMARY,
  DeployPreflightSchema,
  PromoteDeployPreflightResponseSchema,
  ShipSnapshotResponseSchema,
  EMPTY_SHIP_SNAPSHOT_RESPONSE,
  CreateReleaseResponseSchema,
  EMPTY_CREATE_RELEASE_RESPONSE,
  ListReleasesResponseSchema,
  EMPTY_LIST_RELEASES_RESPONSE,
  ReleaseDetailResponseSchema,
  EMPTY_RELEASE_DETAIL,
  EMPTY_RELEASE_HEALTH,
  ReleaseSchema,
  ReleaseHealthSchema,
  MergeStateResponseSchema,
  EMPTY_MERGE_STATE_RESPONSE,
  PullRequestDetailsResponseSchema,
  EMPTY_PULL_REQUEST_DETAILS_RESPONSE,
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
      const logLevel = res.status === 404 ? "warn" : "error";
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

  async updateRuntime(
    runtimeId: string,
    patch: { timezone?: string; visibility?: "private" | "public" },
  ): Promise<AgentRuntime> {
    return this.fetch(`/api/runtimes/${runtimeId}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
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

  async getIssueUsage(issueId: string): Promise<IssueUsageSummary> {
    return this.fetch(`/api/issues/${issueId}/usage`);
  }

  async cancelTask(issueId: string, taskId: string): Promise<AgentTask> {
    return this.fetch(`/api/issues/${issueId}/tasks/${taskId}/cancel`, {
      method: "POST",
    });
  }

  async rerunIssue(issueId: string): Promise<AgentTask> {
    return this.fetch(`/api/issues/${issueId}/rerun`, {
      method: "POST",
    });
  }

  // Inbox
  async listInbox(): Promise<InboxItem[]> {
    return this.fetch("/api/inbox");
  }

  async markInboxRead(id: string): Promise<InboxItem> {
    return this.fetch(`/api/inbox/${id}/read`, { method: "POST" });
  }

  async archiveInbox(id: string): Promise<InboxItem> {
    return this.fetch(`/api/inbox/${id}/archive`, { method: "POST" });
  }

  async getUnreadInboxCount(): Promise<{ count: number }> {
    return this.fetch("/api/inbox/unread-count");
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

  // Notification preferences
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

  async updateWorkspace(
    id: string,
    data: {
      name?: string;
      description?: string;
      context?: string;
      settings?: Record<string, unknown>;
      repos?: WorkspaceRepo[];
      // Channels feature flag — see migration 065 and the channels spec.
      // Pair with channels_enabled_set=true to actually mutate; otherwise
      // a missing field would leave the value untouched (handler convention).
      channels_enabled?: boolean;
      channels_enabled_set?: boolean;
      channel_retention_days?: number | null;
      channel_retention_days_set?: boolean;
      // Paired-bool pattern so callers can distinguish "don't touch" from
      // "explicitly clear to null". Pass orchestrator_agent_id_set=true and
      // orchestrator_agent_id=null to clear; orchestrator_agent_id="<uuid>"
      // to set; both fields omitted to leave the value untouched.
      orchestrator_agent_id?: string | null;
      orchestrator_agent_id_set?: boolean;
      // Ship Hub feature gate. Same paired-bool pattern as channels_enabled —
      // pair with ship_hub_enabled_set=true to actually mutate.
      ship_hub_enabled?: boolean;
      ship_hub_enabled_set?: boolean;
      // GitHub PAT for Ship Hub. Write-only — the server never echoes the
      // token back; the read path returns only `github_token_set: bool`.
      // Pair with github_token_set=true and github_token=null to clear, or
      // github_token="ghp_..." to overwrite. Both fields omitted leaves the
      // existing token untouched.
      github_token?: string | null;
      github_token_set?: boolean;
      // Phase 7d follow-up — per-risk-tier approval rule. Each tier
      // uses the paired-bool gate (FooSet=true to apply; missing means
      // "leave alone"). Server validates the value against the same
      // enum the SQL CHECK enforces, so a typo round-trips as a 400
      // not a 500.
      ship_hub_approval_low?: string | null;
      ship_hub_approval_low_set?: boolean;
      ship_hub_approval_medium?: string | null;
      ship_hub_approval_medium_set?: boolean;
      ship_hub_approval_high?: string | null;
      ship_hub_approval_high_set?: boolean;
      ship_hub_approval_critical?: string | null;
      ship_hub_approval_critical_set?: boolean;
      ship_hub_approver_can_be_author?: boolean | null;
      ship_hub_approver_can_be_author_set?: boolean;
      // Phase 7d follow-up — auto-detect deploys via GitHub Actions
      // polling. Pair with the *_set flag to actually mutate; pass
      // `null` (or `""`) with set=true to clear and turn auto-detect
      // off (manual Mark-deployed becomes the active path).
      ship_hub_deploy_workflow_staging?: string | null;
      ship_hub_deploy_workflow_staging_set?: boolean;
      ship_hub_deploy_workflow_production?: string | null;
      ship_hub_deploy_workflow_production_set?: boolean;
    },
  ): Promise<Workspace> {
    return this.fetch(`/api/workspaces/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
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
    const query = params?.status ? `?status=${params.status}` : "";
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

  async listChatMessages(sessionId: string): Promise<ChatMessage[]> {
    return this.fetch(`/api/chat/sessions/${sessionId}/messages`);
  }

  async sendChatMessage(sessionId: string, content: string): Promise<SendChatMessageResponse> {
    return this.fetch(`/api/chat/sessions/${sessionId}/messages`, {
      method: "POST",
      body: JSON.stringify({ content }),
    });
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

  // Channels (multi-participant chat). The handler responds 404 to every
  // endpoint when workspace.channels_enabled is FALSE — callers should gate
  // on that flag before invoking these.

  async listChannels(): Promise<Channel[]> {
    return this.fetch(`/api/channels`);
  }

  async getChannel(id: string): Promise<Channel> {
    return this.fetch(`/api/channels/${id}`);
  }

  async createChannel(data: CreateChannelRequest): Promise<Channel> {
    return this.fetch(`/api/channels`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async updateChannel(id: string, data: UpdateChannelRequest): Promise<Channel> {
    return this.fetch(`/api/channels/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    });
  }

  async archiveChannel(id: string): Promise<void> {
    await this.fetch(`/api/channels/${id}`, { method: "DELETE" });
  }

  async listChannelMembers(channelId: string): Promise<ChannelMembership[]> {
    return this.fetch(`/api/channels/${channelId}/members`);
  }

  async addChannelMember(channelId: string, data: AddChannelMemberRequest): Promise<ChannelMembership> {
    return this.fetch(`/api/channels/${channelId}/members`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async removeChannelMember(channelId: string, memberType: string, memberId: string): Promise<void> {
    await this.fetch(`/api/channels/${channelId}/members/${memberType}/${memberId}`, { method: "DELETE" });
  }

  async listChannelMessages(channelId: string, params?: { before?: string; limit?: number; includeThreaded?: boolean }): Promise<ChannelMessage[]> {
    const search = new URLSearchParams();
    if (params?.before) search.set("before", params.before);
    if (params?.limit) search.set("limit", String(params.limit));
    if (params?.includeThreaded) search.set("include_threaded", "true");
    const qs = search.toString();
    return this.fetch(`/api/channels/${channelId}/messages${qs ? `?${qs}` : ""}`);
  }

  async sendChannelMessage(channelId: string, data: CreateChannelMessageRequest): Promise<ChannelMessage> {
    return this.fetch(`/api/channels/${channelId}/messages`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async markChannelRead(channelId: string, data: MarkChannelReadRequest): Promise<void> {
    await this.fetch(`/api/channels/${channelId}/read`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async createOrFetchDM(data: CreateOrFetchDMRequest): Promise<Channel> {
    return this.fetch(`/api/dms`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  // Phase 4: threads + reactions

  async updateChannelMessage(channelId: string, messageId: string, content: string): Promise<ChannelMessage> {
    return this.fetch(`/api/channels/${channelId}/messages/${messageId}`, {
      method: "PATCH",
      body: JSON.stringify({ content }),
    });
  }

  async deleteChannelMessage(channelId: string, messageId: string): Promise<void> {
    await this.fetch(`/api/channels/${channelId}/messages/${messageId}`, {
      method: "DELETE",
    });
  }

  async searchChannelMessages(params: {
    q: string;
    channelId?: string;
    limit?: number;
    offset?: number;
  }): Promise<ChannelSearchHit[]> {
    const search = new URLSearchParams();
    search.set("q", params.q);
    if (params.channelId) search.set("channel_id", params.channelId);
    if (params.limit) search.set("limit", String(params.limit));
    if (params.offset) search.set("offset", String(params.offset));
    return this.fetch(`/api/channels/search?${search}`);
  }

  async getChannelMessageThread(channelId: string, messageId: string): Promise<ChannelMessageThread> {
    return this.fetch(`/api/channels/${channelId}/messages/${messageId}/thread`);
  }

  async dispatchThreadIssueTask(
    channelId: string,
    messageId: string,
    data: {
      agent_id: string;
      project_id?: string;
      parent_issue_id?: string;
      instruction?: string;
    },
  ): Promise<{ task_id: string }> {
    return this.fetch(
      `/api/channels/${channelId}/messages/${messageId}/dispatch-issue-task`,
      {
        method: "POST",
        body: JSON.stringify(data),
      },
    );
  }

  async addChannelReaction(channelId: string, messageId: string, emoji: string): Promise<ChannelReaction> {
    return this.fetch(`/api/channels/${channelId}/messages/${messageId}/reactions`, {
      method: "POST",
      body: JSON.stringify({ emoji }),
    });
  }

  async removeChannelReaction(channelId: string, messageId: string, emoji: string): Promise<void> {
    await this.fetch(`/api/channels/${channelId}/messages/${messageId}/reactions`, {
      method: "DELETE",
      body: JSON.stringify({ emoji }),
    });
  }

  async listAttachments(issueId: string): Promise<Attachment[]> {
    return this.fetch(`/api/issues/${issueId}/attachments`);
  }

  // Fetches a fresh attachment metadata record. The server re-signs
  // `download_url` on every call (30 min expiry), so the click-time
  // download flow uses this endpoint to avoid handing the user a stale
  // signed URL cached in TanStack Query.
  async getAttachment(id: string): Promise<Attachment> {
    const raw = await this.fetch<unknown>(`/api/attachments/${id}`);
    return parseWithFallback(raw, AttachmentResponseSchema, EMPTY_ATTACHMENT, {
      endpoint: "GET /api/attachments/{id}",
    });
  }

  async deleteAttachment(id: string): Promise<void> {
    await this.fetch(`/api/attachments/${id}`, { method: "DELETE" });
  }

  // Projects
  async listProjects(params?: { status?: string; include_archived?: boolean }): Promise<ListProjectsResponse> {
    const search = new URLSearchParams();
    if (params?.status) search.set("status", params.status);
    // Default false: archived projects are hidden from the default list.
    // The "Show archived" toggle on the projects page sets this true.
    if (params?.include_archived) search.set("include_archived", "true");
    return this.fetch(`/api/projects?${search}`);
  }

  async getProject(id: string): Promise<Project> {
    return this.fetch(`/api/projects/${id}`);
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

  async deleteProject(id: string): Promise<void> {
    await this.fetch(`/api/projects/${id}`, { method: "DELETE" });
  }

  /**
   * Soft-delete the project: stamps archived_at + archived_by. Issue
   * references and resources stay attached. Reversible via restoreProject.
   */
  async archiveProject(id: string): Promise<Project> {
    return this.fetch(`/api/projects/${id}/archive`, { method: "POST" });
  }

  /**
   * Reverse archiveProject — clear archived_at + archived_by.
   */
  async restoreProject(id: string): Promise<Project> {
    return this.fetch(`/api/projects/${id}/restore`, { method: "POST" });
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

  // Memory artifacts — workspace-scoped, kind-discriminated markdown
  // primitives (wiki pages, agent notes, runbooks, decision records).
  // Server contract lives in server/internal/handler/memory_artifact.go.
  async listMemoryArtifacts(
    params?: ListMemoryArtifactsParams,
  ): Promise<ListMemoryArtifactsResponse> {
    const search = new URLSearchParams();
    if (params?.kind) search.set("kind", params.kind);
    if (params?.parent_id) search.set("parent_id", params.parent_id);
    if (params?.include_archived) search.set("include_archived", "true");
    if (params?.limit !== undefined) search.set("limit", String(params.limit));
    if (params?.offset !== undefined) search.set("offset", String(params.offset));
    const qs = search.toString();
    return this.fetch(`/api/memory${qs ? `?${qs}` : ""}`);
  }

  async getMemoryArtifact(id: string): Promise<MemoryArtifact> {
    return this.fetch(`/api/memory/${id}`);
  }

  // "Show me everything anchored to issue X" — used by the daemon's
  // runtime context injection and by issue/project detail pages.
  async listMemoryArtifactsByAnchor(
    anchorType: MemoryArtifactAnchorType,
    anchorId: string,
    params?: { limit?: number },
  ): Promise<ListMemoryArtifactsResponse> {
    const search = new URLSearchParams();
    if (params?.limit !== undefined) search.set("limit", String(params.limit));
    const qs = search.toString();
    return this.fetch(
      `/api/memory/by-anchor/${anchorType}/${anchorId}${qs ? `?${qs}` : ""}`,
    );
  }

  async searchMemoryArtifacts(
    params: SearchMemoryArtifactsParams,
  ): Promise<ListMemoryArtifactsResponse> {
    const search = new URLSearchParams();
    search.set("q", params.q);
    if (params.kind) search.set("kind", params.kind);
    if (params.limit !== undefined) search.set("limit", String(params.limit));
    if (params.offset !== undefined) search.set("offset", String(params.offset));
    return this.fetch(`/api/memory/search?${search.toString()}`);
  }

  async createMemoryArtifact(
    data: CreateMemoryArtifactRequest,
  ): Promise<MemoryArtifact> {
    return this.fetch("/api/memory", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async updateMemoryArtifact(
    id: string,
    data: UpdateMemoryArtifactRequest,
  ): Promise<MemoryArtifact> {
    return this.fetch(`/api/memory/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  async archiveMemoryArtifact(id: string): Promise<MemoryArtifact> {
    return this.fetch(`/api/memory/${id}/archive`, { method: "POST" });
  }

  async restoreMemoryArtifact(id: string): Promise<MemoryArtifact> {
    return this.fetch(`/api/memory/${id}/restore`, { method: "POST" });
  }

  async verifyMemoryArtifact(id: string): Promise<MemoryArtifact> {
    return this.fetch(`/api/memory/${id}/verify`, { method: "POST" });
  }

  async deleteMemoryArtifact(id: string): Promise<void> {
    await this.fetch(`/api/memory/${id}`, { method: "DELETE" });
  }

  // Ship Hub — GitHub PR Kanban + deploy strip. Every endpoint 404s when
  // workspace.ship_hub_enabled is false; the UI gates the surface upstream
  // so the request is never even sent in that case.
  async listShipProjects(): Promise<ListShipProjectsResponse> {
    const raw = await this.fetch<unknown>("/api/ship/projects");
    return parseWithFallback(
      raw,
      ListShipProjectsResponseSchema,
      EMPTY_LIST_SHIP_PROJECTS_RESPONSE,
      { endpoint: "GET /api/ship/projects" },
    );
  }

  async listProjectPullRequests(
    projectId: string,
    params?: { state?: PullRequestState | "all" },
  ): Promise<ListPullRequestsResponse> {
    const search = new URLSearchParams();
    if (params?.state) search.set("state", params.state);
    const qs = search.toString();
    const raw = await this.fetch<unknown>(
      `/api/projects/${projectId}/pull_requests${qs ? `?${qs}` : ""}`,
    );
    return parseWithFallback(
      raw,
      ListPullRequestsResponseSchema,
      EMPTY_LIST_PULL_REQUESTS_RESPONSE,
      { endpoint: "GET /api/projects/:id/pull_requests" },
    );
  }

  async syncProjectPullRequests(projectId: string): Promise<SyncPullRequestsResult> {
    return this.fetch(`/api/projects/${projectId}/pull_requests/sync`, {
      method: "POST",
    });
  }

  async listProjectDeployEnvironments(
    projectId: string,
  ): Promise<ListDeployEnvironmentsResponse> {
    const raw = await this.fetch<unknown>(
      `/api/projects/${projectId}/deploy_environments`,
    );
    return parseWithFallback(
      raw,
      ListDeployEnvironmentsResponseSchema,
      EMPTY_LIST_DEPLOY_ENVIRONMENTS_RESPONSE,
      { endpoint: "GET /api/projects/:id/deploy_environments" },
    );
  }

  async upsertProjectDeployEnvironment(
    projectId: string,
    data: CreateDeployEnvironmentRequest,
  ): Promise<DeployEnvironment> {
    return this.fetch(`/api/projects/${projectId}/deploy_environments`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async updateDeployEnvironment(
    environmentId: string,
    data: UpdateDeployEnvironmentRequest,
  ): Promise<DeployEnvironment> {
    return this.fetch(`/api/deploy_environments/${environmentId}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    });
  }

  async logDeploy(
    environmentId: string,
    data: LogDeployRequest,
  ): Promise<Deploy> {
    return this.fetch(`/api/deploy_environments/${environmentId}/deploys`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  // Phase 6 — multi-adapter deploy. The dialog calls listDeployAdapters
  // to populate the dropdown; configureDeployAdapter persists the
  // adapter kind + encrypted config; pollDeployEnvironment forces a
  // refresh against the provider's API; rollbackDeployEnvironment
  // dispatches a redeploy of a prior SHA.
  async listDeployAdapters(): Promise<ListDeployAdaptersResponse> {
    const raw = await this.fetch<unknown>("/api/deploy/adapters");
    return parseWithFallback(
      raw,
      ListDeployAdaptersResponseSchema,
      EMPTY_LIST_DEPLOY_ADAPTERS_RESPONSE,
      { endpoint: "GET /api/deploy/adapters" },
    );
  }

  async configureDeployAdapter(
    environmentId: string,
    data: ConfigureDeployAdapterRequest,
  ): Promise<ConfigureDeployAdapterResponse> {
    const raw = await this.fetch<unknown>(
      `/api/deploy_environments/${environmentId}/adapter`,
      {
        method: "PUT",
        body: JSON.stringify(data),
      },
    );
    return parseWithFallback(
      raw,
      ConfigureDeployAdapterResponseSchema,
      EMPTY_CONFIGURE_DEPLOY_ADAPTER_RESPONSE,
      { endpoint: "PUT /api/deploy_environments/:id/adapter" },
    );
  }

  async pollDeployEnvironment(
    environmentId: string,
  ): Promise<PollDeployEnvironmentResponse> {
    const raw = await this.fetch<unknown>(
      `/api/deploy_environments/${environmentId}/poll_now`,
      { method: "POST" },
    );
    return parseWithFallback(
      raw,
      PollDeployEnvironmentResponseSchema,
      EMPTY_POLL_DEPLOY_ENVIRONMENT_RESPONSE,
      { endpoint: "POST /api/deploy_environments/:id/poll_now" },
    );
  }

  async rollbackDeployEnvironment(
    environmentId: string,
    data: RollbackDeployRequest,
  ): Promise<{ environment_id: string; target_sha: string; deploy_id?: string }> {
    return this.fetch(`/api/deploy_environments/${environmentId}/rollback`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async listDeploys(
    environmentId: string,
    params?: { limit?: number },
  ): Promise<ListDeploysResponse> {
    const search = new URLSearchParams();
    if (params?.limit !== undefined) search.set("limit", String(params.limit));
    const qs = search.toString();
    const raw = await this.fetch<unknown>(
      `/api/deploy_environments/${environmentId}/deploys${qs ? `?${qs}` : ""}`,
    );
    return parseWithFallback(
      raw,
      ListDeploysResponseSchema,
      EMPTY_LIST_DEPLOYS_RESPONSE,
      { endpoint: "GET /api/deploy_environments/:id/deploys" },
    );
  }

  // Phase 7a — Releases. The Ship Hub release object groups a set
  // of PRs through staging → production. Phase 7a is read + create
  // only; phases 7b/7c/7d add merge train + promotions.
  async createRelease(
    projectId: string,
    data: CreateReleaseRequest,
  ): Promise<CreateReleaseResponse> {
    const raw = await this.fetch<unknown>(
      `/api/projects/${projectId}/releases`,
      { method: "POST", body: JSON.stringify(data) },
    );
    return parseWithFallback(
      raw,
      CreateReleaseResponseSchema,
      EMPTY_CREATE_RELEASE_RESPONSE,
      { endpoint: "POST /api/projects/:id/releases" },
    );
  }

  async getRelease(releaseId: string): Promise<ReleaseDetailResponse> {
    const raw = await this.fetch<unknown>(`/api/releases/${releaseId}`);
    return parseWithFallback(
      raw,
      ReleaseDetailResponseSchema,
      EMPTY_RELEASE_DETAIL,
      { endpoint: "GET /api/releases/:id" },
    );
  }

  async listProjectReleases(
    projectId: string,
    params?: { status?: "active" | "all" },
  ): Promise<ListReleasesResponse> {
    const search = new URLSearchParams();
    if (params?.status) search.set("status", params.status);
    const qs = search.toString();
    const raw = await this.fetch<unknown>(
      `/api/projects/${projectId}/releases${qs ? `?${qs}` : ""}`,
    );
    return parseWithFallback(
      raw,
      ListReleasesResponseSchema,
      EMPTY_LIST_RELEASES_RESPONSE,
      { endpoint: "GET /api/projects/:id/releases" },
    );
  }

  async listWorkspaceActiveReleases(
    workspaceId: string,
  ): Promise<ListReleasesResponse> {
    const raw = await this.fetch<unknown>(
      `/api/workspaces/${workspaceId}/releases/active`,
    );
    return parseWithFallback(
      raw,
      ListReleasesResponseSchema,
      EMPTY_LIST_RELEASES_RESPONSE,
      { endpoint: "GET /api/workspaces/:id/releases/active" },
    );
  }

  async updateRelease(
    releaseId: string,
    data: UpdateReleaseRequest,
  ): Promise<Release> {
    const raw = await this.fetch<unknown>(`/api/releases/${releaseId}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    });
    return parseWithFallback(
      raw,
      ReleaseSchema,
      EMPTY_RELEASE_DETAIL.release,
      { endpoint: "PATCH /api/releases/:id" },
    );
  }

  async addPullRequestToRelease(
    releaseId: string,
    data: AddPullRequestToReleaseRequest,
  ): Promise<unknown> {
    return this.fetch(`/api/releases/${releaseId}/pull_requests`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async removePullRequestFromRelease(
    releaseId: string,
    pullRequestId: string,
  ): Promise<void> {
    await this.fetch(
      `/api/releases/${releaseId}/pull_requests/${pullRequestId}`,
      { method: "DELETE" },
    );
  }

  async cancelRelease(
    releaseId: string,
    data?: CancelReleaseRequest,
  ): Promise<Release> {
    const raw = await this.fetch<unknown>(`/api/releases/${releaseId}/cancel`, {
      method: "POST",
      body: JSON.stringify(data ?? {}),
    });
    return parseWithFallback(
      raw,
      ReleaseSchema,
      EMPTY_RELEASE_DETAIL.release,
      { endpoint: "POST /api/releases/:id/cancel" },
    );
  }

  // Phase 7b — Merge train. start / resume / abort all return
  // 202 Accepted with a small status payload; the actual orchestration
  // runs server-side. Clients listen on WS or poll merge_state for
  // progress.
  async startReleaseMerge(
    releaseId: string,
    data?: StartMergeRequest,
  ): Promise<unknown> {
    return this.fetch(`/api/releases/${releaseId}/start_merge`, {
      method: "POST",
      body: JSON.stringify(data ?? {}),
    });
  }

  async resumeReleaseMerge(
    releaseId: string,
    data?: ResumeMergeRequest,
  ): Promise<unknown> {
    return this.fetch(`/api/releases/${releaseId}/resume_merge`, {
      method: "POST",
      body: JSON.stringify(data ?? {}),
    });
  }

  async abortReleaseMerge(
    releaseId: string,
    data?: AbortMergeRequest,
  ): Promise<Release> {
    const raw = await this.fetch<unknown>(
      `/api/releases/${releaseId}/abort_merge`,
      {
        method: "POST",
        body: JSON.stringify(data ?? {}),
      },
    );
    return parseWithFallback(
      raw,
      ReleaseSchema,
      EMPTY_RELEASE_DETAIL.release,
      { endpoint: "POST /api/releases/:id/abort_merge" },
    );
  }

  async getReleaseMergeState(releaseId: string): Promise<MergeStateResponse> {
    const raw = await this.fetch<unknown>(
      `/api/releases/${releaseId}/merge_state`,
    );
    return parseWithFallback(
      raw,
      MergeStateResponseSchema,
      EMPTY_MERGE_STATE_RESPONSE,
      { endpoint: "GET /api/releases/:id/merge_state" },
    );
  }

  // Phase 7c — Staging deploy linkage + smoke + verify gate. Each
  // mutation parses the response through ReleaseSchema so a server-
  // side drift in one of the new fields downgrades to a usable shape
  // rather than throwing at the call site.
  async runReleaseSmokeTests(
    releaseId: string,
    data?: RunReleaseSmokeTestsRequest,
  ): Promise<Release> {
    const raw = await this.fetch<unknown>(
      `/api/releases/${releaseId}/run_smoke_tests`,
      { method: "POST", body: JSON.stringify(data ?? {}) },
    );
    return parseWithFallback(
      raw,
      ReleaseSchema,
      EMPTY_RELEASE_DETAIL.release,
      { endpoint: "POST /api/releases/:id/run_smoke_tests" },
    );
  }

  async markReleaseSmokePass(
    releaseId: string,
    data?: MarkSmokePassRequest,
  ): Promise<Release> {
    const raw = await this.fetch<unknown>(
      `/api/releases/${releaseId}/mark_smoke_pass`,
      { method: "POST", body: JSON.stringify(data ?? {}) },
    );
    return parseWithFallback(
      raw,
      ReleaseSchema,
      EMPTY_RELEASE_DETAIL.release,
      { endpoint: "POST /api/releases/:id/mark_smoke_pass" },
    );
  }

  async markReleaseVerified(
    releaseId: string,
    data?: MarkReleaseVerifiedRequest,
  ): Promise<Release> {
    const raw = await this.fetch<unknown>(
      `/api/releases/${releaseId}/mark_verified`,
      { method: "POST", body: JSON.stringify(data ?? {}) },
    );
    return parseWithFallback(
      raw,
      ReleaseSchema,
      EMPTY_RELEASE_DETAIL.release,
      { endpoint: "POST /api/releases/:id/mark_verified" },
    );
  }

  async unverifyRelease(
    releaseId: string,
    data: UnverifyReleaseRequest,
  ): Promise<Release> {
    const raw = await this.fetch<unknown>(
      `/api/releases/${releaseId}/unverify`,
      { method: "POST", body: JSON.stringify(data) },
    );
    return parseWithFallback(
      raw,
      ReleaseSchema,
      EMPTY_RELEASE_DETAIL.release,
      { endpoint: "POST /api/releases/:id/unverify" },
    );
  }

  // Phase 7c polish — manual escape hatch for repos whose CI doesn't
  // fire GitHub deployment_status events (Vercel / Netlify / Cloudflare /
  // custom CI). Synthesizes a deploy row with the release's
  // merged_main_sha and runs the same linkage flow the webhook path runs.
  async markReleaseStagingDeployed(releaseId: string): Promise<Release> {
    const raw = await this.fetch<unknown>(
      `/api/releases/${releaseId}/mark_staging_deployed`,
      { method: "POST", body: "{}" },
    );
    return parseWithFallback(
      raw,
      ReleaseSchema,
      EMPTY_RELEASE_DETAIL.release,
      { endpoint: "POST /api/releases/:id/mark_staging_deployed" },
    );
  }

  // Phase 7d — production-stage mutations. Same response-validation
  // pattern as the Phase 7c mutations.
  async promoteRelease(
    releaseId: string,
    data?: PromoteReleaseRequest,
  ): Promise<Release> {
    const raw = await this.fetch<unknown>(
      `/api/releases/${releaseId}/promote`,
      { method: "POST", body: JSON.stringify(data ?? {}) },
    );
    return parseWithFallback(
      raw,
      ReleaseSchema,
      EMPTY_RELEASE_DETAIL.release,
      { endpoint: "POST /api/releases/:id/promote" },
    );
  }

  async markReleaseProductionDeployed(releaseId: string): Promise<Release> {
    const raw = await this.fetch<unknown>(
      `/api/releases/${releaseId}/mark_production_deployed`,
      { method: "POST", body: "{}" },
    );
    return parseWithFallback(
      raw,
      ReleaseSchema,
      EMPTY_RELEASE_DETAIL.release,
      { endpoint: "POST /api/releases/:id/mark_production_deployed" },
    );
  }

  async rollbackRelease(
    releaseId: string,
    data: RollbackReleaseRequest,
  ): Promise<Release> {
    const raw = await this.fetch<unknown>(
      `/api/releases/${releaseId}/rollback`,
      { method: "POST", body: JSON.stringify(data) },
    );
    return parseWithFallback(
      raw,
      ReleaseSchema,
      EMPTY_RELEASE_DETAIL.release,
      { endpoint: "POST /api/releases/:id/rollback" },
    );
  }

  async markReleaseDone(releaseId: string): Promise<Release> {
    const raw = await this.fetch<unknown>(
      `/api/releases/${releaseId}/mark_done`,
      { method: "POST", body: "{}" },
    );
    return parseWithFallback(
      raw,
      ReleaseSchema,
      EMPTY_RELEASE_DETAIL.release,
      { endpoint: "POST /api/releases/:id/mark_done" },
    );
  }

  async getReleaseHealth(releaseId: string): Promise<ReleaseHealth> {
    const raw = await this.fetch<unknown>(
      `/api/releases/${releaseId}/health`,
    );
    return parseWithFallback(
      raw,
      ReleaseHealthSchema,
      EMPTY_RELEASE_HEALTH,
      { endpoint: "GET /api/releases/:id/health" },
    );
  }

  // Phase 2 — mints a fresh GitHub webhook secret for the workspace's
  // Ship Hub config. The response carries the plaintext secret EXACTLY
  // ONCE (mirrors PAT-create UX); subsequent reads of the workspace
  // only echo `ship_hub_webhook_secret_set: true`. The plaintext is
  // never written to localStorage or cached — the caller is expected
  // to display it in a one-time-display modal and discard.
  async regenerateShipHubWebhookSecret(
    workspaceId: string,
  ): Promise<WebhookSecretResponse> {
    const raw = await this.fetch<unknown>(
      `/api/workspaces/${workspaceId}/ship_hub/regenerate_webhook_secret`,
      { method: "POST" },
    );
    return parseWithFallback(
      raw,
      WebhookSecretResponseSchema,
      EMPTY_WEBHOOK_SECRET_RESPONSE,
      { endpoint: "POST /api/workspaces/:id/ship_hub/regenerate_webhook_secret" },
    );
  }

  // Phase 3 — PR card chip actions. Each method maps one-to-one to a backend
  // POST /api/pull_requests/{id}/{action} endpoint. Every response is parsed
  // through ActionResultSchema so a malformed body downgrades to a generic
  // failure rather than throwing into the chip's optimistic-update path
  // (per CLAUDE.md "API Response Compatibility").
  private async postPullRequestAction(
    prId: string,
    action: string,
    body?: unknown,
  ): Promise<ActionResult> {
    const raw = await this.fetch<unknown>(`/api/pull_requests/${prId}/${action}`, {
      method: "POST",
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    return parseWithFallback(raw, ActionResultSchema, EMPTY_ACTION_RESULT, {
      endpoint: `POST /api/pull_requests/:id/${action}`,
    });
  }

  async mergePullRequest(prId: string, body?: MergePullRequestRequest): Promise<ActionResult> {
    return this.postPullRequestAction(prId, "merge", body ?? {});
  }

  async rebasePullRequestOnMain(prId: string): Promise<ActionResult> {
    return this.postPullRequestAction(prId, "rebase_on_main");
  }

  async commentOnPullRequest(prId: string, body: CommentPullRequestRequest): Promise<ActionResult> {
    return this.postPullRequestAction(prId, "comment", body);
  }

  async dismissPullRequestReview(
    prId: string,
    body: DismissPullRequestReviewRequest,
  ): Promise<ActionResult> {
    return this.postPullRequestAction(prId, "dismiss_review", body);
  }

  async diagnoseCIFailure(prId: string): Promise<ActionResult> {
    return this.postPullRequestAction(prId, "diagnose_ci_failure");
  }

  async summarizeReviewFeedback(prId: string): Promise<ActionResult> {
    return this.postPullRequestAction(prId, "summarize_review_feedback");
  }

  async nudgePullRequestAuthor(
    prId: string,
    body?: NudgePullRequestAuthorRequest,
  ): Promise<ActionResult> {
    return this.postPullRequestAction(prId, "nudge_author", body ?? {});
  }

  async runSmokeTests(prId: string, body: RunSmokeTestsRequest): Promise<ActionResult> {
    return this.postPullRequestAction(prId, "run_smoke_tests", body);
  }

  async closePullRequestAsStale(
    prId: string,
    body?: ClosePullRequestAsStaleRequest,
  ): Promise<ActionResult> {
    return this.postPullRequestAction(prId, "close_as_stale", body ?? {});
  }

  // Phase 6.5 — submit a PR review (Approve / Request changes / Comment).
  // The endpoint is named "review" rather than "submit_review" to keep
  // the URL ergonomic; the action name in the audit row IS submit_review.
  async submitPullRequestReview(
    prId: string,
    body: SubmitPullRequestReviewRequest,
  ): Promise<ActionResult> {
    return this.postPullRequestAction(prId, "review", body);
  }

  // Phase 3 — recent-actions footer on the PR card. Today the backend has
  // no list endpoint registered (only the per-action POST handlers); this
  // method is implemented optimistically against
  // GET /api/pull_requests/{id}/actions so the frontend hook is ready to
  // light up the moment the backend lands the route. Until then the call
  // 404s and parseWithFallback returns the empty list — the footer simply
  // doesn't render. See packages/views/ship/hooks/use-ship-card-actions.ts
  // for the consumer-side TODO.
  async listShipCardActions(prId: string): Promise<ListShipCardActionsResponse> {
    const raw = await this.fetch<unknown>(`/api/pull_requests/${prId}/actions`);
    return parseWithFallback(
      raw,
      ListShipCardActionsResponseSchema,
      EMPTY_LIST_SHIP_CARD_ACTIONS_RESPONSE,
      { endpoint: "GET /api/pull_requests/:id/actions" },
    );
  }

  // Phase 5 — workspace-wide Ship Hub summary. Drives the multi-segment
  // sidebar widget (`🟢 4 staging · 🟡 2 to review · 🔴 1 failing`).
  async getShipHubSummary(): Promise<ShipHubSummary> {
    const raw = await this.fetch<unknown>(`/api/ship_hub/summary`);
    return parseWithFallback(
      raw,
      ShipHubSummarySchema,
      EMPTY_SHIP_HUB_SUMMARY,
      { endpoint: "GET /api/ship_hub/summary" },
    );
  }

  // Phase 5 — pre-flight production gate. Three endpoints; the gate
  // status is recomputed on every read so the frontend can poll a
  // single endpoint after each PATCH and re-render the chip row.
  async createOrGetDeployPreflight(
    environmentId: string,
    body: CreatePreflightRequest,
  ): Promise<DeployPreflight> {
    const raw = await this.fetch<unknown>(
      `/api/deploy_environments/${environmentId}/preflight`,
      { method: "POST", body: JSON.stringify(body) },
    );
    return parseWithFallback(
      raw,
      DeployPreflightSchema,
      // No "empty" preflight makes sense for a write endpoint — the
      // server always returns a row. Leaving fallback explicit so a
      // partial body still parses without crashing the dialog.
      {
        id: "",
        workspace_id: "",
        environment_id: environmentId,
        target_sha: body.target_sha,
        migrations_ok: false,
        smoke_tests_ok: false,
        qa_verified_at: null,
        qa_verified_by: null,
        rollback_plan: null,
        approver_id: null,
        second_approver_id: null,
        approved_at: null,
        promoted_at: null,
        created_at: "",
        updated_at: "",
        required_risk_level: "high",
        gate_status: "blocked",
        gate_blocked_reasons: [],
      },
      { endpoint: "POST /api/deploy_environments/:id/preflight" },
    );
  }

  async updateDeployPreflight(
    preflightId: string,
    body: UpdatePreflightRequest,
  ): Promise<DeployPreflight> {
    const raw = await this.fetch<unknown>(
      `/api/deploy_preflight/${preflightId}`,
      { method: "PATCH", body: JSON.stringify(body) },
    );
    return parseWithFallback(
      raw,
      DeployPreflightSchema,
      {
        id: preflightId,
        workspace_id: "",
        environment_id: "",
        target_sha: "",
        migrations_ok: false,
        smoke_tests_ok: false,
        qa_verified_at: null,
        qa_verified_by: null,
        rollback_plan: null,
        approver_id: null,
        second_approver_id: null,
        approved_at: null,
        promoted_at: null,
        created_at: "",
        updated_at: "",
        required_risk_level: "high",
        gate_status: "blocked",
        gate_blocked_reasons: [],
      },
      { endpoint: "PATCH /api/deploy_preflight/:id" },
    );
  }

  async promoteDeployPreflight(
    preflightId: string,
  ): Promise<PromoteDeployPreflightResponse> {
    const raw = await this.fetch<unknown>(
      `/api/deploy_preflight/${preflightId}/promote`,
      { method: "POST" },
    );
    return parseWithFallback(
      raw,
      PromoteDeployPreflightResponseSchema,
      // Conservative empty fallback — the UI checks gate_status to
      // know it should re-fetch on success.
      {
        preflight: {
          id: preflightId,
          workspace_id: "",
          environment_id: "",
          target_sha: "",
          migrations_ok: false,
          smoke_tests_ok: false,
          qa_verified_at: null,
          qa_verified_by: null,
          rollback_plan: null,
          approver_id: null,
          second_approver_id: null,
          approved_at: null,
          promoted_at: null,
          created_at: "",
          updated_at: "",
          required_risk_level: "high",
          gate_status: "blocked",
          gate_blocked_reasons: [],
        },
        deploy: {
          id: "",
          workspace_id: "",
          environment_id: "",
          ref: "",
          sha: "",
          status: "pending",
          triggered_by: null,
          triggered_at: "",
          started_at: null,
          completed_at: null,
          log_url: null,
          error_message: null,
          created_at: "",
        },
      },
      { endpoint: "POST /api/deploy_preflight/:id/promote" },
    );
  }

  // Phase 5 — time-machine snapshot.
  async getProjectShipSnapshot(
    projectId: string,
    at: string,
  ): Promise<ShipSnapshotResponse> {
    const raw = await this.fetch<unknown>(
      `/api/projects/${projectId}/ship_snapshot?at=${encodeURIComponent(at)}`,
    );
    return parseWithFallback(
      raw,
      ShipSnapshotResponseSchema,
      EMPTY_SHIP_SNAPSHOT_RESPONSE,
      { endpoint: "GET /api/projects/:id/ship_snapshot" },
    );
  }

  // Phase 4 — linkage spine. Each method goes through parseWithFallback so
  // an older Electron build talking to a phase-4 server (or vice versa)
  // never white-screens on a contract drift; the worst case is the chip
  // simply doesn't render.

  /** PATCH /api/pull_requests/{id} — manually override auto-detected linkage. */
  async updatePullRequest(
    prId: string,
    body: UpdatePullRequestRequest,
  ): Promise<unknown> {
    return this.fetch(`/api/pull_requests/${prId}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
  }

  /** GET /api/pull_requests/{id}/linked_issues. Returns null fields when
   *  the PR isn't linked to anything. */
  async getLinkedIssues(prId: string): Promise<LinkedIssuesResponse> {
    const raw = await this.fetch<unknown>(`/api/pull_requests/${prId}/linked_issues`);
    return parseWithFallback(
      raw,
      LinkedIssuesResponseSchema,
      EMPTY_LINKED_ISSUES_RESPONSE,
      { endpoint: "GET /api/pull_requests/:id/linked_issues" },
    );
  }

  /** GET /api/pull_requests/{id}/details — bundled drawer payload.
   *  Single round-trip so the Sheet renders without an N+1 fetch on
   *  open. Every optional section degrades gracefully via
   *  parseWithFallback's empty-response path. */
  async getPullRequestDetails(prId: string): Promise<PullRequestDetailsResponse> {
    const raw = await this.fetch<unknown>(`/api/pull_requests/${prId}/details`);
    return parseWithFallback(
      raw,
      PullRequestDetailsResponseSchema,
      EMPTY_PULL_REQUEST_DETAILS_RESPONSE as PullRequestDetailsResponse,
      { endpoint: "GET /api/pull_requests/:id/details" },
    );
  }

  /** POST /api/pull_requests/{id}/talk_to_agent. Returns the new chat
   *  session id; the frontend routes the user into the chat panel. */
  async talkToAgent(
    prId: string,
    body?: TalkToAgentRequest,
  ): Promise<TalkToAgentResponse> {
    const raw = await this.fetch<unknown>(`/api/pull_requests/${prId}/talk_to_agent`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body ?? {}),
    });
    return parseWithFallback(
      raw,
      TalkToAgentResponseSchema,
      EMPTY_TALK_TO_AGENT_RESPONSE,
      { endpoint: "POST /api/pull_requests/:id/talk_to_agent" },
    );
  }

  /** GET /api/issues/{id}/pull_requests — PRs whose
   *  originating_issue_id matches. */
  async listIssuePullRequests(issueId: string): Promise<ListPullRequestsResponse> {
    const raw = await this.fetch<unknown>(`/api/issues/${issueId}/pull_requests`);
    return parseWithFallback(
      raw,
      ListPullRequestsResponseSchema,
      EMPTY_LIST_PULL_REQUESTS_RESPONSE,
      { endpoint: "GET /api/issues/:id/pull_requests" },
    );
  }

  /** POST /api/pull_requests/{id}/conversation_channel — get or create
   *  the per-PR Multica channel. Idempotent. */
  async getOrCreatePRConversationChannel(prId: string): Promise<unknown> {
    return this.fetch(`/api/pull_requests/${prId}/conversation_channel`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: "{}",
    });
  }

  /** POST /api/releases/{id}/channel — get or create the release's
   *  discussion channel. Idempotent. Returns the channel object. */
  async openReleaseChannel(releaseId: string): Promise<unknown> {
    return this.fetch(`/api/releases/${releaseId}/channel`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: "{}",
    });
  }

  /** GET /api/projects/{id}/pull_request_stacks — stack-tree shape for
   *  the nested-card rendering in the Kanban. */
  async listPullRequestStacks(projectId: string): Promise<ListPullRequestStacksResponse> {
    const raw = await this.fetch<unknown>(`/api/projects/${projectId}/pull_request_stacks`);
    return parseWithFallback(
      raw,
      ListPullRequestStacksResponseSchema,
      EMPTY_LIST_PULL_REQUEST_STACKS_RESPONSE,
      { endpoint: "GET /api/projects/:id/pull_request_stacks" },
    );
  }
}
