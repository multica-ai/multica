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
  private createdInboxItemIds: string[] = [];

  async login(email: string, name: string) {
    const client = new pg.Client(DATABASE_URL);
    await client.connect();
    try {
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

  async deleteIssue(id: string) {
    await this.authedFetch(`/api/issues/${id}`, { method: "DELETE" });
  }

  /** Clean up all issues + inbox items created during this test. */
  async cleanup() {
    for (const id of this.createdIssueIds) {
      try {
        await this.deleteIssue(id);
      } catch {
        /* ignore — may already be deleted */
      }
    }
    this.createdIssueIds = [];

    if (this.createdInboxItemIds.length > 0) {
      const client = new pg.Client(DATABASE_URL);
      await client.connect();
      try {
        await client.query("DELETE FROM inbox_item WHERE id = ANY($1::uuid[])", [
          this.createdInboxItemIds,
        ]);
      } finally {
        await client.end();
      }
      this.createdInboxItemIds = [];
    }

    if (this.userId) {
      // Reset user's preferences blob so route choices don't leak between tests.
      try {
        await this.authedFetch("/api/me/preferences", {
          method: "PATCH",
          body: JSON.stringify({ notifications: {} }),
        });
      } catch {
        /* best-effort cleanup */
      }
    }
  }

  getToken() {
    return this.token;
  }

  getUserId() {
    return this.userId;
  }

  /**
   * Create a second user, add them as a member of this workspace, log them
   * in, and return a tiny client capable of API calls. Used by tests that
   * need a non-actor user to trigger events that fire notifications back to
   * the primary e2e user.
   */
  async loginSecondaryUser(email: string, name: string): Promise<{
    token: string;
    userId: string;
    fetch: (path: string, init?: RequestInit) => Promise<Response>;
  }> {
    if (!this.workspaceId) {
      throw new Error("ensureWorkspace must run before loginSecondaryUser");
    }

    const client = new pg.Client(DATABASE_URL);
    await client.connect();
    let secondaryToken = "";
    let secondaryUserId = "";
    try {
      await client.query("DELETE FROM verification_code WHERE email = $1", [email]);

      const sendRes = await fetch(`${API_BASE}/auth/send-code`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email }),
      });
      if (!sendRes.ok) throw new Error(`send-code failed: ${sendRes.status}`);

      const codeRow = await client.query(
        "SELECT code FROM verification_code WHERE email = $1 AND used = FALSE AND expires_at > now() ORDER BY created_at DESC LIMIT 1",
        [email],
      );
      if (codeRow.rows.length === 0) throw new Error(`no code for ${email}`);

      const verifyRes = await fetch(`${API_BASE}/auth/verify-code`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, code: codeRow.rows[0].code }),
      });
      if (!verifyRes.ok) throw new Error(`verify-code failed: ${verifyRes.status}`);
      const data = await verifyRes.json();
      secondaryToken = data.token;
      secondaryUserId = data.user.id;

      // Add as workspace member (idempotent).
      await client.query(
        `INSERT INTO member (workspace_id, user_id, role)
         VALUES ($1, $2, 'member')
         ON CONFLICT (workspace_id, user_id) DO NOTHING`,
        [this.workspaceId, secondaryUserId],
      );

      if (name) {
        await fetch(`${API_BASE}/api/me`, {
          method: "PATCH",
          headers: {
            "Content-Type": "application/json",
            Authorization: `Bearer ${secondaryToken}`,
          },
          body: JSON.stringify({ name }),
        });
      }

      await client.query("DELETE FROM verification_code WHERE email = $1", [email]);
    } finally {
      await client.end();
    }

    const wsSlug = this.workspaceSlug;
    const sFetch = (path: string, init?: RequestInit) => {
      const headers: Record<string, string> = {
        "Content-Type": "application/json",
        Authorization: `Bearer ${secondaryToken}`,
        ...((init?.headers as Record<string, string>) ?? {}),
      };
      if (wsSlug) headers["X-Workspace-Slug"] = wsSlug;
      return fetch(`${API_BASE}${path}`, { ...init, headers });
    };

    return { token: secondaryToken, userId: secondaryUserId, fetch: sFetch };
  }

  /**
   * Wipe every inbox_item for this user+workspace. Use in beforeEach to
   * guarantee a clean slate — previous test runs (or unrelated leftover
   * notifications) won't leak into per-row count assertions.
   */
  async resetInboxItems() {
    if (!this.workspaceId || !this.userId) {
      throw new Error("Login + ensureWorkspace must run before resetInboxItems");
    }
    const client = new pg.Client(DATABASE_URL);
    await client.connect();
    try {
      await client.query(
        "DELETE FROM inbox_item WHERE workspace_id = $1 AND recipient_id = $2",
        [this.workspaceId, this.userId],
      );
    } finally {
      await client.end();
    }
  }

  /**
   * Insert an inbox item directly into the database.
   * The notification listener flow requires a separate (non-actor) user to
   * trigger naturally; for E2E we seed deterministic items via SQL instead.
   */
  async insertInboxItem(opts: {
    type: string;
    route?: "inbox" | "notifications";
    severity?: "action_required" | "info" | "attention";
    title: string;
    body?: string;
    issueId?: string;
    actorType?: "member" | "agent";
    actorId?: string;
    details?: Record<string, unknown>;
    read?: boolean;
  }): Promise<string> {
    if (!this.workspaceId || !this.userId) {
      throw new Error("Login + ensureWorkspace must run before insertInboxItem");
    }
    const client = new pg.Client(DATABASE_URL);
    await client.connect();
    try {
      const res = await client.query<{ id: string }>(
        `INSERT INTO inbox_item
           (workspace_id, recipient_type, recipient_id, type, severity, route,
            issue_id, title, body, actor_type, actor_id, details, read)
         VALUES ($1, 'member', $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
         RETURNING id`,
        [
          this.workspaceId,
          this.userId,
          opts.type,
          opts.severity ?? "info",
          opts.route ?? "notifications",
          opts.issueId ?? null,
          opts.title,
          opts.body ?? null,
          opts.actorType ?? null,
          opts.actorId ?? null,
          JSON.stringify(opts.details ?? {}),
          opts.read ?? false,
        ],
      );
      const id = res.rows[0]!.id;
      this.createdInboxItemIds.push(id);
      return id;
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
    if (this.workspaceSlug) headers["X-Workspace-Slug"] = this.workspaceSlug;
    else if (this.workspaceId) headers["X-Workspace-ID"] = this.workspaceId;
    return fetch(`${API_BASE}${path}`, { ...init, headers });
  }
}
