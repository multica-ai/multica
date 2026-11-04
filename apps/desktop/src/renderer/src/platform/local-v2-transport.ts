import { LOCAL_V2_VERSION, localV2HelloSchema, localV2UploadSchema, type LocalV2ChooseRequest, type LocalV2ClientMessage, type LocalV2RendererEndpoint, type LocalV2Scope, type LocalV2Transport } from "@multica/core/local-conversation/v2";
import { LOCAL_TIMING_REQUEST_HEADER, consoleLocalTiming, localTimingCloseReason, localTimingRequestId, parseLocalTiming, type LocalTimingRecord, type LocalTimingSink } from "@multica/tutti-agent-gui-host/local-timing";
import type { LocalHostAvailability, LocalHostUnavailableReason } from "../../../shared/local-host-availability";
import type { LocalV2Bridge } from "../../../shared/local-v2-bridge";

/** Error kinds that say why local conversations cannot be used at all. */
const BLOCKING = new Set(["daemon_offline", "not_signed_in", "account_mismatch", "device_unreachable", "device_link_off", "device_unknown", "api_url_mismatch"]);
const RECONNECT_MIN_MS = 500;
const RECONNECT_MAX_MS = 10_000;
/**
 * Chromium allows six connections per host, so another device's slow requests
 * would queue this device's behind them if both used the daemon's one address.
 */
function remoteDeviceBase(base: string) { return base.replace(/^[^:]+/, "localhost"); }

const REQUEST_TIMEOUT_MS = 120_000;
const PROBE_INTERVAL_MS = 15_000;
const HANDSHAKE_TIMEOUT_MS = 20_000;
/** An upload may take as long as it needs while its bytes keep moving. */
const UPLOAD_STALL_MS = 60_000;
const TRACKED_TURNS = 64;

/** A failed request, carried as "kind: message" like every local v2 error. */
const failure = (kind: string, message: string) => new Error(`${kind}: ${message}`);

interface Socket {
  scope: LocalV2Scope;
  ws?: WebSocket;
  connecting?: Promise<WebSocket>;
  backoff: number;
  watch?: AbortController;
  retryNow?: boolean;
  timer?: ReturnType<typeof setTimeout>;
  /** A socket has closed, so the next subscribe is a resubscribe. */
  reconnected?: boolean;
  /** When each conversation last asked to subscribe. */
  subscribing?: Map<string, number>;
  retryDelay?: number;
  /** What each turn's events cost so far, summed for one record at its end. */
  traffic?: Map<string, { batches: number; bytes: number; last: number; maxGap: number }>;
}

export interface DirectLocalV2Options {
  bridge: LocalV2Bridge;
  /** "local", or the peer ID of another device reached over Device link. */
  deviceId?: string;
  /** Shows a folder picker for another device; that device has no OS picker here. */
  chooseRemote?: (scope: LocalV2Scope, request: LocalV2ChooseRequest) => Promise<unknown>;
  WebSocket?: typeof WebSocket;
  fetch?: typeof fetch;
  XMLHttpRequest?: typeof XMLHttpRequest;
  /** Where this link's connection marks go; only another device's link has any. */
  timing?: LocalTimingSink;
}

const SNAPSHOT_PATH = /^\/sessions\/([^/]+)\/snapshot$/;

/**
 * The renderer's link to the daemon's `/local/v2` API: JSON and file
 * requests with the renderer token, one WebSocket per scope opened with a
 * single-use ticket, and file URLs carrying the file key. The main process
 * only supplies those credentials, the OS picker and the native part of
 * host requests.
 */
export class DirectLocalV2Transport implements LocalV2Transport {
  private endpointValue?: LocalV2RendererEndpoint;
  private endpointLoad?: Promise<LocalV2RendererEndpoint>;
  private sockets = new Map<string, Socket>();
  private reads = new Map<string, Set<AbortController>>();
  private messageListeners = new Set<(scope: LocalV2Scope, message: unknown) => void>();
  private connectionListeners = new Set<(scope: LocalV2Scope, connected: boolean) => void>();
  private availabilityListeners = new Set<(value: LocalHostAvailability) => void>();
  private availabilityValue: LocalHostAvailability | null = null;
  private probeTimer?: ReturnType<typeof setInterval>;
  private readonly WS: typeof WebSocket;
  private readonly fetch: typeof fetch;
  private readonly XHR: typeof XMLHttpRequest;
  readonly deviceId: string;
  /** Another device's API sits under this daemon's /local/devices/{peer}. */
  private readonly basePath: string;
  /** This client, so the daemon sends a conversation's host requests back here, also after a reconnect. */
  private readonly clientId = crypto.randomUUID();
  /** Read once per connection; a reconnect may have reached an updated daemon. */
  private capabilityLoad?: Promise<string[]>;

  constructor(private options: DirectLocalV2Options) {
    this.WS = options.WebSocket ?? WebSocket;
    this.fetch = options.fetch ?? fetch.bind(globalThis);
    this.XHR = options.XMLHttpRequest ?? globalThis.XMLHttpRequest;
    this.deviceId = options.deviceId ?? "local";
    this.basePath = this.deviceId === "local" ? "" : `/local/devices/${encodeURIComponent(this.deviceId)}`;
  }

  private endpoint(refresh = false): Promise<LocalV2RendererEndpoint> {
    if (!refresh && this.endpointValue) return Promise.resolve(this.endpointValue);
    if (refresh || !this.endpointLoad) {
      this.endpointLoad = this.options.bridge.endpoint().then((value) => { this.endpointValue = value; return value; })
        .finally(() => { this.endpointLoad = undefined; });
    }
    return this.endpointLoad;
  }

  private mark(stage: LocalTimingRecord["stage"], extra: Partial<LocalTimingRecord> = {}) {
    if (this.deviceId === "local") return;
    const record = parseLocalTiming({ version: 2, source: "renderer", clientKind: "desktop", origin: "p2p", at: new Date().toISOString(), stage, event: extra.event ?? "mark", elapsedMs: 0, peer: this.deviceId, ...extra });
    if (record) (this.options.timing ?? consoleLocalTiming)(record);
  }

  private url(endpoint: LocalV2RendererEndpoint, path: string) { return `http://${this.deviceId === "local" ? endpoint.base : remoteDeviceBase(endpoint.base)}${this.basePath}/local/v2${path}`; }

  private headers(endpoint: LocalV2RendererEndpoint, scope: LocalV2Scope): Record<string, string> {
    return { Authorization: `Bearer ${endpoint.token}`, "X-Multica-API-Origin": endpoint.apiUrl, "X-Multica-User-ID": scope.userId, "X-Workspace-ID": scope.workspaceId, "X-Multica-Client-ID": this.clientId };
  }

  // --- availability ---

  availability(): LocalHostAvailability | null { return this.availabilityValue; }

  onAvailability(listener: (value: LocalHostAvailability) => void): () => void {
    this.availabilityListeners.add(listener);
    this.startProbing();
    return () => { this.availabilityListeners.delete(listener); };
  }

  private report(value: LocalHostAvailability) {
    if (JSON.stringify(value) === JSON.stringify(this.availabilityValue)) return;
    this.availabilityValue = value;
    for (const listener of this.availabilityListeners) listener(value);
  }

  /** Probes until the service works; requests and connections report changes after that. */
  private startProbing() {
    if (this.probeTimer) return;
    void this.probe();
    this.probeTimer = setInterval(() => { if (this.availabilityValue?.state !== "connected") void this.probe(); }, PROBE_INTERVAL_MS);
  }

  /** Checks that the daemon serves this API version. */
  async probe(): Promise<void> {
    let response: Response;
    try {
      const endpoint = await this.endpoint(true);
      response = await this.fetch(this.url(endpoint, "/hello"), { headers: { Authorization: `Bearer ${endpoint.token}` }, signal: AbortSignal.timeout(this.deviceId === "local" ? 5000 : HANDSHAKE_TIMEOUT_MS) });
    } catch { this.report({ state: "unavailable", reason: "daemon_offline" }); return; }
    if (response.status === 404) { this.report({ state: "unavailable", reason: "unsupported" }); return; }
    if (!response.ok) {
      const kind = (await response.json().catch(() => null) as { error?: { kind?: unknown } } | null)?.error?.kind;
      this.report({ state: "unavailable", reason: typeof kind === "string" && BLOCKING.has(kind) ? kind as LocalHostUnavailableReason : "daemon_offline" });
      return;
    }
    const hello = await response.json().catch(() => null) as { version?: unknown } | null;
    const capabilities = Array.isArray((hello as { capabilities?: unknown } | null)?.capabilities) ? (hello as { capabilities: unknown[] }).capabilities : [];
    const compatible = hello?.version === LOCAL_V2_VERSION && (this.deviceId === "local" || capabilities.includes("device-link"));
    this.report(compatible ? { state: "connected" } : { state: "unavailable", reason: "incompatible" });
  }

  /** Read fresh: the point is noticing the daemon's database was replaced. */
  async storeGeneration(): Promise<string> {
    const endpoint = await this.endpoint();
    const response = await this.fetch(this.url(endpoint, "/hello"), { headers: { Authorization: `Bearer ${endpoint.token}` }, signal: AbortSignal.timeout(10_000) });
    if (!response.ok) throw failure(`http_${response.status}`, "The daemon did not answer");
    const hello = localV2HelloSchema.safeParse(await response.json());
    return hello.success ? hello.data.store_generation : "";
  }

  capabilities(): Promise<string[]> {
    return this.capabilityLoad ??= (async () => {
      const endpoint = await this.endpoint();
      const asked = Date.now();
      const response = await this.fetch(this.url(endpoint, "/hello"), { headers: { Authorization: `Bearer ${endpoint.token}` }, signal: AbortSignal.timeout(HANDSHAKE_TIMEOUT_MS) });
      this.mark("ws.hello", { event: "end", durationMs: Date.now() - asked, outcome: response.ok ? "completed" : "failed" });
      if (!response.ok) throw failure(`http_${response.status}`, "The daemon did not answer");
      const hello = localV2HelloSchema.safeParse(await response.json());
      return hello.success ? hello.data.capabilities : [];
    })().catch((error: unknown) => { this.capabilityLoad = undefined; throw error; });
  }

  private outcome(kind: string | null) {
    if (kind === null) this.report({ state: "connected" });
    else if (kind === "draining") this.report({ state: "updating" });
    else if (BLOCKING.has(kind)) this.report({ state: "unavailable", reason: kind as LocalHostUnavailableReason });
  }

  // --- requests ---

  /** One authenticated request; credentials refused once are read again. */
  private async fetchDaemon(scope: LocalV2Scope, method: string, path: string, init: { body?: BodyInit; headers?: Record<string, string> } = {}, signal?: AbortSignal): Promise<Response> {
    if (!path.startsWith("/") || path.includes("..")) throw failure("invalid_request", "Invalid local conversation path");
    for (let attempt = 0; attempt < 2; attempt++) {
      let endpoint: LocalV2RendererEndpoint;
      let response: Response;
      try {
        endpoint = await this.endpoint(attempt > 0);
        response = await this.fetch(this.url(endpoint, path), { method, headers: { ...this.headers(endpoint, scope), ...init.headers }, body: init.body, signal: signal ? AbortSignal.any([signal, AbortSignal.timeout(path === "/ws-ticket" ? 5_000 : REQUEST_TIMEOUT_MS)]) : AbortSignal.timeout(path === "/ws-ticket" ? 5_000 : REQUEST_TIMEOUT_MS) });
      } catch (error) {
        if (signal?.aborted) throw error;
        this.outcome("daemon_offline");
        throw failure("daemon_offline", "Start the local daemon to use local conversations");
      }
      // The daemon restarted with a new token, or moved to another port.
      if (response.status === 403 && attempt === 0 && !(response.headers.get("Content-Type") ?? "").includes("json")) continue;
      return response;
    }
    throw failure("forbidden", "The local daemon refused the connection");
  }

  private async json(scope: LocalV2Scope, method: string, path: string, init: { body?: BodyInit; headers?: Record<string, string> } = {}): Promise<unknown> {
    const key = this.key(scope);
    for (let recovery = 0; recovery < 2; recovery++) {
      const controller = this.deviceId !== "local" && method === "GET" ? new AbortController() : undefined;
      if (controller) {
        let reads = this.reads.get(key);
        if (!reads) { reads = new Set(); this.reads.set(key, reads); }
        reads.add(controller);
      }
      try {
        const snapshot = method === "GET" ? SNAPSHOT_PATH.exec(path)?.[1] : undefined;
        const conversationId = snapshot ? decodeURIComponent(snapshot) : undefined;
        const began = Date.now();
        if (conversationId) this.mark("snapshot.request", { conversationId });
        const response = await this.fetchDaemon(scope, method, path, init, controller?.signal);
        if (conversationId) this.mark("snapshot.first_byte", { conversationId, durationMs: Date.now() - began });
        // Keep the abort controller until the body is consumed: headers can
        // arrive before the network disappears halfway through a snapshot.
        const body = await response.text();
        if (conversationId) this.mark("snapshot.received", { conversationId, durationMs: Date.now() - began, bytes: body.length });
        let value: unknown = null;
        try { value = JSON.parse(body); } catch { /* Invalid bodies retain the HTTP error handling below. */ }
        if (conversationId) this.mark("snapshot.parsed", { conversationId, durationMs: Date.now() - began });
        return this.jsonOutcome(response.ok, response.status, value);
      } catch (error) {
        if (controller?.signal.reason === "connection_replaced" && recovery === 0) continue;
        if (controller?.signal.reason === "scope_released") throw failure("scope_released", "The conversation connection was released");
        throw error;
      } finally {
        if (controller) {
          const reads = this.reads.get(key);
          reads?.delete(controller);
          if (!reads?.size) this.reads.delete(key);
        }
      }
    }
    throw failure("device_unreachable", "The device connection changed while reading");
  }

  private jsonOutcome(ok: boolean, status: number, value: unknown): unknown {
    if (!ok) {
      const error = (value as { error?: { kind?: unknown; message?: unknown } } | null)?.error;
      const kind = typeof error?.kind === "string" ? error.kind : `http_${status}`;
      this.outcome(kind);
      throw failure(kind, typeof error?.message === "string" ? error.message : `Local conversation request failed (HTTP ${status})`);
    }
    this.outcome(null);
    return value;
  }

  request(scope: LocalV2Scope, method: "GET" | "POST" | "DELETE", path: string, body?: unknown): Promise<unknown> {
    const requestId = this.deviceId === "local" ? undefined : localTimingRequestId(body);
    return this.json(scope, method, path, body === undefined ? {} : { body: JSON.stringify(body), headers: { "Content-Type": "application/json", ...(requestId ? { [LOCAL_TIMING_REQUEST_HEADER]: requestId } : {}) } });
  }

  /**
   * XMLHttpRequest because fetch cannot report upload progress, and a file on a
   * slow link is only a failure once it stops moving. Losing one upload says
   * nothing about the device, so it never changes availability by itself.
   */
  async upload(scope: LocalV2Scope, file: File, options: { id?: string; signal?: AbortSignal } = {}) {
    for (let attempt = 0; attempt < 2; attempt++) {
      const endpoint = await this.endpoint(attempt > 0);
      const headers = { ...this.headers(endpoint, scope), "Content-Type": file.type || "application/octet-stream", "X-File-Name": encodeURIComponent(file.name), ...(options.id ? { "X-Attachment-ID": options.id } : {}) };
      const result = await this.sendFile(this.url(endpoint, "/attachments"), headers, file, options.signal);
      // The daemon restarted with a new token, or moved to another port.
      if (result.status === 403 && attempt === 0 && !result.contentType.includes("json")) continue;
      let value: unknown = null;
      try { value = JSON.parse(result.body); } catch { /* Invalid bodies retain the HTTP error handling below. */ }
      return localV2UploadSchema.parse(this.jsonOutcome(result.status >= 200 && result.status < 300, result.status, value));
    }
    throw failure("forbidden", "The local daemon refused the connection");
  }

  private sendFile(url: string, headers: Record<string, string>, file: File, signal?: AbortSignal) {
    return new Promise<{ status: number; contentType: string; body: string }>((resolve, reject) => {
      const xhr = new this.XHR();
      let watchdog: ReturnType<typeof setTimeout> | undefined;
      const watch = () => {
        clearTimeout(watchdog);
        watchdog = setTimeout(() => {
          settle(() => reject(failure("upload_stalled", "The file stopped uploading; check the connection and try again")));
          xhr.abort();
        }, UPLOAD_STALL_MS);
      };
      let settled = false;
      const settle = (finish: () => void) => {
        if (settled) return;
        settled = true;
        clearTimeout(watchdog);
        signal?.removeEventListener("abort", aborted);
        finish();
      };
      const aborted = () => {
        settle(() => reject(signal?.reason ?? new DOMException("Aborted", "AbortError")));
        xhr.abort();
      };
      if (signal?.aborted) { aborted(); return; }
      signal?.addEventListener("abort", aborted, { once: true });
      xhr.open("POST", url);
      for (const [name, value] of Object.entries(headers)) xhr.setRequestHeader(name, value);
      xhr.upload.addEventListener("progress", watch);
      xhr.addEventListener("load", () => settle(() => resolve({ status: xhr.status, contentType: xhr.getResponseHeader("Content-Type") ?? "", body: xhr.responseText })));
      xhr.addEventListener("error", () => settle(() => reject(failure("upload_failed", "The file could not be uploaded; check the connection and try again"))));
      xhr.addEventListener("abort", () => settle(() => reject(new DOMException("Aborted", "AbortError"))));
      watch();
      xhr.send(file);
    });
  }

  /**
   * A file URL for <img>, <video> and previews. Files are shown only after
   * the conversation that names them was read, so the endpoint is known by
   * then; a URL from before a daemon restart stops working and the view
   * reloads with the restart.
   */
  fileUrl(scope: LocalV2Scope, path: string): string {
    const endpoint = this.endpointValue;
    if (!endpoint) return "";
    const params = new URLSearchParams({ file_key: endpoint.fileKey, api_origin: endpoint.apiUrl, user_id: scope.userId, workspace_id: scope.workspaceId });
    return `${this.url(endpoint, path)}${path.includes("?") ? "&" : "?"}${params}`;
  }

  choose(scope: LocalV2Scope, request: LocalV2ChooseRequest) {
    if (this.deviceId === "local") return this.options.bridge.choose(scope, request);
    if (!this.options.chooseRemote) return Promise.reject(failure("unsupported", "Choose a folder on this device"));
    return this.options.chooseRemote(scope, request);
  }

  // --- this device's Device link (local transport only) ---

  private async own(method: string, path: string, body?: unknown): Promise<unknown> {
    const endpoint = await this.endpoint();
    const response = await this.fetch(`http://${endpoint.base}${path}`, { method, headers: { Authorization: `Bearer ${endpoint.token}`, ...(body === undefined ? {} : { "Content-Type": "application/json" }) }, body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(10_000) });
    const value: unknown = await response.json().catch(() => null);
    if (!response.ok) throw failure(`http_${response.status}`, "The local daemon refused the request");
    return value;
  }

  /** The account's other devices that are connected and share their conversations. */
  devices(): Promise<unknown> { return this.own("GET", "/local/devices"); }

  /** Whether this device shares its conversations, and whether it can. */
  deviceLink(): Promise<unknown> { return this.own("GET", "/local/device-link"); }

  setDeviceLink(enabled: boolean): Promise<unknown> { return this.own("PUT", "/local/device-link", { enabled }); }

  /** Whether opening a conversation on this device may start its agent early. */
  conversationPreheat(): Promise<unknown> { return this.own("GET", "/local/conversation-preheat"); }
  setConversationPreheat(enabled: boolean): Promise<unknown> { return this.own("PUT", "/local/conversation-preheat", { old_conversations: enabled }); }

  servicePreview(): Promise<unknown> { return this.own("GET", "/local/service-preview"); }
  setServicePreview(value: { enabled: boolean; ports: number[] }): Promise<unknown> { return this.own("PUT", "/local/service-preview", value); }
  clearAgentServicePreviews(): Promise<unknown> { return this.own("DELETE", "/local/service-preview"); }

  // --- WebSocket ---

  subscribe(listener: (scope: LocalV2Scope, message: unknown) => void): () => void {
    this.messageListeners.add(listener);
    return () => { this.messageListeners.delete(listener); };
  }

  onConnection(listener: (scope: LocalV2Scope, connected: boolean) => void): () => void {
    this.connectionListeners.add(listener);
    return () => { this.connectionListeners.delete(listener); };
  }

  async send(scope: LocalV2Scope, message: LocalV2ClientMessage): Promise<void> {
    const ws = await this.connect(scope);
    ws.send(JSON.stringify(message));
    if (message.type === "subscribe") {
      this.sockets.get(this.key(scope))?.subscribing?.set(message.conversation_id, Date.now());
      this.mark("ws.subscribe", { conversationId: message.conversation_id });
    }
  }

  private key(scope: LocalV2Scope) { return JSON.stringify([scope.userId, scope.workspaceId]); }

  private connect(scope: LocalV2Scope): Promise<WebSocket> {
    const key = this.key(scope);
    let socket = this.sockets.get(key);
    if (!socket) {
      socket = { scope, backoff: RECONNECT_MIN_MS, subscribing: new Map(), traffic: new Map() }; this.sockets.set(key, socket);
      if (this.deviceId !== "local") this.watchLink(socket);
    }
    if (socket.ws?.readyState === this.WS.OPEN) return Promise.resolve(socket.ws);
    const current = socket;
    return current.connecting ??= (async () => {
      try {
        const asked = Date.now();
        const { ticket } = await this.json(scope, "POST", "/ws-ticket") as { ticket: string };
        this.mark("ws.ticket", { event: "end", durationMs: Date.now() - asked, outcome: "completed" });
        const endpoint = await this.endpoint();
        if (this.sockets.get(key) !== current) throw failure("scope_released", "The conversation connection was released");
        const ws = new this.WS(`ws://${endpoint.base}${this.basePath}/local/v2/ws?${new URLSearchParams({ ticket })}`);
        current.ws = ws;
        const opening = Date.now();
        await new Promise<void>((resolve, reject) => {
          let settled = false;
          const finish = (error?: Error) => {
            if (settled) return;
            settled = true; clearTimeout(timer);
            ws.removeEventListener("open", opened); ws.removeEventListener("close", closed);
            if (error) {
              reject(error);
              // Keep the error listener for the close of a still-connecting socket.
              if (ws.readyState === this.WS.CONNECTING) ws.close();
            } else { ws.removeEventListener("error", failed); resolve(); }
          };
          const opened = () => finish();
          const closed = () => finish(failure("device_unreachable", "The connection closed before the handshake completed"));
          const failed = () => finish(failure(this.deviceId === "local" ? "daemon_offline" : "device_unreachable", "Could not connect to the conversation service"));
          const timer = setTimeout(() => {
            this.mark("ws.closed", { reason: "ws_handshake_timeout" });
            finish(failure("device_unreachable", "The conversation connection handshake timed out"));
          }, HANDSHAKE_TIMEOUT_MS);
          ws.addEventListener("open", opened, { once: true });
          ws.addEventListener("close", closed, { once: true });
          ws.addEventListener("error", failed, { once: true });
        });
        if (this.sockets.get(key) !== current || ws.readyState !== this.WS.OPEN) {
          ws.close(); throw failure("scope_released", "The conversation connection was released");
        }
        current.retryDelay = undefined;
        this.mark("ws.open", { event: "end", durationMs: Date.now() - opening, outcome: "completed" });
        this.attach(current, ws);
        return ws;
      } catch (error) {
        current.ws = undefined;
        if (this.sockets.get(key) === current) {
          if (this.availabilityValue?.state !== "unavailable") this.outcome(this.deviceId === "local" ? "daemon_offline" : "device_unreachable");
          for (const listener of this.connectionListeners) listener(scope, false);
          this.reconnect(current);
        }
        throw error;
      } finally {
        current.connecting = undefined;
        if (current.retryNow) { current.retryNow = false; this.resume(current); }
      }
    })();
  }

  private attach(socket: Socket, ws: WebSocket) {
    if (this.deviceId !== "local" && !socket.watch) this.watchLink(socket);
    this.abortReads(socket.scope, "connection_replaced");
    this.capabilityLoad = undefined;
    socket.ws = ws; socket.backoff = RECONNECT_MIN_MS;
    ws.addEventListener("message", (event) => {
      if (typeof event.data !== "string") return;
      let message: unknown;
      try { message = JSON.parse(event.data); } catch { return; }
      const value = message as { type?: unknown; request_id?: unknown; kind?: unknown; payload?: unknown; status?: unknown; conversation_id?: unknown };
      if (value?.type === "subscription.status") this.markSubscription(socket, value.status, value.conversation_id);
      if (value?.type === "conversation.events") this.markEvents(socket, message, event.data.length);
      if (value?.type === "host.request" && typeof value.request_id === "string" && typeof value.kind === "string") {
        void this.answer(socket.scope, ws, value.request_id, value.kind, (value.payload ?? {}) as Record<string, unknown>);
        return;
      }
      for (const listener of this.messageListeners) listener(socket.scope, message);
    });
    ws.addEventListener("close", (event) => {
      if (this.sockets.get(this.key(socket.scope)) !== socket || socket.ws !== ws) return;
      socket.ws = undefined;
      socket.reconnected = true;
      this.mark("ws.closed", { reason: localTimingCloseReason((event as CloseEvent | undefined)?.code) });
      this.outcome(this.deviceId === "local" ? "daemon_offline" : "device_unreachable");
      for (const listener of this.connectionListeners) listener(socket.scope, false);
      this.reconnect(socket);
    }, { once: true });
    this.outcome(null);
    for (const listener of this.connectionListeners) listener(socket.scope, true);
  }

  private async answer(scope: LocalV2Scope, ws: WebSocket, requestId: string, kind: string, payload: Record<string, unknown>) {
    let reply: Record<string, unknown>;
    try {
      reply = { result: (await this.options.bridge.hostRequest(this.deviceId, scope, kind, payload)) ?? null };
    } catch (error) {
      const text = error instanceof Error ? error.message.replace(/^Error invoking remote method '[^']+': (?:Error: )?/, "") : String(error);
      const match = /^([a-z][a-z0-9_]*): ([\s\S]*)$/.exec(text);
      reply = { error: { kind: match?.[1] ?? "host_request_failed", message: match?.[2] ?? text } };
    }
    if (ws.readyState === this.WS.OPEN) ws.send(JSON.stringify({ version: LOCAL_V2_VERSION, type: "host.reply", request_id: requestId, ...reply }));
  }

  private resume(socket: Socket) {
    if (this.sockets.get(this.key(socket.scope)) !== socket || socket.ws?.readyState === this.WS.OPEN) return;
    socket.backoff = RECONNECT_MIN_MS;
    if (socket.timer) { clearTimeout(socket.timer); socket.timer = undefined; }
    if (socket.connecting) {
      socket.retryNow = true;
      if (socket.ws?.readyState === this.WS.CONNECTING) socket.ws.close();
      return;
    }
    void this.connect(socket.scope).catch(() => this.reconnect(socket));
  }

  private watchLink(socket: Socket) {
    const watch = new AbortController(); socket.watch = watch;
    void (async () => {
      let lastConnected: boolean | undefined;
      let delay = RECONNECT_MIN_MS;
      while (!watch.signal.aborted) {
        try {
          const endpoint = await this.endpoint();
          const response = await this.fetch(this.url(endpoint, "/link-events"), {
            headers: this.headers(endpoint, socket.scope), signal: AbortSignal.any([watch.signal, AbortSignal.timeout(25_000)]),
          });
          // Older daemons continue using the existing socket retry path.
          if (response.status === 404 || (response.ok && !response.headers.get("Content-Type")?.startsWith("text/event-stream"))) {
            await response.body?.cancel(); return;
          }
          if (!response.ok || !response.body) {
            await response.body?.cancel();
            if (response.status === 401 || response.status === 403) await this.endpoint(true);
            throw new Error("The local daemon cannot watch the device link yet");
          }
          delay = RECONNECT_MIN_MS;
          const reader = response.body.pipeThrough(new TextDecoderStream()).getReader();
          let pending = "";
          try {
            while (!watch.signal.aborted) {
              const { value, done } = await reader.read();
              if (done) break;
              pending += value;
              if (pending.length > 4096) throw new Error("Invalid device link event");
              let end: number;
              while ((end = pending.indexOf("\n")) >= 0) {
                const line = pending.slice(0, end).trim(); pending = pending.slice(end + 1);
                if (watch.signal.aborted) break;
                if (line === "event: link.connected") {
                  const recovered = lastConnected === false; lastConnected = true;
                  if (recovered || !socket.connecting) this.resume(socket);
                }
                if (line === "event: link.disconnected") { lastConnected = false; socket.ws?.close(); }
              }
            }
          } finally { await reader.cancel().catch(() => undefined); reader.releaseLock(); }
        } catch { /* The local daemon may be restarting; retry until the scope is released. */ }
        if (watch.signal.aborted) return;
        await new Promise<void>((resolve) => {
          const finish = () => { clearTimeout(timer); watch.signal.removeEventListener("abort", finish); resolve(); };
          const timer = setTimeout(finish, delay);
          watch.signal.addEventListener("abort", finish, { once: true });
        });
        delay = Math.min(delay * 2, 5_000);
      }
    })().finally(() => { if (socket.watch === watch) socket.watch = undefined; });
  }

  private reconnect(socket: Socket) {
    if (socket.timer || this.sockets.get(this.key(socket.scope)) !== socket) return;
    const delay = socket.backoff; socket.backoff = Math.min(socket.backoff * 2, RECONNECT_MAX_MS);
    // Only a new backoff is news; retrying at the cap would tick while offline.
    if (delay !== socket.retryDelay) { socket.retryDelay = delay; this.mark("ws.retry", { durationMs: delay }); }
    socket.timer = setTimeout(() => {
      socket.timer = undefined;
      void this.connect(socket.scope).catch(() => this.reconnect(socket));
    }, delay);
  }

  private markEvents(socket: Socket, message: unknown, size: number) {
    if (this.deviceId === "local" || !socket.traffic) return;
    const { conversation_id: conversationId, events } = message as { conversation_id?: unknown; events?: unknown };
    if (typeof conversationId !== "string" || !Array.isArray(events)) return;
    const now = Date.now();
    let bytes = size;
    const seen = new Set<string>();
    for (const event of events as { turn_id?: unknown; type?: unknown }[]) {
      if (typeof event?.turn_id !== "string") continue;
      const turn = socket.traffic.get(event.turn_id) ?? { batches: 0, bytes: 0, last: now, maxGap: 0 };
      if (!seen.has(event.turn_id)) {
        seen.add(event.turn_id);
        turn.maxGap = Math.max(turn.maxGap, now - turn.last);
        turn.last = now; turn.batches++; turn.bytes += bytes;
        // A batch nearly always holds one turn; otherwise its size stays with the first.
        bytes = 0;
      }
      socket.traffic.set(event.turn_id, turn);
      // A turn whose end this socket never sees would otherwise stay for its lifetime.
      if (socket.traffic.size > TRACKED_TURNS) socket.traffic.delete(socket.traffic.keys().next().value!);
      if (event.type === "turn.completed") {
        socket.traffic.delete(event.turn_id);
        this.mark("link.event_received", { conversationId, turnId: event.turn_id, count: turn.batches, bytes: turn.bytes, maxGapMs: turn.maxGap });
      }
    }
  }

  private markSubscription(socket: Socket, status: unknown, conversation: unknown) {
    if (typeof conversation !== "string" || this.deviceId === "local") return;
    if (status === "subscribed") {
      this.mark("ws.subscribed", { conversationId: conversation });
      if (socket.reconnected) { socket.reconnected = false; this.mark("ws.resubscribed", { conversationId: conversation }); }
    } else if (status === "live") {
      const asked = socket.subscribing?.get(conversation);
      this.mark("ws.catch_up", { conversationId: conversation, ...(asked === undefined ? {} : { durationMs: Date.now() - asked }) });
      socket.subscribing?.delete(conversation);
    }
  }

  private abortReads(scope: LocalV2Scope, reason: string) {
    for (const read of this.reads.get(this.key(scope)) ?? []) read.abort(reason);
  }

  /** Stops keeping a scope connected, e.g. after the account changed. */
  release(scope: LocalV2Scope): void {
    this.abortReads(scope, "scope_released");
    const socket = this.sockets.get(this.key(scope));
    if (!socket) return;
    this.sockets.delete(this.key(scope));
    if (socket.timer) clearTimeout(socket.timer);
    socket.watch?.abort();
    socket.ws?.close();
    for (const listener of this.connectionListeners) listener(scope, false);
  }
}

const transports = new Map<string, DirectLocalV2Transport>();
let remoteChooser: ((deviceId: string, scope: LocalV2Scope, request: LocalV2ChooseRequest) => Promise<unknown>) | undefined;

/** Sets how folders are chosen on other devices; the conversation page provides its picker. */
export function setRemoteFolderChooser(choose: typeof remoteChooser) { remoteChooser = choose; }

/**
 * This window's transport to the conversations of a device: this one by
 * default, or another device of the account through this daemon.
 */
export function localV2Transport(deviceId = "local"): DirectLocalV2Transport {
  let transport = transports.get(deviceId);
  if (!transport) {
    transport = new DirectLocalV2Transport({ bridge: window.desktopAPI.localV2, deviceId,
      chooseRemote: (scope, request) => remoteChooser ? remoteChooser(deviceId, scope, request) : Promise.reject(new Error("unsupported: Open the device's conversations to choose a folder")) });
    transports.set(deviceId, transport);
  }
  return transport;
}
