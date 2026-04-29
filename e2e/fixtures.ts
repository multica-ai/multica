/**
 * TestApiClient — lightweight API helper for E2E test data setup/teardown.
 *
 * Uses raw fetch so E2E tests have zero build-time coupling to the web app.
 */

import "./env";
import pg from "pg";

// `||` (not `??`) so an empty `NEXT_PUBLIC_API_URL=` in .env still falls
// back to localhost. dotenv sets unset-vs-empty both as "" — treating them
// the same matches user intent.
const API_BASE = process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;
const DATABASE_URL = process.env.DATABASE_URL ?? "postgres://multica:multica@localhost:5432/multica?sslmode=disable";

interface TestWorkspace {
  id: string;
  name: string;
  slug: string;
}

export class TestApiClient {
  private token: string | null = null;
  private userId: string | null = null;
  private workspaceSlug: string | null = null;
  private workspaceId: string | null = null;
  private createdIssueIds: string[] = [];
  private createdCommentIds: string[] = [];
  private createdTaskIds: string[] = [];
  private createdAgentIds: string[] = [];
  private createdRuntimeIds: string[] = [];

  async login(email: string, name: string) {
    const client = new pg.Client(DATABASE_URL);
    await client.connect();
    const advisoryKey = `e2e-login:${email}`;
    try {
      await client.query(
        "SELECT pg_advisory_lock(hashtext($1)::bigint)",
        [advisoryKey],
      );

      // Keep each E2E login isolated so previous test runs do not trip the
      // per-email send-code rate limit.
      await client.query("DELETE FROM verification_code WHERE email = $1", [email]);

      // Step 1: Send verification code
      const sendRes = await fetch(`${API_BASE}/auth/send-code`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email }),
      });
      if (!sendRes.ok) {
        throw new Error(`send-code failed: ${sendRes.status}`);
      }

      // Step 2: Read code from database
      const result = await client.query(
        "SELECT code FROM verification_code WHERE email = $1 AND used = FALSE AND expires_at > now() ORDER BY created_at DESC LIMIT 1",
        [email],
      );
      if (result.rows.length === 0) {
        throw new Error(`No verification code found for ${email}`);
      }

      // Step 3: Verify code to get JWT
      const verifyRes = await fetch(`${API_BASE}/auth/verify-code`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, code: result.rows[0].code }),
      });
      if (!verifyRes.ok) {
        throw new Error(`verify-code failed: ${verifyRes.status}`);
      }
      const data = await verifyRes.json();

      this.token = data.token;
      this.userId = data.user?.id ?? null;

      // Update user name if needed
      if (name && data.user?.name !== name) {
        await this.authedFetch("/api/me", {
          method: "PATCH",
          body: JSON.stringify({ name }),
        });
      }

      await client.query("DELETE FROM verification_code WHERE email = $1", [email]);

      return data;
    } finally {
      await client.query(
        "SELECT pg_advisory_unlock(hashtext($1)::bigint)",
        [advisoryKey],
      ).catch(() => {});
      await client.end();
    }
  }

  async getWorkspaces(): Promise<TestWorkspace[]> {
    const res = await this.authedFetch("/api/workspaces");
    return res.json();
  }

  setWorkspaceId(id: string) {
    this.workspaceId = id;
  }

  setWorkspaceSlug(slug: string) {
    this.workspaceSlug = slug;
  }

  async ensureWorkspace(name = "E2E Workspace", slug = "e2e-workspace") {
    const workspaces = await this.getWorkspaces();
    const workspace = workspaces.find((item) => item.slug === slug) ?? workspaces[0];
    if (workspace) {
      this.workspaceId = workspace.id;
      this.workspaceSlug = workspace.slug;
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

  async seedIssueExecution(
    issueId: string,
    opts: {
      status: "queued" | "dispatched" | "running" | "completed" | "failed" | "cancelled";
      triggerText?: string;
      priority?: number;
      error?: string;
    },
  ) {
    if (!this.workspaceId || !this.userId) {
      throw new Error("workspace and user must be initialized before seeding execution");
    }

    const client = new pg.Client(DATABASE_URL);
    await client.connect();
    try {
      const unique = `${Date.now()}-${Math.floor(Math.random() * 10000)}`;

      const runtimeResult = await client.query(
        `
          INSERT INTO agent_runtime (
            workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
          )
          VALUES ($1, NULL, $2, 'cloud', $3, 'online', $4, '{}'::jsonb, now())
          RETURNING id
        `,
        [
          this.workspaceId,
          `E2E Runtime ${unique}`,
          `e2e_runtime_${unique}`,
          "E2E seeded runtime",
        ],
      );
      const runtimeId = runtimeResult.rows[0]?.id as string;
      this.createdRuntimeIds.push(runtimeId);

      const agentResult = await client.query(
        `
          INSERT INTO agent (
            workspace_id, name, description, runtime_mode, runtime_config,
            runtime_id, visibility, max_concurrent_tasks, owner_id
          )
          VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
          RETURNING id
        `,
        [this.workspaceId, `E2E Agent ${unique}`, runtimeId, this.userId],
      );
      const agentId = agentResult.rows[0]?.id as string;
      this.createdAgentIds.push(agentId);

      let triggerCommentId: string | null = null;
      if (opts.triggerText) {
        const commentResult = await client.query(
          `
            INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
            VALUES ($1, $2, 'member', $3, $4, 'comment')
            RETURNING id
          `,
          [issueId, this.workspaceId, this.userId, opts.triggerText],
        );
        triggerCommentId = commentResult.rows[0]?.id as string;
        this.createdCommentIds.push(triggerCommentId);
      }

      const timestamps =
        opts.status === "running"
          ? { dispatchedAt: null, startedAt: "now()", completedAt: null }
          : opts.status === "dispatched"
            ? { dispatchedAt: "now()", startedAt: null, completedAt: null }
            : opts.status === "completed" || opts.status === "failed" || opts.status === "cancelled"
              ? { dispatchedAt: "now() - interval '1 minute'", startedAt: "now() - interval '1 minute'", completedAt: "now()" }
              : { dispatchedAt: null, startedAt: null, completedAt: null };

      const taskResult = await client.query(
        `
          INSERT INTO agent_task_queue (
            agent_id, runtime_id, issue_id, status, priority, trigger_comment_id, error,
            dispatched_at, started_at, completed_at, created_at
          )
          VALUES (
            $1, $2, $3, $4, $5, $6, $7,
            ${timestamps.dispatchedAt ?? "NULL"},
            ${timestamps.startedAt ?? "NULL"},
            ${timestamps.completedAt ?? "NULL"},
            now()
          )
          RETURNING id
        `,
        [
          agentId,
          runtimeId,
          issueId,
          opts.status,
          opts.priority ?? 1,
          triggerCommentId,
          opts.error ?? null,
        ],
      );

      const taskId = taskResult.rows[0]?.id as string;
      this.createdTaskIds.push(taskId);

      return { agentId, runtimeId, taskId, triggerCommentId };
    } finally {
      await client.end();
    }
  }

  async deleteIssue(id: string) {
    await this.authedFetch(`/api/issues/${id}`, { method: "DELETE" });
  }

  /** Clean up all issues created during this test. */
  async cleanup() {
    if (this.createdTaskIds.length > 0) {
      const taskIds = [...this.createdTaskIds];
      this.createdTaskIds = [];
      const client = new pg.Client(DATABASE_URL);
      await client.connect();
      try {
        await client.query(`DELETE FROM agent_task_queue WHERE id = ANY($1::uuid[])`, [taskIds]);
      } finally {
        await client.end();
      }
    }

    if (this.createdCommentIds.length > 0) {
      const commentIds = [...this.createdCommentIds];
      this.createdCommentIds = [];
      const client = new pg.Client(DATABASE_URL);
      await client.connect();
      try {
        await client.query(`DELETE FROM comment WHERE id = ANY($1::uuid[])`, [commentIds]);
      } finally {
        await client.end();
      }
    }

    for (const id of this.createdIssueIds) {
      try {
        await this.deleteIssue(id);
      } catch {
        /* ignore — may already be deleted */
      }
    }
    this.createdIssueIds = [];

    if (this.createdAgentIds.length > 0) {
      const agentIds = [...this.createdAgentIds];
      this.createdAgentIds = [];
      const client = new pg.Client(DATABASE_URL);
      await client.connect();
      try {
        await client.query(`DELETE FROM agent WHERE id = ANY($1::uuid[])`, [agentIds]);
      } finally {
        await client.end();
      }
    }

    if (this.createdRuntimeIds.length > 0) {
      const runtimeIds = [...this.createdRuntimeIds];
      this.createdRuntimeIds = [];
      const client = new pg.Client(DATABASE_URL);
      await client.connect();
      try {
        await client.query(`DELETE FROM agent_runtime WHERE id = ANY($1::uuid[])`, [runtimeIds]);
      } finally {
        await client.end();
      }
    }
  }

  getToken() {
    return this.token;
  }

  private async authedFetch(path: string, init?: RequestInit) {
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      ...((init?.headers as Record<string, string>) ?? {}),
    };
    if (this.token) headers["Authorization"] = `Bearer ${this.token}`;
    if (this.workspaceSlug) headers["X-Workspace-Slug"] = this.workspaceSlug;
    else if (this.workspaceId) headers["X-Workspace-ID"] = this.workspaceId;
    return fetch(`${API_BASE}${path}`, { ...init, headers });
  }
}
