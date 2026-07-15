export interface MulticaAgentClientOptions {
  baseUrl: string;
  token: string;
  workspaceId: string;
  agentId: string;
  embedded?: {
    userAssertion: string;
  };
  fetcher?: typeof fetch;
}

export interface ChatSession {
  id: string;
  agent_id?: string;
  title?: string;
}

export interface SentMessage {
  message_id: string;
  task_id: string;
  created_at?: string;
}

export type UIMessageStreamChunk = { type: string; [key: string]: unknown };

export class MulticaAPIError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
    this.name = "MulticaAPIError";
  }
}

export class MulticaAgentClient {
  private readonly baseUrl: string;
  private readonly fetcher: typeof fetch;

  constructor(private readonly options: MulticaAgentClientOptions) {
    this.baseUrl = options.baseUrl.replace(/\/$/, "");
    this.fetcher = options.fetcher ?? fetch;
    if (options.embedded && !options.embedded.userAssertion.trim()) {
      throw new Error("Embedded Chat user assertion is required");
    }
  }

  private sessionPath(sessionId?: string): string {
    const root = this.options.embedded
      ? "/api/cerebro/embedded-chat/sessions"
      : "/api/chat/sessions";
    return sessionId ? `${root}/${encodeURIComponent(sessionId)}` : root;
  }

  async createSession(title: string): Promise<ChatSession> {
    return this.request<ChatSession>(this.sessionPath(), {
      method: "POST",
      body: JSON.stringify({ agent_id: this.options.agentId, title }),
    });
  }

  async sendMessage(sessionId: string, content: string): Promise<SentMessage> {
    return this.request<SentMessage>(
      `${this.sessionPath(sessionId)}/messages`,
      { method: "POST", body: JSON.stringify({ content }) },
    );
  }

  async streamRun(sessionId: string, taskId: string): Promise<Response> {
    const query = new URLSearchParams({ task_id: taskId });
    return this.stream(`${this.sessionPath(sessionId)}/stream?${query}`);
  }

  async resumeRun(sessionId: string): Promise<Response | null> {
    const response = await this.stream(`${this.sessionPath(sessionId)}/stream`);
    return response.status === 204 ? null : response;
  }

  private headers(): HeadersInit {
    const headers: Record<string, string> = {
      Authorization: `Bearer ${this.options.token}`,
      "X-Workspace-Id": this.options.workspaceId,
      "Content-Type": "application/json",
    };
    if (this.options.embedded) {
      headers["X-Multica-User-Assertion"] = this.options.embedded.userAssertion;
    }
    return headers;
  }

  private async stream(path: string): Promise<Response> {
    const response = await this.fetcher(`${this.baseUrl}${path}`, {
      method: "GET",
      headers: this.headers(),
      cache: "no-store",
    });
    if (!response.ok && response.status !== 204) await throwAPIError(response);
    return response;
  }

  private async request<T>(path: string, init: RequestInit): Promise<T> {
    const response = await this.fetcher(`${this.baseUrl}${path}`, {
      ...init,
      headers: { ...this.headers(), ...init.headers },
      cache: "no-store",
    });
    if (!response.ok) await throwAPIError(response);
    return (await response.json()) as T;
  }
}

async function throwAPIError(response: Response): Promise<never> {
  const body = await response.text();
  let message = body || response.statusText || `Multica request failed (${response.status})`;
  try {
    const parsed = JSON.parse(body) as { error?: string; message?: string };
    message = parsed.error ?? parsed.message ?? message;
  } catch {
    // Plain-text errors are valid API responses.
  }
  throw new MulticaAPIError(response.status, message);
}

export async function* parseUIMessageStream(response: Response): AsyncGenerator<UIMessageStreamChunk> {
  if (!response.body) throw new Error("Multica stream has no response body");
  const reader = response.body.pipeThrough(new TextDecoderStream()).getReader();
  let buffer = "";
  while (true) {
    const { done, value } = await reader.read();
    buffer += value ?? "";
    const lines = buffer.split("\n");
    buffer = lines.pop() ?? "";
    for (const line of lines) {
      if (!line.startsWith("data: ")) continue;
      const payload = line.slice(6);
      if (payload === "[DONE]") return;
      yield JSON.parse(payload) as UIMessageStreamChunk;
    }
    if (done) break;
  }
}
