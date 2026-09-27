export interface MulticaClientOptions {
  apiUrl: string;
  token: string;
  workspaceId: string;
}

export interface ChatMessage {
  id: string;
  role: string;
  content: string;
  task_id: string | null;
  failure_reason: string | null;
}

export class MulticaApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "MulticaApiError";
    this.status = status;
  }
}

const REPLY_POLL_INTERVAL_MS = 1500;

/** The slice of the Multica REST API the Weixin host talks to. */
export class MulticaClient {
  private readonly opts: MulticaClientOptions;

  constructor(opts: MulticaClientOptions) {
    this.opts = { ...opts, apiUrl: opts.apiUrl.replace(/\/+$/, "") };
  }

  async getAgent(agentId: string): Promise<{ id: string; name: string }> {
    return this.request("GET", `/api/agents/${agentId}`);
  }

  async createChatSession(agentId: string, title: string): Promise<{ id: string }> {
    return this.request("POST", "/api/chat/sessions", { agent_id: agentId, title });
  }

  async sendChatMessage(sessionId: string, content: string): Promise<{ task_id: string }> {
    return this.request("POST", `/api/chat/sessions/${sessionId}/messages`, { content });
  }

  async listChatMessages(sessionId: string): Promise<ChatMessage[]> {
    return this.request("GET", `/api/chat/sessions/${sessionId}/messages`);
  }

  /**
   * Resolves with the assistant message produced by `taskId`. A queued task
   * keeps the same id, so this also covers turns that wait behind another run.
   */
  async waitForReply(
    sessionId: string,
    taskId: string,
    { timeoutMs, signal }: { timeoutMs: number; signal?: AbortSignal },
  ): Promise<ChatMessage> {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      signal?.throwIfAborted();
      const messages = await this.listChatMessages(sessionId);
      const reply = messages.find((m) => m.role === "assistant" && m.task_id === taskId);
      if (reply) return reply;
      await sleep(REPLY_POLL_INTERVAL_MS, signal);
    }
    throw new Error(`timed out after ${Math.round(timeoutMs / 1000)}s waiting for the agent`);
  }

  private async request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const res = await fetch(`${this.opts.apiUrl}${path}`, {
      method,
      headers: {
        Authorization: `Bearer ${this.opts.token}`,
        "X-Workspace-ID": this.opts.workspaceId,
        ...(body === undefined ? {} : { "Content-Type": "application/json" }),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const text = await res.text();
    if (!res.ok) {
      let message = text;
      try {
        message = (JSON.parse(text) as { error?: string }).error ?? text;
      } catch {
        // Non-JSON error body: keep the raw text.
      }
      throw new MulticaApiError(res.status, `${method} ${path} → ${res.status}: ${message}`);
    }
    return (text ? JSON.parse(text) : undefined) as T;
  }
}

function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(resolve, ms);
    signal?.addEventListener(
      "abort",
      () => {
        clearTimeout(timer);
        reject(signal.reason);
      },
      { once: true },
    );
  });
}
