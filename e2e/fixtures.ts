/**
 * TestApiClient — lightweight API helper for E2E test data setup/teardown.
 *
 * Uses raw fetch so E2E tests have zero build-time coupling to the web app.
 */

import pg from "pg";

const envApiBase = process.env.VITE_API_URL;
const API_BASE = envApiBase && envApiBase.length > 0 ? envApiBase : `http://localhost:${process.env.PORT ?? "8080"}`;
const DATABASE_URL = process.env.DATABASE_URL ?? "postgres://multica:multica@localhost:5432/multica?sslmode=disable";

interface TestWorkspace {
  id: string;
  name: string;
  slug: string;
}

export class TestApiClient {
  private token: string | null = null;
  private workspaceId: string | null = null;
  private createdIssueIds: string[] = [];
  private createdIssueLabelIds: string[] = [];
  private createdProjectIds: string[] = [];
  private createdTimeEntryIds: string[] = [];
  private user: Record<string, unknown> | null = null;
  /** Whether a pomodoro session was started during this test (needs reset in cleanup). */
  private pomodoroSessionActive = false;

  async login(email: string, name: string) {
    const client = new pg.Client(DATABASE_URL);
    await client.connect();
    try {
      await client.query("DELETE FROM verification_code WHERE email = $1", [email]);
      await client.query(
        `INSERT INTO verification_code (email, code, expires_at, used, attempts)
         VALUES ($1, $2, now() + interval '10 minutes', FALSE, 0)`,
        [email, "888888"]
      );
    } finally {
      await client.end();
    }

    const verifyRes = await fetch(`${API_BASE}/auth/verify-code`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email, code: "888888" }),
    });
    if (!verifyRes.ok) {
      throw new Error(`verify-code failed: ${verifyRes.status}`);
    }

    const data = await verifyRes.json();
    this.token = data.token;
    this.user = data.user ?? null;

    if (name && data.user?.name !== name) {
      await this.authedFetch("/api/me", {
        method: "PATCH",
        body: JSON.stringify({ name }),
      });
    }

    return data;
  }

  async getWorkspaces(): Promise<TestWorkspace[]> {
    const res = await this.authedFetch("/api/workspaces");
    return res.json();
  }

  setWorkspaceId(id: string) {
    this.workspaceId = id;
  }

  async ensureWorkspace(name = "E2E Workspace", slug = "e2e-workspace") {
    const workspaces = await this.getWorkspaces();
    const workspace = workspaces.find((item) => item.slug === slug) ?? workspaces[0];
    if (workspace) {
      this.workspaceId = workspace.id;
      return workspace;
    }

    const res = await this.authedFetch("/api/workspaces", {
      method: "POST",
      body: JSON.stringify({ name, slug }),
    });
    if (res.ok) {
      const created = (await res.json()) as TestWorkspace;
      this.workspaceId = created.id;
      return created;
    }

    const refreshed = await this.getWorkspaces();
    const created = refreshed.find((item) => item.slug === slug) ?? refreshed[0];
    if (created) {
      this.workspaceId = created.id;
      return created;
    }

    throw new Error(`Failed to ensure workspace ${slug}: ${res.status} ${res.statusText}`);
  }

  async createIssue(title: string, opts?: Record<string, unknown>) {
    const res = await this.authedFetch("/api/issues", {
      method: "POST",
      body: JSON.stringify({ title, ...opts }),
    });
    const issue = await res.json();
    this.createdIssueIds.push(issue.id);
    return issue;
  }

  async createIssueLabel(name: string, color = "#3b82f6") {
    const res = await this.authedFetch("/api/labels", {
      method: "POST",
      body: JSON.stringify({ name, color }),
    });
    const label = await res.json();
    if (res.status === 201 && label?.id) {
      this.createdIssueLabelIds.push(label.id);
    }
    return label;
  }

  async getIssue(id: string) {
    const res = await this.authedFetch(`/api/issues/${id}`);
    return res.json();
  }

  async listIssues(params?: Record<string, string | null | undefined>) {
    const search = new URLSearchParams();
    for (const [key, value] of Object.entries(params ?? {})) {
      if (value) {
        search.set(key, value);
      }
    }

    const query = search.toString();
    const res = await this.authedFetch(query ? `/api/issues?${query}` : "/api/issues");
    return res.json();
  }

  async updateIssue(id: string, updates: Record<string, unknown>) {
    const res = await this.authedFetch(`/api/issues/${id}`, {
      method: "PUT",
      body: JSON.stringify(updates),
    });
    return res.json();
  }

  async listProjects(params?: { status?: string }) {
    const search = new URLSearchParams();
    if (params?.status) {
      search.set("status", params.status);
    }

    const query = search.toString();
    const res = await this.authedFetch(query ? `/api/projects?${query}` : "/api/projects");
    return res.json();
  }

  async getProject(id: string) {
    const res = await this.authedFetch(`/api/projects/${id}`);
    return res.json();
  }

  async createProject(data: Record<string, unknown>) {
    const res = await this.authedFetch("/api/projects", {
      method: "POST",
      body: JSON.stringify(data),
    });
    const project = await res.json();
    this.createdProjectIds.push(project.id);
    return project;
  }

  trackProject(id: string) {
    if (!this.createdProjectIds.includes(id)) {
      this.createdProjectIds.push(id);
    }
  }

  async deleteProject(id: string) {
    await this.authedFetch(`/api/projects/${id}`, { method: "DELETE" });
  }

  async listRuntimes() {
    const wsId = this.workspaceId;
    if (!wsId) {
      throw new Error("Missing workspace id");
    }
    const res = await this.authedFetch(`/api/runtimes?workspace_id=${wsId}`);
    return res.json();
  }

  async ensureRuntime(name = "E2E Runtime") {
    const runtimes = await this.listRuntimes();
    const existing = runtimes.find((runtime: { name: string }) => runtime.name === name);
    if (existing) {
      return existing;
    }

    if (!this.workspaceId) {
      throw new Error("Missing workspace id");
    }

    const client = new pg.Client(DATABASE_URL);
    await client.connect();
    try {
      const result = await client.query(
        `INSERT INTO agent_runtime (
          workspace_id, daemon_id, name, runtime_mode, provider,
          status, device_info, metadata, last_seen_at
        ) VALUES ($1, $2, $3, 'local', 'codex', 'online', $4, $5, now())
        RETURNING id, name`,
        [
          this.workspaceId,
          "e2e-runtime",
          name,
          `${name} device`,
          JSON.stringify({ source: "e2e" }),
        ],
      );
      return result.rows[0];
    } finally {
      await client.end();
    }
  }

  async listAgents() {
    const wsId = this.workspaceId;
    if (!wsId) {
      throw new Error("Missing workspace id");
    }
    const res = await this.authedFetch(`/api/agents?workspace_id=${wsId}`);
    return res.json();
  }

  async ensureAgent(name = "E2E Route Agent") {
    const agents = await this.listAgents();
    const existing = agents.find((agent: { name: string }) => agent.name === name);
    if (existing) {
      return existing;
    }

    const runtime = await this.ensureRuntime();
    const res = await this.authedFetch("/api/agents", {
      method: "POST",
      body: JSON.stringify({
        name,
        description: "E2E route agent",
        runtime_id: runtime.id,
        visibility: "workspace",
        triggers: [
          { id: crypto.randomUUID(), type: "on_assign", enabled: true, config: {} },
          { id: crypto.randomUUID(), type: "on_comment", enabled: true, config: {} },
        ],
      }),
    });
    return res.json();
  }

  async deleteIssue(id: string) {
    await this.authedFetch(`/api/issues/${id}`, { method: "DELETE" });
  }

  async deleteIssueLabel(id: string) {
    await this.authedFetch(`/api/labels/${id}`, { method: "DELETE" });
  }

  // ── Time entry helpers ─────────────────────────────────────────────────────

  /** Start a live timer (no stop_time). Returns the created TimeEntry. */
  async startTimer(opts?: { issue_id?: string; description?: string }): Promise<Record<string, unknown>> {
    const res = await this.authedFetch("/api/time-entries", {
      method: "POST",
      body: JSON.stringify({
        start_time: new Date().toISOString(),
        ...opts,
      }),
    });
    const entry = await res.json();
    this.createdTimeEntryIds.push(entry.id);
    return entry;
  }

  /** Create a finished (manual) time entry. */
  async createTimeEntry(opts: {
    start_time: string;
    stop_time: string;
    description?: string;
    issue_id?: string;
  }): Promise<Record<string, unknown>> {
    const res = await this.authedFetch("/api/time-entries", {
      method: "POST",
      body: JSON.stringify(opts),
    });
    const entry = await res.json();
    this.createdTimeEntryIds.push(entry.id);
    return entry;
  }

  /** Stop the currently running timer by entry id. */
  async stopTimer(entryId: string): Promise<Record<string, unknown>> {
    const res = await this.authedFetch(`/api/time-entries/${entryId}/stop`, {
      method: "PATCH",
    });
    return res.json();
  }

  /** Delete a time entry. */
  async deleteTimeEntry(id: string): Promise<void> {
    await this.authedFetch(`/api/time-entries/${id}`, { method: "DELETE" });
    this.createdTimeEntryIds = this.createdTimeEntryIds.filter((tid) => tid !== id);
  }

  // ── Pomodoro session helpers ───────────────────────────────────────────────

  /** Start (or resume) the user's pomodoro session. */
  async startPomodoroSession(): Promise<Record<string, unknown>> {
    const res = await this.authedFetch("/api/pomodoro/start", { method: "POST" });
    this.pomodoroSessionActive = true;
    return res.json();
  }

  /** Pause the running pomodoro session. */
  async pausePomodoroSession(): Promise<Record<string, unknown>> {
    const res = await this.authedFetch("/api/pomodoro/pause", { method: "POST" });
    return res.json();
  }

  /**
   * Complete the current work phase — creates a time_entry with type='pomodoro'.
   * @param opts Optional issue_id and note to attach.
   */
  async completePomodoroSession(opts?: {
    issue_id?: string;
    note?: string;
    long_break_after?: number;
  }): Promise<Record<string, unknown>> {
    const res = await this.authedFetch("/api/pomodoro/complete", {
      method: "POST",
      body: JSON.stringify(opts ?? {}),
    });
    return res.json();
  }

  /** Reset the pomodoro session back to idle. */
  async resetPomodoroSession(): Promise<void> {
    await this.authedFetch("/api/pomodoro/reset", { method: "POST" });
    this.pomodoroSessionActive = false;
  }

  /** Fetch the current pomodoro session (GET /api/pomodoro/current). */
  async getCurrentPomodoroSession(): Promise<Record<string, unknown> | null> {
    const res = await this.authedFetch("/api/pomodoro/current");
    if (res.status === 404) return null;
    return res.json();
  }

  /** Fetch pomodoro history and stats. */
  async getPomodoroHistory(params?: {
    limit?: number;
    offset?: number;
  }): Promise<Record<string, unknown>> {
    const query = new URLSearchParams();
    if (params?.limit !== undefined) query.set("limit", String(params.limit));
    if (params?.offset !== undefined) query.set("offset", String(params.offset));
    const qs = query.toString();
    const res = await this.authedFetch(`/api/pomodoro/history${qs ? `?${qs}` : ""}`);
    return res.json();
  }

  /** Delete all pomodoro-generated time entries for the current user/workspace. */
  async clearPomodoroHistory(): Promise<void> {
    const userId = typeof this.user?.id === "string" ? this.user.id : null;
    if (!userId || !this.workspaceId) {
      throw new Error("Missing user or workspace for pomodoro cleanup");
    }

    const client = new pg.Client(DATABASE_URL);
    await client.connect();
    try {
      await client.query(
        `DELETE FROM time_entry
         WHERE workspace_id = $1
           AND user_id = $2
           AND type = 'pomodoro'`,
        [this.workspaceId, userId],
      );
    } finally {
      await client.end();
    }
  }

  async createPomodoroHistoryEntry(opts: {
    start_time: string;
    stop_time: string;
    description?: string;
    issue_id?: string;
  }): Promise<{ id: string }> {
    const userId = typeof this.user?.id === "string" ? this.user.id : null;
    if (!userId || !this.workspaceId) {
      throw new Error("Missing user or workspace for pomodoro history seed");
    }

    const client = new pg.Client(DATABASE_URL);
    await client.connect();
    try {
      const result = await client.query<{ id: string }>(
        `INSERT INTO time_entry (
          workspace_id,
          user_id,
          issue_id,
          description,
          start_time,
          stop_time,
          duration_seconds,
          type
        ) VALUES (
          $1,
          $2,
          $3,
          $4,
          $5::timestamptz,
          $6::timestamptz,
          GREATEST(0, EXTRACT(EPOCH FROM ($6::timestamptz - $5::timestamptz)))::bigint,
          'pomodoro'
        )
        RETURNING id`,
        [
          this.workspaceId,
          userId,
          opts.issue_id ?? null,
          opts.description ?? null,
          opts.start_time,
          opts.stop_time,
        ],
      );
      const entry = result.rows[0];
      this.createdTimeEntryIds.push(entry.id);
      return entry;
    } finally {
      await client.end();
    }
  }

  /** Clean up all issues created during this test. */
  async cleanup() {
    for (const id of this.createdIssueIds) {
      try {
        await this.deleteIssue(id);
      } catch {
        /* ignore — may already be deleted */
      }
    }
    this.createdIssueIds = [];

    for (const id of this.createdIssueLabelIds) {
      try {
        await this.deleteIssueLabel(id);
      } catch {
        /* ignore — may already be deleted */
      }
    }
    this.createdIssueLabelIds = [];

    for (const id of this.createdProjectIds) {
      try {
        await this.deleteProject(id);
      } catch {
        /* ignore — may already be deleted */
      }
    }
    this.createdProjectIds = [];

    for (const id of this.createdTimeEntryIds) {
      try {
        await this.deleteTimeEntry(id);
      } catch {
        /* ignore — may already be deleted */
      }
    }
    this.createdTimeEntryIds = [];

    if (this.pomodoroSessionActive) {
      try {
        await this.resetPomodoroSession();
      } catch {
        /* ignore — session may already be idle */
      }
    }
  }

  getToken() {
    return this.token;
  }

  getUser() {
    return this.user;
  }

  getWorkspaceId() {
    return this.workspaceId;
  }

  async createInboxItem(issueId: string, title: string, body = "E2E inbox item") {
    if (!this.workspaceId) {
      throw new Error("Missing workspace id");
    }
    const userId = this.user?.id;
    if (typeof userId !== "string") {
      throw new Error("Missing user id");
    }

    const client = new pg.Client(DATABASE_URL);
    await client.connect();
    try {
      await client.query(
        `INSERT INTO inbox_item (
          workspace_id, recipient_type, recipient_id,
          type, severity, issue_id, title, body,
          actor_type, actor_id, details
        ) VALUES ($1, 'member', $2, 'mentioned', 'info', $3, $4, $5, 'member', $2, $6)`,
        [this.workspaceId, userId, issueId, title, body, JSON.stringify({})],
      );
    } finally {
      await client.end();
    }
  }

  private async authedFetch(path: string, init?: RequestInit) {
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      ...((init?.headers as Record<string, string>) ?? {}),
    };
    if (this.token) headers["Authorization"] = `Bearer ${this.token}`;
    if (this.workspaceId) headers["X-Workspace-ID"] = this.workspaceId;
    return fetch(`${API_BASE}${path}`, { ...init, headers });
  }
}
