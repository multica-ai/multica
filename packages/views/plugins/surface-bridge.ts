import { api } from "@multica/core/api";

/**
 * The host half of the bridge.
 *
 * A surface asks; the host performs the call on the signed-in user's own
 * session and returns the result. The plugin holds no credential, so this is
 * the only path from a surface into Multica.
 *
 * Identity is bound by the MessagePort, not by `event.origin`. A sandboxed
 * frame without `allow-same-origin` has the opaque origin "null" — every
 * surface would present the same string, so comparing it would authenticate
 * nothing. Instead the host creates one channel per iframe, transfers one end
 * in, and only ever listens on the other; which plugin sent a message is
 * answered by which channel it arrived on.
 */

const BRIDGE_PROTOCOL_VERSION = 1;
const BRIDGE_INIT_MESSAGE = "multica:plugin-bridge-init";
const BRIDGE_READY_MESSAGE = "multica:plugin-surface-ready";

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
  /** Mounted-on issue, forwarded to /context so the surface knows where it is. */
  issueId?: string;
  onResize?: (height: number) => void;
}

export interface SurfaceBridge {
  /**
   * Waits for the surface to announce itself, then hands it a port.
   *
   * Driven by the guest rather than by the iframe's load event: a srcdoc frame
   * can finish loading before React attaches `onLoad`, so a load-driven host
   * either never fires or sends the port before the guest is listening. Both
   * failures look the same from outside — a blank panel.
   */
  connect(frame: HTMLIFrameElement, theme: Record<string, string>): void;
  /** Pushes a theme change to an already-connected frame. */
  pushTheme(theme: Record<string, string>): void;
  close(): void;
}

export function createSurfaceBridge(options: SurfaceBridgeOptions): SurfaceBridge {
  let channel: MessageChannel | null = null;
  let closed = false;
  const readyListeners: Array<(event: MessageEvent) => void> = [];

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
      const onReady = (event: MessageEvent) => {
        if ((event.data as { type?: string } | null)?.type !== BRIDGE_READY_MESSAGE) return;
        // The only check that matters: the signal came from the window this
        // bridge owns. Origin cannot be used — every sandboxed frame reports
        // "null" — so identity is the window reference plus, from here on, the
        // private port.
        if (!frame.contentWindow || event.source !== frame.contentWindow) return;
        window.removeEventListener("message", onReady);
        if (closed) return;

        channel?.port1.close();
        channel = new MessageChannel();
        channel.port1.onmessage = (portEvent) => {
          if (!isBridgeRequest(portEvent.data)) return;
          void handle(portEvent.data, channel!.port1);
        };
        channel.port1.start();
        // targetOrigin "*" is required, not lax: an opaque origin can never
        // match a string. The message carries no secret, and the port is
        // transferred into this specific contentWindow.
        frame.contentWindow.postMessage(
          { type: BRIDGE_INIT_MESSAGE, version: BRIDGE_PROTOCOL_VERSION, theme },
          "*",
          [channel.port2],
        );
      };
      readyListeners.push(onReady);
      window.addEventListener("message", onReady);
    },
    pushTheme(theme) {
      channel?.port1.postMessage({ kind: "theme", theme });
    },
    close() {
      closed = true;
      for (const listener of readyListeners.splice(0)) window.removeEventListener("message", listener);
      channel?.port1.close();
      channel = null;
    },
  };
}
