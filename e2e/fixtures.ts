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
  private createdProjectIds: string[] = [];
  private createdSprintIds: string[] = [];
  private createdArtifactIds: string[] = [];
  private createdAgentIds: string[] = [];
  private createdRuntimeIds: string[] = [];
  private createdAccountIds: string[] = [];
  private createdFolderIds: string[] = [];

  async login(email: string, name: string) {
    const client = new pg.Client(DATABASE_URL);
    await client.connect();
    try {
      await client.query("SELECT pg_advisory_lock(hashtext($1))", [`e2e-login:${email}`]);

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
      await client.query("SELECT pg_advisory_unlock(hashtext($1))", [`e2e-login:${email}`]).catch(() => {});
      await client.end();
    }
  }

  async getWorkspaces(): Promise<TestWorkspace[]> {
    const res = await this.authedFetch("/api/workspaces");
    return res.json();
  }

  async getAgentCapabilities(agentId: string): Promise<{
    tools: Array<{
      key: string;
      delivery_channel?: string;
      permission: string;
      allowed?: boolean;
      available?: boolean;
      enforced?: boolean;
      callable?: boolean;
      verified?: boolean;
      availability?: {
        level: string;
        proven: boolean;
        reason: string;
      };
    }>;
    availability?: {
      runtime_type: string;
      status: string;
      verified: number;
      discovered: number;
      declared: number;
      unproven: number;
    };
    observed_access?: {
      status: string;
      drift_count: number;
      tools: Array<{
        name: string;
        permission: string;
        status: string;
        drift: boolean;
      }>;
    };
  }> {
    const res = await this.authedFetch(`/api/agents/${agentId}/capabilities`);
    if (!res.ok) {
      throw new Error(
        `getAgentCapabilities failed: ${res.status} ${await res.text()}`,
      );
    }
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
      this.workspaceSlug = created.slug;
      return created;
    }

    const refreshed = await this.getWorkspaces();
    const created = refreshed.find((item) => item.slug === slug) ?? refreshed[0];
    if (created) {
      this.workspaceId = created.id;
      this.workspaceSlug = created.slug;
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

  // `parentId` hangs the new comment under an existing one, which is the only
  // way to build a multi-comment thread for the move-to-thread specs.
  async createComment(issueId: string, content: string, parentId?: string) {
    const res = await this.authedFetch(`/api/issues/${issueId}/comments`, {
      method: "POST",
      body: JSON.stringify(parentId ? { content, parent_id: parentId } : { content }),
    });
    if (!res.ok) {
      throw new Error(`createComment failed: ${res.status} ${await res.text()}`);
    }
    return res.json();
  }

  async listComments(issueId: string): Promise<Array<{
    id: string;
    parent_id: string | null;
    content: string | null;
    type: string;
  }>> {
    const res = await this.authedFetch(`/api/issues/${issueId}/comments`);
    if (!res.ok) {
      throw new Error(`listComments failed: ${res.status} ${await res.text()}`);
    }
    const body = await res.json();
    return Array.isArray(body) ? body : (body.comments ?? []);
  }

  // FIR-2680 — create a named channel (kind='channel'). Channels are issues, so
  // the returned id is tracked in createdIssueIds and torn down by cleanup().
  async createChannel(name: string, memberIds: string[] = [], agentIds: string[] = []) {
    const res = await this.authedFetch("/api/channels", {
      method: "POST",
      body: JSON.stringify({ kind: "channel", name, member_ids: memberIds, agent_ids: agentIds }),
    });
    if (!res.ok) {
      throw new Error(`createChannel failed: ${res.status} ${await res.text()}`);
    }
    const channel = await res.json();
    this.createdIssueIds.push(channel.id);
    return channel;
  }

  // FIR-2873 — create or reopen the caller's DM with one workspace member.
  // DMs are issues too, so cleanup uses the same tracked issue ids as Channels.
  async createDirectMessage(memberId: string) {
    const res = await this.authedFetch("/api/channels", {
      method: "POST",
      body: JSON.stringify({ kind: "dm", member_ids: [memberId] }),
    });
    if (!res.ok) {
      throw new Error(`createDirectMessage failed: ${res.status} ${await res.text()}`);
    }
    const directMessage = await res.json();
    this.createdIssueIds.push(directMessage.id);
    return directMessage;
  }

  // FIR-2680 — flip a cerebro workspace feature flag on/off directly in the DB
  // (the flag defaults OFF; the guard + prompt only run when it is ON).
  async setWorkspaceFeatureFlag(flagKey: string, enabled: boolean) {
    if (!this.workspaceId) throw new Error("ensureWorkspace must run before setWorkspaceFeatureFlag");
    const client = new pg.Client(DATABASE_URL);
    await client.connect();
    try {
      await client.query(
        `INSERT INTO cerebro_feature_flags (workspace_id, user_id, flag_key, enabled)
         VALUES ($1, '00000000-0000-0000-0000-000000000000', $2, $3)
         ON CONFLICT (workspace_id, user_id, flag_key)
         DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = NOW()`,
        [this.workspaceId, flagKey, enabled],
      );
    } finally {
      await client.end();
    }
  }

  // FIR-2680 — count 'mentioned' inbox rows for a recipient on an issue/channel,
  // so a test can assert whether a mention notification was (not) delivered.
  async countMentionedRows(issueId: string, recipientId: string): Promise<number> {
    const client = new pg.Client(DATABASE_URL);
    await client.connect();
    try {
      const res = await client.query<{ n: string }>(
        `SELECT COUNT(*)::text AS n FROM inbox_item
         WHERE issue_id = $1 AND recipient_type = 'member' AND recipient_id = $2 AND type = 'mentioned'`,
        [issueId, recipientId],
      );
      return Number(res.rows[0]?.n ?? "0");
    } finally {
      await client.end();
    }
  }

  async createProject(title: string, opts?: Record<string, unknown>) {
    const res = await this.authedFetch("/api/projects", {
      method: "POST",
      body: JSON.stringify({
        title,
        status: "in_progress",
        priority: "medium",
        ...opts,
      }),
    });
    if (!res.ok) {
      throw new Error(`createProject failed: ${res.status} ${await res.text()}`);
    }
    const project = await res.json();
    this.createdProjectIds.push(project.id);
    return project;
  }

  async deleteProject(id: string) {
    await this.authedFetch(`/api/projects/${id}`, { method: "DELETE" });
  }

  async createProjectSprint(
    projectId: string,
    input: {
      name: string;
      start_date: string;
      end_date: string;
      status?: "planned" | "active" | "completed";
      goal?: string;
    },
  ) {
    const res = await this.authedFetch(
      `/api/cerebro/projects/${projectId}/sprints`,
      {
        method: "POST",
        body: JSON.stringify(input),
      },
    );
    if (!res.ok) {
      throw new Error(
        `createProjectSprint failed: ${res.status} ${await res.text()}`,
      );
    }
    const sprint = await res.json();
    this.createdSprintIds.push(sprint.id);
    return sprint;
  }

  async deleteSprint(id: string) {
    await this.authedFetch(`/api/cerebro/sprints/${id}`, {
      method: "DELETE",
    });
  }

  async dismissStarterContent() {
    if (!this.workspaceId) {
      throw new Error("ensureWorkspace must run before dismissStarterContent");
    }
    if (!this.userId) {
      throw new Error("login must run before dismissStarterContent");
    }

    // The /api/me/starter-content/{import,dismiss} HTTP routes were removed
    // with the starter-content kit (see migration 095). Mark every workspace
    // prompt terminal for the shared test user: starter content is dismissed,
    // and the delayed source-attribution backfill is explicitly skipped so it
    // cannot intercept unrelated E2E interactions after a page has settled.
    const client = new pg.Client(DATABASE_URL);
    await client.connect();
    try {
      await client.query(
        `UPDATE "user"
            SET starter_content_state = COALESCE(starter_content_state, 'dismissed'),
                onboarding_questionnaire =
                  COALESCE(onboarding_questionnaire, '{}'::jsonb)
                  || '{"source_skipped": true}'::jsonb
          WHERE id = $1`,
        [this.userId],
      );
    } finally {
      await client.end();
    }
  }

  async updateProjectAccess(id: string, access: "workspace" | "restricted") {
    const res = await this.authedFetch(`/api/projects/${id}/access`, {
      method: "PATCH",
      body: JSON.stringify({ access }),
    });
    if (!res.ok) {
      throw new Error(`updateProjectAccess failed: ${res.status} ${await res.text()}`);
    }
    return res.json();
  }

  async listProjectMembers(id: string): Promise<{ members: Array<{ user_id: string; name: string }> }> {
    const res = await this.authedFetch(`/api/projects/${id}/members`);
    if (!res.ok) {
      throw new Error(`listProjectMembers failed: ${res.status}`);
    }
    return res.json();
  }

  /**
   * FIR-3778 — seed the one artifact shape the public API cannot produce from a
   * member session: a document authored by an AGENT for this logged-in user.
   * Creating it over HTTP would need agent task credentials the E2E harness has
   * no way to mint, so the row is inserted directly.
   */
  async createAgentDocument(title: string, body: string) {
    if (!this.workspaceId) {
      throw new Error("createAgentDocument: no workspace — call login() first");
    }
    const client = new pg.Client(DATABASE_URL);
    await client.connect();
    try {
      const runtime = await client.query(
        `INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
         VALUES ($1, NULL, 'FIR-3778 E2E Runtime', 'cloud', 'e2e_runtime', 'online', 'E2E runtime', '{}'::jsonb, now())
         RETURNING id`,
        [this.workspaceId],
      );
      this.createdRuntimeIds.push(runtime.rows[0].id);
      const agent = await client.query(
        `INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config,
                            runtime_id, visibility, max_concurrent_tasks, owner_id)
         VALUES ($1, 'FIR-3778 E2E Agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3)
         RETURNING id`,
        [this.workspaceId, runtime.rows[0].id, this.userId],
      );
      const artifact = await client.query(
        `INSERT INTO artifact (workspace_id, kind, format, title, body, metadata,
                               author_type, author_id, requester_user_id)
         VALUES ($1, 'note', 'md', $2, $3, '{}'::jsonb, 'agent', $4, $5)
         RETURNING id`,
        [this.workspaceId, title, body, agent.rows[0].id, this.userId],
      );
      this.createdArtifactIds.push(artifact.rows[0].id);
      this.createdAgentIds.push(agent.rows[0].id);
      return { id: artifact.rows[0].id as string };
    } finally {
      await client.end();
    }
  }

  /** FIR-3660 — seed an agent, runtime, and account for the hover-card contract. */
  async createAgentProfileFixture() {
    if (!this.workspaceId || !this.userId) {
      throw new Error("createAgentProfileFixture: call login() and ensureWorkspace() first");
    }
    const client = new pg.Client(DATABASE_URL);
    await client.connect();
    try {
      const identity = `fir-3660-${Date.now()}@example.com`;
      const account = await client.query(
        `INSERT INTO cerebro_account (
           workspace_id, provider, login_identity, usage_5h_pct,
           usage_5h_resets_at, usage_7d_pct, usage_7d_resets_at
         ) VALUES ($1, 'claude', $2, 27, now() + interval '2 hours 15 minutes', 59, now() + interval '3 days 4 hours')
         RETURNING id`,
        [this.workspaceId, identity],
      );
      this.createdAccountIds.push(account.rows[0].id);

      await client.query(
        `INSERT INTO cerebro_account_token_usage (account_id, workspace_id, tokens, created_at)
         VALUES ($1, $2, 12400, now()), ($1, $2, 1787600, now() - interval '2 days')`,
        [account.rows[0].id, this.workspaceId],
      );

      const runtime = await client.query(
        `INSERT INTO agent_runtime (
           workspace_id, daemon_id, name, runtime_mode, provider, status,
           device_info, metadata, last_seen_at, current_account_id
         ) VALUES ($1, NULL, 'FIR-3660 Runtime', 'local', 'claude', 'online',
                   'fir-3660.local', '{}'::jsonb, now(), $2)
         RETURNING id`,
        [this.workspaceId, account.rows[0].id],
      );
      this.createdRuntimeIds.push(runtime.rows[0].id);

      const agent = await client.query(
        `INSERT INTO agent (
           workspace_id, name, description, runtime_mode, runtime_config,
           runtime_id, visibility, max_concurrent_tasks, owner_id, model, thinking_level
         ) VALUES ($1, 'FIR-3660 Agent', 'Tooltip verification agent', 'local',
                   '{}'::jsonb, $2, 'workspace', 3, $3, 'claude-opus-5', 'high')
         RETURNING id`,
        [this.workspaceId, runtime.rows[0].id, this.userId],
      );
      this.createdAgentIds.push(agent.rows[0].id);

      return {
        agentId: agent.rows[0].id as string,
        agentName: "FIR-3660 Agent",
        runtimeId: runtime.rows[0].id as string,
        runtimeName: "FIR-3660 Runtime",
        identity,
      };
    } finally {
      await client.end();
    }
  }

  /**
   * FIR-1317 — seed a note two workspace members may both EDIT, which is what
   * live co-editing needs. CanUserEditNote grants write to the owner or to
   * anyone who reaches the note's folder through a Collections grant, so a
   * root note (no folder) is owner-only and cannot be co-edited. This creates
   * a folder carrying the "whole workspace" grant and puts the note inside it.
   */
  async createSharedNote(title: string, body = "") {
    if (!this.workspaceId) {
      throw new Error("createSharedNote: no workspace — call login() first");
    }
    const folderRes = await this.authedFetch("/api/artifact-folders", {
      method: "POST",
      body: JSON.stringify({ name: `${title} folder`, kind: "note" }),
    });
    if (!folderRes.ok) {
      throw new Error(`create folder failed: ${folderRes.status} ${await folderRes.text()}`);
    }
    const folder = await folderRes.json();
    this.createdFolderIds.push(folder.id);

    const client = new pg.Client(DATABASE_URL);
    await client.connect();
    try {
      await client.query(
        `INSERT INTO cerebro_folder_grant (surface, folder_id, grantee_type, grantee_id, role, created_by)
         VALUES ('artifact', $1, 'workspace', NULL, 'full_access', $2)
         ON CONFLICT DO NOTHING`,
        [folder.id, this.userId],
      );
    } finally {
      await client.end();
    }

    const noteRes = await this.authedFetch("/api/notes", {
      method: "POST",
      body: JSON.stringify({ title, body, folder_id: folder.id, visibility: "workspace" }),
    });
    if (!noteRes.ok) {
      throw new Error(`create note failed: ${noteRes.status} ${await noteRes.text()}`);
    }
    const note = await noteRes.json();
    this.createdArtifactIds.push(note.id);
    return { id: note.id as string, folderId: folder.id as string };
  }

  /** Clean up all issues + inbox items created during this test. */
  async cleanup() {
    if (
      this.createdArtifactIds.length ||
      this.createdAgentIds.length ||
      this.createdRuntimeIds.length ||
      this.createdAccountIds.length ||
      this.createdFolderIds.length
    ) {
      const client = new pg.Client(DATABASE_URL);
      await client.connect();
      try {
        for (const id of this.createdArtifactIds) {
          await client.query("DELETE FROM artifact WHERE id = $1", [id]);
        }
        for (const id of this.createdAgentIds) {
          await client.query("DELETE FROM agent WHERE id = $1", [id]);
        }
        for (const id of this.createdRuntimeIds) {
          await client.query("DELETE FROM agent_runtime WHERE id = $1", [id]);
        }
        for (const id of this.createdAccountIds) {
          await client.query("DELETE FROM cerebro_account WHERE id = $1", [id]);
        }
        for (const id of this.createdFolderIds) {
          await client.query(
            "DELETE FROM cerebro_folder_grant WHERE surface = 'artifact' AND folder_id = $1",
            [id],
          );
          await client.query("DELETE FROM artifact_folder WHERE id = $1", [id]);
        }
      } catch {
        /* ignore — best-effort cleanup */
      } finally {
        await client.end();
      }
      this.createdArtifactIds = [];
      this.createdAgentIds = [];
      this.createdRuntimeIds = [];
      this.createdAccountIds = [];
      this.createdFolderIds = [];
    }

    for (const id of this.createdIssueIds) {
      try {
        await this.deleteIssue(id);
      } catch {
        /* ignore — may already be deleted */
      }
    }
    this.createdIssueIds = [];

    for (const id of this.createdSprintIds) {
      try {
        await this.deleteSprint(id);
      } catch {
        /* ignore — may already be deleted with its project */
      }
    }
    this.createdSprintIds = [];

    for (const id of this.createdProjectIds) {
      try {
        await this.deleteProject(id);
      } catch {
        /* ignore — may already be deleted */
      }
    }
    this.createdProjectIds = [];

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

  getWorkspaceId() {
    return this.workspaceId;
  }

  getWorkspaceSlug() {
    return this.workspaceSlug;
  }

  async createCerebroGroup(
    name: string,
    description?: string,
  ): Promise<{ id: string; name: string }> {
    if (!this.workspaceId) {
      throw new Error("ensureWorkspace must run before createCerebroGroup");
    }
    const res = await this.authedFetch(
      `/api/workspaces/${this.workspaceId}/groups`,
      {
        method: "POST",
        body: JSON.stringify({ name, description: description ?? null }),
      },
    );
    if (!res.ok) {
      throw new Error(`createCerebroGroup failed: ${res.status} ${await res.text()}`);
    }
    return res.json();
  }

  async setAuthSettingsDefaultGroup(groupId: string | null) {
    if (!this.workspaceId) {
      throw new Error("ensureWorkspace must run before setAuthSettingsDefaultGroup");
    }
    const current = await this.authedFetch(
      `/api/cerebro/workspaces/${this.workspaceId}/auth-settings`,
    );
    const body = current.ok
      ? await current.json()
      : { google_signup_domains: [], default_role: "member", google_workspace_sync_enabled: false };
    const res = await this.authedFetch(
      `/api/cerebro/workspaces/${this.workspaceId}/auth-settings`,
      {
        method: "PUT",
        body: JSON.stringify({
          google_signup_domains: body.google_signup_domains ?? [],
          default_role: body.default_role ?? "member",
          default_group_id: groupId,
          google_workspace_sync_enabled: body.google_workspace_sync_enabled === true,
        }),
      },
    );
    if (!res.ok) {
      throw new Error(
        `setAuthSettingsDefaultGroup failed: ${res.status} ${await res.text()}`,
      );
    }
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
      await client.query("SELECT pg_advisory_lock(hashtext($1))", [`e2e-login:${email}`]);
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
      await client.query("SELECT pg_advisory_unlock(hashtext($1))", [`e2e-login:${email}`]).catch(() => {});
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
