// OpenClaw-specific `runtime_config` schema (issue #3260).
//
// Stored under `agent.runtime_config` as freeform JSONB; only meaningful for
// agents whose runtime provider is openclaw. The daemon decodes the same
// schema in `server/internal/daemon/openclaw_runtime_config.go` — keep both
// sides in lockstep when changing field names.

export type OpenclawRoutingMode = "local" | "gateway";

export interface OpenclawGatewayPin {
  host?: string;
  port?: number;
  token?: string;
  tls?: boolean;
}

export interface OpenclawRuntimeConfig {
  mode?: OpenclawRoutingMode;
  gateway?: OpenclawGatewayPin;
}

// Sentinel the API substitutes for a non-empty `gateway.token` on every read.
// When the form re-submits the same sentinel we strip the field client-side
// so the backend's matching preserve hook restores the persisted token
// instead of overwriting it. Mirrors `runtimeConfigGatewayTokenMask` in
// server/internal/handler/agent.go.
export const OPENCLAW_GATEWAY_TOKEN_MASK = "***";

// Parse an arbitrary runtime_config payload into the typed schema. Unknown
// keys are dropped, malformed payloads collapse to an empty object. The form
// never throws on bad input — invalid configs simply render as defaults so
// the user can correct them without a JSON parse error blocking the UI.
export function parseOpenclawRuntimeConfig(
  raw: unknown,
): OpenclawRuntimeConfig {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return {};
  const root = raw as Record<string, unknown>;
  const out: OpenclawRuntimeConfig = {};
  if (root.mode === "local" || root.mode === "gateway") {
    out.mode = root.mode;
  }
  if (root.gateway && typeof root.gateway === "object" && !Array.isArray(root.gateway)) {
    const gw = root.gateway as Record<string, unknown>;
    const pin: OpenclawGatewayPin = {};
    if (typeof gw.host === "string" && gw.host !== "") pin.host = gw.host;
    if (typeof gw.port === "number" && Number.isFinite(gw.port) && gw.port > 0) pin.port = gw.port;
    if (typeof gw.token === "string" && gw.token !== "") pin.token = gw.token;
    if (typeof gw.tls === "boolean") pin.tls = gw.tls;
    if (Object.keys(pin).length > 0) out.gateway = pin;
  }
  return out;
}

// Render the typed form state back into the wire shape the API accepts.
// Empty gateway sub-objects collapse to `undefined` so the wire payload
// only carries fields the user actually populated — partial pins (host+port
// only, etc.) work as documented.
export function serializeOpenclawRuntimeConfig(
  cfg: OpenclawRuntimeConfig,
): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  if (cfg.mode) out.mode = cfg.mode;
  if (cfg.gateway) {
    const gw: Record<string, unknown> = {};
    if (cfg.gateway.host) gw.host = cfg.gateway.host;
    if (cfg.gateway.port) gw.port = cfg.gateway.port;
    if (cfg.gateway.tls) gw.tls = true;
    // The mask sentinel must NEVER round-trip back to the server; the
    // matching preserve hook on the backend treats its presence as "keep
    // the persisted token", but emitting it here would still hit the wire
    // and trip any future schema validation. Drop it client-side so the
    // payload genuinely omits the field.
    if (cfg.gateway.token && cfg.gateway.token !== OPENCLAW_GATEWAY_TOKEN_MASK) {
      gw.token = cfg.gateway.token;
    }
    if (Object.keys(gw).length > 0) out.gateway = gw;
  }
  return out;
}

// Stable shallow equality across two parsed configs, used by the form's
// dirty detector. Treats absent gateway block and an empty gateway block as
// identical so toggling between local/gateway without filling endpoint
// fields doesn't surface a spurious "unsaved changes" notice.
export function openclawRuntimeConfigEquals(
  a: OpenclawRuntimeConfig,
  b: OpenclawRuntimeConfig,
): boolean {
  if ((a.mode ?? "local") !== (b.mode ?? "local")) return false;
  const aGw = a.gateway ?? {};
  const bGw = b.gateway ?? {};
  if ((aGw.host ?? "") !== (bGw.host ?? "")) return false;
  if ((aGw.port ?? 0) !== (bGw.port ?? 0)) return false;
  if ((aGw.token ?? "") !== (bGw.token ?? "")) return false;
  if (Boolean(aGw.tls) !== Boolean(bGw.tls)) return false;
  return true;
}
