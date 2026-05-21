/**
 * Multica API client for the MCP server.
 * Wraps REST calls to the Multica backend.
 */

export interface Issue {
  id: string;
  sequence_number: number;
  title: string;
  description: string;
  status: string;
  priority: string;
  assignee_type: string | null;
  assignee_id: string | null;
  assignee_name?: string;
  created_at: string;
  updated_at: string;
}

export interface Agent {
  id: string;
  name: string;
  status: string;
  runtime_mode: string;
  provider?: string;
}

export interface Member {
  id: string;
  user_id: string;
  user_name: string;
  user_email: string;
  role: string;
}

export interface Workspace {
  id: string;
  name: string;
  slug: string;
}

export interface Comment {
  id: string;
  content: string;
  author_type: string;
  author_id: string;
  created_at: string;
}

export class MulticaClient {
  private baseUrl: string;
  private token: string;
  private workspaceId: string | null = null;

  constructor(baseUrl: string, token: string) {
    this.baseUrl = baseUrl.replace(/\/+$/, "");
    this.token = token;
  }

  private headers(): Record<string, string> {
    const h: Record<string, string> = {
      Authorization: `Bearer ${this.token}`,
      "Content-Type": "application/json",
    };
    if (this.workspaceId) h["X-Workspace-ID"] = this.workspaceId;
    return h;
  }

  private async request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const res = await fetch(`${this.baseUrl}${path}`, {
      method,
      headers: this.headers(),
      body: body ? JSON.stringify(body) : undefined,
    });
    if (!res.ok) {
      const text = await res.text().catch(() => "");
      throw new Error(`${method} ${path} returned ${res.status}: ${text}`);
    }
    if (res.status === 204) return undefined as T;
    return res.json() as Promise<T>;
  }

  async listWorkspaces(): Promise<Workspace[]> {
    return this.request("GET", "/api/workspaces");
  }

  setWorkspaceId(id: string) {
    this.workspaceId = id;
  }

  async autoSelectWorkspace(): Promise<Workspace> {
    const workspaces = await this.listWorkspaces();
    if (workspaces.length === 0) throw new Error("No workspaces found");
    this.workspaceId = workspaces[0].id;
    return workspaces[0];
  }

  async listIssues(params?: { status?: string; priority?: string; assignee_id?: string; open_only?: boolean }): Promise<{ issues: Issue[] }> {
    const search = new URLSearchParams();
    if (params?.status) search.set("status", params.status);
    if (params?.priority) search.set("priority", params.priority);
    if (params?.assignee_id) search.set("assignee_id", params.assignee_id);
    if (params?.open_only) search.set("open_only", "true");
    return this.request("GET", `/api/issues?${search}`);
  }

  async getIssue(id: string): Promise<Issue> {
    return this.request("GET", `/api/issues/${id}`);
  }

  async createIssue(data: { title: string; description?: string; priority?: string; status?: string; assignee_type?: string; assignee_id?: string }): Promise<Issue> {
    return this.request("POST", "/api/issues", data);
  }

  async updateIssue(id: string, data: { title?: string; description?: string; status?: string; priority?: string; assignee_type?: string; assignee_id?: string }): Promise<Issue> {
    return this.request("PUT", `/api/issues/${id}`, data);
  }

  async createComment(issueId: string, content: string): Promise<Comment> {
    return this.request("POST", `/api/issues/${issueId}/comments`, { content });
  }

  async listComments(issueId: string): Promise<Comment[]> {
    return this.request("GET", `/api/issues/${issueId}/comments`);
  }

  async listAgents(): Promise<Agent[]> {
    return this.request("GET", "/api/agents");
  }

  async listMembers(workspaceId: string): Promise<Member[]> {
    return this.request("GET", `/api/workspaces/${workspaceId}/members`);
  }

  /**
   * Resolve an agent or member name to an assignee (type + id).
   * Tries agents first, then members.
   */
  async resolveAssignee(name: string): Promise<{ type: string; id: string; name: string } | null> {
    const lowerName = name.toLowerCase();

    const agents = await this.listAgents();
    const agent = agents.find((a) => a.name.toLowerCase() === lowerName || a.name.toLowerCase().includes(lowerName));
    if (agent) return { type: "agent", id: agent.id, name: agent.name };

    if (this.workspaceId) {
      const members = await this.listMembers(this.workspaceId);
      const member = members.find(
        (m) => m.user_name.toLowerCase() === lowerName || m.user_email.toLowerCase() === lowerName || m.user_name.toLowerCase().includes(lowerName)
      );
      if (member) return { type: "member", id: member.user_id, name: member.user_name };
    }

    return null;
  }
}
