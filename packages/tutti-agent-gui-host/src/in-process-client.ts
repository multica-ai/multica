import type {
  AgentGuiHostEvent,
  AgentGuiRpcClient,
  AgentGuiRpcError,
  AgentGuiRpcMethod,
  AgentGuiRpcMethods,
} from "./protocol";

export class AgentGuiHostError extends Error {
  code?: string;
  status?: number;

  constructor(error: AgentGuiRpcError) {
    super(error.message);
    this.name = "AgentGuiHostError";
    this.code = error.code;
    this.status = error.status;
  }
}

export interface AgentGuiHostService {
  request<M extends AgentGuiRpcMethod>(method: M, params: AgentGuiRpcMethods[M]["params"]): Promise<AgentGuiRpcMethods[M]["result"]>;
  subscribe(listener: (event: AgentGuiHostEvent) => void): () => void;
}

/** Methods a hidden or unfocused GUI may still call; every other one is a user action. */
function allowedInBackground(method: string, params: unknown): boolean {
  if (method === "workspace.load" || method === "session.load" || method === "session.watch" || method === "task.messages") return true;
  // A transfer outlives the focus that started it.
  if (method === "attachment.cancel" || method === "attachment.retry" || method === "attachment.forget" || method === "attachment.url" || method === "image.url") return true;
  if (method === "attachment.read" || method === "image.read" || method === "context.tasks" || method === "host.promptApplied" || method === "host.showMessage" || method.startsWith("storage.")) return true;
  // A prompt admitted to the queue must survive focus and tab changes.
  return ["session.send", "queue.enqueue", "host.openSide", "host.startSide"].includes(method) && (params as { queued?: unknown } | null)?.queued === true;
}

/**
 * The GUI's client of its host in the same document. Calls stay asynchronous
 * and copy their data, so the GUI's and the host's objects never alias and a
 * host event cannot re-enter a runtime operation in progress.
 */
export function createInProcessRpcClient(service: AgentGuiHostService, options: { interactive(): boolean; current(): boolean }): {
  client: AgentGuiRpcClient;
  dispose(): void;
} {
  let disposed = false;
  const pending = new Set<(reason: unknown) => void>();
  const listeners = new Set<(event: AgentGuiHostEvent) => void>();
  let latestInit: AgentGuiHostEvent | null = null;
  const unappliedPrompts = new Map<number, AgentGuiHostEvent>();
  const release = service.subscribe((event) => {
    if (disposed || !options.current()) return;
    const copy = structuredClone(event);
    if (copy.type === "init") latestInit = copy;
    if (copy.type === "appendPrompt") unappliedPrompts.set(copy.sequence, copy);
    queueMicrotask(() => { if (!disposed) for (const listener of [...listeners]) listener(copy); });
  });

  const client: AgentGuiRpcClient = {
    request<TMethod extends AgentGuiRpcMethod>(
      method: TMethod,
      params: AgentGuiRpcMethods[TMethod]["params"],
      requestOptions?: { signal?: AbortSignal },
    ): Promise<AgentGuiRpcMethods[TMethod]["result"]> {
      if (disposed || !options.current()) return Promise.reject(new DOMException("Host disconnected", "AbortError"));
      const signal = requestOptions?.signal;
      if (signal?.aborted) return Promise.reject(signal.reason ?? new DOMException("Aborted", "AbortError"));
      if (!options.interactive() && !allowedInBackground(method, params)) {
        return Promise.reject(new AgentGuiHostError({ message: "This Agent GUI tab is inactive", code: "inactive_host", status: 409 }));
      }
      return new Promise((resolve, reject) => {
        const settle = () => {
          clearTimeout(timer);
          pending.delete(reject);
          signal?.removeEventListener("abort", abort);
        };
        const abort = () => { settle(); reject(signal?.reason ?? new DOMException("Aborted", "AbortError")); };
        const timer = setTimeout(() => {
          settle();
          reject(new AgentGuiHostError({ code: "timeout", message: "The host request timed out" }));
        // Existing runtime discovery can spend 30s pending + 60s running;
        // its canonical poller allows 100s including network slack.
        }, method === "models.list" ? 110_000 : 60_000);
        pending.add(reject);
        signal?.addEventListener("abort", abort, { once: true });
        void Promise.resolve().then(() => service.request(method, structuredClone(params))).then(
          (result) => {
            if (!pending.has(reject)) return;
            settle();
            if (method === "host.promptApplied") unappliedPrompts.delete((params as AgentGuiRpcMethods["host.promptApplied"]["params"]).sequence);
            resolve(structuredClone(result));
          },
          (error: unknown) => {
            if (!pending.has(reject)) return;
            settle();
            const detail = error as { message?: string; code?: string; status?: number };
            reject(new AgentGuiHostError({ message: detail?.message || "GUI request failed", code: detail?.code, status: detail?.status }));
          },
        );
      });
    },
    subscribe(listener) {
      listeners.add(listener);
      queueMicrotask(() => {
        if (!listeners.has(listener)) return;
        if (latestInit) listener(latestInit);
        for (const prompt of unappliedPrompts.values()) listener(prompt);
      });
      return () => listeners.delete(listener);
    },
  };

  return {
    client,
    dispose() {
      if (disposed) return;
      disposed = true;
      release();
      for (const reject of pending) reject(new DOMException("Host disconnected", "AbortError"));
      pending.clear();
      listeners.clear();
      unappliedPrompts.clear();
      latestInit = null;
    },
  };
}
