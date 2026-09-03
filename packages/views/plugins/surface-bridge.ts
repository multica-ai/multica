import { api } from "@multica/core/api";

/**
 * The host half of the bridge.
 *
 * A surface asks; the host performs the call on the signed-in user's own
 * session and returns the result. The plugin holds no credential, so this is
 * the only path from a surface into Multica.
 *
 * Identity is bound by three things together: the expected outer frame window,
 * a single-use launch challenge, and the guest-created MessagePort. Origin is
 * not useful for the inner plugin frame because its sandbox makes it opaque.
 */

const BRIDGE_PROTOCOL_VERSION = 2;
const BRIDGE_CONNECT_MESSAGE = "multica:plugin-bridge-connect";

type BridgeMethod = "GET" | "POST" | "PATCH" | "PUT" | "DELETE";

type BridgeRequest =
  | { id: string; kind: "action"; method: BridgeMethod; path: string; body?: unknown }
  | { id: string; kind: "ui.resize"; height: number };

/** Paths a surface may name. Anything else is refused before it reaches fetch. */
const ALLOWED_PATHS: RegExp[] = [
  /^\/context$/,
  /^\/issues\/[^/]+$/,
  /^\/issues\/[^/]+\/comments$/,
  /^\/storage\/(workspace|user)$/,
  /^\/storage\/(workspace|user)\/[^/]+$/,
  // A surface invoking its OWN plugin's hook: the `ui` trigger. The server
  // still checks the manifest declared it, and the installation is the one
  // this bridge was created for — a surface cannot reach another plugin's
  // hook by naming it here.
  /^\/hooks\/[^/]+$/,
];

const MAX_RESIZE_PX = 4000;

function isAllowedPath(path: string): boolean {
  return ALLOWED_PATHS.some((pattern) => pattern.test(path));
}

const ALLOWED_METHODS = new Set(["GET", "POST", "PATCH", "PUT", "DELETE"]);

function isBridgeRequest(value: unknown): value is BridgeRequest {
  if (typeof value !== "object" || value === null) return false;
  const candidate = value as Record<string, unknown>;
  if (typeof candidate.id !== "string") return false;
  if (candidate.kind === "ui.resize") return typeof candidate.height === "number";
  return (
    candidate.kind === "action" &&
    typeof candidate.path === "string" &&
    // An allowlist, not "is a string": an arbitrary verb would otherwise reach
    // fetch and rely on the server's 405 to stop it.
    typeof candidate.method === "string" && ALLOWED_METHODS.has(candidate.method)
  );
}

export interface SurfaceBridgeOptions {
  installationId: string;
  /** Single-use proof returned with this exact hosted document URL. */
  bridgeToken: string;
  /** Mounted-on issue, forwarded to /context so the surface knows where it is. */
  issueId?: string;
  onResize?: (height: number) => void;
}

export interface SurfaceBridge {
  /**
   * Arms the listener before the trusted outer document is assigned. The guest
   * creates the channel; the host accepts its port only once all launch proofs
   * match.
   */
  connect(frame: HTMLIFrameElement, theme: Record<string, string>): void;
  /** Pushes a theme change to an already-connected frame. */
  pushTheme(theme: Record<string, string>): void;
  close(): void;
}

export function createSurfaceBridge(options: SurfaceBridgeOptions): SurfaceBridge {
  let port: MessagePort | null = null;
  let closed = false;
  let connected = false;
  const connectListeners: Array<(event: MessageEvent) => void> = [];

  const handle = async (request: BridgeRequest, port: MessagePort) => {
    if (request.kind === "ui.resize") {
      // Clamped: a surface asking for a 10-million-pixel frame is a bug or an
      // attempt to push the rest of the page out of view.
      options.onResize?.(Math.min(Math.max(0, request.height), MAX_RESIZE_PX));
      return;
    }

    if (!isAllowedPath(request.path)) {
      port.postMessage({ id: request.id, ok: false, status: 400, error: `unsupported path ${request.path}` });
      return;
    }

    try {
      const result = await api.callPluginAction(options.installationId, {
        method: request.method,
        path: request.path,
        body: request.body,
        issueId: options.issueId,
      });
      port.postMessage({ id: request.id, ok: true, status: 200, data: result });
    } catch (error) {
      // The status matters to the surface: 403 means "the admin did not grant
      // this", which is a different thing for a plugin author to fix than 404.
      const status = typeof (error as { status?: number })?.status === "number" ? (error as { status: number }).status : 500;
      const message = error instanceof Error ? error.message : "plugin action failed";
      port.postMessage({ id: request.id, ok: false, status, error: message });
    }
  };

  return {
    connect(frame, theme) {
      if (closed) return;
      const onConnect = (event: MessageEvent) => {
        const data = event.data as { type?: string; version?: number; challenge?: string } | null;
        if (data?.type !== BRIDGE_CONNECT_MESSAGE) return;
        // The outer frame is trusted host code. It forwards an inner port only
        // after its own source, protocol and challenge checks have passed.
        if (!frame.contentWindow || event.source !== frame.contentWindow) return;
        if (data.version !== BRIDGE_PROTOCOL_VERSION || data.challenge !== options.bridgeToken) return;
        const guestPort = event.ports?.[0];
        if (!guestPort || closed || connected) return;

        connected = true;
        window.removeEventListener("message", onConnect);
        port = guestPort;
        port.onmessage = (portEvent) => {
          if (!isBridgeRequest(portEvent.data)) return;
          void handle(portEvent.data, guestPort);
        };
        port.start();
        port.postMessage({ kind: "theme", theme });
      };
      connectListeners.push(onConnect);
      window.addEventListener("message", onConnect);
    },
    pushTheme(theme) {
      port?.postMessage({ kind: "theme", theme });
    },
    close() {
      closed = true;
      for (const listener of connectListeners.splice(0)) window.removeEventListener("message", listener);
      port?.close();
      port = null;
    },
  };
}
