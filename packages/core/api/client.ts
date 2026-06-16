import type {
  Issue,
  CreateIssueRequest,
  UpdateIssueRequest,
  GroupedIssuesResponse,
  ListIssuesResponse,
  SearchIssuesResponse,
  SearchProjectsResponse,
  // CEREBRO-PATCH(chat-search-cerebro-client): FIR-902 — Cmd+K chat-session search.
  SearchChatSessionsResponse,
  UpdateMeRequest,
  CreateMemberRequest,
  UpdateMemberRequest,
  ListIssuesParams,
  ListGroupedIssuesParams,
  Agent,
  AgentAvatarBackfillStatus,
  CreateAgentRequest,
  AgentTemplate,
  AgentTemplateSummary,
  CreateAgentFromTemplateRequest,
  CreateAgentFromTemplateResponse,
  UpdateAgentRequest,
  AgentInfisicalFolder,
  AgentEnvResponse,
  UpdateAgentEnvRequest,
  AgentTask,
  AgentActivityBucket,
  AgentRunCount,
  AgentRuntime,
  InboxItem,
  IssueSubscriber,
  Comment,
  MoveCommentToSubIssueResponse,
  MoveCommentsToThreadResponse,
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
  SkillForkParent,
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
  IssueCommentCosts, // CEREBRO-PATCH(issue-comment-cost-client): FIR-39
  RuntimeHourlyActivity,
  RuntimeUsageByAgent,
  RuntimeUsageByHour,
  DashboardUsageDaily,
  DashboardUsageByAgent,
  DashboardAgentRunTime,
  DashboardRunTimeDaily,
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
  ChatMessagesPage,
  ChatPendingTask,
  ChatSessionUsage,
  ChatSessionMessageCosts, // CEREBRO-PATCH(chat-message-cost-client): FIR-31
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
  UpdateProjectResourceRequest,
  ListProjectResourcesResponse,
  Label,
  CreateLabelRequest,
  UpdateLabelRequest,
  ListLabelsResponse,
  IssueLabelsResponse,
  // CEREBRO-PATCH(issue-dependencies): dependencies response type.
  IssueDependenciesResponse,
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
  ListWebhookDeliveriesResponse,
  WebhookDelivery,
  WorkSession,
  UserProfileResponse,
  UserProfileRequest,
  PushSubscriptionResponse,
  NotificationPreferenceResponse,
  NotificationPreferences,
  GitHubPullRequest,
  ListGitHubInstallationsResponse,
  GitHubConnectResponse,
  ListLarkInstallationsResponse,
  BeginLarkInstallResponse,
  LarkInstallStatusResponse,
  RedeemLarkBindingTokenResponse,
  Squad,
  SquadMember,
  SquadMemberStatusListResponse,
  // CEREBRO-PATCH(capability-register-client): FIR-2129 capability register types.
  CapabilityListResponse,
  CapabilityReportInput,
  CapabilitySubject,
  BillingBalance,
  BillingTransactionsPage,
  BillingBatchesPage,
  BillingTopupsPage,
  BillingPriceTier,
  CreateBillingCheckoutSessionRequest,
  CreateBillingCheckoutSessionResponse,
  BillingCheckoutSessionStatus,
  CreateBillingPortalSessionResponse,
  // CEREBRO-PATCH(cerebro-focus-list-client): TECH-2947
  FocusListItem,
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
  AgentToolsListSchema,
  RuntimeToolsListSchema,
  RuntimeToolEffectiveAccessListSchema,
  RuntimeToolGrantsSchema,
  CapabilityListResponseSchema,
  AgentAvatarBackfillStatusSchema,
  AgentTemplateSchema,
  AgentTemplateSummaryListSchema,
  AttachmentResponseSchema,
  ChildIssuesResponseSchema,
  CommentsListSchema,
  CloudRuntimeNodeListSchema,
  CloudRuntimeNodeSchema,
  EMPTY_CLOUD_RUNTIME_NODE,
  EMPTY_CLOUD_RUNTIME_NODE_LIST,
  CreateAgentFromTemplateResponseSchema,
  DashboardAgentRunTimeListSchema,
  DashboardRunTimeDailyListSchema,
  DashboardUsageByAgentListSchema,
  DashboardUsageDailyListSchema,
  EMPTY_AGENT_TEMPLATE_DETAIL,
  EMPTY_AGENT_TEMPLATE_SUMMARY_LIST,
  EMPTY_AGENT_AVATAR_BACKFILL_STATUS,
  EMPTY_APP_CONFIG,
  EMPTY_ATTACHMENT,
  EMPTY_CREATE_AGENT_FROM_TEMPLATE_RESPONSE,
  EMPTY_GROUPED_ISSUES_RESPONSE,
  // CEREBRO-PATCH(issue-dependencies): dependencies schema + fallback.
  EMPTY_ISSUE_DEPENDENCIES,
  IssueDependenciesResponseSchema,
  EMPTY_LIST_ISSUES_RESPONSE,
  EMPTY_SQUAD,
  EMPTY_SQUAD_LIST,
  EMPTY_LIST_WEBHOOK_DELIVERIES_RESPONSE,
  EMPTY_WEBHOOK_DELIVERY,
  EMPTY_TIMELINE_ENTRIES,
  EMPTY_USER,
  AppConfigSchema,
  type AppConfigResponse,
  GroupedIssuesResponseSchema,
  ListIssuesResponseSchema,
  ListWebhookDeliveriesResponseSchema,
  OnboardingNoRuntimeBootstrapResponseSchema,
  OnboardingRuntimeBootstrapResponseSchema,
  SquadSchema,
  SquadListSchema,
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
  SquadMemberStatusListResponseSchema,
  EMPTY_SQUAD_MEMBER_STATUS_LIST,
} from "./schemas";
// CEREBRO-PATCH(api-client-active-terminal-session): inline zod schema for active terminal session lookup.
import { z } from "zod";
const ActiveTerminalSessionSchema = z.object({ session_id: z.string(), attach_path: z.string(), created_at: z.string() }).loose();

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
export interface ImportStarterIssuePayload {
  title: string;
  description: string;
  status: string;
  priority: string;
  assign_to_self: boolean;
}

export interface ImportStarterWelcomeIssueTemplate {
  title: string;
  description: string;
  priority: string;
}

export interface ImportStarterContentPayload {
  workspace_id: string;
  project: { title: string; description: string; icon: string };
  welcome_issue_template: ImportStarterWelcomeIssueTemplate;
  agent_guided_sub_issues: ImportStarterIssuePayload[];
  self_serve_sub_issues: ImportStarterIssuePayload[];
}

export interface ImportStarterContentResponse {
  user: User;
  project_id: string;
  welcome_issue_id: string | null;
}

export interface OnboardingRuntimeBootstrapResponse {
  workspace_id: string;
  agent_id: string;
  issue_id: string;
}

const EMPTY_ONBOARDING_RUNTIME_BOOTSTRAP_RESPONSE:
  OnboardingRuntimeBootstrapResponse = {
  workspace_id: "",
  agent_id: "",
  issue_id: "",
};

export interface OnboardingNoRuntimeBootstrapResponse {
  workspace_id: string;
  issue_id: string;
}

// CEREBRO-PATCH(cerebro-account-client): JEH-921 workspace account types. JEH-881 adds availability fields. JEH-998 adds usage fields.
export interface CerebroAccount {
  id: string;
  workspace_id: string;
  provider: string;
  login_identity: string;
  usage_window_pct: number | null;
  throttled_until: string | null;
  tokens_5h: number; // CEREBRO-PATCH(cerebro-account-client): JEH-1365 rolling account token load.
  tokens_7d: number;
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

const EMPTY_ONBOARDING_NO_RUNTIME_BOOTSTRAP_RESPONSE:
  OnboardingNoRuntimeBootstrapResponse = {
  workspace_id: "",
  issue_id: "",
};

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

  // CEREBRO-PATCH(terminal-ws-auth): exposes the stored bearer token so the
  // terminal WS hook can send it as a first-message auth frame (browsers cannot
  // set Authorization headers on WebSocket upgrades).
  getToken(): string | null {
    return this.token;
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
    // CEREBRO-PATCH(api-client-202-no-body): FIR-2284 + FIR-2321 — some cerebro
    // endpoints (POST /api/runtimes/{id}/tools/scan-now) reply 202 with an empty
    // body; res.json() would throw "Unexpected end of JSON input". But others
    // (POST /api/agents/backfill-avatars, FIR-2321) reply 202 WITH a status body
    // the caller needs, so parse the body when present and only fall back to
    // undefined for a genuinely empty 202.
    if (res.status === 202) {
      const text = await res.text();
      return (text ? JSON.parse(text) : undefined) as T;
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
    const raw = await this.fetch<unknown>("/api/me");
    return parseWithFallback(raw, UserSchema, EMPTY_USER, {
      endpoint: "GET /api/me",
    });
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

  async bootstrapOnboardingRuntime(payload: {
    workspace_id: string;
    runtime_id: string;
  }): Promise<OnboardingRuntimeBootstrapResponse> {
    const raw = await this.fetch<unknown>(
      "/api/me/onboarding/runtime-bootstrap",
      {
        method: "POST",
        body: JSON.stringify(payload),
      },
    );
    return parseWithFallback(
      raw,
      OnboardingRuntimeBootstrapResponseSchema,
      EMPTY_ONBOARDING_RUNTIME_BOOTSTRAP_RESPONSE,
      { endpoint: "POST /api/me/onboarding/runtime-bootstrap" },
    );
  }

  async bootstrapOnboardingNoRuntime(payload: {
    workspace_id: string;
  }): Promise<OnboardingNoRuntimeBootstrapResponse> {
    const raw = await this.fetch<unknown>(
      "/api/me/onboarding/no-runtime-bootstrap",
      {
        method: "POST",
        body: JSON.stringify(payload),
      },
    );
    return parseWithFallback(
      raw,
      OnboardingNoRuntimeBootstrapResponseSchema,
      EMPTY_ONBOARDING_NO_RUNTIME_BOOTSTRAP_RESPONSE,
      { endpoint: "POST /api/me/onboarding/no-runtime-bootstrap" },
    );
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
    const raw = await this.fetch<unknown>("/api/me", {
      method: "PATCH",
      body: JSON.stringify(data),
    });
    return parseWithFallback(raw, UserSchema, EMPTY_USER, {
      endpoint: "PATCH /api/me",
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
  async listFeatureFlags(wsId: string): Promise<{
    overrides: Record<string, boolean>;
    workspace_overrides?: Record<string, boolean>;
    locked?: Record<string, boolean>;
  }> {
    return this.fetch(`/api/workspaces/${wsId}/feature-flags`);
  }

  async setFeatureFlag(wsId: string, key: string, enabled: boolean): Promise<void> {
    await this.fetch(`/api/workspaces/${wsId}/feature-flags/${key}`, {
      method: "PUT",
      body: JSON.stringify({ enabled }),
    });
  }

  // CEREBRO-PATCH(feature-flags-workspace-overrides): FIR-2505 — workspace-level
  // override (owner/admin): force a flag on/off for every member. `locked`
  // forbids members from overriding it personally. listFeatureFlags also gains
  // the workspace_overrides + locked maps in the same patch.
  async setWorkspaceFeatureFlag(
    wsId: string,
    key: string,
    enabled: boolean,
    locked: boolean,
  ): Promise<void> {
    await this.fetch(`/api/workspaces/${wsId}/feature-flags/${key}/workspace`, {
      method: "PUT",
      body: JSON.stringify({ enabled, locked }),
    });
  }

  async clearWorkspaceFeatureFlag(wsId: string, key: string): Promise<void> {
    await this.fetch(`/api/workspaces/${wsId}/feature-flags/${key}/workspace`, {
      method: "DELETE",
    });
  }

  // CEREBRO-PATCH(cost-optimization-client): FIR-2325 per-workspace agent-saving
  // mode overrides (off/shadow/on). Server returns ONLY the overrides — defaults
  // are applied client-side from the cerebro-cost-optimization registry. PUT sets
  // a mode; DELETE reverts a saving to its registry default (clears the override).
  async listCostOptimization(wsId: string): Promise<{ overrides: Record<string, string> }> {
    return this.fetch(`/api/workspaces/${wsId}/cost-optimization`);
  }

  async setCostOptimization(wsId: string, key: string, mode: string): Promise<void> {
    await this.fetch(`/api/workspaces/${wsId}/cost-optimization/${key}`, {
      method: "PUT",
      body: JSON.stringify({ mode }),
    });
  }

  async clearCostOptimization(wsId: string, key: string): Promise<void> {
    await this.fetch(`/api/workspaces/${wsId}/cost-optimization/${key}`, {
      method: "DELETE",
    });
  }

  // CEREBRO-PATCH(cost-optimization-dashboard-client): FIR-2325 phase-5 savings
  // dashboard (estimated would-save vs holdout-measured actual). Returns raw;
  // the cerebro-cost-optimization package validates the shape with a zod schema.
  async getCostOptimizationDashboard(wsId: string): Promise<unknown> {
    return this.fetch(`/api/workspaces/${wsId}/cost-optimization/dashboard`);
  }

  // CEREBRO-PATCH(cost-optimization-holdout-client): FIR-2640 PER-SAVING holdout
  // share (percent of a saving's "on" runs withheld as the A/B control arm). GET
  // returns the raw overrides map; cerebro-cost-optimization validates it. PUT
  // sets one saving's share; DELETE clears it (reverts to the server default).
  async getCostOptimizationHoldout(wsId: string): Promise<unknown> {
    return this.fetch(`/api/workspaces/${wsId}/cost-optimization/holdout`);
  }

  async setCostOptimizationHoldout(wsId: string, key: string, pct: number): Promise<void> {
    await this.fetch(`/api/workspaces/${wsId}/cost-optimization/holdout/${key}`, {
      method: "PUT",
      body: JSON.stringify({ holdout_pct: pct }),
    });
  }

  async clearCostOptimizationHoldout(wsId: string, key: string): Promise<void> {
    await this.fetch(`/api/workspaces/${wsId}/cost-optimization/holdout/${key}`, {
      method: "DELETE",
    });
  }

  // CEREBRO-PATCH(cost-optimization-analytics-client): FIR-2765 drill-down analytics.
  async getCostOptimizationAnalyticsSummary(wsId: string): Promise<unknown> {
    return this.fetch(`/api/workspaces/${wsId}/cost-optimization/analytics`);
  }

  async getCostOptimizationAnalyticsIssues(
    wsId: string,
    key: string,
    opts?: { days?: number; limit?: number },
  ): Promise<unknown> {
    const params = new URLSearchParams();
    if (opts?.days !== undefined) params.set("days", String(opts.days));
    if (opts?.limit !== undefined) params.set("limit", String(opts.limit));
    const qs = params.toString();
    const suffix = qs ? `?${qs}` : "";
    return this.fetch(
      `/api/workspaces/${wsId}/cost-optimization/analytics/${encodeURIComponent(key)}/issues${suffix}`,
    );
  }

  async getCostOptimizationAnalyticsIssueRuns(
    wsId: string,
    key: string,
    issueId: string,
    opts?: { days?: number; limit?: number },
  ): Promise<unknown> {
    const params = new URLSearchParams();
    if (opts?.days !== undefined) params.set("days", String(opts.days));
    if (opts?.limit !== undefined) params.set("limit", String(opts.limit));
    const qs = params.toString();
    const suffix = qs ? `?${qs}` : "";
    return this.fetch(
      `/api/workspaces/${wsId}/cost-optimization/analytics/${encodeURIComponent(key)}/issues/${encodeURIComponent(issueId)}/runs${suffix}`,
    );
  }

  // CEREBRO-PATCH(cost-optimization-prompt-inspector-client): FIR-2765 composed prompt view.
  async getCostOptimizationPromptInspector(
    wsId: string,
    repoUrl?: string,
  ): Promise<unknown> {
    const params = new URLSearchParams();
    if (repoUrl) params.set("repo_url", repoUrl);
    const qs = params.toString();
    const suffix = qs ? `?${qs}` : "";
    return this.fetch(
      `/api/workspaces/${wsId}/cost-optimization/prompt-inspector${suffix}`,
    );
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

  // CEREBRO-PATCH(cerebro-credentials-client): JEH-1199 credential
  // registry methods. Bodies are `unknown` so the cerebro-credentials package
  // owns the schema via parseWithFallback (the API Response Compatibility
  // rule in CLAUDE.md).
  async listCerebroCredentials<T = unknown>(wsId: string): Promise<T> {
    return this.fetch<T>(`/api/workspaces/${wsId}/credentials`);
  }
  async createCerebroCredential<T = unknown>(
    wsId: string,
    body: {
      type: string;
      name: string;
      description?: string;
      value: string;
      metadata?: unknown;
      expires_at?: string | null;
    },
  ): Promise<T> {
    return this.fetch<T>(`/api/workspaces/${wsId}/credentials`, {
      method: "POST",
      body: JSON.stringify(body),
    });
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
  // CEREBRO-PATCH(cerebro-credentials-client): JEH-1530 browser rotation/revoke actions.
  async rotateCerebroCredential<T = unknown>(
    wsId: string,
    credId: string,
    body: { value: string },
  ): Promise<T> {
    return this.fetch<T>(`/api/workspaces/${wsId}/credentials/${credId}/rotate`, {
      method: "POST",
      body: JSON.stringify(body),
    });
  }
  async revokeCerebroCredential(wsId: string, credId: string): Promise<void> {
    await this.fetch<void>(`/api/workspaces/${wsId}/credentials/${credId}`, {
      method: "DELETE",
    });
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

  // CEREBRO-PATCH(cerebro-roles-client): FIR-2130 role subject CRUD + assignment.
  // Server routes are mounted by `cerebro-roles-routes` in router.go on the
  // generic /api/workspaces/{id}/roles (workspace-scoped) and /api/roles/{id}
  // (workspace-membership-gated) paths.
  async listCerebroRoles<T = unknown>(wsId: string): Promise<T> {
    return this.fetch<T>(`/api/workspaces/${wsId}/roles`);
  }

  async getCerebroRole<T = unknown>(roleId: string): Promise<T> {
    return this.fetch<T>(`/api/roles/${roleId}`);
  }

  async createCerebroRole<T = unknown>(
    wsId: string,
    body: { name: string; description?: string | null },
  ): Promise<T> {
    return this.fetch<T>(`/api/workspaces/${wsId}/roles`, {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  async updateCerebroRole<T = unknown>(
    roleId: string,
    body: { name?: string; description?: string | null },
  ): Promise<T> {
    return this.fetch<T>(`/api/roles/${roleId}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    });
  }

  async deleteCerebroRole(roleId: string): Promise<void> {
    await this.fetch(`/api/roles/${roleId}`, { method: "DELETE" });
  }

  async listCerebroRoleAssignments<T = unknown>(roleId: string): Promise<T> {
    return this.fetch<T>(`/api/roles/${roleId}/assignments`);
  }

  async assignCerebroRole<T = unknown>(
    roleId: string,
    body: { subject_type: string; subject_id: string },
  ): Promise<T> {
    return this.fetch<T>(`/api/roles/${roleId}/assignments`, {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  async unassignCerebroRole(
    roleId: string,
    subjectType: string,
    subjectId: string,
  ): Promise<void> {
    await this.fetch(
      `/api/roles/${roleId}/assignments/${subjectType}/${subjectId}`,
      { method: "DELETE" },
    );
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

  // CEREBRO-PATCH(cerebro-dashboard-actor-messages-client): TECH-3093 per-member message history
  async getCerebroDashboardActorMessages<T = unknown>(
    actorId: string,
    range: "24h" | "7d" | "30d",
  ): Promise<T> {
    const params = new URLSearchParams({ actor_id: actorId, range });
    return this.fetch<T>(`/api/cerebro/dashboard/actor-messages?${params.toString()}`);
  }

  // CEREBRO-PATCH(cerebro-dashboard-all-messages-client): TECH-3093 all messages table
  async getCerebroDashboardAllMessages<T = unknown>(
    range: "24h" | "7d" | "30d",
    filter?: { actor_type?: string; actor_id?: string | null },
  ): Promise<T> {
    const params = new URLSearchParams({ range });
    if (filter?.actor_type) params.set("actor_type", filter.actor_type);
    if (filter?.actor_id) params.set("actor_id", filter.actor_id);
    return this.fetch<T>(`/api/cerebro/dashboard/all-messages?${params.toString()}`);
  }

  // CEREBRO-PATCH(cerebro-dashboard-session-messages-client): TECH-3139 full session + cost for detail sheet
  async getCerebroDashboardSessionMessages<T = unknown>(sessionId: string): Promise<T> {
    const params = new URLSearchParams({ session_id: sessionId });
    return this.fetch<T>(`/api/cerebro/dashboard/session-messages?${params.toString()}`);
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

  // CEREBRO-PATCH(cerebro-api-request): authenticated request primitive for cerebro packages.
  // CEREBRO-PATCH(cerebro-agent-tools-client): agent tool override client moved to @multica/cerebro-agent-tools.
  async cerebroRequest<T = unknown>(path: string, init?: RequestInit): Promise<T> {
    return this.fetch<T>(path, init);
  }

  // CEREBRO-PATCH(cerebro-references-api): JEH-837/JEH-838 issue reference API.
  //   GET    /api/issues/{id}/references             — list references on an issue
  //   POST   /api/issues/{id}/references             — UPSERT a reference on an issue
  //   PATCH  /api/cerebro/references/{refId}         — patch label/url/metadata
  //   DELETE /api/cerebro/references/{refId}         — drop a reference
  //   GET    /api/cerebro/references?object=&ref_id= — reverse-lookup
  // Bodies are `unknown` so the cerebro-references package owns the schema.
  async listCerebroIssueReferences<T = unknown>(issueId: string): Promise<T> {
    return this.fetch<T>(`/api/issues/${issueId}/references`);
  }
  async createCerebroIssueReference<T = unknown>(issueId: string, payload: unknown): Promise<T> {
    return this.fetch<T>(`/api/issues/${issueId}/references`, {
      method: "POST",
      body: JSON.stringify(payload),
    });
  }
  async updateCerebroReference<T = unknown>(refId: string, payload: unknown): Promise<T> {
    return this.fetch<T>(`/api/cerebro/references/${refId}`, {
      method: "PATCH",
      body: JSON.stringify(payload),
    });
  }
  async deleteCerebroReference(refId: string): Promise<void> {
    await this.fetch<void>(`/api/cerebro/references/${refId}`, { method: "DELETE" });
  }
  async listCerebroReferencesByObject<T = unknown>(object: string, refId: string): Promise<T> {
    const params = new URLSearchParams({ object, ref_id: refId });
    return this.fetch<T>(`/api/cerebro/references?${params.toString()}`);
  }

  // CEREBRO-PATCH(cerebro-identity-client): FIR-2523 — workspace Google
  // identity settings. Body is `unknown` so the cerebro-identity package owns
  // the Zod schema (lenient parsing per API Response Compatibility rules).
  async getCerebroAuthSettings<T = unknown>(workspaceId: string): Promise<T> {
    return this.fetch<T>(
      `/api/cerebro/workspaces/${encodeURIComponent(workspaceId)}/auth-settings`,
    );
  }

  async updateCerebroAuthSettings<T = unknown>(
    workspaceId: string,
    payload: {
      google_signup_domains: string[];
      default_role: string;
      // CEREBRO-PATCH(cerebro-identity-default-group): FIR-2732 fallback group for new members
      default_group_id: string | null;
      // CEREBRO-PATCH(cerebro-identity-sync-toggle): FIR-2596 per-workspace Google group sync flag
      google_workspace_sync_enabled: boolean;
    },
  ): Promise<T> {
    return this.fetch<T>(
      `/api/cerebro/workspaces/${encodeURIComponent(workspaceId)}/auth-settings`,
      { method: "PUT", body: JSON.stringify(payload) },
    );
  }

  // CEREBRO-PATCH(cerebro-duplicate-check-client): FIR-2504 — ask the server
  // for the top similar open issues + LLM verdict when composing a new issue.
  // Body is `unknown` so the cerebro-duplicate-check package owns the schema.
  async checkSimilarCerebroIssues<T = unknown>(payload: {
    title: string;
    description?: string;
    project_id?: string;
  }): Promise<T> {
    return this.fetch<T>(`/api/cerebro/issues/check-similar`, {
      method: "POST",
      body: JSON.stringify(payload),
    });
  }

  // CEREBRO-PATCH(cerebro-duplicate-check-client): FIR-2504 — fire-and-forget
  // adoption event from the create-issue panel (opened existing / attached as
  // sub-issue / dismissed / created_anyway). Returns 204; the panel ignores
  // the response so a slow log write never blocks issue creation.
  async recordCerebroDuplicateCheckEvent(payload: {
    action: "opened" | "attached" | "dismissed" | "created_anyway";
    match_id?: string;
    verdict?: string;
    match_count?: number;
  }): Promise<void> {
    try {
      await this.fetch<unknown>(`/api/cerebro/issues/check-similar/event`, {
        method: "POST",
        body: JSON.stringify(payload),
      });
    } catch {
      // Best-effort telemetry — never surface a failure to the user.
    }
  }

  // CEREBRO-PATCH(cerebro-persona-grants-client-removed): FIR-2284 Bite 3 — the Persona grant control-plane client methods (listPersonaGrants/getPersonaGrant/createPersonaGrant/updatePersonaGrant/deletePersonaGrant/evaluatePersonaGrant/listPersonaGrantAudit) were removed when the old grant-based Access page was retired. The /grants endpoints still exist for the CLI; no remaining web/desktop caller.
  // CEREBRO-PATCH(approvals-client-removed): FIR-2230 phase 5 — the approval-ask client methods (listPendingApprovalAsks/approveAsk/rejectAsk) were removed when the duplicate "Pending" view was consolidated into the dedicated approvals inbox (which calls cerebroRequest). No remaining caller in the upstream client.
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
    // CEREBRO-PATCH(issue-on-behalf-of-filter): MUL-2553 forward on-behalf-of member filter.
    if (params?.on_behalf_of_ids?.length) search.set("on_behalf_of_ids", params.on_behalf_of_ids.join(","));
    // CEREBRO-PATCH(issue-sprint-filter): TECH-3620 forward sprint member filter.
    if (params?.sprint_id) search.set("sprint_id", params.sprint_id);
    if (params?.creator_id) search.set("creator_id", params.creator_id);
    if (params?.project_id) search.set("project_id", params.project_id);
    if (params?.involves_user_id) search.set("involves_user_id", params.involves_user_id);
    if (params?.open_only) search.set("open_only", "true");
    if (params?.scheduled) search.set("scheduled", "true");
    if (params?.reference) search.set("reference", params.reference);
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
    // CEREBRO-PATCH(issue-on-behalf-of-filter): MUL-2553 forward on-behalf-of member filter (grouped).
    if (params.on_behalf_of_ids?.length) search.set("on_behalf_of_ids", params.on_behalf_of_ids.join(","));
    // CEREBRO-PATCH(issue-sprint-filter): TECH-3620 forward sprint member filter (grouped).
    if (params.sprint_id) search.set("sprint_id", params.sprint_id);
    if (params.creator_id) search.set("creator_id", params.creator_id);
    if (params.project_id) search.set("project_id", params.project_id);
    if (params.involves_user_id) search.set("involves_user_id", params.involves_user_id);
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
    if (params.reference) search.set("reference", params.reference);
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

  // CEREBRO-PATCH(chat-search-cerebro-client): FIR-902 — Cmd+K chat-session search via JEH-901 backend.
  async searchChatSessions(params: { q: string; limit?: number; offset?: number; signal?: AbortSignal }): Promise<SearchChatSessionsResponse> {
    const search = new URLSearchParams({ q: params.q });
    if (params.limit !== undefined) search.set("limit", String(params.limit));
    if (params.offset !== undefined) search.set("offset", String(params.offset));
    return this.fetch(`/api/chat/sessions/search?${search}`, params.signal ? { signal: params.signal } : undefined);
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

  async updateComment(commentId: string, content: string, attachmentIds?: string[]): Promise<Comment> {
    return this.fetch(`/api/comments/${commentId}`, {
      method: "PUT",
      body: JSON.stringify({ content, attachment_ids: attachmentIds }),
    });
  }

  async deleteComment(commentId: string): Promise<void> {
    await this.fetch(`/api/comments/${commentId}`, { method: "DELETE" });
  }

  // CEREBRO-PATCH(comments-move-to-subissue-ui): JEH-1309 call backend thread lift endpoint.
  async moveCommentToSubIssue(commentId: string, title?: string): Promise<MoveCommentToSubIssueResponse> {
    return this.fetch(`/api/comments/${commentId}/move-to-subissue`, {
      method: "POST",
      body: JSON.stringify(title ? { title } : {}),
    });
  }

  // CEREBRO-PATCH(comments-move-to-thread-ui): JEH-2488 lift picked comments into a new thread.
  async moveCommentsToNewThread(commentIds: string[]): Promise<MoveCommentsToThreadResponse> {
    return this.fetch(`/api/comments/move-to-thread`, {
      method: "POST",
      body: JSON.stringify({ comment_ids: commentIds }),
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

  // CEREBRO-PATCH(agent-avatar-backfill): admin backfill status endpoint.
  async getAgentAvatarBackfillStatus(): Promise<AgentAvatarBackfillStatus> {
    const raw = await this.fetch<unknown>("/api/agents/backfill-avatars");
    return parseWithFallback(
      raw,
      AgentAvatarBackfillStatusSchema,
      EMPTY_AGENT_AVATAR_BACKFILL_STATUS,
      { endpoint: "GET /api/agents/backfill-avatars" },
    );
  }

  // CEREBRO-PATCH(agent-avatar-backfill): mode/exclude regenerates all avatars except kept agents.
  async startAgentAvatarBackfill(opts?: {
    mode?: "missing" | "all";
    excludeAgentIds?: string[];
  }): Promise<AgentAvatarBackfillStatus> {
    const raw = await this.fetch<unknown>("/api/agents/backfill-avatars", {
      method: "POST",
      body: opts
        ? JSON.stringify({
            mode: opts.mode,
            exclude_agent_ids: opts.excludeAgentIds,
          })
        : undefined,
    });
    return parseWithFallback(
      raw,
      AgentAvatarBackfillStatusSchema,
      EMPTY_AGENT_AVATAR_BACKFILL_STATUS,
      { endpoint: "POST /api/agents/backfill-avatars" },
    );
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

  // CEREBRO-PATCH(agent-infisical-secrets): CRUD for per-agent Infisical folder grants.
  async listAgentInfisicalFolders(id: string): Promise<AgentInfisicalFolder[]> {
    const res = await this.fetch<{ folders?: AgentInfisicalFolder[] }>(
      `/api/agents/${id}/infisical-folders`,
    );
    return res.folders ?? [];
  }

  async replaceAgentInfisicalFolders(
    id: string,
    folders: AgentInfisicalFolder[],
  ): Promise<AgentInfisicalFolder[]> {
    const res = await this.fetch<{ folders?: AgentInfisicalFolder[] }>(
      `/api/agents/${id}/infisical-folders`,
      {
        method: "PUT",
        body: JSON.stringify({ folders }),
      },
    );
    return res.folders ?? [];
  }

  // Folders the agent owner is allowed to grant — the picker source for the
  // agent Infisical tab. Saving a folder outside this list is rejected server-side.
  async listAgentAllowedInfisicalFolders(
    id: string,
  ): Promise<AgentInfisicalFolder[]> {
    const res = await this.fetch<{ folders?: AgentInfisicalFolder[] }>(
      `/api/agents/${id}/infisical-allowed-folders`,
    );
    return res.folders ?? [];
  }

  // CEREBRO-PATCH(user-infisical-folders): admin allow-list of Infisical folders
  // a member may grant to their agents.
  async listMemberInfisicalFolders(
    workspaceId: string,
    memberId: string,
  ): Promise<AgentInfisicalFolder[]> {
    const res = await this.fetch<{ folders?: AgentInfisicalFolder[] }>(
      `/api/workspaces/${workspaceId}/members/${memberId}/infisical-folders`,
    );
    return res.folders ?? [];
  }

  async replaceMemberInfisicalFolders(
    workspaceId: string,
    memberId: string,
    folders: AgentInfisicalFolder[],
  ): Promise<AgentInfisicalFolder[]> {
    const res = await this.fetch<{ folders?: AgentInfisicalFolder[] }>(
      `/api/workspaces/${workspaceId}/members/${memberId}/infisical-folders`,
      {
        method: "PUT",
        body: JSON.stringify({ folders }),
      },
    );
    return res.folders ?? [];
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
  // Permission: workspace admin/owner. Server returns the
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

  // CEREBRO-PATCH(cloud-runtime-bootstrap): client wrappers for upstream Fleet proxy endpoints.
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

  // CEREBRO-PATCH(api-client-runtime-pause): cerebro pause/unpause endpoints.
  async pauseRuntime(runtimeId: string, body: { unpause_at?: string; reason?: string } = {}): Promise<AgentRuntime> {
    return this.fetch(`/api/runtimes/${runtimeId}/pause`, { method: "POST", body: JSON.stringify(body) });
  }

  // CEREBRO-PATCH(api-client-runtime-pause): cerebro pause/unpause endpoints.
  async unpauseRuntime(runtimeId: string): Promise<AgentRuntime> {
    return this.fetch(`/api/runtimes/${runtimeId}/unpause`, { method: "POST" });
  }

  // CEREBRO-PATCH(api-client-runtime-tools-config): runtime-level MCP defaults (9031).
  async updateRuntimeToolsConfig(
    runtimeId: string,
    toolsConfig: unknown | null,
  ): Promise<AgentRuntime> {
    return this.fetch(`/api/runtimes/${runtimeId}/tools-config`, {
      method: "PATCH",
      body: JSON.stringify({ tools_config: toolsConfig }),
    });
  }

  // CEREBRO-PATCH(api-client-runtime-tools-admin): JEH-1710 — workspace
  // owner/admin endpoints that drive the RuntimeToolsCard. Schema-validated
  // per API Response Compatibility rules so a future server-side drift
  // (e.g. an unknown source enum value) renders gracefully.
  async listRuntimeTools(runtimeId: string): Promise<import("@multica/cerebro-types").RuntimeTool[]> {
    const path = `/api/runtimes/${runtimeId}/tools`;
    const raw = await this.fetch(path);
    return parseWithFallback(raw, RuntimeToolsListSchema, [], {
      endpoint: path,
    }) as import("@multica/cerebro-types").RuntimeTool[];
  }

  // CEREBRO-PATCH(runtime-agnostic-tool-access): TECH-3071 read-only effective runtime tool access preview.
  async listRuntimeToolEffectiveAccess(
    runtimeId: string,
    params: { agent_id?: string; user_id?: string } = {},
  ): Promise<import("@multica/cerebro-types").RuntimeToolEffectiveAccess[]> {
    const search = new URLSearchParams();
    if (params.agent_id) search.set("agent_id", params.agent_id);
    if (params.user_id) search.set("user_id", params.user_id);
    const query = search.toString();
    const path = `/api/runtimes/${runtimeId}/tools/effective${query ? `?${query}` : ""}`;
    const raw = await this.fetch(path);
    return parseWithFallback(raw, RuntimeToolEffectiveAccessListSchema, [], {
      endpoint: path,
    }) as import("@multica/cerebro-types").RuntimeToolEffectiveAccess[];
  }

  async setRuntimeToolEnabled(
    runtimeId: string,
    toolName: string,
    enabled: boolean,
  ): Promise<void> {
    await this.fetch(
      `/api/runtimes/${runtimeId}/tools/${encodeURIComponent(toolName)}`,
      { method: "PATCH", body: JSON.stringify({ enabled }) },
    );
  }

  async listRuntimeToolGrants(
    runtimeId: string,
  ): Promise<import("@multica/cerebro-types").RuntimeToolGrants> {
    const path = `/api/runtimes/${runtimeId}/tool-grants`;
    const raw = await this.fetch(path);
    return parseWithFallback(
      raw,
      RuntimeToolGrantsSchema,
      { group_grants: [], user_grants: [] },
      { endpoint: path },
    ) as import("@multica/cerebro-types").RuntimeToolGrants;
  }

  // CEREBRO-PATCH(capability-register-client): FIR-2129 single capability register client API.
  async listCapabilities(params: {
    workspace_id: string;
    subject?: string;
    key?: string;
  }): Promise<CapabilityListResponse> {
    const search = new URLSearchParams();
    search.set("workspace_id", params.workspace_id);
    if (params.subject) search.set("subject", params.subject);
    if (params.key) search.set("key", params.key);
    const path = `/api/capabilities?${search}`;
    const raw = await this.fetch(path);
    return parseWithFallback(raw, CapabilityListResponseSchema, { capabilities: [] }, {
      endpoint: path,
    }) as CapabilityListResponse;
  }

  async reportCapabilities(body: {
    workspace_id: string;
    subject: CapabilitySubject;
    capabilities: CapabilityReportInput[];
  }): Promise<CapabilityListResponse> {
    const path = "/api/capabilities/report";
    const raw = await this.fetch(path, {
      method: "POST",
      body: JSON.stringify(body),
    });
    return parseWithFallback(raw, CapabilityListResponseSchema, { capabilities: [] }, {
      endpoint: path,
    }) as CapabilityListResponse;
  }

  async addRuntimeToolGroupGrant(
    runtimeId: string,
    toolName: string,
    groupId: string,
  ): Promise<void> {
    await this.fetch(
      `/api/runtimes/${runtimeId}/tools/${encodeURIComponent(toolName)}/groups/${groupId}`,
      { method: "POST" },
    );
  }

  async removeRuntimeToolGroupGrant(
    runtimeId: string,
    toolName: string,
    groupId: string,
  ): Promise<void> {
    await this.fetch(
      `/api/runtimes/${runtimeId}/tools/${encodeURIComponent(toolName)}/groups/${groupId}`,
      { method: "DELETE" },
    );
  }

  async addRuntimeToolUserGrant(
    runtimeId: string,
    toolName: string,
    userId: string,
  ): Promise<void> {
    await this.fetch(
      `/api/runtimes/${runtimeId}/tools/${encodeURIComponent(toolName)}/users/${userId}`,
      { method: "POST" },
    );
  }

  async removeRuntimeToolUserGrant(
    runtimeId: string,
    toolName: string,
    userId: string,
  ): Promise<void> {
    await this.fetch(
      `/api/runtimes/${runtimeId}/tools/${encodeURIComponent(toolName)}/users/${userId}`,
      { method: "DELETE" },
    );
  }

  // CEREBRO-PATCH(api-client-terminal): cerebro interactive-terminal endpoints.
  async getRuntimePresentationMode(runtimeId: string): Promise<{ runtime_id: string; presentation_mode: "headless" | "interactive" }> {
    return this.fetch(`/api/cerebro/terminal/runtimes/${runtimeId}/presentation-mode`);
  }
  async setRuntimePresentationMode(runtimeId: string, mode: "headless" | "interactive"): Promise<{ runtime_id: string; presentation_mode: "headless" | "interactive" }> {
    return this.fetch(`/api/cerebro/terminal/runtimes/${runtimeId}/presentation-mode`, { method: "PUT", body: JSON.stringify({ presentation_mode: mode }) });
  }
  async createTerminalSession(runtimeId: string, command?: string[]): Promise<{ id: string; runtime_id: string; command: string[]; created_at: string; attach_path: string }> {
    return this.fetch(`/api/cerebro/terminal/sessions`, { method: "POST", body: JSON.stringify({ runtime_id: runtimeId, command }) });
  }
  async deleteTerminalSession(sessionId: string): Promise<void> {
    await this.fetch(`/api/cerebro/terminal/sessions/${sessionId}`, { method: "DELETE" });
  }
  terminalAttachUrl(attachPath: string): string {
    const base = this.baseUrl.replace(/^http/, "ws");
    return `${base}${attachPath}`;
  }
  // CEREBRO-PATCH(api-client-active-terminal-session): runtime-keyed lookup of a daemon-published terminal session.
  async getActiveTerminalSession(runtimeId: string): Promise<{ session_id: string; attach_path: string; created_at: string } | null> {
    const raw = await this.fetch<unknown>(`/api/cerebro/terminal/runtimes/${runtimeId}/session`);
    if (raw === undefined) return null; // 204 No Content path
    return parseWithFallback(raw, ActiveTerminalSessionSchema, null, { endpoint: "GET /api/cerebro/terminal/runtimes/:id/session" });
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

  async updateRuntimeSandboxPolicy(
    runtimeId: string,
    sandboxPolicy: AgentRuntime["sandbox_policy"],
  ): Promise<AgentRuntime> {
    return this.fetch(`/api/runtimes/${runtimeId}/sandbox-policy`, {
      method: "PATCH",
      body: JSON.stringify({ sandbox_policy: sandboxPolicy }),
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

  // ---------------------------------------------------------------------------
  // Workspace dashboard — three independent rollups for `/{slug}/dashboard`.
  // Each accepts an optional `project_id` to narrow the scope to one project.
  // Cost is computed client-side from the model pricing table (same contract
  // as the per-runtime endpoints above).
  // ---------------------------------------------------------------------------

  async getDashboardUsageDaily(
    params: { days?: number; project_id?: string | null },
  ): Promise<DashboardUsageDaily[]> {
    const search = new URLSearchParams();
    if (params.days) search.set("days", String(params.days));
    if (params.project_id) search.set("project_id", params.project_id);
    const raw = await this.fetch<unknown>(`/api/dashboard/usage/daily?${search}`);
    return parseWithFallback<DashboardUsageDaily[]>(
      raw,
      DashboardUsageDailyListSchema,
      [],
      { endpoint: "GET /api/dashboard/usage/daily" },
    );
  }

  async getDashboardUsageByAgent(
    params: { days?: number; project_id?: string | null },
  ): Promise<DashboardUsageByAgent[]> {
    const search = new URLSearchParams();
    if (params.days) search.set("days", String(params.days));
    if (params.project_id) search.set("project_id", params.project_id);
    const raw = await this.fetch<unknown>(`/api/dashboard/usage/by-agent?${search}`);
    return parseWithFallback<DashboardUsageByAgent[]>(
      raw,
      DashboardUsageByAgentListSchema,
      [],
      { endpoint: "GET /api/dashboard/usage/by-agent" },
    );
  }

  async getDashboardAgentRunTime(
    params: { days?: number; project_id?: string | null },
  ): Promise<DashboardAgentRunTime[]> {
    const search = new URLSearchParams();
    if (params.days) search.set("days", String(params.days));
    if (params.project_id) search.set("project_id", params.project_id);
    const raw = await this.fetch<unknown>(`/api/dashboard/agent-runtime?${search}`);
    return parseWithFallback<DashboardAgentRunTime[]>(
      raw,
      DashboardAgentRunTimeListSchema,
      [],
      { endpoint: "GET /api/dashboard/agent-runtime" },
    );
  }

  async getDashboardRunTimeDaily(
    params: { days?: number; project_id?: string | null },
  ): Promise<DashboardRunTimeDaily[]> {
    const search = new URLSearchParams();
    if (params.days) search.set("days", String(params.days));
    if (params.project_id) search.set("project_id", params.project_id);
    const raw = await this.fetch<unknown>(`/api/dashboard/runtime/daily?${search}`);
    return parseWithFallback<DashboardRunTimeDaily[]>(
      raw,
      DashboardRunTimeDailyListSchema,
      [],
      { endpoint: "GET /api/dashboard/runtime/daily" },
    );
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

  // CEREBRO-PATCH(issue-comment-cost-client): FIR-39 per-comment cost badge.
  async getIssueCommentCosts(issueId: string): Promise<IssueCommentCosts> {
    return this.fetch(`/api/issues/${issueId}/comment-costs`);
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

  // CEREBRO-PATCH(channel-state-client): TECH-3352 — per-user snooze ("remind
  // me") + mark-unread endpoints, mirroring the inbox mute/unread client.
  async muteChannel(id: string, mutedUntil: Date): Promise<{ muted_until: string | null }> {
    return this.fetch(`/api/channels/${id}/mute`, {
      method: "POST",
      body: JSON.stringify({ muted_until: mutedUntil.toISOString() }),
    });
  }

  async unmuteChannel(id: string): Promise<{ muted_until: string | null }> {
    return this.fetch(`/api/channels/${id}/mute`, { method: "DELETE" });
  }

  async markChannelUnread(id: string): Promise<{ unread: boolean }> {
    return this.fetch(`/api/channels/${id}/unread`, { method: "POST" });
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

  async rerunIssue(issueId: string, taskId?: string): Promise<AgentTask> {
    return this.fetch(`/api/issues/${issueId}/rerun`, {
      method: "POST",
      body: taskId ? JSON.stringify({ task_id: taskId }) : undefined,
    });
  }

  // Inbox
  // CEREBRO-PATCH(inbox-archive-pagination): archived inbox can request a bounded page.
  async listInbox(params?: { archived?: boolean; limit?: number; offset?: number }): Promise<InboxItem[]> {
    const qs = new URLSearchParams();
    if (params?.archived) qs.set("archived", "1");
    if (params?.limit !== undefined) qs.set("limit", String(params.limit));
    if (params?.offset !== undefined) qs.set("offset", String(params.offset));
    const query = qs.toString();
    return this.fetch(`/api/inbox${query ? `?${query}` : ""}`);
  }

  // CEREBRO-PATCH(active-issue-tasks-status): extended response with per-task status for run-state pip (JEH-1332)
  // CEREBRO-PATCH(active-issue-tasks-parent): FIR-2326 — parent_issue_id surfaces a running sub-issue on its parent row.
  async listActiveIssueTasks(): Promise<{ issue_ids: string[]; tasks?: { issue_id: string; status: string; parent_issue_id?: string | null }[] }> {
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

  // CEREBRO-PATCH(inbox-reminders-client): create global reminders from the inbox toolbar.
  async createInboxReminder(params: {
    text: string;
    plannedAt: Date;
    issueId?: string | null;
    // CEREBRO-PATCH(comment-reminder-client): FIR-2643 — pin a reminder to one comment.
    commentId?: string | null;
  }): Promise<InboxItem> {
    return this.fetch("/api/inbox/reminders", {
      method: "POST",
      body: JSON.stringify({
        text: params.text,
        planned_at: params.plannedAt.toISOString(),
        issue_id: params.issueId ?? null,
        comment_id: params.commentId ?? null, // CEREBRO-PATCH(comment-reminder-client): FIR-2643
      }),
    });
  }

  // CEREBRO-PATCH(inbox-run-private-agent-client): FIR-2385 — the agent owner
  // accepts a private_agent_run_request; the server enqueues the agent on the
  // tagged comment and archives the request. Returns a small ack, not a full
  // InboxItem (the row is removed from the active inbox on settle).
  async runPrivateAgentRunRequest(id: string): Promise<{ id: string; status: string }> {
    return this.fetch(`/api/inbox/${id}/run-private-agent`, { method: "POST" });
  }

  // CEREBRO-PATCH(cerebro-inbox-add-issue): manually place an issue in the member's inbox.
  async addIssueToInbox(issueId: string): Promise<InboxItem> {
    return this.fetch("/api/inbox/add-issue", {
      method: "POST",
      body: JSON.stringify({ issue_id: issueId }),
    });
  }

  // CEREBRO-PATCH(cerebro-focus-list-client): TECH-2947 — personal focus list API.
  async listFocusListItems(): Promise<FocusListItem[]> {
    return this.fetch("/api/cerebro/focus-list");
  }

  async createFocusListItem(params: { text: string; issueId?: string | null }): Promise<FocusListItem> {
    return this.fetch("/api/cerebro/focus-list", {
      method: "POST",
      body: JSON.stringify({ text: params.text, issue_id: params.issueId ?? null }),
    });
  }

  async updateFocusListItem(id: string, params: { text?: string; issueId?: string | null }): Promise<FocusListItem> {
    return this.fetch(`/api/cerebro/focus-list/${id}`, {
      method: "PATCH",
      body: JSON.stringify({ text: params.text, issue_id: params.issueId ?? null }),
    });
  }

  async markFocusListItemDone(id: string): Promise<FocusListItem> {
    return this.fetch(`/api/cerebro/focus-list/${id}/done`, { method: "POST" });
  }

  async snoozeFocusListItem(id: string, until: Date): Promise<FocusListItem> {
    return this.fetch(`/api/cerebro/focus-list/${id}/snooze`, {
      method: "POST",
      body: JSON.stringify({ until: until.toISOString() }),
    });
  }

  async deleteFocusListItem(id: string): Promise<void> {
    return this.fetch(`/api/cerebro/focus-list/${id}`, { method: "DELETE" });
  }

  // CEREBRO-PATCH(cerebro-focus-list-client): TECH-2947 reorder for drag-and-drop priorities.
  async reorderFocusListItems(ids: string[]): Promise<void> {
    return this.fetch(`/api/cerebro/focus-list/reorder`, {
      method: "POST",
      body: JSON.stringify({ ids }),
    });
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
  // `workspaceSlug` overrides the default `X-Workspace-Slug` header (which
  // follows the active workspace) so a caller can read a SPECIFIC workspace's
  // preferences — e.g. honoring the mute setting of the workspace an inbox
  // notification came from while the user is viewing a different one (#3766).
  async getNotificationPreferences(workspaceSlug?: string): Promise<NotificationPreferenceResponse> {
    return this.fetch(
      "/api/notification-preferences",
      workspaceSlug ? { headers: { "X-Workspace-Slug": workspaceSlug } } : undefined,
    );
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

  // CEREBRO-PATCH(workspace-avatar-update): FIR-2580 — accept avatar_url ("" clears the logo).
  async updateWorkspace(id: string, data: { name?: string; description?: string; context?: string; settings?: Record<string, unknown>; repos?: WorkspaceRepo[]; issue_prefix?: string; avatar_url?: string }): Promise<Workspace> {
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
  // CEREBRO-PATCH(skill-list-workspace-param): FIR-1094 pass workspace_id explicitly so /skill mention lookup does not depend on ambient route context.
  async listSkills(params?: { workspace_id?: string }): Promise<SkillSummary[]> {
    const search = new URLSearchParams();
    if (params?.workspace_id) search.set("workspace_id", params.workspace_id);
    const query = search.toString();
    return this.fetch(`/api/skills${query ? `?${query}` : ""}`);
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

  // CEREBRO-PATCH(skill-fork-parent-lineage): FIR-2629 — "forked from" lineage; resolves to null when the skill is an original (404).
  async getSkillForkParent(id: string): Promise<SkillForkParent | null> {
    try {
      return await this.fetch(`/api/skills/${id}/fork-parent`);
    } catch {
      return null;
    }
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
  async uploadFile(
    file: File,
    opts?: { issueId?: string; commentId?: string; chatSessionId?: string },
  ): Promise<Attachment> {
    const formData = new FormData();
    formData.append("file", file);
    if (opts?.issueId) formData.append("issue_id", opts.issueId);
    if (opts?.commentId) formData.append("comment_id", opts.commentId);
    if (opts?.chatSessionId) formData.append("chat_session_id", opts.chatSessionId);

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
    const raw = (await res.json()) as unknown;
    return parseWithFallback(raw, AttachmentResponseSchema, EMPTY_ATTACHMENT, {
      endpoint: "POST /api/upload-file",
    });
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

  async getChatSessionUsage(sessionId: string): Promise<ChatSessionUsage> {
    return this.fetch(`/api/chat/sessions/${sessionId}/usage`);
  }

  // CEREBRO-PATCH(chat-message-cost-client): FIR-31 per-reply cost badge.
  async getChatSessionMessageCosts(sessionId: string): Promise<ChatSessionMessageCosts> {
    return this.fetch(`/api/chat/sessions/${sessionId}/message-costs`);
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

  // CEREBRO-PATCH(chat-state-client): TECH-3352 — chat row snooze + mark-unread.
  async muteChatSession(sessionId: string, mutedUntil: Date): Promise<{ muted_until: string | null }> {
    return this.fetch(`/api/chat/sessions/${sessionId}/mute`, {
      method: "POST",
      body: JSON.stringify({ muted_until: mutedUntil.toISOString() }),
    });
  }

  async unmuteChatSession(sessionId: string): Promise<{ muted_until: string | null }> {
    return this.fetch(`/api/chat/sessions/${sessionId}/mute`, { method: "DELETE" });
  }

  async markChatSessionUnread(sessionId: string): Promise<{ unread: boolean }> {
    return this.fetch(`/api/chat/sessions/${sessionId}/unread`, { method: "POST" });
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

  // CEREBRO-PATCH(issue-dependencies): blocks / blocked-by / related relations.
  // Every response runs through IssueDependenciesResponseSchema so backend
  // drift downgrades to the empty fallback instead of white-screening the
  // sidebar (API Response Compatibility rule).
  async getIssueDependencies(issueId: string): Promise<IssueDependenciesResponse> {
    const raw = await this.fetch<unknown>(`/api/issues/${issueId}/dependencies`);
    return parseWithFallback(raw, IssueDependenciesResponseSchema, EMPTY_ISSUE_DEPENDENCIES, {
      endpoint: "getIssueDependencies",
    });
  }

  async addBlocks(issueId: string, otherId: string): Promise<IssueDependenciesResponse> {
    const raw = await this.fetch<unknown>(`/api/issues/${issueId}/blocks`, {
      method: "POST",
      body: JSON.stringify({ issue_id: otherId }),
    });
    return parseWithFallback(raw, IssueDependenciesResponseSchema, EMPTY_ISSUE_DEPENDENCIES, {
      endpoint: "addBlocks",
    });
  }

  async addBlockedBy(issueId: string, otherId: string): Promise<IssueDependenciesResponse> {
    const raw = await this.fetch<unknown>(`/api/issues/${issueId}/blocked-by`, {
      method: "POST",
      body: JSON.stringify({ issue_id: otherId }),
    });
    return parseWithFallback(raw, IssueDependenciesResponseSchema, EMPTY_ISSUE_DEPENDENCIES, {
      endpoint: "addBlockedBy",
    });
  }

  async addRelated(issueId: string, otherId: string): Promise<IssueDependenciesResponse> {
    const raw = await this.fetch<unknown>(`/api/issues/${issueId}/related`, {
      method: "POST",
      body: JSON.stringify({ issue_id: otherId }),
    });
    return parseWithFallback(raw, IssueDependenciesResponseSchema, EMPTY_ISSUE_DEPENDENCIES, {
      endpoint: "addRelated",
    });
  }

  async removeBlocks(issueId: string, otherId: string): Promise<IssueDependenciesResponse> {
    const raw = await this.fetch<unknown>(`/api/issues/${issueId}/blocks/${otherId}`, {
      method: "DELETE",
    });
    return parseWithFallback(raw, IssueDependenciesResponseSchema, EMPTY_ISSUE_DEPENDENCIES, {
      endpoint: "removeBlocks",
    });
  }

  async removeBlockedBy(issueId: string, otherId: string): Promise<IssueDependenciesResponse> {
    const raw = await this.fetch<unknown>(`/api/issues/${issueId}/blocked-by/${otherId}`, {
      method: "DELETE",
    });
    return parseWithFallback(raw, IssueDependenciesResponseSchema, EMPTY_ISSUE_DEPENDENCIES, {
      endpoint: "removeBlockedBy",
    });
  }

  async removeRelated(issueId: string, otherId: string): Promise<IssueDependenciesResponse> {
    const raw = await this.fetch<unknown>(`/api/issues/${issueId}/related/${otherId}`, {
      method: "DELETE",
    });
    return parseWithFallback(raw, IssueDependenciesResponseSchema, EMPTY_ISSUE_DEPENDENCIES, {
      endpoint: "removeRelated",
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

  async createSquad(data: {
    name: string;
    description?: string;
    leader_id: string;
    avatar_url?: string; // CEREBRO-PATCH(upstream-create-squad-avatar): JEH-1541 align typed client with squad create modal/backend.
  }): Promise<Squad> {
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

  async getSquadMemberStatus(squadId: string): Promise<SquadMemberStatusListResponse> {
    const raw = await this.fetch<unknown>(`/api/squads/${squadId}/members/status`);
    return parseWithFallback(raw, SquadMemberStatusListResponseSchema, EMPTY_SQUAD_MEMBER_STATUS_LIST, {
      endpoint: "GET /api/squads/:id/members/status",
    }) as SquadMemberStatusListResponse;
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

  async rotateAutopilotTriggerWebhookToken(autopilotId: string, triggerId: string): Promise<AutopilotTrigger> {
    return this.fetch(`/api/autopilots/${autopilotId}/triggers/${triggerId}/rotate-webhook-token`, {
      method: "POST",
    });
  }

  async setAutopilotTriggerSigningSecret(
    autopilotId: string,
    triggerId: string,
    signingSecret: string,
  ): Promise<AutopilotTrigger> {
    return this.fetch(`/api/autopilots/${autopilotId}/triggers/${triggerId}/signing-secret`, {
      method: "PUT",
      body: JSON.stringify({ signing_secret: signingSecret }),
    });
  }

  async listAutopilotDeliveries(
    autopilotId: string,
    params?: { limit?: number; offset?: number },
  ): Promise<ListWebhookDeliveriesResponse> {
    const search = new URLSearchParams();
    if (params?.limit) search.set("limit", params.limit.toString());
    if (params?.offset) search.set("offset", params.offset.toString());
    const raw = await this.fetch<unknown>(`/api/autopilots/${autopilotId}/deliveries?${search}`);
    return parseWithFallback(raw, ListWebhookDeliveriesResponseSchema, EMPTY_LIST_WEBHOOK_DELIVERIES_RESPONSE, {
      endpoint: "listAutopilotDeliveries",
    });
  }

  async getAutopilotDelivery(autopilotId: string, deliveryId: string): Promise<WebhookDelivery> {
    const raw = await this.fetch<unknown>(`/api/autopilots/${autopilotId}/deliveries/${deliveryId}`);
    return parseWithFallback(raw, WebhookDeliveryResponseSchema, EMPTY_WEBHOOK_DELIVERY, {
      endpoint: "getAutopilotDelivery",
    });
  }

  async replayAutopilotDelivery(autopilotId: string, deliveryId: string): Promise<WebhookDelivery> {
    const raw = await this.fetch<unknown>(`/api/autopilots/${autopilotId}/deliveries/${deliveryId}/replay`, {
      method: "POST",
    });
    return parseWithFallback(raw, WebhookDeliveryResponseSchema, EMPTY_WEBHOOK_DELIVERY, {
      endpoint: "replayAutopilotDelivery",
    });
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
  // CEREBRO-PATCH(artifact-folder-kind): TECH-3637 — optional kind filter so
  // notes and documents list separate folder trees.
  async listArtifactFolders(kind?: string): Promise<ArtifactFolder[]> {
    const qs = kind ? `?kind=${encodeURIComponent(kind)}` : "";
    return this.fetch(`/api/artifact-folders${qs}`);
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

  // CEREBRO-PATCH(agent-avatar-generate): JEH-1563 AI avatar generation via OpenRouter gpt-5-image-mini.
  async generateAgentAvatar(agentName: string, customPrompt?: string): Promise<{ url: string }> {
    const body = JSON.stringify({ agent_name: agentName, custom_prompt: customPrompt });
    return this.fetch("/api/agents/generate-avatar", { method: "POST", body });
  }

  // CEREBRO-PATCH(cerebro-connections-client): TECH-3108 workspace connection registry methods.
  // Bodies are unknown so cerebro-connections package owns the schema via parseWithFallback.
  async listCerebroConnections<T = unknown>(wsId: string): Promise<T> {
    return this.fetch<T>(`/api/workspaces/${wsId}/connections`);
  }
  async getCerebroConnection<T = unknown>(wsId: string, connId: string): Promise<T> {
    return this.fetch<T>(`/api/workspaces/${wsId}/connections/${connId}`);
  }
  async createCerebroConnection<T = unknown>(wsId: string, body: unknown): Promise<T> {
    return this.fetch<T>(`/api/workspaces/${wsId}/connections`, {
      method: "POST",
      body: JSON.stringify(body),
    });
  }
  async updateCerebroConnection<T = unknown>(wsId: string, connId: string, body: unknown): Promise<T> {
    return this.fetch<T>(`/api/workspaces/${wsId}/connections/${connId}`, {
      method: "PUT",
      body: JSON.stringify(body),
    });
  }
  async deleteCerebroConnection(wsId: string, connId: string): Promise<void> {
    await this.fetch<void>(`/api/workspaces/${wsId}/connections/${connId}`, {
      method: "DELETE",
    });
  }
  // CEREBRO-PATCH(cerebro-connections-test-client): TECH-3108 test connection endpoint.
  async testCerebroConnection<T = unknown>(wsId: string, body: unknown): Promise<T> {
    return this.fetch<T>(`/api/workspaces/${wsId}/connections/test`, {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  // CEREBRO-PATCH(cerebro-web-fetch-policy-client): TECH-3522 per-workspace web_fetch policy methods.
  async getCerebroWebFetchPolicy<T = unknown>(wsId: string): Promise<T> {
    return this.fetch<T>(`/api/workspaces/${wsId}/web-fetch-policy`); // CEREBRO-PATCH(cerebro-web-fetch-policy-client)
  }
  async updateCerebroWebFetchPolicy<T = unknown>(wsId: string, body: unknown): Promise<T> {
    return this.fetch<T>(`/api/workspaces/${wsId}/web-fetch-policy`, { // CEREBRO-PATCH(cerebro-web-fetch-policy-client)
      method: "PUT",
      body: JSON.stringify(body),
    });
  }

  // CEREBRO-PATCH(cerebro-agentvault-client): TECH-3196 per-agent Agent Vault access-table methods.
  async listCerebroAgentVaultAccess<T = unknown>(wsId: string, agentId: string): Promise<T> {
    return this.fetch<T>(
      `/api/workspaces/${wsId}/agentvault/access?agent_id=${encodeURIComponent(agentId)}`,
    );
  }
  async setCerebroAgentVaultAccess<T = unknown>(wsId: string, body: unknown): Promise<T> {
    return this.fetch<T>(`/api/workspaces/${wsId}/agentvault/access`, {
      method: "PUT",
      body: JSON.stringify(body),
    });
  }
  async deleteCerebroAgentVaultAccess(wsId: string, body: unknown): Promise<void> {
    await this.fetch<void>(`/api/workspaces/${wsId}/agentvault/access`, {
      method: "DELETE",
      body: JSON.stringify(body),
    });
  }

  // CEREBRO-PATCH(cerebro-wakeup-sidebar): list and cancel agent wakeups per issue for the sidebar.
  async listIssueWakeups(issueId: string, state = "pending"): Promise<{
    wakeups: {
      id: string;
      agent_id: string;
      issue_id: string;
      prompt: string;
      trigger_type: string;
      fire_at?: string;
      watch_status?: string;
      state: string;
      created_at: string;
    }[];
  }> {
    const qs = new URLSearchParams({ issue_id: issueId, state, limit: "50" });
    return this.fetch(`/api/cerebro/wakeups?${qs.toString()}`);
  }

  // CEREBRO-PATCH(cerebro-wakeup-inbox-list): TECH-3322 — list pending wakeups across the workspace so the inbox can mark scheduled issues as Running.
  async listWorkspaceWakeups(state = "pending", limit = 200): Promise<{
    wakeups: { id: string; issue_id: string; trigger_type: string; fire_at?: string; state: string }[];
  }> {
    const qs = new URLSearchParams({ state, limit: String(limit) });
    return this.fetch(`/api/cerebro/wakeups?${qs.toString()}`);
  }

  async cancelWakeup(id: string): Promise<{ id: string; state: string }> {
    return this.fetch(`/api/cerebro/wakeups/${encodeURIComponent(id)}/cancel`, {
      method: "POST",
      body: JSON.stringify({}),
    });
  }

  // CEREBRO-PATCH(cerebro-wakeup-settings): TECH-3298 per-workspace self-wakeup
  // limits (max wakeups per issue + minimum gap between two time wakeups).
  async getWakeupSettings(wsId: string): Promise<{
    max_self_per_issue: number;
    min_interval_minutes: number;
  }> {
    return this.fetch(`/api/workspaces/${wsId}/wakeup-settings`);
  }

  async setWakeupSettings(
    wsId: string,
    maxSelfPerIssue: number,
    minIntervalMinutes: number,
  ): Promise<{ max_self_per_issue: number; min_interval_minutes: number }> {
    return this.fetch(`/api/workspaces/${wsId}/wakeup-settings`, {
      method: "PUT",
      body: JSON.stringify({
        max_self_per_issue: maxSelfPerIssue,
        min_interval_minutes: minIntervalMinutes,
      }),
    });
  }

  // CEREBRO-PATCH(workspace-logo-generate): FIR-2580 AI workspace-logo generation (up to 5 square icon variants).
  async generateWorkspaceLogos(
    workspaceId: string,
    prompt: string,
    count = 5,
  ): Promise<{ urls: string[] }> {
    const body = JSON.stringify({ prompt, count });
    const res = await this.fetch<{ urls?: string[] }>(
      `/api/workspaces/${workspaceId}/generate-logo`,
      { method: "POST", body },
    );
    // Defensive: tolerate a malformed/empty body so the UI shows a clean
    // "no images" state instead of throwing (API Response Compatibility).
    return { urls: Array.isArray(res?.urls) ? res.urls.filter((u) => typeof u === "string") : [] };
  }

  // Lark integration
  async listLarkInstallations(workspaceId: string): Promise<ListLarkInstallationsResponse> {
    return this.fetch(`/api/workspaces/${workspaceId}/lark/installations`);
  }

  async beginLarkInstall(
    workspaceId: string,
    agentId: string,
    region: "feishu" | "lark",
  ): Promise<BeginLarkInstallResponse> {
    // The user picks the cloud explicitly in the UI ("Bind to Feishu"
    // vs "Bind to Lark"), and the backend POSTs the device-flow `begin`
    // against the corresponding accounts host (accounts.feishu.cn vs
    // accounts.larksuite.com) so the QR renders against the right
    // cloud up front. Empty / omitted region still resolves to Feishu
    // server-side (RegionOrDefault) — we surface region as a required
    // arg here so every call site is forced to make a deliberate
    // choice rather than silently defaulting to mainland.
    const search = new URLSearchParams({ agent_id: agentId, region });
    return this.fetch(`/api/workspaces/${workspaceId}/lark/install/begin?${search.toString()}`, {
      method: "POST",
    });
  }

  async getLarkInstallStatus(workspaceId: string, sessionId: string): Promise<LarkInstallStatusResponse> {
    return this.fetch(`/api/workspaces/${workspaceId}/lark/install/${sessionId}/status`);
  }

  async deleteLarkInstallation(workspaceId: string, installationId: string): Promise<void> {
    await this.fetch(`/api/workspaces/${workspaceId}/lark/installations/${installationId}`, {
      method: "DELETE",
    });
  }

  async redeemLarkBindingToken(token: string): Promise<RedeemLarkBindingTokenResponse> {
    return this.fetch(`/api/lark/binding/redeem`, {
      method: "POST",
      body: JSON.stringify({ token }),
    });
  }

  // CEREBRO-PATCH(cerebro-notes-client): TECH-3421 Notes feature. Bodies are
  // generic so the @multica/cerebro-notes package owns the schema via its own
  // zod parse (API Response Compatibility rule). Notes = artifacts(kind=note)
  // + a cerebro_note row (owner/visibility/pin); see /api/notes.
  async listNotes<T = unknown>(params?: {
    q?: string;
    limit?: number;
    offset?: number;
  }): Promise<T> {
    const s = new URLSearchParams();
    if (params?.q) s.set("q", params.q);
    if (params?.limit != null) s.set("limit", String(params.limit));
    if (params?.offset != null) s.set("offset", String(params.offset));
    const qs = s.toString();
    return this.fetch<T>(`/api/notes${qs ? `?${qs}` : ""}`);
  }

  async listRecentNotes<T = unknown>(limit?: number): Promise<T> {
    const qs = limit != null ? `?limit=${limit}` : "";
    return this.fetch<T>(`/api/notes/recent${qs}`);
  }

  async getNote<T = unknown>(id: string): Promise<T> {
    return this.fetch<T>(`/api/notes/${id}`);
  }

  async createNote<T = unknown>(body: {
    title?: string;
    body?: string;
    folder_id?: string | null;
    visibility?: string;
  }): Promise<T> {
    return this.fetch<T>(`/api/notes`, {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  async updateNote<T = unknown>(
    id: string,
    body: { title?: string; body?: string },
  ): Promise<T> {
    return this.fetch<T>(`/api/notes/${id}`, {
      method: "PUT",
      body: JSON.stringify(body),
    });
  }

  async deleteNote(id: string): Promise<void> {
    await this.fetch(`/api/notes/${id}`, { method: "DELETE" });
  }

  async setNoteVisibility<T = unknown>(
    id: string,
    body: { visibility: string; shared_user_ids?: string[] },
  ): Promise<T> {
    return this.fetch<T>(`/api/notes/${id}/visibility`, {
      method: "PUT",
      body: JSON.stringify(body),
    });
  }

  async setNotePin<T = unknown>(id: string, pinned: boolean): Promise<T> {
    return this.fetch<T>(`/api/notes/${id}/pin`, {
      method: "PUT",
      body: JSON.stringify({ pinned }),
    });
  }

  // CEREBRO-PATCH(cerebro-notes-client): TECH-3421 — Note references: mirror of
  // the issue-reference methods above, keyed by the note's artifact id.
  //   GET    /api/notes/{id}/references            — list references on a note
  //   POST   /api/notes/{id}/references            — UPSERT a reference (owner)
  //   DELETE /api/notes/{id}/references/{refId}    — drop a reference (owner)
  // Bodies are `unknown` so the cerebro-notes package owns the schema.
  async listNoteReferences<T = unknown>(noteId: string): Promise<T> {
    return this.fetch<T>(`/api/notes/${noteId}/references`);
  }
  async createNoteReference<T = unknown>(
    noteId: string,
    payload: unknown,
  ): Promise<T> {
    return this.fetch<T>(`/api/notes/${noteId}/references`, {
      method: "POST",
      body: JSON.stringify(payload),
    });
  }
  async deleteNoteReference(noteId: string, refId: string): Promise<void> {
    await this.fetch<void>(`/api/notes/${noteId}/references/${refId}`, {
      method: "DELETE",
    });
  }

  // CEREBRO-PATCH(cerebro-notes-client): TECH-3556 Wave 3 — comments +
  // suggestions, version history, and the interim edit lock on a note. Bodies
  // are generic so @multica/cerebro-notes owns the zod schema.
  async listNoteComments<T = unknown>(noteId: string): Promise<T> {
    return this.fetch<T>(`/api/notes/${noteId}/comments`);
  }
  async createNoteComment<T = unknown>(
    noteId: string,
    payload: unknown,
  ): Promise<T> {
    return this.fetch<T>(`/api/notes/${noteId}/comments`, {
      method: "POST",
      body: JSON.stringify(payload),
    });
  }
  async updateNoteComment<T = unknown>(
    noteId: string,
    commentId: string,
    payload: unknown,
  ): Promise<T> {
    return this.fetch<T>(`/api/notes/${noteId}/comments/${commentId}`, {
      method: "PUT",
      body: JSON.stringify(payload),
    });
  }
  async deleteNoteComment(noteId: string, commentId: string): Promise<void> {
    await this.fetch<void>(`/api/notes/${noteId}/comments/${commentId}`, {
      method: "DELETE",
    });
  }
  async resolveNoteComment<T = unknown>(
    noteId: string,
    commentId: string,
    resolved: boolean,
  ): Promise<T> {
    return this.fetch<T>(`/api/notes/${noteId}/comments/${commentId}/resolve`, {
      method: "POST",
      body: JSON.stringify({ resolved }),
    });
  }
  async decideNoteSuggestion<T = unknown>(
    noteId: string,
    commentId: string,
    state: "accepted" | "rejected",
  ): Promise<T> {
    return this.fetch<T>(
      `/api/notes/${noteId}/comments/${commentId}/suggestion`,
      { method: "POST", body: JSON.stringify({ state }) },
    );
  }
  async listNoteVersions<T = unknown>(noteId: string): Promise<T> {
    return this.fetch<T>(`/api/notes/${noteId}/versions`);
  }
  async saveNoteVersion<T = unknown>(
    noteId: string,
    label?: string,
  ): Promise<T> {
    return this.fetch<T>(`/api/notes/${noteId}/versions`, {
      method: "POST",
      body: JSON.stringify({ label }),
    });
  }
  async restoreNoteVersion<T = unknown>(
    noteId: string,
    versionId: string,
  ): Promise<T> {
    return this.fetch<T>(
      `/api/notes/${noteId}/versions/${versionId}/restore`,
      { method: "POST" },
    );
  }
  async getNoteLock<T = unknown>(noteId: string): Promise<T> {
    return this.fetch<T>(`/api/notes/${noteId}/lock`);
  }
  async acquireNoteLock<T = unknown>(
    noteId: string,
    force?: boolean,
  ): Promise<T> {
    const qs = force ? "?force=true" : "";
    return this.fetch<T>(`/api/notes/${noteId}/lock${qs}`, { method: "POST" });
  }
  async releaseNoteLock(noteId: string): Promise<void> {
    await this.fetch<void>(`/api/notes/${noteId}/lock`, { method: "DELETE" });
  }
}
