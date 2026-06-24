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
  private static readonly DEFAULT_NODE_GRID_COLUMNS = 4;
  private static readonly DEFAULT_NODE_GRID_X_START = 120;
  private static readonly DEFAULT_NODE_GRID_Y_START = 80;
  private static readonly DEFAULT_NODE_GRID_X_GAP = 260;
  private static readonly DEFAULT_NODE_GRID_Y_GAP = 180;

  private token: string | null = null;
  private workspaceSlug: string | null = null;
  private workspaceId: string | null = null;
  private createdIssueIds: string[] = [];
  private createdWorkflowIds: string[] = [];
  private createdWorkflowStageIds: string[] = [];
  private createdAgentIds: string[] = [];
  private workflowNodeCounts = new Map<string, number>();

  async login(email: string, name: string) {
    const devCode = process.env.MULTICA_DEV_VERIFICATION_CODE;

    // When MULTICA_DEV_VERIFICATION_CODE is set, the backend uses a fixed
    // verification code and does not write to the verification_code table.
    if (devCode) {
      // With a fixed dev code, we can skip send-code entirely and
      // verify directly — avoids rate limiting on /auth/send-code.
      const verifyRes = await fetch(`${API_BASE}/auth/verify-code`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, code: devCode }),
      });
      if (!verifyRes.ok) {
        throw new Error(`verify-code failed: ${verifyRes.status}`);
      }
      const data = await verifyRes.json();

      this.token = data.token;

      if (name && data.user?.name !== name) {
        await this.authedFetch("/api/me", {
          method: "PATCH",
          body: JSON.stringify({ name }),
        });
      }

      return data;
    }

    // Production path: use database-backed verification_code table
    const client = new pg.Client(DATABASE_URL);
    await client.connect();
    try {
      await client.query("DELETE FROM verification_code WHERE email = $1", [email]);

      const sendRes = await fetch(`${API_BASE}/auth/send-code`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email }),
      });
      if (!sendRes.ok) {
        throw new Error(`send-code failed: ${sendRes.status}`);
      }

      const result = await client.query(
        "SELECT code FROM verification_code WHERE email = $1 AND used = FALSE AND expires_at > now() ORDER BY created_at DESC LIMIT 1",
        [email],
      );
      if (result.rows.length === 0) {
        throw new Error(`No verification code found for ${email}`);
      }

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

  async createWorkflow(title: string) {
    const res = await this.authedFetch("/api/workflows", {
      method: "POST",
      body: JSON.stringify({ title }),
    });
    const workflow = await res.json();
    this.createdWorkflowIds.push(workflow.id);
    this.workflowNodeCounts.set(workflow.id, 0);
    return workflow;
  }

  async listWorkflows(workspaceId?: string) {
    const query = workspaceId ? `?workspace_id=${encodeURIComponent(workspaceId)}` : "";
    const res = await this.authedFetch(`/api/workflows${query}`);
    return res.json();
  }

  async getWorkflow(id: string) {
    const res = await this.authedFetch(`/api/workflows/${id}`);
    return res.json();
  }

  async listWorkflowNodes(workflowId: string) {
    const res = await this.authedFetch(`/api/workflows/${workflowId}/nodes`);
    return res.json();
  }

  async listWorkflowEdges(workflowId: string) {
    const res = await this.authedFetch(`/api/workflows/${workflowId}/edges`);
    return res.json();
  }

  async createWorkflowStage(workflowId: string, name: string, sortOrder: number) {
    const res = await this.authedFetch(`/api/workflows/${workflowId}/stages`, {
      method: "POST",
      body: JSON.stringify({ name, sort_order: sortOrder }),
    });
    const stage = await res.json();
    this.createdWorkflowStageIds.push(stage.id);
    return stage;
  }

  async createWorkflowNode(workflowId: string, data: {
    title: string;
    description?: string;
    position_x?: number;
    position_y?: number;
    worker_type?: string;
    worker_id?: string | null;
    critic_type?: string;
    critic_id?: string | null;
    stage_id?: string | null;
    format_schema?: unknown;
    critic_api_url?: string | null;
  }) {
    const defaultPosition = this.getDefaultNodePosition(workflowId);
    const body: Record<string, unknown> = {
      title: data.title,
      description: data.description ?? "",
      position_x: data.position_x ?? defaultPosition.x,
      position_y: data.position_y ?? defaultPosition.y,
      worker_type: data.worker_type ?? "agent",
      critic_type: data.critic_type ?? "human",
    };
    if (data.worker_id !== undefined) body.worker_id = data.worker_id;
    if (data.critic_id !== undefined) body.critic_id = data.critic_id;
    if (data.format_schema !== undefined) body.format_schema = data.format_schema;
    if (data.critic_api_url !== undefined) body.critic_api_url = data.critic_api_url;

    const res = await this.authedFetch(`/api/workflows/${workflowId}/nodes`, {
      method: "POST",
      body: JSON.stringify(body),
    });
    const node = await res.json();
    this.incrementWorkflowNodeCount(workflowId);

    // If stage_id is provided, assign the node to the stage
    if (data.stage_id !== undefined) {
      await this.assignNodeToStage(workflowId, node.id, data.stage_id);
    }

    return node;
  }

  async createWorkflowEdge(
    workflowId: string,
    sourceNodeId: string,
    targetNodeId: string
  ) {
    const res = await this.authedFetch(
      `/api/workflows/${workflowId}/edges`,
      {
        method: "POST",
        body: JSON.stringify({
          source_node_id: sourceNodeId,
          target_node_id: targetNodeId,
        }),
      }
    );
    if (!res.ok) {
      throw new Error(`create workflow edge failed: ${res.status} ${await res.text()}`);
    }
    return res.json();
  }

  async assignNodeToStage(workflowId: string, nodeId: string, stageId: string | null) {
    const res = await this.authedFetch(`/api/workflows/${workflowId}/nodes/${nodeId}/stage`, {
      method: "PUT",
      body: JSON.stringify({ stage_id: stageId }),
    });
    return res.json();
  }

  // ── Agent / Runtime / Plugin methods ──

  async listRuntimes(params?: { owner?: string }) {
    const query = new URLSearchParams();
    if (params?.owner) query.set("owner", params.owner);
    const qs = query.toString();
    const res = await this.authedFetch(`/api/runtimes${qs ? `?${qs}` : ""}`);
    return res.json();
  }

  async createAgent(data: {
    name: string;
    description?: string;
    instructions?: string;
    runtime_id: string;
    runtime_mode?: string;
    visibility?: string;
    model?: string;
    thinking_level?: string;
    max_concurrent_tasks?: number;
    plugin_id?: string;
  }) {
    const res = await this.authedFetch("/api/agents", {
      method: "POST",
      body: JSON.stringify(data),
    });
    const agent = await res.json();
    this.createdAgentIds.push(agent.id);
    return agent;
  }

  async deleteAgent(id: string) {
    await this.authedFetch(`/api/agents/${id}`, { method: "DELETE" });
  }

  async getAgent(id: string) {
    const res = await this.authedFetch(`/api/agents/${id}`);
    return res.json();
  }

  async listAgents(params?: { include_archived?: boolean }) {
    const query = new URLSearchParams();
    if (params?.include_archived) query.set("include_archived", "true");
    const qs = query.toString();
    const res = await this.authedFetch(`/api/agents${qs ? `?${qs}` : ""}`);
    return res.json();
  }

  async listBuiltinPlugins() {
    const res = await this.authedFetch("/api/plugins/builtin");
    return res.json();
  }

  // ── Workflow node update (for setting worker_id on existing nodes) ──

  async updateWorkflowNode(workflowId: string, nodeId: string, data: {
    title?: string;
    description?: string;
    worker_type?: string;
    worker_id?: string | null;
    critic_type?: string;
    critic_id?: string | null;
    stage_id?: string | null;
    format_schema?: unknown;
    position_x?: number;
    position_y?: number;
  }) {
    const res = await this.authedFetch(`/api/workflows/${workflowId}/nodes/${nodeId}`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
    return res.json();
  }

  async deleteIssue(id: string) {
    await this.authedFetch(`/api/issues/${id}`, { method: "DELETE" });
  }

  // ── Cleanup helpers ──

  async deleteWorkflow(id: string) {
    await this.authedFetch(`/api/workflows/${id}`, { method: "DELETE" });
  }

  /** Clean up all issues, workflows, agents created during this test.
   *  Workflow cascade deletion handles associated stages and nodes. */
  async cleanup() {
    for (const id of this.createdWorkflowIds) {
      try {
        await this.deleteWorkflow(id);
      } catch {
        /* ignore — may already be deleted */
      }
    }
    this.createdWorkflowIds = [];
    this.createdWorkflowStageIds = [];
    this.workflowNodeCounts.clear();

    for (const id of this.createdAgentIds) {
      try {
        await this.deleteAgent(id);
      } catch {
        /* ignore — may already be deleted */
      }
    }
    this.createdAgentIds = [];

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

  private getDefaultNodePosition(workflowId: string) {
    const index = this.workflowNodeCounts.get(workflowId) ?? 0;
    const column = index % TestApiClient.DEFAULT_NODE_GRID_COLUMNS;
    const row = Math.floor(index / TestApiClient.DEFAULT_NODE_GRID_COLUMNS);
    return {
      x: TestApiClient.DEFAULT_NODE_GRID_X_START + column * TestApiClient.DEFAULT_NODE_GRID_X_GAP,
      y: TestApiClient.DEFAULT_NODE_GRID_Y_START + row * TestApiClient.DEFAULT_NODE_GRID_Y_GAP,
    };
  }

  private incrementWorkflowNodeCount(workflowId: string) {
    const nextCount = (this.workflowNodeCounts.get(workflowId) ?? 0) + 1;
    this.workflowNodeCounts.set(workflowId, nextCount);
  }
}
