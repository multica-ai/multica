import type {
  Issue,
  CreateIssueRequest,
  UpdateIssueRequest,
  GroupedIssuesResponse,
  ListIssuesResponse,
  SearchIssuesResponse,
  SearchProjectsResponse,
  UpdateMeRequest,
  CreateMemberRequest,
  UpdateMemberRequest,
  ListIssuesParams,
  ListGroupedIssuesParams,
  Agent,
  CreateAgentRequest,
  AgentTemplate,
  AgentTemplateSummary,
  CreateAgentFromTemplateRequest,
  CreateAgentFromTemplateResponse,
  UpdateAgentRequest,
  AgentAllowedPrincipal,
  UpdateAgentAllowedPrincipalsRequest,
  AgentEnvResponse,
  UpdateAgentEnvRequest,
  AgentTask,
  LocalPreview,
  LocalPreviewLogs,
  TaskInteraction,
  TaskTraceResponse,
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
  BatchImportSkillsResponse,
  DiscoverImportSkillsResponse,
  PersonalAccessToken,
  CreatePersonalAccessTokenRequest,
  CreatePersonalAccessTokenResponse,
  RuntimeUsage,
  IssueUsageSummary,
  RuntimeHourlyActivity,
  RuntimeUsageByAgent,
  RuntimeUsageByHour,
  DashboardUsageDaily,
  DashboardUsageByAgent,
  DashboardLocalUsageByRunner,
  DashboardLocalRunTimeByRunner,
  DashboardAgentRunTime,
  DashboardRunTimeDaily,
  AgentRunDashboard,
  AgentRunDashboardRunDetail,
  RuntimeUpdate,
  CLIUpdateManifest,
  RuntimePing,
  RuntimeModelListRequest,
  RuntimeLocalSkillListRequest,
  CreateRuntimeLocalSkillImportRequest,
  RuntimeLocalSkillImportRequest,
  TimelineEntry,
  AssigneeFrequencyEntry,
  MentionFrequencyEntry,
  TaskMessagePayload,
  Attachment,
  ChatSession,
  ChatMessage,
  ChatMessagesPage,
  ChatPendingTask,
  PendingChatTasksResponse,
  SendChatMessageResponse,
  Project,
  CreateProjectRequest,
  UpdateProjectRequest,
  ListProjectsResponse,
  ProjectResource,
  CreateProjectResourceRequest,
  UpdateProjectResourceRequest,
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
  InviteLink,
  CreateInviteLinkRequest,
  ListNotificationBindingsResponse,
  ListNotificationPreferencesResponse,
  NotificationChannelPreference,
  NotificationWebhook,
  AutoSubscribePreferenceResponse,
  ListNotificationWebhooksResponse,
  CreateNotificationWebhookRequest,
  UpdateNotificationWebhookRequest,
  TestNotificationWebhookResponse,
  UpdateNotificationPreferenceRequest,
  UpdateAutoSubscribePreferenceRequest,
  StartDingTalkBindingRequest,
  StartDingTalkBindingResponse,
  CompleteDingTalkBindingResponse,
  StartEmailBindingRequest,
  StartEmailBindingResponse,
  VerifyEmailBindingRequest,
  VerifyEmailBindingResponse,
  StartGoogleBindingRequest,
  StartGoogleBindingResponse,
  CompleteGoogleBindingResponse,
  BindOpenclawWeixinRequest,
  BindOpenclawWeixinResponse,
  Autopilot,
  AutopilotTrigger,
  AutopilotRun,
  CreateAutopilotRequest,
  UpdateAutopilotRequest,
  TriggerAutopilotRequest,
  CreateAutopilotTriggerRequest,
  UpdateAutopilotTriggerRequest,
  ListAutopilotsResponse,
  GetAutopilotResponse,
  ListAutopilotRunsResponse,
  ListWebhookDeliveriesResponse,
  WebhookDelivery,
  NotificationPreferenceResponse,
  NotificationPreferences,
  AgentDefaults,
  AgentDefaultsWithUser,
  InstructionsHistoryScope,
  InstructionsHistoryDetail,
  ListInstructionsHistoryResponse,
  WikiPage,
  ListWikiPagesResponse,
  ListWikiPageActivitiesResponse,
  CreateWikiPageRequest,
  UpdateWikiPageRequest,
  ReorderWikiPagesRequest,
  GitHubPullRequest,
  ListGitHubInstallationsResponse,
  GitHubConnectResponse,
  GiteeWebhookConfig,
  Squad,
  SquadMember,
  SquadMemberStatusListResponse,
  BillingBalance,
  BillingTransactionsPage,
  BillingBatchesPage,
  BillingTopupsPage,
  BillingPriceTier,
  CreateBillingCheckoutSessionRequest,
  CreateBillingCheckoutSessionResponse,
  BillingCheckoutSessionStatus,
  CreateBillingPortalSessionResponse,
  MobilePushRegistrationResponse,
  UpsertMobilePushRegistrationRequest,
} from "../types";
import type { OnboardingCompletionPath } from "../onboarding/types";
import type {
  CloudRuntimeNode,
  CreateCloudRuntimeNodeRequest,
  ListCloudRuntimeNodesParams,
} from "../runtimes/cloud-runtime";
import { type Logger, noopLogger } from "../logger";
import { createRequestId } from "../utils";
import { getCurrentSlug } from "../platform/workspace-storage";
import { parseWithFallback } from "./schema";
import {
  AgentTemplateSchema,
  AgentTemplateSummaryListSchema,
  AutopilotResponseSchema,
  AutopilotRunResponseSchema,
  AttachmentResponseSchema,
  ChildIssuesResponseSchema,
  CommentsListSchema,
  CloudRuntimeNodeListSchema,
  CloudRuntimeNodeSchema,
  CreateAgentFromTemplateResponseSchema,
  AutoSubscribePreferenceResponseSchema,
  DashboardAgentRunTimeListSchema,
  DashboardRunTimeDailyListSchema,
  DashboardLocalRunTimeByRunnerListSchema,
  DashboardLocalUsageByRunnerListSchema,
  AgentRunDashboardRunDetailSchema,
  AgentRunDashboardSchema,
  EMPTY_AGENT_RUN_DASHBOARD,
  EMPTY_AGENT_RUN_DETAIL,
  DashboardUsageByAgentListSchema,
  DashboardUsageDailyListSchema,
  EMPTY_AUTOPILOT,
  EMPTY_AUTOPILOT_RUN,
  EMPTY_AGENT_TEMPLATE_DETAIL,
  EMPTY_AGENT_TEMPLATE_SUMMARY_LIST,
  EMPTY_APP_CONFIG,
  EMPTY_ATTACHMENT,
  EMPTY_AUTO_SUBSCRIBE_PREFERENCE_RESPONSE,
  EMPTY_CLOUD_RUNTIME_NODE,
  EMPTY_CLOUD_RUNTIME_NODE_LIST,
  EMPTY_CREATE_AGENT_FROM_TEMPLATE_RESPONSE,
  EMPTY_DISCOVER_IMPORT_SKILLS_RESPONSE,
  EMPTY_GET_AUTOPILOT_RESPONSE,
  EMPTY_GROUPED_ISSUES_RESPONSE,
  EMPTY_ISSUE_LABELS_RESPONSE,
  EMPTY_LIST_AUTOPILOT_RUNS_RESPONSE,
  EMPTY_LIST_AUTOPILOTS_RESPONSE,
  EMPTY_LIST_LABELS_RESPONSE,
  EMPTY_LIST_ISSUES_RESPONSE,
  EMPTY_SQUAD,
  EMPTY_SQUAD_LIST,
  EMPTY_SQUAD_MEMBER_STATUS_LIST,
  EMPTY_TIMELINE_ENTRIES,
  EMPTY_USER,
  EMPTY_LIST_WEBHOOK_DELIVERIES_RESPONSE,
  EMPTY_WEBHOOK_DELIVERY,
  AppConfigSchema,
  type AppConfigResponse,
  GroupedIssuesResponseSchema,
  GetAutopilotResponseSchema,
  DiscoverImportSkillsResponseSchema,
  IssueLabelsResponseSchema,
  LabelSchema,
  ListAutopilotRunsResponseSchema,
  ListAutopilotsResponseSchema,
  ListIssuesResponseSchema,
  ListLabelsResponseSchema,
  ListWebhookDeliveriesResponseSchema,
  RuntimeHourlyActivityListSchema,
  RuntimeUsageByAgentListSchema,
  RuntimeUsageByHourListSchema,
  RuntimeUsageListSchema,
  SquadSchema,
  SquadListSchema,
  SquadMemberStatusListResponseSchema,
  SubscribersListSchema,
  TimelineEntriesSchema,
  UserSchema,
  WebhookDeliveryResponseSchema,
  BillingBalanceSchema,
  BillingTransactionsPageSchema,
  BillingBatchesPageSchema,
  BillingTopupsPageSchema,
  BillingPriceTierListSchema,
  CreateBillingCheckoutSessionResponseSchema,
  BillingCheckoutSessionStatusSchema,
  CreateBillingPortalSessionResponseSchema,
  EMPTY_BILLING_BALANCE,
  EMPTY_BILLING_TRANSACTIONS_PAGE,
  EMPTY_BILLING_BATCHES_PAGE,
  EMPTY_BILLING_TOPUPS_PAGE,
  EMPTY_BILLING_PRICE_TIER_LIST,
  EMPTY_CREATE_BILLING_CHECKOUT_SESSION_RESPONSE,
  EMPTY_BILLING_CHECKOUT_SESSION_STATUS,
  EMPTY_CREATE_BILLING_PORTAL_SESSION_RESPONSE,
  EMPTY_MOBILE_PUSH_REGISTRATION_RESPONSE,
  MobilePushRegistrationResponseSchema,
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

interface DirectUploadInitiateResponse {
  attachment_id: string;
  object_key: string;
  upload_url: string;
  headers?: Record<string, string>;
  upload_token: string;
  expires_at: string;
}

interface MultipartUploadInitiateResponse {
  session_id: string;
  attachment_id: string;
  object_key: string;
  upload_id: string;
  headers?: Record<string, string>;
  part_size_bytes: number;
  part_count: number;
  expires_at: string;
}

interface MultipartUploadSignPartsResponse {
  parts: Array<{
    part_number: number;
    upload_url: string;
    headers?: Record<string, string>;
  }>;
  expires_at: string;
}

const MULTIPART_UPLOAD_THRESHOLD_BYTES = 64 * 1024 * 1024;
const MULTIPART_UPLOAD_PART_SIZE_BYTES = 16 * 1024 * 1024;
const MULTIPART_UPLOAD_CONCURRENCY = 3;
const MULTIPART_UPLOAD_MAX_RETRIES = 3;

export interface OnboardingRuntimeBootstrapResponse {
  workspace_id: string;
  agent_id: string;
  issue_id: string;
}

export interface OnboardingNoRuntimeBootstrapResponse {
  workspace_id: string;
  issue_id: string;
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

// Thrown by getAttachmentTextContent when the server refuses to inline a
// file because it exceeds the 2 MB cap. UI maps to a "too large, please
// download" affordance with the Download CTA still available.
export class PreviewTooLargeError extends Error {
  constructor() {
    super("attachment too large for inline preview");
    this.name = "PreviewTooLargeError";
  }
}

// Thrown by getAttachmentTextContent when the server's text whitelist
// rejects the content type. Normally the client's isPreviewable() guard
// catches this earlier, but the two whitelists can drift — surfacing the
// 415 as a typed error makes the drift visible.
export class PreviewUnsupportedError extends Error {
  constructor() {
    super("attachment type not supported for inline preview");
    this.name = "PreviewUnsupportedError";
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

  private handleUnauthorized(requestToken: string | null) {
    if (requestToken !== this.token) {
      return;
    }
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

  // Sends the request with the standard headers (auth, CSRF, request id,
  // client identity) and runs the shared error path (401 → handleUnauthorized,
  // structured ApiError, status-aware log level). Returns the raw Response so
  // callers can decide how to decode the body — JSON for the typed `fetch<T>`
  // path, plain text for the attachment-preview proxy, etc.
  private async fetchRaw(
    path: string,
    init?: RequestInit & { extraHeaders?: Record<string, string> },
  ): Promise<Response> {
    const rid = createRequestId();
    const start = Date.now();
    const method = init?.method ?? "GET";
    const requestToken = this.token;

    const headers: Record<string, string> = {
      "X-Request-ID": rid,
      ...this.authHeaders(),
      ...(init?.extraHeaders ?? {}),
      ...((init?.headers as Record<string, string>) ?? {}),
    };

    this.logger.info(`→ ${method} ${path}`, { rid });

    const res = await fetch(`${this.baseUrl}${path}`, {
      ...init,
      headers,
      credentials: "include",
    });

    if (!res.ok) {
      if (res.status === 401) this.handleUnauthorized(requestToken);
      const { message, body } = await this.parseErrorBody(res, `API error: ${res.status} ${res.statusText}`);
      const logLevel = res.status === 404 ? "warn" : "error";
      this.logger[logLevel](`← ${res.status} ${path}`, { rid, duration: `${Date.now() - start}ms`, error: message });
      throw new ApiError(message, res.status, res.statusText, body);
    }

    this.logger.info(`← ${res.status} ${path}`, { rid, duration: `${Date.now() - start}ms` });
    return res;
  }

  private async fetch<T>(path: string, init?: RequestInit): Promise<T> {
    const res = await this.fetchRaw(path, {
      ...init,
      extraHeaders: { "Content-Type": "application/json" },
    });
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

  async googleMobileLogin(idToken: string, platform: string): Promise<LoginResponse> {
    return this.fetch("/auth/google/mobile", {
      method: "POST",
      body: JSON.stringify({ id_token: idToken, platform }),
    });
  }

  async dingtalkLogin(code: string, redirectUri: string): Promise<LoginResponse> {
    return this.fetch("/auth/dingtalk", {
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
    const raw = await this.fetch<unknown>("/api/me");
    return parseWithFallback(raw, UserSchema, EMPTY_USER, {
      endpoint: "GET /api/me",
    });
  }

  async markOnboardingComplete(payload?: {
    completion_path?: OnboardingCompletionPath;
    workspace_id?: string;
  }): Promise<User> {
    const raw = await this.fetch<unknown>("/api/me/onboarding/complete", {
      method: "POST",
      body: payload ? JSON.stringify(payload) : undefined,
    });
    return parseWithFallback(raw, UserSchema, EMPTY_USER, {
      endpoint: "POST /api/me/onboarding/complete",
    });
  }

  async joinCloudWaitlist(payload: {
    email: string;
    reason?: string;
  }): Promise<User> {
    const raw = await this.fetch<unknown>("/api/me/onboarding/cloud-waitlist", {
      method: "POST",
      body: JSON.stringify(payload),
    });
    return parseWithFallback(raw, UserSchema, EMPTY_USER, {
      endpoint: "POST /api/me/onboarding/cloud-waitlist",
    });
  }

  async patchOnboarding(payload: {
    questionnaire?: Record<string, unknown>;
  }): Promise<User> {
    const raw = await this.fetch<unknown>("/api/me/onboarding", {
      method: "PATCH",
      body: JSON.stringify(payload),
    });
    return parseWithFallback(raw, UserSchema, EMPTY_USER, {
      endpoint: "PATCH /api/me/onboarding",
    });
  }

  async updateMe(data: UpdateMeRequest): Promise<User> {
    const raw = await this.fetch<unknown>("/api/me", {
      method: "PATCH",
      body: JSON.stringify(data),
    });
    return parseWithFallback(raw, UserSchema, EMPTY_USER, {
      endpoint: "PATCH /api/me",
    });
  }

  async listNotificationBindings(): Promise<ListNotificationBindingsResponse> {
    return this.fetch("/api/me/notification-bindings");
  }

  async deleteNotificationBinding(id: string): Promise<void> {
    await this.fetch(`/api/me/notification-bindings/${id}`, {
      method: "DELETE",
    });
  }

  async startDingTalkBinding(
    payload: StartDingTalkBindingRequest,
  ): Promise<StartDingTalkBindingResponse> {
    return this.fetch("/api/me/notification-bindings/dingtalk/start", {
      method: "POST",
      body: JSON.stringify(payload),
    });
  }

  async completeDingTalkBinding(
    code: string,
    state: string,
  ): Promise<CompleteDingTalkBindingResponse> {
    return this.fetch("/api/me/notification-bindings/dingtalk/callback", {
      method: "POST",
      body: JSON.stringify({ code, state }),
    });
  }

  async startEmailBinding(
    payload: StartEmailBindingRequest,
  ): Promise<StartEmailBindingResponse> {
    return this.fetch("/api/me/notification-bindings/email/start", {
      method: "POST",
      body: JSON.stringify(payload),
    });
  }

  async verifyEmailBinding(
    payload: VerifyEmailBindingRequest,
  ): Promise<VerifyEmailBindingResponse> {
    return this.fetch("/api/me/notification-bindings/email/verify", {
      method: "POST",
      body: JSON.stringify(payload),
    });
  }

  async startGoogleBinding(
    payload: StartGoogleBindingRequest,
  ): Promise<StartGoogleBindingResponse> {
    return this.fetch("/api/me/notification-bindings/google/start", {
      method: "POST",
      body: JSON.stringify(payload),
    });
  }

  async completeGoogleBinding(
    code: string,
    state: string,
  ): Promise<CompleteGoogleBindingResponse> {
    return this.fetch("/api/notification-bindings/google/callback", {
      method: "POST",
      body: JSON.stringify({ code, state }),
    });
  }

  async bindOpenclawWeixin(
    payload: BindOpenclawWeixinRequest,
  ): Promise<BindOpenclawWeixinResponse> {
    return this.fetch("/api/me/notification-bindings/openclaw-weixin", {
      method: "PUT",
      body: JSON.stringify(payload),
    });
  }

  async listNotificationPreferences(): Promise<ListNotificationPreferencesResponse> {
    return this.fetch("/api/me/notification-preferences");
  }

  async listNotificationWebhooks(): Promise<ListNotificationWebhooksResponse> {
    return this.fetch("/api/me/notification-webhooks");
  }

  async createNotificationWebhook(
    data: CreateNotificationWebhookRequest,
  ): Promise<NotificationWebhook> {
    return this.fetch("/api/me/notification-webhooks", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async updateNotificationWebhook(
    id: string,
    data: UpdateNotificationWebhookRequest,
  ): Promise<NotificationWebhook> {
    return this.fetch(`/api/me/notification-webhooks/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    });
  }

  async deleteNotificationWebhook(id: string): Promise<void> {
    await this.fetch(`/api/me/notification-webhooks/${id}`, {
      method: "DELETE",
    });
  }

  async testNotificationWebhook(id: string): Promise<TestNotificationWebhookResponse> {
    return this.fetch(`/api/me/notification-webhooks/${id}/test`, {
      method: "POST",
    });
  }

  async updateNotificationPreference(
    data: UpdateNotificationPreferenceRequest,
  ): Promise<NotificationChannelPreference> {
    return this.fetch("/api/me/notification-preferences", {
      method: "PATCH",
      body: JSON.stringify(data),
    });
  }

  async getAutoSubscribePreferences(): Promise<AutoSubscribePreferenceResponse> {
    const raw = await this.fetch<unknown>("/api/auto-subscribe-preferences");
    return parseWithFallback(
      raw,
      AutoSubscribePreferenceResponseSchema,
      EMPTY_AUTO_SUBSCRIBE_PREFERENCE_RESPONSE,
      { endpoint: "GET /api/auto-subscribe-preferences" },
    );
  }

  async updateAutoSubscribePreferences(
    preferences: UpdateAutoSubscribePreferenceRequest["preferences"],
  ): Promise<AutoSubscribePreferenceResponse> {
    const raw = await this.fetch<unknown>("/api/auto-subscribe-preferences", {
      method: "PATCH",
      body: JSON.stringify({ preferences }),
    });
    return parseWithFallback(
      raw,
      AutoSubscribePreferenceResponseSchema,
      EMPTY_AUTO_SUBSCRIBE_PREFERENCE_RESPONSE,
      { endpoint: "PATCH /api/auto-subscribe-preferences" },
    );
  }

  // Issues
  async listIssues(params?: ListIssuesParams): Promise<ListIssuesResponse> {
    const search = new URLSearchParams();
    if (params?.limit) search.set("limit", String(params.limit));
    if (params?.offset) search.set("offset", String(params.offset));
    if (params?.workspace_id) search.set("workspace_id", params.workspace_id);
    if (params?.status) search.set("status", params.status);
    if (params?.statuses?.length) search.set("statuses", params.statuses.join(","));
    if (params?.priority) search.set("priority", params.priority);
    if (params?.priorities?.length) search.set("priorities", params.priorities.join(","));
    if (params?.assignee_types?.length) search.set("assignee_types", params.assignee_types.join(","));
    if (params?.assignee_id) search.set("assignee_id", params.assignee_id);
    if (params?.assignee_ids?.length) search.set("assignee_ids", params.assignee_ids.join(","));
    if (params?.assignees?.length) {
      search.set("assignees", params.assignees.map((actor) => `${actor.type}:${actor.id}`).join(","));
    }
    if (params?.include_no_assignee) search.set("include_no_assignee", "true");
    if (params?.creator_id) search.set("creator_id", params.creator_id);
    if (params?.creators?.length) {
      search.set("creators", params.creators.map((actor) => `${actor.type}:${actor.id}`).join(","));
    }
    if (params?.project_id) search.set("project_id", params.project_id);
    if (params?.project_ids?.length) search.set("project_ids", params.project_ids.join(","));
    if (params?.include_no_project) search.set("include_no_project", "true");
    if (params?.label_ids?.length) search.set("label_ids", params.label_ids.join(","));
    if (params?.involves_user_id) search.set("involves_user_id", params.involves_user_id);
    if (params?.metadata && Object.keys(params.metadata).length > 0) {
      search.set("metadata", JSON.stringify(params.metadata));
    }
    if (params?.open_only) search.set("open_only", "true");
    if (params?.scheduled) search.set("scheduled", "true");
    if (params?.sort_by) search.set("sort", params.sort_by);
    if (params?.sort_direction) search.set("direction", params.sort_direction);
    const path = `/api/issues?${search}`;
    const raw = await this.fetch<unknown>(path);
    return parseWithFallback(raw, ListIssuesResponseSchema, EMPTY_LIST_ISSUES_RESPONSE, {
      endpoint: "GET /api/issues",
    });
  }

  async listGroupedIssues(params: ListGroupedIssuesParams): Promise<GroupedIssuesResponse> {
    const search = new URLSearchParams({ group_by: params.group_by });
    if (params.limit) search.set("limit", String(params.limit));
    if (params.offset) search.set("offset", String(params.offset));
    if (params.workspace_id) search.set("workspace_id", params.workspace_id);
    if (params.statuses?.length) search.set("statuses", params.statuses.join(","));
    if (params.priorities?.length) search.set("priorities", params.priorities.join(","));
    if (params.assignee_types?.length) search.set("assignee_types", params.assignee_types.join(","));
    if (params.assignee_id) search.set("assignee_id", params.assignee_id);
    if (params.assignee_ids?.length) search.set("assignee_ids", params.assignee_ids.join(","));
    if (params.creator_id) search.set("creator_id", params.creator_id);
    if (params.project_id) search.set("project_id", params.project_id);
    if (params.involves_user_id) search.set("involves_user_id", params.involves_user_id);
    if (params.metadata && Object.keys(params.metadata).length > 0) {
      search.set("metadata", JSON.stringify(params.metadata));
    }
    if (params.assignee_filters?.length) {
      search.set("assignee_filters", params.assignee_filters.map((f) => `${f.type}:${f.id}`).join(","));
    }
    if (params.include_no_assignee) search.set("include_no_assignee", "true");
    if (params.creator_filters?.length) {
      search.set("creator_filters", params.creator_filters.map((f) => `${f.type}:${f.id}`).join(","));
    }
    if (params.project_ids?.length) search.set("project_ids", params.project_ids.join(","));
    if (params.include_no_project) search.set("include_no_project", "true");
    if (params.label_ids?.length) search.set("label_ids", params.label_ids.join(","));
    if (params.group_assignee_type) search.set("group_assignee_type", params.group_assignee_type);
    if (params.group_assignee_id) search.set("group_assignee_id", params.group_assignee_id);
    if (params.sort_by) search.set("sort", params.sort_by);
    if (params.sort_direction) search.set("direction", params.sort_direction);
    const raw = await this.fetch<unknown>(`/api/issues/grouped?${search}`);
    return parseWithFallback(raw, GroupedIssuesResponseSchema, EMPTY_GROUPED_ISSUES_RESPONSE, {
      endpoint: "GET /api/issues/grouped",
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

  async quickCreateIssue(data: {
    agent_id?: string;
    squad_id?: string;
    prompt: string;
    project_id?: string | null;
    parent_issue_id?: string | null;
  }): Promise<{ task_id: string }> {
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

  /** Batched variant — returns children for multiple parents in one request.
   *  Avoids an N-request fan-out in Swimlane (one per visible parent lane).
   *  parentIds must be non-empty; pass a sorted, deduplicated list so the
   *  React Query cache key is stable across renders. */
  async listChildrenByParents(parentIds: string[]): Promise<{ issues: Issue[] }> {
    const raw = await this.fetch<unknown>(
      `/api/issues/children?parent_ids=${parentIds.join(",")}`,
    );
    return parseWithFallback(raw, ChildIssuesResponseSchema, { issues: [] }, {
      endpoint: "GET /api/issues/children",
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

  async clearIssueHistory(
    issueId: string,
    options: { clear_comments: boolean; clear_tasks: boolean },
  ): Promise<{ comments_deleted: number; tasks_deleted: number }> {
    return this.fetch(`/api/issues/${issueId}/clear-history`, {
      method: "POST",
      body: JSON.stringify(options),
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

  async listTimeline(
    issueId: string,
    options?: { mode: "around"; id: string },
  ): Promise<TimelineEntry[]> {
    const query = new URLSearchParams();
    if (options?.mode === "around" && options.id) {
      query.set("around", options.id);
    }

    const raw = await this.fetch<unknown>(
      `/api/issues/${issueId}/timeline${query.size > 0 ? `?${query.toString()}` : ""}`,
    );

    if (
      options?.mode === "around" &&
      raw &&
      typeof raw === "object" &&
      Array.isArray((raw as { entries?: unknown }).entries)
    ) {
      const entries = parseWithFallback(
        (raw as { entries: unknown }).entries,
        TimelineEntriesSchema,
        EMPTY_TIMELINE_ENTRIES,
        { endpoint: "GET /api/issues/:id/timeline?around=..." },
      );
      return [...entries].reverse();
    }

    return parseWithFallback(raw, TimelineEntriesSchema, EMPTY_TIMELINE_ENTRIES, {
      endpoint: "GET /api/issues/:id/timeline",
    });
  }

  async getAssigneeFrequency(): Promise<AssigneeFrequencyEntry[]> {
    return this.fetch("/api/assignee-frequency");
  }

  async getMentionFrequency(): Promise<MentionFrequencyEntry[]> {
    return this.fetch("/api/mention-frequency");
  }

  async updateComment(commentId: string, content: string, attachmentIds?: string[]): Promise<Comment> {
    return this.fetch(`/api/comments/${commentId}`, {
      method: "PUT",
      body: JSON.stringify({ content, attachment_ids: attachmentIds }),
    });
  }

  async deleteComment(commentId: string): Promise<void> {
    await this.fetch(`/api/comments/${commentId}`, { method: "DELETE" });
  }

  /** Re-queue the agent for this reply (issue threads only). */
  async retryAgentComment(commentId: string, retryInstruction?: string): Promise<void> {
    await this.fetch(`/api/comments/${commentId}/retry-agent`, {
      method: "POST",
      body: retryInstruction ? JSON.stringify({ retry_instruction: retryInstruction }) : undefined,
    });
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
  async listAgents(params?: {
    workspace_id?: string;
    include_archived?: boolean;
    owner?: "me";
    slim?: boolean;
  }): Promise<Agent[]> {
    const search = new URLSearchParams();
    if (params?.workspace_id) search.set("workspace_id", params.workspace_id);
    if (params?.include_archived) search.set("include_archived", "true");
    if (params?.owner === "me") search.set("owner", "me");
    if (params?.slim) search.set("slim", "true");
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

  async listAgentTemplates(): Promise<AgentTemplateSummary[]> {
    const raw = await this.fetch<unknown>("/api/agent-templates");
    return parseWithFallback(
      raw,
      AgentTemplateSummaryListSchema,
      EMPTY_AGENT_TEMPLATE_SUMMARY_LIST,
      { endpoint: "GET /api/agent-templates" },
    );
  }

  async getAgentTemplate(slug: string): Promise<AgentTemplate> {
    const raw = await this.fetch<unknown>(
      `/api/agent-templates/${encodeURIComponent(slug)}`,
    );
    // Round-trip the requested slug into the fallback so a malformed
    // detail response still produces a navigable record matching the URL
    // the user clicked.
    return parseWithFallback(
      raw,
      AgentTemplateSchema,
      { ...EMPTY_AGENT_TEMPLATE_DETAIL, slug },
      { endpoint: "GET /api/agent-templates/:slug" },
    );
  }

  /** Creates an agent from a curated template. The server fetches every
   *  referenced skill URL in parallel, materializes them into the workspace
   *  (find-or-create by name), and writes the agent + skill bindings in a
   *  single transaction. On any upstream fetch failure, the entire write is
   *  rolled back and the API returns 422 with `failed_urls`. */
  async createAgentFromTemplate(
    data: CreateAgentFromTemplateRequest,
  ): Promise<CreateAgentFromTemplateResponse> {
    const raw = await this.fetch<unknown>("/api/agents/from-template", {
      method: "POST",
      body: JSON.stringify(data),
    });
    return parseWithFallback(
      raw,
      CreateAgentFromTemplateResponseSchema,
      EMPTY_CREATE_AGENT_FROM_TEMPLATE_RESPONSE,
      { endpoint: "POST /api/agents/from-template" },
    );
  }

  async updateAgent(id: string, data: UpdateAgentRequest): Promise<Agent> {
    return this.fetch(`/api/agents/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  async listAgentAllowedPrincipals(agentId: string): Promise<AgentAllowedPrincipal[]> {
    return this.fetch(`/api/agents/${agentId}/allowed-principals`);
  }

  async updateAgentAllowedPrincipals(
    agentId: string,
    data: UpdateAgentAllowedPrincipalsRequest,
  ): Promise<AgentAllowedPrincipal[]> {
    return this.fetch(`/api/agents/${agentId}/allowed-principals`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  async copyAgent(id: string, data?: { name?: string }): Promise<Agent> {
    return this.fetch(`/api/agents/${id}/copy`, {
      method: "POST",
      body: JSON.stringify(data ?? {}),
    });
  }

  async archiveAgent(id: string): Promise<Agent> {
    return this.fetch(`/api/agents/${id}/archive`, { method: "POST" });
  }

  /**
   * Returns the plaintext `custom_env` map for an agent. Owner/admin
   * only; calls from agent-actor sessions get a 403. Every successful
   * call writes an `agent_env_revealed` activity_log row server-side.
   * MUL-2600.
   */
  async getAgentEnv(id: string): Promise<AgentEnvResponse> {
    return this.fetch(`/api/agents/${id}/env`);
  }

  /**
   * Replaces an agent's `custom_env` wholesale. Values equal to
   * `"****"` are preserved server-side (the **** guard) so a partial
   * UI edit doesn't overwrite real secrets with the masked
   * placeholder. Owner/admin only; agent actors get a 403. Every
   * successful call writes an `agent_env_updated` activity_log row.
   * MUL-2600.
   */
  async updateAgentEnv(id: string, data: UpdateAgentEnvRequest): Promise<AgentEnvResponse> {
    return this.fetch(`/api/agents/${id}/env`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
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

  async listRuntimes(params?: { workspace_id?: string; owner?: "me"; owner_id?: string }): Promise<AgentRuntime[]> {
    const search = new URLSearchParams();
    if (params?.workspace_id) search.set("workspace_id", params.workspace_id);
    if (params?.owner) search.set("owner", params.owner);
    if (params?.owner_id) search.set("owner_id", params.owner_id);
    return this.fetch(`/api/runtimes?${search}`);
  }

  async listCloudRuntimeNodes(
    params?: ListCloudRuntimeNodesParams,
  ): Promise<CloudRuntimeNode[]> {
    const search = new URLSearchParams();
    if (params?.limit !== undefined) search.set("limit", String(params.limit));
    if (params?.offset !== undefined) search.set("offset", String(params.offset));
    const query = search.toString();
    const raw = await this.fetch<unknown>(
      `/api/cloud-runtime/nodes${query ? `?${query}` : ""}`,
    );
    return parseWithFallback(
      raw,
      CloudRuntimeNodeListSchema,
      EMPTY_CLOUD_RUNTIME_NODE_LIST,
      { endpoint: "GET /api/cloud-runtime/nodes" },
    );
  }

  async createCloudRuntimeNode(
    data: CreateCloudRuntimeNodeRequest,
  ): Promise<CloudRuntimeNode> {
    const res = await this.fetchRaw("/api/cloud-runtime/nodes", {
      method: "POST",
      body: JSON.stringify(data),
      extraHeaders: { "Content-Type": "application/json" },
    });
    const raw = await res.json() as unknown;
    return parseWithFallback(
      raw,
      CloudRuntimeNodeSchema,
      EMPTY_CLOUD_RUNTIME_NODE,
      { endpoint: "POST /api/cloud-runtime/nodes" },
    );
  }

  async deleteCloudRuntimeNode(instanceId: string): Promise<void> {
    await this.fetchRaw("/api/cloud-runtime/nodes", {
      method: "DELETE",
      body: JSON.stringify({ instance_id: instanceId }),
      extraHeaders: { "Content-Type": "application/json" },
    });
  }

  // ---------------------------------------------------------------------
  // Cloud Billing — proxies to multica-cloud /api/v1/billing/*. The
  // multica-api server stamps X-User-ID and forwards bytes; everything
  // here is upstream-shaped. See packages/core/types/billing.ts for the
  // response field documentation.
  // ---------------------------------------------------------------------

  async getCloudBillingBalance(): Promise<BillingBalance> {
    const raw = await this.fetch<unknown>("/api/cloud-billing/balance");
    return parseWithFallback(raw, BillingBalanceSchema, EMPTY_BILLING_BALANCE, {
      endpoint: "GET /api/cloud-billing/balance",
    });
  }

  async listCloudBillingTransactions(
    params?: { page?: number; page_size?: number },
  ): Promise<BillingTransactionsPage> {
    const search = new URLSearchParams();
    if (params?.page !== undefined) search.set("page", String(params.page));
    if (params?.page_size !== undefined) search.set("page_size", String(params.page_size));
    const query = search.toString();
    const raw = await this.fetch<unknown>(
      `/api/cloud-billing/transactions${query ? `?${query}` : ""}`,
    );
    return parseWithFallback(
      raw,
      BillingTransactionsPageSchema,
      EMPTY_BILLING_TRANSACTIONS_PAGE,
      { endpoint: "GET /api/cloud-billing/transactions" },
    );
  }

  async listCloudBillingBatches(
    params?: { page?: number; page_size?: number },
  ): Promise<BillingBatchesPage> {
    const search = new URLSearchParams();
    if (params?.page !== undefined) search.set("page", String(params.page));
    if (params?.page_size !== undefined) search.set("page_size", String(params.page_size));
    const query = search.toString();
    const raw = await this.fetch<unknown>(
      `/api/cloud-billing/batches${query ? `?${query}` : ""}`,
    );
    return parseWithFallback(
      raw,
      BillingBatchesPageSchema,
      EMPTY_BILLING_BATCHES_PAGE,
      { endpoint: "GET /api/cloud-billing/batches" },
    );
  }

  async listCloudBillingTopups(
    params?: { page?: number; page_size?: number },
  ): Promise<BillingTopupsPage> {
    const search = new URLSearchParams();
    if (params?.page !== undefined) search.set("page", String(params.page));
    if (params?.page_size !== undefined) search.set("page_size", String(params.page_size));
    const query = search.toString();
    const raw = await this.fetch<unknown>(
      `/api/cloud-billing/topups${query ? `?${query}` : ""}`,
    );
    return parseWithFallback(
      raw,
      BillingTopupsPageSchema,
      EMPTY_BILLING_TOPUPS_PAGE,
      { endpoint: "GET /api/cloud-billing/topups" },
    );
  }

  async listCloudBillingPriceTiers(): Promise<BillingPriceTier[]> {
    const raw = await this.fetch<unknown>("/api/cloud-billing/price-tiers");
    return parseWithFallback(
      raw,
      BillingPriceTierListSchema,
      EMPTY_BILLING_PRICE_TIER_LIST,
      { endpoint: "GET /api/cloud-billing/price-tiers" },
    );
  }

  async createCloudBillingCheckoutSession(
    data: CreateBillingCheckoutSessionRequest,
  ): Promise<CreateBillingCheckoutSessionResponse> {
    const res = await this.fetchRaw("/api/cloud-billing/checkout-sessions", {
      method: "POST",
      body: JSON.stringify(data),
      extraHeaders: { "Content-Type": "application/json" },
    });
    const raw = (await res.json()) as unknown;
    return parseWithFallback(
      raw,
      CreateBillingCheckoutSessionResponseSchema,
      EMPTY_CREATE_BILLING_CHECKOUT_SESSION_RESPONSE,
      { endpoint: "POST /api/cloud-billing/checkout-sessions" },
    );
  }

  async getCloudBillingCheckoutSession(
    sessionId: string,
  ): Promise<BillingCheckoutSessionStatus> {
    // Stripe session ids are `cs_<base62>` so they're URL-safe by
    // construction; encodeURIComponent is paranoia for the case where a
    // future Stripe format change adds a non-alphanumeric character. The
    // server has its own allow-list rejection for unsafe ids.
    const raw = await this.fetch<unknown>(
      `/api/cloud-billing/checkout-sessions/${encodeURIComponent(sessionId)}`,
    );
    return parseWithFallback(
      raw,
      BillingCheckoutSessionStatusSchema,
      EMPTY_BILLING_CHECKOUT_SESSION_STATUS,
      { endpoint: "GET /api/cloud-billing/checkout-sessions/{sessionId}" },
    );
  }

  async createCloudBillingPortalSession(): Promise<CreateBillingPortalSessionResponse> {
    const res = await this.fetchRaw("/api/cloud-billing/portal-sessions", {
      method: "POST",
      // Body is intentionally absent — the upstream endpoint requires no
      // payload today. fetchRaw with no body skips the Content-Type
      // default; that's fine because there's nothing to declare.
    });
    const raw = (await res.json()) as unknown;
    return parseWithFallback(
      raw,
      CreateBillingPortalSessionResponseSchema,
      EMPTY_CREATE_BILLING_PORTAL_SESSION_RESPONSE,
      { endpoint: "POST /api/cloud-billing/portal-sessions" },
    );
  }

  async deleteRuntime(runtimeId: string): Promise<void> {
    await this.fetch(`/api/runtimes/${runtimeId}`, { method: "DELETE" });
  }

  // Cascade variant of deleteRuntime. The strict DELETE refuses with
  // structured 409 (`code: "runtime_has_active_agents"`, body carries the
  // blocking agents) when active agents are bound; the front-end then opens
  // the cascade-mode confirmation dialog and submits the user-confirmed
  // active agent set here. Server compares the snapshot to the live set
  // inside the transaction and refuses with `code: "runtime_delete_plan_changed"`
  // (same shape, fresh `active_agents`) if they don't match — caller should
  // re-render the agent list and force the user to re-confirm.
  async archiveAgentsAndDeleteRuntime(
    runtimeId: string,
    expectedActiveAgentIds: string[],
  ): Promise<{ status: string; agents_archived: number; tasks_cancelled: number }> {
    return this.fetch(`/api/runtimes/${runtimeId}/archive-agents-and-delete`, {
      method: "POST",
      body: JSON.stringify({ expected_active_agent_ids: expectedActiveAgentIds }),
    });
  }

  async updateRuntime(
    runtimeId: string,
    patch: { visibility?: "private" | "public" },
  ): Promise<AgentRuntime> {
    return this.fetch(`/api/runtimes/${runtimeId}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    });
  }

  async getRuntimeUsage(
    runtimeId: string,
    params?: { days?: number; tz?: string },
  ): Promise<RuntimeUsage[]> {
    const search = new URLSearchParams();
    if (params?.days) search.set("days", String(params.days));
    // `tz` drives the calendar-day boundary for the trend chart (Viewing
    // layer). Caller-supplied; the backend falls back to user.timezone /
    // UTC if omitted.
    if (params?.tz) search.set("tz", params.tz);
    const raw = await this.fetch<unknown>(
      `/api/runtimes/${runtimeId}/usage?${search}`,
    );
    return parseWithFallback<RuntimeUsage[]>(raw, RuntimeUsageListSchema, [], {
      endpoint: "GET /api/runtimes/:id/usage",
    });
  }

  async getRuntimeTaskActivity(
    runtimeId: string,
    params?: { tz?: string },
  ): Promise<RuntimeHourlyActivity[]> {
    // Hour-of-day heatmap follows the viewer's tz, like the other reports on
    // this page. Pass the viewer's IANA zone so the server buckets correctly.
    const search = new URLSearchParams();
    if (params?.tz) search.set("tz", params.tz);
    const raw = await this.fetch<unknown>(
      `/api/runtimes/${runtimeId}/activity?${search}`,
    );
    return parseWithFallback<RuntimeHourlyActivity[]>(
      raw,
      RuntimeHourlyActivityListSchema,
      [],
      { endpoint: "GET /api/runtimes/:id/activity" },
    );
  }

  async getLocalTaskTrace(
    healthPort: number,
    taskId: string,
    params?: { run_id?: string; after_seq?: number; tail?: number },
  ): Promise<TaskTraceResponse> {
    const search = new URLSearchParams();
    if (params?.run_id) search.set("run_id", params.run_id);
    if (params?.after_seq !== undefined) search.set("after_seq", String(params.after_seq));
    if (params?.tail !== undefined) search.set("tail", String(params.tail));
    const suffix = search.toString() ? `?${search}` : "";
    const res = await fetch(`http://127.0.0.1:${healthPort}/traces/tasks/${taskId}${suffix}`);
    if (!res.ok) {
      throw new ApiError(await this.parseErrorMessage(res, "Failed to load local task trace"), res.status, res.statusText);
    }
    return res.json() as Promise<TaskTraceResponse>;
  }

  async listLocalPreviews(healthPort: number, params?: { workspace_id?: string; issue_id?: string }): Promise<{ previews: LocalPreview[] }> {
    const search = new URLSearchParams();
    if (params?.workspace_id) search.set("workspace_id", params.workspace_id);
    if (params?.issue_id) search.set("issue_id", params.issue_id);
    const suffix = search.toString() ? `?${search}` : "";
    const res = await fetch(`http://127.0.0.1:${healthPort}/preview/list${suffix}`);
    if (!res.ok) {
      throw new ApiError(await this.parseErrorMessage(res, "Failed to load local previews"), res.status, res.statusText);
    }
    return res.json() as Promise<{ previews: LocalPreview[] }>;
  }

  async stopLocalPreview(healthPort: number, id: string): Promise<LocalPreview> {
    const res = await fetch(`http://127.0.0.1:${healthPort}/preview/stop`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id }),
    });
    if (!res.ok) {
      throw new ApiError(await this.parseErrorMessage(res, "Failed to stop local preview"), res.status, res.statusText);
    }
    return res.json() as Promise<LocalPreview>;
  }

  async getLocalPreviewLogs(healthPort: number, id: string, tail = 200): Promise<LocalPreviewLogs> {
    const search = new URLSearchParams({ id, tail: String(tail) });
    const res = await fetch(`http://127.0.0.1:${healthPort}/preview/logs?${search}`);
    if (!res.ok) {
      throw new ApiError(await this.parseErrorMessage(res, "Failed to load local preview logs"), res.status, res.statusText);
    }
    return res.json() as Promise<LocalPreviewLogs>;
  }

  getLocalPreviewStreamUrl(
    healthPort: number,
    params?: { workspace_id?: string; issue_id?: string },
  ): string {
    const search = new URLSearchParams();
    if (params?.workspace_id) search.set("workspace_id", params.workspace_id);
    if (params?.issue_id) search.set("issue_id", params.issue_id);
    const suffix = search.toString() ? `?${search}` : "";
    return `http://127.0.0.1:${healthPort}/preview/stream${suffix}`;
  }

  getLocalTaskTraceStreamUrl(
    healthPort: number,
    taskId: string,
    params?: { run_id?: string; after_seq?: number; tail?: number },
  ): string {
    const search = new URLSearchParams();
    if (params?.run_id) search.set("run_id", params.run_id);
    if (params?.after_seq !== undefined) search.set("after_seq", String(params.after_seq));
    if (params?.tail !== undefined) search.set("tail", String(params.tail));
    const suffix = search.toString() ? `?${search}` : "";
    return `http://127.0.0.1:${healthPort}/traces/tasks/${taskId}/stream${suffix}`;
  }

  async getRuntimeUsageByAgent(
    runtimeId: string,
    params?: { days?: number; tz?: string },
  ): Promise<RuntimeUsageByAgent[]> {
    const search = new URLSearchParams();
    if (params?.days) search.set("days", String(params.days));
    if (params?.tz) search.set("tz", params.tz);
    const raw = await this.fetch<unknown>(
      `/api/runtimes/${runtimeId}/usage/by-agent?${search}`,
    );
    return parseWithFallback<RuntimeUsageByAgent[]>(
      raw,
      RuntimeUsageByAgentListSchema,
      [],
      { endpoint: "GET /api/runtimes/:id/usage/by-agent" },
    );
  }

  async getRuntimeUsageByHour(
    runtimeId: string,
    params?: { days?: number; tz?: string },
  ): Promise<RuntimeUsageByHour[]> {
    const search = new URLSearchParams();
    if (params?.days) search.set("days", String(params.days));
    if (params?.tz) search.set("tz", params.tz);
    const raw = await this.fetch<unknown>(
      `/api/runtimes/${runtimeId}/usage/by-hour?${search}`,
    );
    return parseWithFallback<RuntimeUsageByHour[]>(
      raw,
      RuntimeUsageByHourListSchema,
      [],
      { endpoint: "GET /api/runtimes/:id/usage/by-hour" },
    );
  }

  async pingRuntime(runtimeId: string): Promise<RuntimePing> {
    return this.fetch(`/api/runtimes/${runtimeId}/ping`, { method: "POST" });
  }

  async getPingResult(runtimeId: string, pingId: string): Promise<RuntimePing> {
    return this.fetch(`/api/runtimes/${runtimeId}/ping/${pingId}`);
  }

  async getCLIUpdateManifest(): Promise<CLIUpdateManifest> {
    return this.fetch("/api/runtimes/cli-update-manifest");
  }

  // ---------------------------------------------------------------------------
  // Workspace dashboard — three independent rollups for `/{slug}/dashboard`.
  // Each accepts an optional `project_id` to narrow the scope to one project.
  // Cost is computed client-side from the model pricing table (same contract
  // as the per-runtime endpoints above).
  // ---------------------------------------------------------------------------

  async getDashboardUsageDaily(
    params: { days?: number; project_id?: string | null; tz?: string },
  ): Promise<DashboardUsageDaily[]> {
    const search = new URLSearchParams();
    if (params.days) search.set("days", String(params.days));
    if (params.project_id) search.set("project_id", params.project_id);
    if (params.tz) search.set("tz", params.tz);
    const raw = await this.fetch<unknown>(`/api/dashboard/usage/daily?${search}`);
    return parseWithFallback<DashboardUsageDaily[]>(
      raw,
      DashboardUsageDailyListSchema,
      [],
      { endpoint: "GET /api/dashboard/usage/daily" },
    );
  }

  async getDashboardUsageByAgent(
    params: { days?: number; project_id?: string | null; tz?: string },
  ): Promise<DashboardUsageByAgent[]> {
    const search = new URLSearchParams();
    if (params.days) search.set("days", String(params.days));
    if (params.project_id) search.set("project_id", params.project_id);
    if (params.tz) search.set("tz", params.tz);
    const raw = await this.fetch<unknown>(`/api/dashboard/usage/by-agent?${search}`);
    return parseWithFallback<DashboardUsageByAgent[]>(
      raw,
      DashboardUsageByAgentListSchema,
      [],
      { endpoint: "GET /api/dashboard/usage/by-agent" },
    );
  }

  async getDashboardLocalUsageDaily(
    params: { days?: number; project_id?: string | null },
  ): Promise<DashboardUsageDaily[]> {
    const search = new URLSearchParams();
    if (params.days) search.set("days", String(params.days));
    if (params.project_id) search.set("project_id", params.project_id);
    const raw = await this.fetch<unknown>(`/api/dashboard/local-usage/daily?${search}`);
    return parseWithFallback<DashboardUsageDaily[]>(
      raw,
      DashboardUsageDailyListSchema,
      [],
      { endpoint: "GET /api/dashboard/local-usage/daily" },
    );
  }

  async getDashboardLocalUsageByRunner(
    params: { days?: number; project_id?: string | null },
  ): Promise<DashboardLocalUsageByRunner[]> {
    const search = new URLSearchParams();
    if (params.days) search.set("days", String(params.days));
    if (params.project_id) search.set("project_id", params.project_id);
    const raw = await this.fetch<unknown>(`/api/dashboard/local-usage/by-runner?${search}`);
    return parseWithFallback<DashboardLocalUsageByRunner[]>(
      raw,
      DashboardLocalUsageByRunnerListSchema,
      [],
      { endpoint: "GET /api/dashboard/local-usage/by-runner" },
    );
  }

  async getDashboardLocalRunTimeByRunner(
    params: { days?: number; project_id?: string | null },
  ): Promise<DashboardLocalRunTimeByRunner[]> {
    const search = new URLSearchParams();
    if (params.days) search.set("days", String(params.days));
    if (params.project_id) search.set("project_id", params.project_id);
    const raw = await this.fetch<unknown>(`/api/dashboard/local-runtime/by-runner?${search}`);
    return parseWithFallback<DashboardLocalRunTimeByRunner[]>(
      raw,
      DashboardLocalRunTimeByRunnerListSchema,
      [],
      { endpoint: "GET /api/dashboard/local-runtime/by-runner" },
    );
  }

  async getDashboardLocalRunTimeDaily(
    params: { days?: number; project_id?: string | null },
  ): Promise<DashboardRunTimeDaily[]> {
    const search = new URLSearchParams();
    if (params.days) search.set("days", String(params.days));
    if (params.project_id) search.set("project_id", params.project_id);
    const raw = await this.fetch<unknown>(`/api/dashboard/local-runtime/daily?${search}`);
    return parseWithFallback<DashboardRunTimeDaily[]>(
      raw,
      DashboardRunTimeDailyListSchema,
      [],
      { endpoint: "GET /api/dashboard/local-runtime/daily" },
    );
  }

  async getDashboardAgentRunTime(
    params: { days?: number; project_id?: string | null; tz?: string },
  ): Promise<DashboardAgentRunTime[]> {
    const search = new URLSearchParams();
    if (params.days) search.set("days", String(params.days));
    if (params.project_id) search.set("project_id", params.project_id);
    // `tz` aligns the "last N days" cutoff with the viewer's calendar,
    // matching the per-agent token card.
    if (params.tz) search.set("tz", params.tz);
    const raw = await this.fetch<unknown>(`/api/dashboard/agent-runtime?${search}`);
    return parseWithFallback<DashboardAgentRunTime[]>(
      raw,
      DashboardAgentRunTimeListSchema,
      [],
      { endpoint: "GET /api/dashboard/agent-runtime" },
    );
  }

  async getDashboardRunTimeDaily(
    params: { days?: number; project_id?: string | null; tz?: string },
  ): Promise<DashboardRunTimeDaily[]> {
    const search = new URLSearchParams();
    if (params.days) search.set("days", String(params.days));
    if (params.project_id) search.set("project_id", params.project_id);
    // `tz` cuts the day buckets in the viewer's calendar so Time / Tasks
    // align with the Cost / Tokens charts.
    if (params.tz) search.set("tz", params.tz);
    const raw = await this.fetch<unknown>(`/api/dashboard/runtime/daily?${search}`);
    return parseWithFallback<DashboardRunTimeDaily[]>(
      raw,
      DashboardRunTimeDailyListSchema,
      [],
      { endpoint: "GET /api/dashboard/runtime/daily" },
    );
  }

  async getAgentRunDashboard(params: {
    days?: number;
    agent_ids?: string[];
    owner_id?: string;
    start_hour?: number;
    end_hour?: number;
    tz?: string;
    limit?: number;
  }): Promise<AgentRunDashboard> {
    const search = new URLSearchParams();
    if (params.days) search.set("days", String(params.days));
    if (params.start_hour !== undefined) search.set("start_hour", String(params.start_hour));
    if (params.end_hour !== undefined) search.set("end_hour", String(params.end_hour));
    if (params.owner_id) search.set("owner_id", params.owner_id);
    if (params.tz) search.set("tz", params.tz);
    if (params.limit) search.set("limit", String(params.limit));
    for (const id of params.agent_ids ?? []) {
      if (id) search.append("agent_id", id);
    }
    const raw = await this.fetch<unknown>(`/api/dashboard/agent-runs?${search}`);
    return parseWithFallback<AgentRunDashboard>(
      raw,
      AgentRunDashboardSchema,
      EMPTY_AGENT_RUN_DASHBOARD,
      { endpoint: "GET /api/dashboard/agent-runs" },
    );
  }

  async getAgentRunDashboardRunDetail(
    taskId: string,
  ): Promise<AgentRunDashboardRunDetail> {
    const raw = await this.fetch<unknown>(`/api/dashboard/agent-runs/${taskId}`);
    return parseWithFallback<AgentRunDashboardRunDetail>(
      raw,
      AgentRunDashboardRunDetailSchema,
      EMPTY_AGENT_RUN_DETAIL,
      { endpoint: "GET /api/dashboard/agent-runs/{taskId}" },
    );
  }

  async initiateUpdate(
    runtimeId: string,
    targetVersion?: string,
  ): Promise<RuntimeUpdate> {
    const body =
      targetVersion && targetVersion.trim()
        ? { target_version: targetVersion.trim() }
        : {};
    return this.fetch(`/api/runtimes/${runtimeId}/update`, {
      method: "POST",
      body: JSON.stringify(body),
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

  // Interactions (approval requests)
  async listTaskInteractions(taskId: string, status?: string): Promise<TaskInteraction[]> {
    const qs = status ? `?status=${status}` : "";
    return this.fetch(`/api/tasks/${taskId}/interactions${qs}`);
  }

  async respondInteraction(taskId: string, interactionId: string, chosenOption: string, responseMessage?: string): Promise<TaskInteraction> {
    return this.fetch(`/api/tasks/${taskId}/interactions/${interactionId}/respond`, {
      method: "POST",
      body: JSON.stringify({ chosen_option: chosenOption, response_message: responseMessage }),
    });
  }

  async rerunIssue(issueId: string, taskId?: string, retryInstruction?: string): Promise<AgentTask> {
    const body: { task_id?: string; retry_instruction?: string } = {};
    if (taskId) body.task_id = taskId;
    if (retryInstruction) body.retry_instruction = retryInstruction;
    return this.fetch(`/api/issues/${issueId}/rerun`, {
      method: "POST",
      body: JSON.stringify(body),
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

  async upsertMobilePushRegistration(
    request: UpsertMobilePushRegistrationRequest,
  ): Promise<MobilePushRegistrationResponse> {
    const response = await this.fetch("/api/me/mobile-push/registrations", {
      method: "PUT",
      body: JSON.stringify(request),
    });
    return parseWithFallback(
      response,
      MobilePushRegistrationResponseSchema,
      EMPTY_MOBILE_PUSH_REGISTRATION_RESPONSE,
      { endpoint: "PUT /api/me/mobile-push/registrations" },
    );
  }

  async disableMobilePushRegistration(
    installationId: string,
    provider = "getui",
  ): Promise<void> {
    const search = new URLSearchParams({ provider });
    await this.fetch(
      `/api/me/mobile-push/registrations/${encodeURIComponent(installationId)}?${search}`,
      { method: "DELETE" },
    );
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
  async getConfig(): Promise<AppConfigResponse> {
    const raw = await this.fetch<unknown>("/api/config");
    return parseWithFallback<AppConfigResponse>(raw, AppConfigSchema, EMPTY_APP_CONFIG, {
      endpoint: "GET /api/config",
    });
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

  async updateWorkspace(id: string, data: { name?: string; description?: string; context?: string; wiki_content?: string; settings?: Record<string, unknown>; repos?: WorkspaceRepo[]; issue_prefix?: string; avatar_url?: string }): Promise<Workspace> {
    return this.fetch(`/api/workspaces/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    });
  }

  // Wiki pages
  async listWikiPages(): Promise<ListWikiPagesResponse> {
    return this.fetch("/api/wiki-pages");
  }

  async getWikiPage(id: string): Promise<WikiPage> {
    return this.fetch(`/api/wiki-pages/${id}`);
  }

  async createWikiPage(data: CreateWikiPageRequest): Promise<WikiPage> {
    return this.fetch("/api/wiki-pages", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async updateWikiPage(id: string, data: UpdateWikiPageRequest): Promise<WikiPage> {
    return this.fetch(`/api/wiki-pages/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    });
  }

  async deleteWikiPage(id: string): Promise<{ deleted: boolean; page_id: string; child_count: number }> {
    return this.fetch(`/api/wiki-pages/${id}`, { method: "DELETE" });
  }

  async reorderWikiPages(data: ReorderWikiPagesRequest): Promise<ListWikiPagesResponse> {
    return this.fetch("/api/wiki-pages/reorder", {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  async listWikiPageActivities(pageId: string, limit = 50): Promise<ListWikiPageActivitiesResponse> {
    return this.fetch(`/api/wiki-pages/${pageId}/activity?limit=${limit}`);
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

  async createInviteLink(workspaceId: string, data: CreateInviteLinkRequest): Promise<InviteLink> {
    return this.fetch(`/api/workspaces/${workspaceId}/invite-links`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async listInviteLinks(workspaceId: string): Promise<InviteLink[]> {
    return this.fetch(`/api/workspaces/${workspaceId}/invite-links`);
  }

  async revokeInviteLink(workspaceId: string, invitationId: string): Promise<void> {
    await this.fetch(`/api/workspaces/${workspaceId}/invite-links/${invitationId}`, {
      method: "DELETE",
    });
  }

  async validateInviteLink(token: string): Promise<InviteLink> {
    return this.fetch(`/api/invite-links/${encodeURIComponent(token)}`);
  }

  async acceptInviteLink(token: string): Promise<MemberWithUser> {
    return this.fetch(`/api/invite-links/${encodeURIComponent(token)}/accept`, {
      method: "POST",
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

  async importSkill(data: { url: string; gitee_token?: string; overwrite?: boolean }): Promise<Skill> {
    return this.fetch("/api/skills/import", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async discoverImportSkills(data: { url: string; gitee_token?: string }): Promise<DiscoverImportSkillsResponse> {
    const raw = await this.fetch<unknown>("/api/skills/import/discover", {
      method: "POST",
      body: JSON.stringify(data),
    });
    return parseWithFallback(raw, DiscoverImportSkillsResponseSchema, EMPTY_DISCOVER_IMPORT_SKILLS_RESPONSE, {
      endpoint: "POST /api/skills/import/discover",
    });
  }

  async batchImportSkills(data: { skills: CreateSkillRequest[] }): Promise<BatchImportSkillsResponse> {
    return this.fetch("/api/skills/batch-import", {
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
  async uploadFile(
    file: File,
    opts?: { issueId?: string; commentId?: string; chatSessionId?: string },
  ): Promise<Attachment> {
    const uploadOpts = opts ?? {};
    if (file.size >= MULTIPART_UPLOAD_THRESHOLD_BYTES) {
      return this.uploadFileMultipartDirect(file, uploadOpts);
    }
    return this.uploadFileDirect(file, uploadOpts);
  }

  private async uploadFileMultipartDirect(
    file: File,
    opts: { issueId?: string; commentId?: string; chatSessionId?: string },
  ): Promise<Attachment> {
    const initiate = await this.fetch<MultipartUploadInitiateResponse>("/api/attachments/upload/multipart/initiate", {
      method: "POST",
      body: JSON.stringify({
        filename: file.name,
        content_type: file.type || "application/octet-stream",
        size_bytes: file.size,
        part_size_bytes: MULTIPART_UPLOAD_PART_SIZE_BYTES,
        issue_id: opts.issueId ?? null,
        comment_id: opts.commentId ?? null,
        chat_session_id: opts.chatSessionId ?? null,
      }),
    });

    const partSize = initiate.part_size_bytes;
    const uploadedParts: Array<{ part_number: number; etag: string; size_bytes: number }> = [];
    let nextPart = 1;

    const uploadPart = async (partNumber: number): Promise<void> => {
      const signed = await this.fetch<MultipartUploadSignPartsResponse>("/api/attachments/upload/multipart/sign-parts", {
        method: "POST",
        body: JSON.stringify({
          session_id: initiate.session_id,
          part_numbers: [partNumber],
        }),
      });
      const part = signed.parts[0];
      if (!part) throw new Error("Multipart upload signing returned no part URL");
      const start = (partNumber - 1) * partSize;
      const end = Math.min(start + partSize, file.size);
      const blob = file.slice(start, end);
      let lastError: unknown;
      for (let attempt = 1; attempt <= MULTIPART_UPLOAD_MAX_RETRIES; attempt += 1) {
        try {
          const res = await fetch(part.upload_url, {
            method: "PUT",
            headers: part.headers ?? {},
            body: blob,
          });
          if (!res.ok) {
            throw new Error(`Multipart part upload failed: ${res.status}`);
          }
          const etag = res.headers.get("ETag") ?? res.headers.get("etag") ?? "";
          if (!etag) throw new Error("Multipart part upload missing ETag");
          uploadedParts.push({
            part_number: partNumber,
            etag,
            size_bytes: blob.size,
          });
          return;
        } catch (err) {
          lastError = err;
          if (attempt === MULTIPART_UPLOAD_MAX_RETRIES) break;
        }
      }
      throw lastError instanceof Error ? lastError : new Error("Multipart part upload failed");
    };

    const workers = Array.from({ length: Math.min(MULTIPART_UPLOAD_CONCURRENCY, initiate.part_count) }, async () => {
      while (nextPart <= initiate.part_count) {
        const partNumber = nextPart;
        nextPart += 1;
        await uploadPart(partNumber);
      }
    });

    try {
      await Promise.all(workers);
    } catch (err) {
      await this.abortMultipartUpload(initiate.session_id);
      throw err;
    }

    const raw = await this.fetch<unknown>("/api/attachments/upload/multipart/complete", {
      method: "POST",
      body: JSON.stringify({
        session_id: initiate.session_id,
        parts: uploadedParts.sort((a, b) => a.part_number - b.part_number),
      }),
    });
    return parseWithFallback(raw, AttachmentResponseSchema, EMPTY_ATTACHMENT, {
      endpoint: "POST /api/attachments/upload/multipart/complete",
    });
  }

  private async abortMultipartUpload(sessionId: string): Promise<void> {
    try {
      await this.fetch<void>("/api/attachments/upload/multipart/abort", {
        method: "POST",
        body: JSON.stringify({ session_id: sessionId }),
      });
    } catch (err) {
      this.logger.warn("Failed to abort multipart upload", { error: err instanceof Error ? err.message : String(err) });
    }
  }

  private async uploadFileDirect(
    file: File,
    opts: { issueId?: string; commentId?: string; chatSessionId?: string },
  ): Promise<Attachment> {
    const initiate = await this.fetch<DirectUploadInitiateResponse>("/api/attachments/upload/initiate", {
      method: "POST",
      body: JSON.stringify({
        filename: file.name,
        content_type: file.type || "application/octet-stream",
        size_bytes: file.size,
        issue_id: opts.issueId ?? null,
        comment_id: opts.commentId ?? null,
        chat_session_id: opts.chatSessionId ?? null,
      }),
    });

    const putRes = await fetch(initiate.upload_url, {
      method: "PUT",
      headers: initiate.headers ?? {},
      body: file,
    });
    if (!putRes.ok) {
      throw new Error(`Direct upload failed: ${putRes.status}`);
    }

    const raw = await this.fetch<unknown>("/api/attachments/upload/complete", {
      method: "POST",
      body: JSON.stringify({ upload_token: initiate.upload_token }),
    });
    return parseWithFallback(raw, AttachmentResponseSchema, EMPTY_ATTACHMENT, {
      endpoint: "POST /api/attachments/upload/complete",
    });
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

  async updateChatSession(id: string, data: { title: string }): Promise<ChatSession> {
    return this.fetch(`/api/chat/sessions/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    });
  }

  async listChatMessages(sessionId: string): Promise<ChatMessage[]> {
    return this.fetch(`/api/chat/sessions/${sessionId}/messages`);
  }

  async listChatMessagesPage(
    sessionId: string,
    params: { before?: { created_at: string; id: string } | null; limit?: number } = {},
  ): Promise<ChatMessagesPage> {
    const limit = params.limit ?? 50;
    const query = new URLSearchParams({ limit: String(limit) });
    if (params.before) {
      query.set("before_created_at", params.before.created_at);
      query.set("before_id", params.before.id);
    }
    try {
      return await this.fetch(
        `/api/chat/sessions/${sessionId}/messages/page?${query.toString()}`,
      );
    } catch (err) {
      // Deployment-order compatibility: a backend deployed before this endpoint
      // existed returns 404 for the unknown route. Fall back to the legacy
      // full-list endpoint so chat never white-screens regardless of whether
      // the server or the client deploys first. Only the initial (cursorless)
      // page falls back — the legacy endpoint returns every message at once, so
      // the fallback page reports has_more: false and there is no follow-up
      // request to translate. A 404 on a cursor request is an unexpected state
      // and propagates instead of duplicating the whole list.
      if (err instanceof ApiError && err.status === 404 && !params.before) {
        const messages = await this.listChatMessages(sessionId);
        return { messages, limit, has_more: false, next_cursor: null };
      }
      throw err;
    }
  }

  async sendChatMessage(
    sessionId: string,
    content: string,
    attachmentIds?: string[],
  ): Promise<SendChatMessageResponse> {
    const body: { content: string; attachment_ids?: string[] } = { content };
    if (attachmentIds && attachmentIds.length > 0) {
      body.attachment_ids = attachmentIds;
    }
    return this.fetch(`/api/chat/sessions/${sessionId}/messages`, {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  async deleteChatMessage(sessionId: string, messageId: string): Promise<void> {
    await this.fetch(`/api/chat/sessions/${sessionId}/messages/${messageId}`, {
      method: "DELETE",
    });
  }

  async retryChatMessage(sessionId: string, messageId: string): Promise<SendChatMessageResponse> {
    return this.fetch(`/api/chat/sessions/${sessionId}/messages/${messageId}/retry`, {
      method: "POST",
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

  async getAttachmentPreviewURL(id: string): Promise<{ url: string; expires_at: number }> {
    return this.fetch<{ url: string; expires_at: number }>(`/api/attachments/${id}/preview-url`);
  }

  // Fetches the raw bytes of a text-previewable attachment.
  //
  // The endpoint sidesteps CloudFront CORS (not configured on the CDN) and
  // bypasses Content-Disposition: attachment for the `text/*` family, both
  // of which would otherwise prevent the renderer from getting the body.
  // The server always replies with `text/plain; charset=utf-8` for safety;
  // the original MIME ships back in the `X-Original-Content-Type` header so
  // the preview dispatcher can choose between markdown / html / plain code.
  //
  // Routes through `fetchRaw` so it inherits the standard auth headers,
  // 401 → handleUnauthorized recovery, request-id logging, and ApiError
  // shape. 413 / 415 are translated to typed `Preview*Error` instances so
  // the modal can render specific fallbacks instead of generic failure.
  async getAttachmentTextContent(
    id: string,
  ): Promise<{ text: string; originalContentType: string }> {
    let res: Response;
    try {
      res = await this.fetchRaw(`/api/attachments/${id}/content`);
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.status === 413) throw new PreviewTooLargeError();
        if (err.status === 415) throw new PreviewUnsupportedError();
      }
      throw err;
    }
    return {
      text: await res.text(),
      originalContentType: res.headers.get("X-Original-Content-Type") ?? "",
    };
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

  async updateProjectResource(
    projectId: string,
    resourceId: string,
    data: UpdateProjectResourceRequest,
  ): Promise<ProjectResource> {
    return this.fetch(`/api/projects/${projectId}/resources/${resourceId}`, {
      method: "PUT",
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
  async listLabels(params?: { project_id?: string | null }): Promise<ListLabelsResponse> {
    const search = new URLSearchParams();
    if (params?.project_id) search.set("project_id", params.project_id);
    const qs = search.toString();
    const raw = await this.fetch<unknown>(`/api/labels${qs ? `?${qs}` : ""}`);
    return parseWithFallback(raw, ListLabelsResponseSchema, EMPTY_LIST_LABELS_RESPONSE, {
      endpoint: "GET /api/labels",
    });
  }

  async getLabel(id: string): Promise<Label> {
    const raw = await this.fetch<unknown>(`/api/labels/${id}`);
    return parseWithFallback(raw, LabelSchema, {
      id,
      workspace_id: "",
      project_id: null,
      name: "",
      color: "#64748b",
      created_at: "",
      updated_at: "",
    }, {
      endpoint: "GET /api/labels/:id",
    });
  }

  async createLabel(data: CreateLabelRequest): Promise<Label> {
    const raw = await this.fetch<unknown>(`/api/labels`, {
      method: "POST",
      body: JSON.stringify(data),
    });
    return parseWithFallback(raw, LabelSchema, {
      id: "",
      workspace_id: "",
      project_id: data.project_id ?? null,
      name: data.name,
      color: data.color,
      created_at: "",
      updated_at: "",
    }, {
      endpoint: "POST /api/labels",
    });
  }

  async updateLabel(id: string, data: UpdateLabelRequest): Promise<Label> {
    const raw = await this.fetch<unknown>(`/api/labels/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
    return parseWithFallback(raw, LabelSchema, {
      id,
      workspace_id: "",
      project_id: null,
      name: data.name ?? "",
      color: data.color ?? "#64748b",
      created_at: "",
      updated_at: "",
    }, {
      endpoint: "PUT /api/labels/:id",
    });
  }

  async deleteLabel(id: string): Promise<void> {
    await this.fetch(`/api/labels/${id}`, { method: "DELETE" });
  }

  async listLabelsForIssue(issueId: string): Promise<IssueLabelsResponse> {
    const raw = await this.fetch<unknown>(`/api/issues/${issueId}/labels`);
    return parseWithFallback(raw, IssueLabelsResponseSchema, EMPTY_ISSUE_LABELS_RESPONSE, {
      endpoint: "GET /api/issues/:id/labels",
    });
  }

  async attachLabel(issueId: string, labelId: string): Promise<IssueLabelsResponse> {
    const raw = await this.fetch<unknown>(`/api/issues/${issueId}/labels`, {
      method: "POST",
      body: JSON.stringify({ label_id: labelId }),
    });
    return parseWithFallback(raw, IssueLabelsResponseSchema, EMPTY_ISSUE_LABELS_RESPONSE, {
      endpoint: "POST /api/issues/:id/labels",
    });
  }

  async detachLabel(issueId: string, labelId: string): Promise<IssueLabelsResponse> {
    const raw = await this.fetch<unknown>(`/api/issues/${issueId}/labels/${labelId}`, {
      method: "DELETE",
    });
    return parseWithFallback(raw, IssueLabelsResponseSchema, EMPTY_ISSUE_LABELS_RESPONSE, {
      endpoint: "DELETE /api/issues/:id/labels/:labelId",
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

  // Squads
  async listSquads(): Promise<Squad[]> {
    const raw = await this.fetch<unknown>(`/api/squads`);
    return parseWithFallback(raw, SquadListSchema, EMPTY_SQUAD_LIST, {
      endpoint: "GET /api/squads",
    }) as Squad[];
  }

  async getSquad(id: string): Promise<Squad> {
    const raw = await this.fetch<unknown>(`/api/squads/${id}`);
    return parseWithFallback(raw, SquadSchema, EMPTY_SQUAD, {
      endpoint: "GET /api/squads/:id",
    }) as Squad;
  }

  async createSquad(data: { name: string; description?: string; leader_id: string; avatar_url?: string }): Promise<Squad> {
    const raw = await this.fetch<unknown>("/api/squads", { method: "POST", body: JSON.stringify(data) });
    return parseWithFallback(raw, SquadSchema, EMPTY_SQUAD, {
      endpoint: "POST /api/squads",
    }) as Squad;
  }

  async updateSquad(id: string, data: { name?: string; description?: string; instructions?: string; leader_id?: string; avatar_url?: string }): Promise<Squad> {
    const raw = await this.fetch<unknown>(`/api/squads/${id}`, { method: "PUT", body: JSON.stringify(data) });
    return parseWithFallback(raw, SquadSchema, EMPTY_SQUAD, {
      endpoint: "PUT /api/squads/:id",
    }) as Squad;
  }

  async deleteSquad(id: string): Promise<void> {
    await this.fetch(`/api/squads/${id}`, { method: "DELETE" });
  }

  async listSquadMembers(squadId: string): Promise<SquadMember[]> {
    return this.fetch(`/api/squads/${squadId}/members`);
  }

  async addSquadMember(squadId: string, data: { member_type: string; member_id: string; role?: string }): Promise<SquadMember> {
    return this.fetch(`/api/squads/${squadId}/members`, { method: "POST", body: JSON.stringify(data) });
  }

  async removeSquadMember(squadId: string, data: { member_type: string; member_id: string }): Promise<void> {
    await this.fetch(`/api/squads/${squadId}/members`, { method: "DELETE", body: JSON.stringify(data) });
  }

  async updateSquadMemberRole(squadId: string, data: { member_type: string; member_id: string; role: string }): Promise<SquadMember> {
    return this.fetch(`/api/squads/${squadId}/members/role`, { method: "PATCH", body: JSON.stringify(data) });
  }

  // Per-squad members status snapshot: one row per member with derived
  // working/idle/offline/unstable plus the issues each agent is currently
  // running. Parsed with a lenient schema so a new server-side status
  // value or extra field can't white-screen the Squad page (#2143).
  async getSquadMemberStatus(squadId: string): Promise<SquadMemberStatusListResponse> {
    const raw = await this.fetch<unknown>(`/api/squads/${squadId}/members/status`);
    return parseWithFallback(raw, SquadMemberStatusListResponseSchema, EMPTY_SQUAD_MEMBER_STATUS_LIST, {
      endpoint: "GET /api/squads/:id/members/status",
    }) as SquadMemberStatusListResponse;
  }

  // Autopilots
  async listAutopilots(params?: { status?: string }): Promise<ListAutopilotsResponse> {
    const search = new URLSearchParams();
    if (params?.status) search.set("status", params.status);
    const raw = await this.fetch<unknown>(`/api/autopilots?${search}`);
    return parseWithFallback(raw, ListAutopilotsResponseSchema, EMPTY_LIST_AUTOPILOTS_RESPONSE, {
      endpoint: "GET /api/autopilots",
    }) as ListAutopilotsResponse;
  }

  async getAutopilot(id: string): Promise<GetAutopilotResponse> {
    const raw = await this.fetch<unknown>(`/api/autopilots/${id}`);
    return parseWithFallback(raw, GetAutopilotResponseSchema, EMPTY_GET_AUTOPILOT_RESPONSE, {
      endpoint: "GET /api/autopilots/:id",
    }) as GetAutopilotResponse;
  }

  async createAutopilot(data: CreateAutopilotRequest): Promise<Autopilot> {
    const raw = await this.fetch<unknown>("/api/autopilots", {
      method: "POST",
      body: JSON.stringify(data),
    });
    return parseWithFallback(raw, AutopilotResponseSchema, EMPTY_AUTOPILOT, {
      endpoint: "POST /api/autopilots",
    }) as Autopilot;
  }

  async updateAutopilot(id: string, data: UpdateAutopilotRequest): Promise<Autopilot> {
    const raw = await this.fetch<unknown>(`/api/autopilots/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    });
    return parseWithFallback(raw, AutopilotResponseSchema, { ...EMPTY_AUTOPILOT, id }, {
      endpoint: "PATCH /api/autopilots/:id",
    }) as Autopilot;
  }

  async deleteAutopilot(id: string): Promise<void> {
    await this.fetch(`/api/autopilots/${id}`, { method: "DELETE" });
  }

  async triggerAutopilot(id: string, data?: TriggerAutopilotRequest): Promise<AutopilotRun> {
    const init: RequestInit = { method: "POST" };
    if (data) {
      init.body = JSON.stringify(data);
    }
    const raw = await this.fetch<unknown>(`/api/autopilots/${id}/trigger`, init);
    return parseWithFallback(raw, AutopilotRunResponseSchema, { ...EMPTY_AUTOPILOT_RUN, autopilot_id: id }, {
      endpoint: "POST /api/autopilots/:id/trigger",
    }) as AutopilotRun;
  }

  async listAutopilotRuns(id: string, params?: { limit?: number; offset?: number }): Promise<ListAutopilotRunsResponse> {
    const search = new URLSearchParams();
    if (params?.limit) search.set("limit", params.limit.toString());
    if (params?.offset) search.set("offset", params.offset.toString());
    const raw = await this.fetch<unknown>(`/api/autopilots/${id}/runs?${search}`);
    return parseWithFallback(raw, ListAutopilotRunsResponseSchema, EMPTY_LIST_AUTOPILOT_RUNS_RESPONSE, {
      endpoint: "GET /api/autopilots/:id/runs",
    }) as ListAutopilotRunsResponse;
  }

  // Returns a single run including its full trigger_payload. List responses
  // omit trigger_payload to keep them small (a webhook envelope can be
  // up to 256 KiB × limit rows), so the detail view fetches via this route.
  async getAutopilotRun(autopilotId: string, runId: string): Promise<AutopilotRun> {
    const raw = await this.fetch<unknown>(`/api/autopilots/${autopilotId}/runs/${runId}`);
    return parseWithFallback(raw, AutopilotRunResponseSchema, { ...EMPTY_AUTOPILOT_RUN, id: runId, autopilot_id: autopilotId }, {
      endpoint: "GET /api/autopilots/:id/runs/:runId",
    }) as AutopilotRun;
  }

  async cancelAutopilotRun(autopilotId: string, runId: string): Promise<AutopilotRun> {
    const raw = await this.fetch<unknown>(`/api/autopilots/${autopilotId}/runs/${runId}/cancel`, {
      method: "POST",
    });
    return parseWithFallback(raw, AutopilotRunResponseSchema, { ...EMPTY_AUTOPILOT_RUN, id: runId, autopilot_id: autopilotId }, {
      endpoint: "POST /api/autopilots/:id/runs/:runId/cancel",
    }) as AutopilotRun;
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

  // Personal Agent Defaults
  async getPersonalAgentDefaults(workspaceId: string): Promise<AgentDefaults> {
    return this.fetch(`/api/workspaces/${workspaceId}/agent-defaults/me`);
  }

  async updatePersonalAgentDefaults(workspaceId: string, config: Record<string, unknown>): Promise<AgentDefaults> {
    return this.fetch(`/api/workspaces/${workspaceId}/agent-defaults/me`, {
      method: "PUT",
      body: JSON.stringify({ config }),
    });
  }

  async listAllAgentDefaults(workspaceId: string): Promise<AgentDefaultsWithUser[]> {
    return this.fetch(`/api/workspaces/${workspaceId}/agent-defaults`);
  }

  async duplicateAgentDefaults(workspaceId: string, configId: string): Promise<AgentDefaults> {
    return this.fetch(`/api/workspaces/${workspaceId}/agent-defaults/duplicate/${configId}`, {
      method: "POST",
    });
  }

  async listInstructionsHistory(
    workspaceId: string,
    scope: InstructionsHistoryScope,
  ): Promise<ListInstructionsHistoryResponse> {
    const search = new URLSearchParams({ scope });
    return this.fetch(`/api/workspaces/${workspaceId}/instructions-history?${search}`);
  }

  async getInstructionsHistory(
    workspaceId: string,
    versionId: string,
    scope: InstructionsHistoryScope,
  ): Promise<InstructionsHistoryDetail> {
    const search = new URLSearchParams({ scope });
    return this.fetch(`/api/workspaces/${workspaceId}/instructions-history/${versionId}?${search}`);
  }

  async rotateAutopilotTriggerWebhookToken(
    autopilotId: string,
    triggerId: string,
  ): Promise<AutopilotTrigger> {
    return this.fetch(
      `/api/autopilots/${autopilotId}/triggers/${triggerId}/rotate-webhook-token`,
      { method: "POST" },
    );
  }

  // Webhook deliveries — list is slim (no raw_body / selected_headers /
  // response_body); detail returns the full row. Both responses are parsed
  // through a lenient schema so an unknown server-side `status` /
  // `signature_status` value degrades to a generic row instead of dropping
  // the whole list.
  async listAutopilotDeliveries(
    autopilotId: string,
    params?: { limit?: number; offset?: number },
  ): Promise<ListWebhookDeliveriesResponse> {
    const search = new URLSearchParams();
    if (params?.limit) search.set("limit", params.limit.toString());
    if (params?.offset) search.set("offset", params.offset.toString());
    const raw = await this.fetch<unknown>(
      `/api/autopilots/${autopilotId}/deliveries?${search}`,
    );
    return parseWithFallback(
      raw,
      ListWebhookDeliveriesResponseSchema,
      EMPTY_LIST_WEBHOOK_DELIVERIES_RESPONSE,
      { endpoint: "GET /api/autopilots/:id/deliveries" },
    );
  }

  async getAutopilotDelivery(
    autopilotId: string,
    deliveryId: string,
  ): Promise<WebhookDelivery> {
    const raw = await this.fetch<unknown>(
      `/api/autopilots/${autopilotId}/deliveries/${deliveryId}`,
    );
    return parseWithFallback(
      raw,
      WebhookDeliveryResponseSchema,
      { ...EMPTY_WEBHOOK_DELIVERY, id: deliveryId, autopilot_id: autopilotId },
      { endpoint: "GET /api/autopilots/:id/deliveries/:deliveryId" },
    );
  }

  // Replay creates a NEW delivery row referencing the original via
  // `replayed_from_delivery_id`. Server rejects replays of
  // signature-invalid / rejected deliveries with 400 — the UI keeps the
  // button disabled for those rows, but the server is the source of truth.
  async replayAutopilotDelivery(
    autopilotId: string,
    deliveryId: string,
  ): Promise<WebhookDelivery> {
    const raw = await this.fetch<unknown>(
      `/api/autopilots/${autopilotId}/deliveries/${deliveryId}/replay`,
      { method: "POST" },
    );
    return parseWithFallback(
      raw,
      WebhookDeliveryResponseSchema,
      { ...EMPTY_WEBHOOK_DELIVERY, autopilot_id: autopilotId },
      { endpoint: "POST /api/autopilots/:id/deliveries/:deliveryId/replay" },
    );
  }

  // GitHub integration
  async getGitHubConnectURL(workspaceId: string): Promise<GitHubConnectResponse> {
    return this.fetch(`/api/workspaces/${workspaceId}/github/connect`);
  }

  async listGitHubInstallations(workspaceId: string): Promise<ListGitHubInstallationsResponse> {
    return this.fetch(`/api/workspaces/${workspaceId}/github/installations`);
  }

  async deleteGitHubInstallation(workspaceId: string, installationId: string): Promise<void> {
    await this.fetch(`/api/workspaces/${workspaceId}/github/installations/${installationId}`, {
      method: "DELETE",
    });
  }

  async listIssuePullRequests(issueId: string): Promise<{ pull_requests: GitHubPullRequest[] }> {
    return this.fetch(`/api/issues/${issueId}/pull-requests`);
  }

  // ── Gitee Integration ───────────────────────────────────────────────────

  async listGiteeWebhookConfigs(workspaceId: string): Promise<{ configs: GiteeWebhookConfig[] }> {
    return this.fetch(`/api/workspaces/${workspaceId}/gitee/webhook-configs`);
  }

  async createGiteeWebhookConfig(
    workspaceId: string,
    repoOwner: string,
    repoName: string,
  ): Promise<GiteeWebhookConfig> {
    return this.fetch(`/api/workspaces/${workspaceId}/gitee/webhook-configs`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ repo_owner: repoOwner, repo_name: repoName }),
    });
  }

  async deleteGiteeWebhookConfig(workspaceId: string, configId: string): Promise<void> {
    await this.fetch(`/api/workspaces/${workspaceId}/gitee/webhook-configs/${configId}`, {
      method: "DELETE",
    });
  }
}
