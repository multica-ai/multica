import type {
  Issue,
  CreateIssueRequest,
  UpdateIssueRequest,
  ListIssuesResponse,
  UpdateMeRequest,
  CreateMemberRequest,
  UpdateMemberRequest,
  ListIssuesParams,
  Agent,
  CreateAgentRequest,
  UpdateAgentRequest,
  AgentTask,
  AgentRuntime,
  InboxItem,
  IssueSubscriber,
  Comment,
  Reaction,
  IssueReaction,
  Workspace,
  WorkspaceRepo,
  MemberWithUser,
  WorkspaceInviteInfo,
  User,
  Skill,
  CreateSkillRequest,
  UpdateSkillRequest,
  SetAgentSkillsRequest,
  PersonalAccessToken,
  CreatePersonalAccessTokenRequest,
  CreatePersonalAccessTokenResponse,
  RuntimeUsage,
  RuntimeHourlyActivity,
  RuntimePing,
  RuntimeUpdate,
  TimelineEntry,
  TaskMessagePayload,
  Attachment,
  Project,
  CreateProjectRequest,
  UpdateProjectRequest,
  ListProjectsResponse,
  BulkCreateIssueItem,
  BulkCreateIssueError,
  BulkCreateIssuesResponse,
  NotificationPreference,
  UpdateNotificationPreferenceRequest,
  TestNotificationPreferenceRequest,
  AISettingsResponse,
  UpdateAISettingsRequest,
  TimeEntry,
  CreateTimeEntryRequest,
  UpdateTimeEntryRequest,
  TeamTimeStats,
  DailyReview,
  DailyPlan,
  AutomationTemplate,
  StandupSummaryResult,
  PomodoroSession,
} from "@/shared/types";
import { type Logger, noopLogger } from "@/shared/logger";

export interface LoginResponse {
  token: string;
  user: User;
}

export class BulkCreateApiError extends Error {
  errors: BulkCreateIssueError[];
  constructor(errors: BulkCreateIssueError[]) {
    super("Some rows have validation errors");
    this.errors = errors;
  }
}

export class ApiClient {
  private baseUrl: string;
  private token: string | null = null;
  private workspaceId: string | null = null;
  private logger: Logger;

  constructor(baseUrl: string, options?: { logger?: Logger }) {
    this.baseUrl = baseUrl;
    this.logger = options?.logger ?? noopLogger;
  }

  setToken(token: string | null) {
    this.token = token;
  }

  setWorkspaceId(id: string | null) {
    this.workspaceId = id;
  }

  private authHeaders(): Record<string, string> {
    const headers: Record<string, string> = {};
    if (this.token) headers["Authorization"] = `Bearer ${this.token}`;
    if (this.workspaceId) headers["X-Workspace-ID"] = this.workspaceId;
    return headers;
  }

  private handleUnauthorized() {
    if (typeof window !== "undefined") {
      localStorage.removeItem("multica_token");
      localStorage.removeItem("multica_workspace_id");
      this.token = null;
      this.workspaceId = null;
      const next = `${window.location.pathname}${window.location.search}`;
      const loginUrl = next && next !== "/login"
        ? `/login?next=${encodeURIComponent(next)}`
        : "/login";
      if (window.location.pathname !== "/login") {
        window.location.href = loginUrl;
      }
    }
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

  private async fetch<T>(path: string, init?: RequestInit): Promise<T> {
    const rid = crypto.randomUUID().slice(0, 8);
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
      const message = await this.parseErrorMessage(res, `API error: ${res.status} ${res.statusText}`);
      this.logger.error(`← ${res.status} ${path}`, { rid, duration: `${Date.now() - start}ms`, error: message });
      throw new Error(message);
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

  async getMe(): Promise<User> {
    return this.fetch("/api/me");
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
    const wsId = params?.workspace_id ?? this.workspaceId;
    if (wsId) search.set("workspace_id", wsId);
    if (params?.status) search.set("status", params.status);
    if (params?.priority) search.set("priority", params.priority);
    if (params?.assignee_id) search.set("assignee_id", params.assignee_id);
    if (params?.assignee_type) search.set("assignee_type", params.assignee_type);
    if (params?.creator_id) search.set("creator_id", params.creator_id);
    if (params?.creator_type) search.set("creator_type", params.creator_type);
    if (params?.project_id) search.set("project_id", params.project_id);
    if (params?.search) search.set("search", params.search);
    if (params?.due_from) search.set("due_from", params.due_from);
    if (params?.due_to) search.set("due_to", params.due_to);
    if (params?.start_from) search.set("start_from", params.start_from);
    if (params?.start_to) search.set("start_to", params.start_to);
    if (params?.end_from) search.set("end_from", params.end_from);
    if (params?.end_to) search.set("end_to", params.end_to);
    if (params?.view) search.set("view", params.view);
    return this.fetch(`/api/issues?${search}`);
  }

  async getIssue(id: string): Promise<Issue> {
    return this.fetch(`/api/issues/${id}`);
  }

  async listLabels(): Promise<{ labels: { id: string; workspace_id: string; name: string; color: string }[]; total: number }> {
    return this.fetch("/api/labels");
  }

  async createLabel(data: { name: string; color?: string }): Promise<{ id: string; workspace_id: string; name: string; color: string }> {
    return this.fetch("/api/labels", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async updateLabel(id: string, data: { name: string; color: string }): Promise<{ id: string; workspace_id: string; name: string; color: string }> {
    return this.fetch(`/api/labels/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    });
  }

  async deleteLabel(id: string): Promise<void> {
    await this.fetch(`/api/labels/${id}`, { method: "DELETE" });
  }

  async createIssue(data: CreateIssueRequest): Promise<Issue> {
    const search = new URLSearchParams();
    if (this.workspaceId) search.set("workspace_id", this.workspaceId);
    return this.fetch(`/api/issues?${search}`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async bulkCreateIssues(items: BulkCreateIssueItem[]): Promise<BulkCreateIssuesResponse> {
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      ...this.authHeaders(),
    };
    const res = await fetch(`${this.baseUrl}/api/issues/bulk`, {
      method: "POST",
      headers,
      credentials: "include",
      body: JSON.stringify({ issues: items }),
    });
    if (!res.ok) {
      if (res.status === 401) this.handleUnauthorized();
      if (res.status === 422) {
        try {
          const data = await res.json() as { errors?: BulkCreateIssueError[]; error?: string };
          if (Array.isArray(data.errors) && data.errors.length > 0) {
            throw new BulkCreateApiError(data.errors);
          }
          if (data.error) throw new Error(data.error);
        } catch (e) {
          if (e instanceof BulkCreateApiError) throw e;
          throw new Error("Validation failed");
        }
      }
      const message = await this.parseErrorMessage(res, `API error: ${res.status} ${res.statusText}`);
      throw new Error(message);
    }
    return res.json() as Promise<BulkCreateIssuesResponse>;
  }

  async updateIssue(id: string, data: UpdateIssueRequest): Promise<Issue> {
    return this.fetch(`/api/issues/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  async deleteIssue(id: string): Promise<void> {
    await this.fetch(`/api/issues/${id}`, { method: "DELETE" });
  }

  async addIssueLabel(id: string, data: { label_id?: string; name?: string; color?: string }): Promise<Issue> {
    return this.fetch(`/api/issues/${id}/labels`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async removeIssueLabel(id: string, labelId: string): Promise<Issue> {
    return this.fetch(`/api/issues/${id}/labels/${labelId}`, {
      method: "DELETE",
    });
  }

  async addIssueDependency(id: string, data: { issue_id: string; type: string }): Promise<Issue> {
    return this.fetch(`/api/issues/${id}/dependencies`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async removeIssueDependency(id: string, dependencyId: string): Promise<Issue> {
    return this.fetch(`/api/issues/${id}/dependencies/${dependencyId}`, {
      method: "DELETE",
    });
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
    const search = new URLSearchParams();
    if (this.workspaceId) search.set("workspace_id", this.workspaceId);
    return this.fetch(`/api/projects?${search}`, {
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
    return this.fetch(`/api/issues/${issueId}/comments`);
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
    return this.fetch(`/api/issues/${issueId}/timeline`);
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
    return this.fetch(`/api/issues/${issueId}/subscribers`);
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
    const wsId = params?.workspace_id ?? this.workspaceId;
    if (wsId) search.set("workspace_id", wsId);
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

  async listRuntimes(params?: { workspace_id?: string }): Promise<AgentRuntime[]> {
    const search = new URLSearchParams();
    const wsId = params?.workspace_id ?? this.workspaceId;
    if (wsId) search.set("workspace_id", wsId);
    return this.fetch(`/api/runtimes?${search}`);
  }

  async getRuntimeUsage(runtimeId: string, params?: { days?: number }): Promise<RuntimeUsage[]> {
    const search = new URLSearchParams();
    if (params?.days) search.set("days", String(params.days));
    return this.fetch(`/api/runtimes/${runtimeId}/usage?${search}`);
  }

  async getRuntimeTaskActivity(runtimeId: string): Promise<RuntimeHourlyActivity[]> {
    return this.fetch(`/api/runtimes/${runtimeId}/activity`);
  }

  async pingRuntime(runtimeId: string): Promise<RuntimePing> {
    return this.fetch(`/api/runtimes/${runtimeId}/ping`, { method: "POST" });
  }

  async getPingResult(runtimeId: string, pingId: string): Promise<RuntimePing> {
    return this.fetch(`/api/runtimes/${runtimeId}/ping/${pingId}`);
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

  async listAgentTasks(agentId: string): Promise<AgentTask[]> {
    return this.fetch(`/api/agents/${agentId}/tasks`);
  }

  async getActiveTaskForIssue(issueId: string): Promise<{ task: AgentTask | null }> {
    return this.fetch(`/api/issues/${issueId}/active-task`);
  }

  async listTaskMessages(taskId: string): Promise<TaskMessagePayload[]> {
    return this.fetch(`/api/daemon/tasks/${taskId}/messages`);
  }

  async listTasksByIssue(issueId: string): Promise<AgentTask[]> {
    return this.fetch(`/api/issues/${issueId}/task-runs`);
  }

  async cancelTask(issueId: string, taskId: string): Promise<AgentTask> {
    return this.fetch(`/api/issues/${issueId}/tasks/${taskId}/cancel`, {
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

  // Members
  async listMembers(workspaceId: string): Promise<MemberWithUser[]> {
    return this.fetch(`/api/workspaces/${workspaceId}/members`);
  }

  async createMember(workspaceId: string, data: CreateMemberRequest): Promise<MemberWithUser> {
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

  async deleteWorkspace(workspaceId: string): Promise<void> {
    await this.fetch(`/api/workspaces/${workspaceId}`, {
      method: "DELETE",
    });
  }

  // Invite link management (admin/owner only)
  async getWorkspaceWithInviteToken(workspaceId: string): Promise<Workspace> {
    return this.fetch(`/api/workspaces/${workspaceId}/invite-link`);
  }

  async resetInviteLink(workspaceId: string): Promise<Workspace> {
    return this.fetch(`/api/workspaces/${workspaceId}/invite-link/reset`, {
      method: "POST",
    });
  }

  async disableInviteLink(workspaceId: string): Promise<Workspace> {
    return this.fetch(`/api/workspaces/${workspaceId}/invite-link`, {
      method: "DELETE",
    });
  }

  // Get workspace info by invite token (public, no auth required)
  async getInviteInfo(token: string): Promise<WorkspaceInviteInfo> {
    return this.fetch(`/api/invite/${token}`);
  }

  // Join workspace via invite token (requires auth)
  async joinByInviteToken(token: string): Promise<MemberWithUser> {
    return this.fetch(`/api/invite/${token}/join`, {
      method: "POST",
    });
  }

  async getAISettings(workspaceId: string): Promise<AISettingsResponse> {
    return this.fetch(`/api/workspaces/${workspaceId}/ai/settings`);
  }

  async updateAISettings(workspaceId: string, data: UpdateAISettingsRequest): Promise<AISettingsResponse> {
    return this.fetch(`/api/workspaces/${workspaceId}/ai/settings`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async suggestLabels(workspaceId: string, issueIds: string[]): Promise<{
    results: Array<{
      issue_id: string;
      suggestions: Array<{
        name: string;
        existing: boolean;
        label_id?: string;
        color?: string;
      }>;
    }>;
  }> {
    return this.fetch(`/api/workspaces/${workspaceId}/ai/label`, {
      method: "POST",
      body: JSON.stringify({ issue_ids: issueIds }),
    });
  }

  async suggestSchedule(workspaceId: string, issueIds: string[]): Promise<{
    suggestions: Array<{
      issue_id: string;
      start_date: string;
      end_date: string;
      reason: string;
    }>;
  }> {
    return this.fetch(`/api/workspaces/${workspaceId}/ai/schedule`, {
      method: "POST",
      body: JSON.stringify({ issue_ids: issueIds }),
    });
  }

  // Skills
  async listSkills(): Promise<Skill[]> {
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

  async listAgentSkills(agentId: string): Promise<Skill[]> {
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

    const rid = crypto.randomUUID().slice(0, 8);
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

  async listAttachments(issueId: string): Promise<Attachment[]> {
    return this.fetch(`/api/issues/${issueId}/attachments`);
  }

  async deleteAttachment(id: string): Promise<void> {
    await this.fetch(`/api/attachments/${id}`, { method: "DELETE" });
  }

  async getNotificationPreferences(): Promise<NotificationPreference> {
    return this.fetch("/api/notification-preferences");
  }

  async updateNotificationPreferences(data: UpdateNotificationPreferenceRequest): Promise<NotificationPreference> {
    return this.fetch("/api/notification-preferences", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(data),
    });
  }

  async testNotificationPreference(data: TestNotificationPreferenceRequest): Promise<void> {
    await this.fetch("/api/notification-preferences/test", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(data),
    });
  }

  // Time Tracking

  /** Start a live timer or create a manual time entry. */
  async startTimeEntry(data: CreateTimeEntryRequest): Promise<TimeEntry> {
    return this.fetch("/api/time-entries", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(data),
    });
  }

  /** Stop the running timer for the given entry ID. */
  async stopTimeEntry(entryId: string): Promise<TimeEntry> {
    return this.fetch(`/api/time-entries/${entryId}/stop`, { method: "PATCH" });
  }

  /** Get the currently running timer for the authenticated user (null if none). */
  async getCurrentTimeEntry(): Promise<TimeEntry | null> {
    return this.fetch("/api/time-entries/current");
  }

  /** List time entries for the current user in the active workspace (most recent first). */
  async listTimeEntries(params?: {
    limit?: number;
    offset?: number;
    /** ISO 8601/RFC 3339 — filter entries with start_time >= since (inclusive). */
    since?: string;
    /** ISO 8601/RFC 3339 — filter entries with start_time < until (exclusive). */
    until?: string;
  }): Promise<TimeEntry[]> {
    const search = new URLSearchParams();
    if (params?.limit) search.set("limit", String(params.limit));
    if (params?.offset) search.set("offset", String(params.offset));
    if (params?.since) search.set("since", params.since);
    if (params?.until) search.set("until", params.until);
    return this.fetch(`/api/time-entries?${search}`);
  }

  /** List time entries linked to a specific issue. */
  async listIssueTimeEntries(issueId: string): Promise<TimeEntry[]> {
    return this.fetch(`/api/issues/${issueId}/time-entries`);
  }

  /** Update description or issue link on a time entry. */
  async updateTimeEntry(entryId: string, data: UpdateTimeEntryRequest): Promise<TimeEntry> {
    return this.fetch(`/api/time-entries/${entryId}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(data),
    });
  }

  /** Delete a time entry. */
  async deleteTimeEntry(entryId: string): Promise<void> {
    await this.fetch(`/api/time-entries/${entryId}`, { method: "DELETE" });
  }

  /** Get workspace-level time aggregation (by member and by project) for a date range. */
  async getTeamTimeStats(params: { since: string; until: string }): Promise<TeamTimeStats> {
    const search = new URLSearchParams({ since: params.since, until: params.until });
    return this.fetch(`/api/time-entries/team-stats?${search}`);
  }

  /** Get total time spent for a project (all time, all members). */
  async getProjectTimeStats(projectId: string): Promise<{ total_seconds: number }> {
    return this.fetch(`/api/projects/${projectId}/time-stats`);
  }

  // Daily Reviews

  /** Trigger (or regenerate) today's review draft for the current user. */
  async generateDailyReview(): Promise<DailyReview> {
    return this.fetch("/api/daily-reviews/generate", { method: "POST" });
  }

  /** Get today's review draft. Returns null (undefined from 204) if none exists. */
  async getTodayReview(): Promise<DailyReview | null> {
    return this.fetch<DailyReview | null>("/api/daily-reviews/today");
  }

  /** List the most recent daily reviews for the current user. */
  async listDailyReviews(limit = 30): Promise<DailyReview[]> {
    return this.fetch(`/api/daily-reviews?limit=${limit}`);
  }

  /** Confirm (sign off) a specific daily review. */
  async confirmDailyReview(reviewId: string): Promise<DailyReview> {
    return this.fetch(`/api/daily-reviews/${reviewId}/confirm`, { method: "POST" });
  }

  // Daily Plans

  /** Trigger (or regenerate) tomorrow's plan draft for the current user. */
  async generateDailyPlan(planDate?: string): Promise<DailyPlan> {
    return this.fetch("/api/daily-plans/generate", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: planDate ? JSON.stringify({ plan_date: planDate }) : undefined,
    });
  }

  /** Get tomorrow's plan draft. Returns null (undefined from 204) if none exists. */
  async getTomorrowPlan(): Promise<DailyPlan | null> {
    return this.fetch<DailyPlan | null>("/api/daily-plans/tomorrow");
  }

  /** List the most recent daily plans for the current user. */
  async listDailyPlans(limit = 30): Promise<DailyPlan[]> {
    return this.fetch(`/api/daily-plans?limit=${limit}`);
  }

  /** Confirm (sign off) a specific daily plan. */
  async confirmDailyPlan(planId: string): Promise<DailyPlan> {
    return this.fetch(`/api/daily-plans/${planId}/confirm`, { method: "POST" });
  }

  // Automation Templates

  /** List all built-in automation templates with their workspace enablement state. */
  async listAutomationTemplates(): Promise<AutomationTemplate[]> {
    return this.fetch("/api/automation/templates");
  }

  /** Enable a built-in automation template for the current workspace. */
  async enableAutomationRule(templateId: string): Promise<unknown> {
    return this.fetch("/api/automation/rules", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ template_id: templateId }),
    });
  }

  /** Disable (remove) an automation rule for the current workspace. */
  async disableAutomationRule(templateId: string): Promise<void> {
    return this.fetch(`/api/automation/rules/${templateId}`, { method: "DELETE" });
  }

  /** Manually run a manual-trigger automation template. */
  async runAutomationTemplate(templateId: string): Promise<StandupSummaryResult> {
    return this.fetch(`/api/automation/rules/${templateId}/run`, { method: "POST" });
  }

  // Pomodoro

  /** Get the current pomodoro session (or an idle default if none exists). */
  async getPomodoroSession(): Promise<PomodoroSession> {
    return this.fetch("/api/pomodoro/current");
  }

  /** Start or resume the pomodoro timer. */
  async startPomodoro(): Promise<PomodoroSession> {
    return this.fetch("/api/pomodoro/start", { method: "POST" });
  }

  /** Pause the running pomodoro timer (backend records elapsed time). */
  async pausePomodoro(): Promise<PomodoroSession> {
    return this.fetch("/api/pomodoro/pause", { method: "POST" });
  }

  /**
   * Complete the current phase.
   * Work-phase completion auto-creates a time_entry with type='pomodoro'.
   */
  async completePomodoro(): Promise<PomodoroSession> {
    return this.fetch("/api/pomodoro/complete", { method: "POST" });
  }

  /** Reset the pomodoro session back to idle. */
  async resetPomodoro(): Promise<PomodoroSession> {
    return this.fetch("/api/pomodoro/reset", { method: "POST" });
  }
}
