import type {
  AgentGuiHostEvent,
  AgentGuiInitSnapshot,
  AgentGuiRpcClient,
  AgentGuiRpcError,
  AgentGuiRpcMethod,
  AgentGuiRpcMethods,
} from "../src/protocol";
import { agentGuiMessageTargetOrigin, isAgentGuiFrameMessage } from "./frame-messaging";

interface PendingRequest {
  resolve(value: unknown): void;
  reject(error: unknown): void;
  cleanup(): void;
}

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

/** One private port owns the lifetime of this exact iframe connection. */
export function createAgentGuiRpcClient(port: MessagePort): {
  client: AgentGuiRpcClient;
  dispose(): void;
} {
  let disposed = false;
  let sequence = 0;
  const pending = new Map<string, PendingRequest>();
  const listeners = new Set<(event: AgentGuiHostEvent) => void>();
  let latestInit: AgentGuiHostEvent | null = null;
  const unappliedPrompts = new Map<number, AgentGuiHostEvent>();
  const receive = (event: MessageEvent) => {
    const message = event.data;
    if (!message || typeof message !== "object") return;
    if (message.type === "event" && message.event) {
      if (message.event.type === "init") latestInit = message.event;
      if (message.event.type === "appendPrompt") unappliedPrompts.set(message.event.sequence, message.event);
      for (const listener of [...listeners]) listener(message.event);
      return;
    }
    if (message.type !== "response" || typeof message.id !== "string") return;
    const request = pending.get(message.id);
    if (!request) return;
    pending.delete(message.id);
    request.cleanup();
    if (message.error) request.reject(new AgentGuiHostError(message.error));
    else request.resolve(message.result);
  };
  port.addEventListener("message", receive);
  port.start();

  const client: AgentGuiRpcClient = {
    request<TMethod extends AgentGuiRpcMethod>(
      method: TMethod,
      params: AgentGuiRpcMethods[TMethod]["params"],
      options?: { signal?: AbortSignal },
    ): Promise<AgentGuiRpcMethods[TMethod]["result"]> {
      if (disposed) return Promise.reject(new DOMException("Host disconnected", "AbortError"));
      if (options?.signal?.aborted) return Promise.reject(options.signal.reason ?? new DOMException("Aborted", "AbortError"));
      const id = `${++sequence}`;
      return new Promise((resolve, reject) => {
        const abort = () => {
          pending.delete(id);
          cleanup();
          port.postMessage({ type: "cancel", id });
          reject(options?.signal?.reason ?? new DOMException("Aborted", "AbortError"));
        };
        const timer = setTimeout(() => {
          pending.delete(id);
          cleanup();
          port.postMessage({ type: "cancel", id });
          reject(new AgentGuiHostError({ code: "timeout", message: "The host request timed out" }));
        // Existing runtime discovery can spend 30s pending + 60s running;
        // its canonical poller allows 100s including network slack.
        }, method === "models.list" ? 110_000 : 60_000);
        const cleanup = () => {
          clearTimeout(timer);
          options?.signal?.removeEventListener("abort", abort);
        };
        pending.set(id, {
          resolve(value) {
            if (method === "host.promptApplied") {
              unappliedPrompts.delete((params as AgentGuiRpcMethods["host.promptApplied"]["params"]).sequence);
            }
            resolve(value as AgentGuiRpcMethods[TMethod]["result"]);
          },
          reject,
          cleanup,
        });
        options?.signal?.addEventListener("abort", abort, { once: true });
        try {
          port.postMessage({ type: "request", id, method, params });
        } catch (error) {
          pending.delete(id);
          cleanup();
          reject(error);
        }
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
      port.removeEventListener("message", receive);
      for (const request of pending.values()) {
        request.cleanup();
        request.reject(new DOMException("Host disconnected", "AbortError"));
      }
      pending.clear();
      listeners.clear();
      unappliedPrompts.clear();
      latestInit = null;
      port.close();
    },
  };
}

export function connectAgentGuiHost(): Promise<{
  client: AgentGuiRpcClient;
  snapshot: AgentGuiInitSnapshot;
  dispose(): void;
}> {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      window.removeEventListener("message", receive);
      reject(new AgentGuiHostError({ code: "connection_timeout", message: "The Agent GUI host did not connect" }));
    }, 20_000);
    const receive = (event: MessageEvent) => {
      if (!isAgentGuiFrameMessage(event, window.parent)) return;
      if (event.data?.type !== "multica:agent-gui:connect" || !event.ports[0]) return;
      clearTimeout(timer);
      window.removeEventListener("message", receive);
      const connection = createAgentGuiRpcClient(event.ports[0]);
      resolve({ ...connection, snapshot: event.data.snapshot });
    };
    window.addEventListener("message", receive);
    window.parent.postMessage({ type: "multica:agent-gui:ready", version: 1 }, agentGuiMessageTargetOrigin());
  });
}
