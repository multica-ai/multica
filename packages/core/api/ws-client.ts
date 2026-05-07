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

// The browser does not surface WebSocket-level ping/pong frames to JS, so a
// silently dropped TCP connection (proxy timeout, NAT, iOS PWA suspended in
// background) leaves `readyState` stuck on OPEN with no events firing. The
// server emits a JSON `server:ping` every appPingPeriod (~25s); the client
// treats the absence of any inbound message for STALE_THRESHOLD_MS as a dead
// socket, force-closes it, and lets the existing reconnect path run — which
// fires the onReconnect callbacks that invalidate the Query cache.
const STALE_THRESHOLD_MS = 50_000;
const STALE_CHECK_INTERVAL_MS = 10_000;

export class WSClient {
  private ws: WebSocket | null = null;
  private baseUrl: string;
  private token: string | null = null;
  private workspaceSlug: string | null = null;
  private cookieAuth = false;
  private identity: WSClientIdentity | undefined;
  private handlers = new Map<WSEventType, Set<EventHandler>>();
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private hasConnectedBefore = false;
  private onReconnectCallbacks = new Set<() => void>();
  private anyHandlers = new Set<(msg: WSMessage) => void>();
  private logger: Logger;

  private lastMessageAt = 0;
  private staleCheckTimer: ReturnType<typeof setInterval> | null = null;
  private visibilityHandler: (() => void) | null = null;

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

    this.ws = new WebSocket(url.toString());
    this.lastMessageAt = Date.now();

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
      // Update before parsing/dispatch: every observed frame — including
      // server:ping — is evidence the connection is still alive.
      this.lastMessageAt = Date.now();

      const msg = JSON.parse(event.data as string) as WSMessage;
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
    };

    this.ws.onclose = () => {
      this.logger.warn("disconnected, reconnecting in 3s");
      this.reconnectTimer = setTimeout(() => this.connect(), 3000);
    };

    this.ws.onerror = () => {
      // Suppress — onclose handles reconnect; errors during StrictMode
      // double-fire are expected in dev and harmless.
    };

    this.startLivenessMonitor();
  }

  private onAuthenticated() {
    this.logger.info("connected");
    this.lastMessageAt = Date.now();
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

  /**
   * Start periodic staleness checks plus a visibilitychange listener that
   * checks immediately when a tab returns to the foreground. Idempotent —
   * called on every connect() but only installs handlers once.
   */
  private startLivenessMonitor() {
    if (!this.staleCheckTimer) {
      this.staleCheckTimer = setInterval(
        () => this.checkLiveness(),
        STALE_CHECK_INTERVAL_MS,
      );
    }
    if (
      typeof document !== "undefined" &&
      this.visibilityHandler === null
    ) {
      this.visibilityHandler = () => {
        // setInterval is paused while a page is hidden (and fully suspended
        // when iOS backgrounds a PWA), so the resume transition is the only
        // reliable signal we have to recheck liveness immediately.
        if (document.visibilityState === "visible") this.checkLiveness();
      };
      document.addEventListener("visibilitychange", this.visibilityHandler);
    }
  }

  private stopLivenessMonitor() {
    if (this.staleCheckTimer) {
      clearInterval(this.staleCheckTimer);
      this.staleCheckTimer = null;
    }
    if (this.visibilityHandler && typeof document !== "undefined") {
      document.removeEventListener("visibilitychange", this.visibilityHandler);
    }
    this.visibilityHandler = null;
  }

  private checkLiveness() {
    if (this.ws?.readyState !== WebSocket.OPEN) return;
    const elapsed = Date.now() - this.lastMessageAt;
    if (elapsed > STALE_THRESHOLD_MS) {
      this.logger.warn(
        `stale connection (${elapsed}ms since last message), forcing reconnect`,
      );
      this.forceReconnect();
    }
  }

  /**
   * Tear down the current socket and reconnect immediately, bypassing the
   * 3-second backoff used for unexpected drops. We strip handlers first so
   * the dying socket's onclose does not race the new connect attempt.
   */
  private forceReconnect() {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.ws) {
      this.ws.onclose = null;
      this.ws.onerror = null;
      try {
        this.ws.close();
      } catch {
        // ignore — already closed
      }
      this.ws = null;
    }
    this.connect();
  }

  disconnect() {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.stopLivenessMonitor();
    if (this.ws) {
      // Remove handlers before close to prevent onclose from scheduling a reconnect
      this.ws.onclose = null;
      this.ws.onerror = null;
      this.ws.close();
      this.ws = null;
    }
    this.hasConnectedBefore = false;
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
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(message));
    }
  }
}
