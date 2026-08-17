/**
 * Mobile WebSocket transport.
 *
 * Layer 1 of the realtime stack — owns a single socket, knows nothing about
 * React, AppState, NetInfo, or business events. Just: dial, redial with
 * backoff, dispatch frames to handlers.
 *
 * Differs from packages/core/api/ws-client.ts:
 *   - Exponential backoff with full jitter (1s base, 30s cap) — web's fixed
 *     3s reconnect would stampede the server when 1000+ phones wake up
 *     together after iOS suspends them.
 *   - Three-state lifecycle (idle / active / paused) so the provider can
 *     pause on background and resume on foreground without racing the
 *     auto-reconnect timer.
 *   - Detaches socket handlers BEFORE close() to avoid the spurious
 *     onclose-triggered reconnect that bites RN (close is async over the
 *     bridge — see facebook/react-native#9465).
 *   - Token-mode auth only (no cookies on native).
 *
 * Server compatibility: same protocol as web/desktop. Sends
 *   {type:"auth", payload:{token}} as first frame; expects {type:"auth_ack"}
 *   before any business events. workspace_slug + client_platform passed as
 *   query params on the upgrade URL (RN's WebSocket can't set headers).
 */
import type {
  WSEventPayload,
  WSEventType,
  WSMessage,
} from "@multica/core/types";

/** Generic handler used internally by the dispatcher map. Each `on<E>()`
 *  call narrows this to `(payload: WSEventPayload<E>, actorId?) => void`
 *  at the call site — callers get the precise payload type and never
 *  need a `as XxxPayload` cast. */
type EventHandler = (payload: unknown, actorId?: string) => void;
type AnyHandler = (msg: WSMessage) => void;

interface Logger {
  info: (...args: unknown[]) => void;
  warn: (...args: unknown[]) => void;
  debug: (...args: unknown[]) => void;
}

const noopLogger: Logger = {
  info: () => {},
  warn: () => {},
  debug: () => {},
};

export interface WSClientOptions {
  /** wss://host/ws — no query params; the client appends them. */
  url: string;
  /** Bearer token sent as the first frame. */
  token: string;
  /** Workspace slug — server resolves to UUID and gates membership. */
  workspaceSlug: string;
  /** Mobile app version, surfaced to server logs for debuggability. */
  clientVersion?: string;
  logger?: Logger;
}

const RECONNECT_BASE_MS = 1_000;
const RECONNECT_CAP_MS = 30_000;
const RECONNECT_MAX_EXPONENT = 6; // 1s → 64s ceiling, capped at 30s

// React Native does not surface WebSocket control Ping/Pong frames to
// JavaScript. The server sends control pings, but an iOS/NAT half-open socket
// can still look OPEN here forever. Its `/ws` handler also supports text
// {type:"ping"} → {type:"pong"}, which gives the mobile transport an
// observable liveness check without changing the protocol.
const HEARTBEAT_INTERVAL_MS = 25_000;
const HEARTBEAT_TIMEOUT_MS = 10_000;

/**
 * Lifecycle state — drives whether onclose schedules a reconnect:
 *   idle    — never connected, or fully disconnected. No reconnect on close.
 *   active  — wants to be connected. onclose → schedule reconnect.
 *   paused  — caller asked us to be off (e.g. app backgrounded). onclose
 *             does nothing; resume() reopens.
 */
type State = "idle" | "active" | "paused";

export class WSClient {
  private ws: WebSocket | null = null;
  private state: State = "idle";
  private reconnectAttempt = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private heartbeatTimer: ReturnType<typeof setInterval> | null = null;
  private pongTimer: ReturnType<typeof setTimeout> | null = null;
  private awaitingPong = false;
  private hasConnectedBefore = false;

  private readonly handlers = new Map<WSEventType, Set<EventHandler>>();
  private readonly anyHandlers = new Set<AnyHandler>();
  private readonly onReconnectCallbacks = new Set<() => void>();

  private readonly opts: WSClientOptions;
  private readonly logger: Logger;

  constructor(opts: WSClientOptions) {
    this.opts = opts;
    this.logger = opts.logger ?? noopLogger;
  }

  // ── public lifecycle ────────────────────────────────────────────────

  /** Idle → active. Initial connect. */
  connect() {
    if (this.state === "active") return;
    this.state = "active";
    this.openSocket();
  }

  /** Anything → idle. Full teardown — used on unmount / sign-out. */
  disconnect() {
    this.state = "idle";
    this.clearReconnect();
    this.teardownSocket();
    this.reconnectAttempt = 0;
    this.hasConnectedBefore = false;
  }

  /** Active → paused. Used by the provider when AppState=background.
   *  iOS will kill backgrounded sockets within seconds anyway; closing
   *  cleanly avoids the kernel-level reset surfacing as an error on
   *  resume. */
  pause() {
    if (this.state !== "active") return;
    this.state = "paused";
    this.clearReconnect();
    this.teardownSocket();
  }

  /** Paused → active. Used by the provider when AppState=active. */
  resume() {
    if (this.state !== "paused") return;
    this.state = "active";
    this.reconnectAttempt = 0;
    this.openSocket();
  }

  /** Force a fresh socket without going through paused. Used when NetInfo
   *  reports we just came back online — the existing socket is likely a
   *  zombie (TCP keepalive timeout takes minutes; we want seconds). */
  forceReconnect() {
    if (this.state !== "active") return;
    this.clearReconnect();
    this.teardownSocket();
    this.reconnectAttempt = 0;
    this.openSocket();
  }

  // ── public subscription ─────────────────────────────────────────────

  on<E extends WSEventType>(
    event: E,
    handler: (payload: WSEventPayload<E>, actorId?: string) => void,
  ) {
    let set = this.handlers.get(event);
    if (!set) {
      set = new Set();
      this.handlers.set(event, set);
    }
    // Store as the erased EventHandler in the map; the public signature
    // gives callers the typed payload at the call site.
    set.add(handler as EventHandler);
    return () => {
      this.handlers.get(event)?.delete(handler as EventHandler);
    };
  }

  onAny(handler: AnyHandler) {
    this.anyHandlers.add(handler);
    return () => {
      this.anyHandlers.delete(handler);
    };
  }

  /** Fires every time auth_ack arrives AFTER the first one — i.e. on each
   *  reconnect. Subscribers use this to invalidate query caches because
   *  any events between disconnect and reconnect were lost (no replay
   *  in v1). */
  onReconnect(cb: () => void) {
    this.onReconnectCallbacks.add(cb);
    return () => {
      this.onReconnectCallbacks.delete(cb);
    };
  }

  send(message: WSMessage) {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(message));
    }
  }

  // ── internal ────────────────────────────────────────────────────────

  private openSocket() {
    const url = new URL(this.opts.url);
    url.searchParams.set("workspace_slug", this.opts.workspaceSlug);
    url.searchParams.set("client_platform", "mobile");
    url.searchParams.set("client_os", "ios");
    if (this.opts.clientVersion) {
      url.searchParams.set("client_version", this.opts.clientVersion);
    }

    const ws = new WebSocket(url.toString());
    this.ws = ws;
    this.logger.info("[ws] dialing", url.toString().replace(/token=[^&]*/, "token=…"));

    ws.onopen = () => {
      this.logger.info("[ws] socket open, sending auth frame");
      ws.send(
        JSON.stringify({ type: "auth", payload: { token: this.opts.token } }),
      );
    };

    ws.onmessage = (event) => {
      let msg: WSMessage;
      try {
        msg = JSON.parse(event.data as string) as WSMessage;
      } catch {
        this.logger.warn("[ws] non-JSON frame ignored");
        return;
      }

      const type = (msg as { type?: string }).type;
      if (type === "auth_ack") {
        this.onAuthenticated();
        return;
      }
      if (type === "pong") {
        this.onPong();
        return;
      }
      if (!type) {
        // Server-side error frames have shape {error: "..."}; log and drop.
        // Reconnect loop is bounded by auth-store's 401 handler eventually
        // tearing this client down via disconnect().
        this.logger.warn("[ws] frame without type", event.data);
        return;
      }

      this.logger.debug("[ws] event", type);
      const set = this.handlers.get(msg.type);
      if (set) {
        for (const handler of set) handler(msg.payload, msg.actor_id);
      }
      for (const handler of this.anyHandlers) handler(msg);
    };

    ws.onerror = () => {
      // onerror is always paired with onclose — let onclose handle
      // reconnect. Logging here adds noise during normal teardown.
    };

    ws.onclose = () => {
      const wasOurs = this.ws === ws;
      // If we already swapped in a new socket, this onclose is from a
      // detached old one — ignore. (We try to detach handlers in
      // teardownSocket() but RN may dispatch onclose async over the bridge.)
      if (!wasOurs) return;

      this.ws = null;
      this.clearHeartbeat();
      this.logger.warn("[ws] socket closed");
      if (this.state === "active") this.scheduleReconnect();
    };
  }

  private onAuthenticated() {
    this.reconnectAttempt = 0;
    this.logger.info("[ws] authenticated");
    this.startHeartbeat();
    if (this.hasConnectedBefore) {
      for (const cb of this.onReconnectCallbacks) {
        try {
          cb();
        } catch (err) {
          this.logger.warn("[ws] onReconnect callback threw", err);
        }
      }
    }
    this.hasConnectedBefore = true;
  }

  private scheduleReconnect() {
    this.reconnectAttempt += 1;
    // Full jitter: random in [0, ceiling]. Spreads thundering herds when
    // many clients reconnect simultaneously (post-server-restart, post-
    // network-flap). AWS Builders' Library "Timeouts, retries, and backoff
    // with jitter" is the canonical reference.
    const exp = Math.min(this.reconnectAttempt, RECONNECT_MAX_EXPONENT);
    const ceiling = Math.min(RECONNECT_BASE_MS * 2 ** exp, RECONNECT_CAP_MS);
    const delay = Math.floor(Math.random() * ceiling);
    this.logger.info(
      `[ws] reconnecting in ${delay}ms (attempt ${this.reconnectAttempt})`,
    );
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      if (this.state === "active") this.openSocket();
    }, delay);
  }

  private clearReconnect() {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
  }

  /**
   * An application-level heartbeat is deliberately scoped to authenticated,
   * foreground sockets. A missed pong is treated like any other disconnect:
   * close the stale socket and feed recovery through the existing exponential
   * backoff + full-jitter scheduler rather than immediately redialing.
   */
  private startHeartbeat() {
    this.clearHeartbeat();
    this.sendHeartbeat();
    this.heartbeatTimer = setInterval(
      () => this.sendHeartbeat(),
      HEARTBEAT_INTERVAL_MS,
    );
  }

  private sendHeartbeat() {
    if (this.state !== "active" || this.ws?.readyState !== WebSocket.OPEN) {
      return;
    }

    this.awaitingPong = true;
    try {
      // `ping`/`pong` are transport frames rather than business events, so
      // they intentionally stay outside WSEventType / WSMessage.
      this.ws.send(JSON.stringify({ type: "ping" }));
    } catch {
      this.reconnectAfterHeartbeatFailure();
      return;
    }

    this.pongTimer = setTimeout(() => {
      if (this.awaitingPong) {
        this.logger.warn("[ws] heartbeat timed out");
        this.reconnectAfterHeartbeatFailure();
      }
    }, HEARTBEAT_TIMEOUT_MS);
  }

  private onPong() {
    if (!this.awaitingPong) return;
    this.awaitingPong = false;
    if (this.pongTimer) {
      clearTimeout(this.pongTimer);
      this.pongTimer = null;
    }
    this.logger.debug("[ws] heartbeat pong");
  }

  private reconnectAfterHeartbeatFailure() {
    if (this.state !== "active") return;
    this.teardownSocket();
    // Do not use forceReconnect(): health failures must retain the normal
    // exponential-backoff + full-jitter protection used by onclose.
    this.scheduleReconnect();
  }

  private clearHeartbeat() {
    if (this.heartbeatTimer) {
      clearInterval(this.heartbeatTimer);
      this.heartbeatTimer = null;
    }
    if (this.pongTimer) {
      clearTimeout(this.pongTimer);
      this.pongTimer = null;
    }
    this.awaitingPong = false;
  }

  private teardownSocket() {
    this.clearHeartbeat();
    if (!this.ws) return;
    const ws = this.ws;
    // Detach BEFORE close — onclose firing after teardown would re-enter
    // the reconnect path against a discarded socket.
    ws.onopen = null;
    ws.onmessage = null;
    ws.onerror = null;
    ws.onclose = null;
    try {
      ws.close();
    } catch {
      // close() can throw if the socket is already in CLOSING/CLOSED;
      // harmless.
    }
    this.ws = null;
  }
}
