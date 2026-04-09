/**
 * TestApiClient — lightweight API helper for E2E test data setup/teardown.
 *
 * Uses raw fetch so E2E tests have zero build-time coupling to the web app.
 */

import pg from "pg";

const API_BASE = process.env.NEXT_PUBLIC_API_URL ?? `http://localhost:${process.env.PORT ?? "8080"}`;
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
  private user: Record<string, unknown> | null = null;

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
