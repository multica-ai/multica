/**
 * The bridge wire format, shared in spirit by the surface (this package) and
 * the host (`packages/views/plugins`). It is duplicated rather than imported:
 * the SDK ships to plugin authors and must not pull `@multica/core` or
 * `@multica/ui` in behind it, and the host must not depend on a package a
 * third party could install a different version of.
 *
 * Two properties matter more than the shape:
 *
 *   - Identity is bound by a MessagePort, not by `event.origin`. A surface runs
 *     in a sandboxed iframe with no `allow-same-origin`, so its origin is the
 *     opaque string "null" — every surface would look identical. The generated
 *     guest bootstrap creates one channel, and the host accepts its peer only
 *     after source, protocol and single-use launch checks. From then on, "which
 *     plugin sent this" is answered by which private channel it arrived on.
 *   - No credential crosses this boundary. The surface asks; the host performs
 *     the call on the user's session and returns the result.
 */

export const BRIDGE_PROTOCOL_VERSION = 2;

/**
 * Installed by Multica's bootstrap before any plugin code executes. The guest
 * creates this port; the trusted wrapper transfers its peer to the host only
 * after the launch challenge and navigation policy have passed.
 */
export const BRIDGE_PORT_GLOBAL = "__multicaPluginBridgePortV2";

/** Design tokens the host pushes so a surface matches the product it sits in. */
export type ThemeTokens = Record<string, string>;

export type BridgeMethod = "GET" | "POST" | "PATCH" | "PUT" | "DELETE";

export type BridgeRequest =
  | {
      id: string;
      kind: "action";
      method: BridgeMethod;
      /** Path under the plugin Action API, e.g. "/issues/{id}/comments". */
      path: string;
      body?: unknown;
    }
  | {
      id: string;
      kind: "ui.resize";
      height: number;
    };

export type BridgeResponse =
  | { id: string; ok: true; status: number; data: unknown }
  | { id: string; ok: false; status: number; error: string };

/** Host-initiated, unsolicited: theme changes while the surface is mounted. */
export type BridgeEvent = { kind: "theme"; theme: ThemeTokens };

export type BridgeMessage = BridgeResponse | BridgeEvent;

export function isBridgeResponse(message: unknown): message is BridgeResponse {
  if (typeof message !== "object" || message === null) return false;
  const candidate = message as Record<string, unknown>;
  return typeof candidate.id === "string" && typeof candidate.ok === "boolean";
}

export function isBridgeEvent(message: unknown): message is BridgeEvent {
  if (typeof message !== "object" || message === null) return false;
  return (message as Record<string, unknown>).kind === "theme";
}

export function isBridgeRequest(message: unknown): message is BridgeRequest {
  if (typeof message !== "object" || message === null) return false;
  const candidate = message as Record<string, unknown>;
  if (typeof candidate.id !== "string") return false;
  if (candidate.kind === "ui.resize") return typeof candidate.height === "number";
  return candidate.kind === "action" && typeof candidate.path === "string" && typeof candidate.method === "string";
}
