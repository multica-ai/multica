import type { WSMessage, WSEventType } from "../types/events";
import { type Logger, noopLogger } from "../logger";

type EventHandler = (payload: unknown, actorId?: string) => void;

/** Identifies the WS client to the server. Sent as `client_platform`,
 *  `client_version`, and `client_os` query parameters on the upgrade URL —
 *  browsers cannot set custom headers on WebSocket handshakes, so query
 *  params are the only portable channel. */
export interface WSClientIdentity {
  platform?: string;
  version?: string;
  os?: string;
}

export class WSClient {
  private ws: WebSocket | null = null;
  private sseAbort: AbortController | null = null;
  private baseUrl: string;
  private token: string | null = null;
  private workspaceSlug: string | null = null;
  private cookieAuth = false;
  private identity: WSClientIdentity | undefined;
  private handlers = new Map<WSEventType, Set<EventHandler>>();
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private hasConnectedBefore = false;
  private closed = false;
  private usingEventStream = false;
  private onReconnectCallbacks = new Set<() => void>();
  private anyHandlers = new Set<(msg: WSMessage) => void>();
  private logger: Logger;

  constructor(
    url: string,
    options?: {
      logger?: Logger;
      cookieAuth?: boolean;
      identity?: WSClientIdentity;
    },
  ) {
    this.baseUrl = url;
    this.logger = options?.logger ?? noopLogger;
    this.cookieAuth = options?.cookieAuth ?? false;
    this.identity = options?.identity;
  }

  setAuth(token: string | null, workspaceSlug: string) {
    this.token = token;
    this.workspaceSlug = workspaceSlug;
  }

  connect() {
    this.closed = false;
    this.connectWebSocket();
  }

  private websocketURL() {
    const url = new URL(this.baseUrl);
    // Token is never sent as a URL query parameter — it would be logged by
    // proxies, CDNs, and browser history.  In cookie mode the HttpOnly cookie
    // is sent automatically with the upgrade request.  In token mode the token
    // is delivered as the first WebSocket message after the connection opens.
    if (this.workspaceSlug)
      url.searchParams.set("workspace_slug", this.workspaceSlug);
    if (this.identity?.platform)
      url.searchParams.set("client_platform", this.identity.platform);
    if (this.identity?.version)
      url.searchParams.set("client_version", this.identity.version);
    if (this.identity?.os)
      url.searchParams.set("client_os", this.identity.os);
    return url;
  }

  private eventStreamURL() {
    const url = this.websocketURL();
    url.protocol = url.protocol === "wss:" ? "https:" : "http:";
    if (url.pathname.endsWith("/ws")) {
      url.pathname = url.pathname.slice(0, -"/ws".length) + "/sse";
    } else {
      url.pathname = url.pathname.replace(/\/$/, "") + "/sse";
    }
    return url;
  }

  private connectWebSocket() {
    if (this.closed) return;
    this.usingEventStream = false;
    const url = this.websocketURL();

    try {
      this.ws = new WebSocket(url.toString());
    } catch (error) {
      this.logger.warn("websocket unavailable, falling back to SSE", error);
      this.connectEventStream();
      return;
    }

    this.ws.onopen = () => {
      if (!this.cookieAuth && this.token) {
        this.ws!.send(
          JSON.stringify({ type: "auth", payload: { token: this.token } }),
        );
        return;
      }

      this.onAuthenticated();
    };

    this.ws.onmessage = (event) => {
      const msg = JSON.parse(event.data as string) as WSMessage;
      this.handleMessage(msg);
    };

    this.ws.onclose = () => {
      this.ws = null;
      if (this.closed) return;
      if (!this.hasConnectedBefore) {
        this.logger.warn("websocket unavailable, falling back to SSE");
        this.connectEventStream();
        return;
      }
      this.logger.warn("disconnected, reconnecting in 3s");
      this.reconnectTimer = setTimeout(() => this.connectWebSocket(), 3000);
    };

    this.ws.onerror = () => {
      // Suppress — onclose handles reconnect/fallback; errors during StrictMode
      // double-fire are expected in dev and harmless.
    };
  }

  private connectEventStream() {
    if (this.closed) return;
    if (!this.cookieAuth && !this.token) return;

    const controller = new AbortController();
    this.sseAbort = controller;
    this.usingEventStream = true;

    const headers: Record<string, string> = { Accept: "text/event-stream" };
    if (!this.cookieAuth && this.token) {
      headers.Authorization = `Bearer ${this.token}`;
    }

    fetch(this.eventStreamURL().toString(), {
      method: "GET",
      headers,
      credentials: this.cookieAuth ? "include" : "same-origin",
      signal: controller.signal,
    })
      .then((response) => {
        if (!response.ok) {
          throw new Error(`SSE connection failed with ${response.status}`);
        }
        if (!response.body) {
          throw new Error("SSE response body is empty");
        }
        return this.readEventStream(response.body);
      })
      .catch((error) => {
        if (this.closed || controller.signal.aborted) return;
        this.logger.warn("SSE disconnected, reconnecting in 3s", error);
        this.reconnectTimer = setTimeout(() => this.connectEventStream(), 3000);
      });
  }

  private async readEventStream(body: ReadableStream<Uint8Array>) {
    const reader = body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";

    try {
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });

        let boundary = buffer.indexOf("\n\n");
        while (boundary !== -1) {
          const rawEvent = buffer.slice(0, boundary);
          buffer = buffer.slice(boundary + 2);
          this.handleEventStreamFrame(rawEvent);
          boundary = buffer.indexOf("\n\n");
        }
      }
    } finally {
      reader.releaseLock();
    }

    if (!this.closed) {
      throw new Error("SSE stream ended");
    }
  }

  private handleEventStreamFrame(rawEvent: string) {
    const data = rawEvent
      .split(/\r?\n/)
      .filter((line) => line.startsWith("data:"))
      .map((line) => line.slice("data:".length).trimStart())
      .join("\n");

    if (!data) return;
    const msg = JSON.parse(data) as WSMessage;
    this.handleMessage(msg);
  }

  private handleMessage(msg: WSMessage) {
    if ((msg as any).type === "auth_ack") {
      this.onAuthenticated();
      return;
    }
    this.logger.debug("received", msg.type);
    const eventHandlers = this.handlers.get(msg.type);
    if (eventHandlers) {
      for (const handler of eventHandlers) {
        handler(msg.payload, msg.actor_id);
      }
    }
    for (const handler of this.anyHandlers) {
      handler(msg);
    }
  }

  private onAuthenticated() {
    this.logger.info("connected");
    if (this.hasConnectedBefore) {
      for (const cb of this.onReconnectCallbacks) {
        try {
          cb();
        } catch {
          // ignore reconnect callback errors
        }
      }
    }
    this.hasConnectedBefore = true;
  }

  disconnect() {
    this.closed = true;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.sseAbort) {
      this.sseAbort.abort();
      this.sseAbort = null;
    }
    if (this.ws) {
      // Remove handlers before close to prevent onclose from scheduling a reconnect
      this.ws.onclose = null;
      this.ws.onerror = null;
      this.ws.close();
      this.ws = null;
    }
    this.hasConnectedBefore = false;
    this.usingEventStream = false;
    this.handlers.clear();
    this.anyHandlers.clear();
    this.onReconnectCallbacks.clear();
  }

  on(event: WSEventType, handler: EventHandler) {
    if (!this.handlers.has(event)) {
      this.handlers.set(event, new Set());
    }
    this.handlers.get(event)!.add(handler);
    return () => {
      this.handlers.get(event)?.delete(handler);
    };
  }

  onAny(handler: (msg: WSMessage) => void) {
    this.anyHandlers.add(handler);
    return () => {
      this.anyHandlers.delete(handler);
    };
  }

  onReconnect(callback: () => void) {
    this.onReconnectCallbacks.add(callback);
    return () => {
      this.onReconnectCallbacks.delete(callback);
    };
  }

  send(message: WSMessage) {
    const ws = this.ws;
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(message));
      return;
    }
    if (this.usingEventStream) {
      this.logger.warn(
        "cannot send realtime frame while using receive-only SSE fallback",
        message.type,
      );
    }
  }
}
