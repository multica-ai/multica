export interface RuntimeConfig {
  schemaVersion: 1;
  apiUrl: string;
  wsUrl: string;
  appUrl: string;
}

/** A saved Multica backend the user can switch to. */
export interface DesktopServerProfile {
  id: string;
  name: string;
  apiUrl: string;
  wsUrl: string;
  appUrl: string;
}

/**
 * Multi-server bookkeeping layered on top of the active RuntimeConfig.
 * `config` is always the currently active endpoints (what the app boots with).
 */
export interface DesktopServersState {
  activeServerId: string;
  servers: DesktopServerProfile[];
  /**
   * False in electron-vite dev where endpoints come from VITE_* env and
   * writing desktop.json would not change the running process.
   */
  editable: boolean;
}

export interface RuntimeConfigError {
  message: string;
}

export type RuntimeConfigResult =
  | { ok: true; config: RuntimeConfig; servers: DesktopServersState }
  | { ok: false; error: RuntimeConfigError };

export const CLOUD_SERVER_ID = "cloud";
export const CLOUD_SERVER_NAME = "Multica Cloud";

export const DEFAULT_RUNTIME_CONFIG: RuntimeConfig = Object.freeze({
  schemaVersion: 1,
  apiUrl: "https://api.multica.ai",
  wsUrl: "wss://api.multica.ai/ws",
  appUrl: "https://multica.ai",
});

const LOCAL_DEV_RUNTIME_CONFIG: RuntimeConfig = Object.freeze({
  schemaVersion: 1,
  apiUrl: "http://localhost:8080",
  wsUrl: "ws://localhost:8080/ws",
  appUrl: "http://localhost:3000",
});

export interface RuntimeConfigEnv {
  apiUrl?: string;
  wsUrl?: string;
  appUrl?: string;
}

export function cloudServerProfile(): DesktopServerProfile {
  return {
    id: CLOUD_SERVER_ID,
    name: CLOUD_SERVER_NAME,
    apiUrl: DEFAULT_RUNTIME_CONFIG.apiUrl,
    wsUrl: DEFAULT_RUNTIME_CONFIG.wsUrl,
    appUrl: DEFAULT_RUNTIME_CONFIG.appUrl,
  };
}

export function profileFromConfig(
  config: RuntimeConfig,
  options?: { id?: string; name?: string },
): DesktopServerProfile {
  const isCloud = urlsMatch(config.apiUrl, DEFAULT_RUNTIME_CONFIG.apiUrl);
  return {
    id: options?.id ?? (isCloud ? CLOUD_SERVER_ID : serverIdFromApiUrl(config.apiUrl)),
    name:
      options?.name ??
      (isCloud ? CLOUD_SERVER_NAME : defaultServerName(config.apiUrl)),
    apiUrl: config.apiUrl,
    wsUrl: config.wsUrl,
    appUrl: config.appUrl,
  };
}

export function serversStateFromConfig(
  config: RuntimeConfig,
  options?: { editable?: boolean; servers?: DesktopServerProfile[]; activeServerId?: string },
): DesktopServersState {
  const editable = options?.editable ?? true;
  let servers = options?.servers?.length
    ? options.servers.map(normalizeProfile)
    : [profileFromConfig(config)];

  // Ensure the active endpoints are represented in the list.
  const activeMatch = servers.find((s) => urlsMatch(s.apiUrl, config.apiUrl));
  if (!activeMatch) {
    servers = [profileFromConfig(config), ...servers];
  } else {
    // Keep active profile's derived ws/app in sync with top-level config.
    servers = servers.map((s) =>
      s.id === activeMatch.id
        ? { ...s, apiUrl: config.apiUrl, wsUrl: config.wsUrl, appUrl: config.appUrl }
        : s,
    );
  }

  // Prefer an explicit activeServerId when it still points at the active URL.
  let activeServerId = options?.activeServerId;
  const byId = activeServerId ? servers.find((s) => s.id === activeServerId) : undefined;
  if (!byId || !urlsMatch(byId.apiUrl, config.apiUrl)) {
    activeServerId = servers.find((s) => urlsMatch(s.apiUrl, config.apiUrl))!.id;
  }

  return {
    activeServerId: activeServerId!,
    servers: dedupeServersByApiUrl(servers),
    editable,
  };
}

export function runtimeConfigFromDevEnv(env: RuntimeConfigEnv): RuntimeConfig {
  const apiUrl = normalizeHttpUrl(
    env.apiUrl || LOCAL_DEV_RUNTIME_CONFIG.apiUrl,
    "VITE_API_URL",
  );
  return {
    schemaVersion: 1,
    apiUrl,
    wsUrl: env.wsUrl
      ? normalizeWsUrl(env.wsUrl, "VITE_WS_URL")
      : deriveWsUrl(apiUrl),
    appUrl: env.appUrl
      ? normalizeHttpUrl(env.appUrl, "VITE_APP_URL")
      : deriveDevAppUrl(apiUrl),
  };
}

export function parseRuntimeConfig(raw: string): RuntimeConfig {
  const { config } = parseDesktopConfigFile(raw);
  return config;
}

/**
 * Parse desktop.json supporting both legacy single-server files and the
 * multi-server extension (`servers` + `activeServerId`). Top-level
 * `apiUrl` remains the source of truth for the active backend so older
 * Desktop builds keep working.
 */
export function parseDesktopConfigFile(raw: string): {
  config: RuntimeConfig;
  servers: DesktopServersState;
} {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch (err) {
    throw new Error(
      `Invalid desktop runtime config JSON: ${err instanceof Error ? err.message : "parse failed"}`,
    );
  }

  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("Invalid desktop runtime config: expected a JSON object");
  }

  const obj = parsed as Record<string, unknown>;
  if (obj.schemaVersion !== 1) {
    throw new Error("Unsupported desktop runtime config schemaVersion: expected 1");
  }

  const serversRaw = obj.servers;
  let listed: DesktopServerProfile[] | undefined;
  if (serversRaw !== undefined) {
    if (!Array.isArray(serversRaw) || serversRaw.length === 0) {
      throw new Error("Invalid desktop runtime config: servers must be a non-empty array when set");
    }
    listed = serversRaw.map((entry, index) => parseServerProfile(entry, index));
  }

  // Active endpoints: prefer matching activeServerId from the list, else
  // top-level apiUrl (legacy), else first server.
  const activeServerIdRaw = optionalString(obj.activeServerId, "activeServerId");
  let activeFromList: DesktopServerProfile | undefined;
  if (listed && activeServerIdRaw) {
    activeFromList = listed.find((s) => s.id === activeServerIdRaw);
    if (!activeFromList) {
      throw new Error(
        `Invalid desktop runtime config: activeServerId "${activeServerIdRaw}" not found in servers`,
      );
    }
  }

  let apiUrl: string;
  let appUrl: string | undefined;
  let wsUrl: string | undefined;

  if (activeFromList && obj.apiUrl === undefined) {
    apiUrl = activeFromList.apiUrl;
    appUrl = activeFromList.appUrl;
    wsUrl = activeFromList.wsUrl;
  } else if (obj.apiUrl !== undefined) {
    apiUrl = requiredString(obj.apiUrl, "apiUrl");
    appUrl = optionalString(obj.appUrl, "appUrl");
    wsUrl = optionalString(obj.wsUrl, "wsUrl");
  } else if (listed?.[0]) {
    apiUrl = listed[0].apiUrl;
    appUrl = listed[0].appUrl;
    wsUrl = listed[0].wsUrl;
  } else {
    throw new Error("Invalid desktop runtime config: apiUrl must be a non-empty string");
  }

  const normalizedApiUrl = normalizeHttpUrl(apiUrl, "apiUrl");
  const config: RuntimeConfig = {
    schemaVersion: 1,
    apiUrl: normalizedApiUrl,
    wsUrl: wsUrl ? normalizeWsUrl(wsUrl, "wsUrl") : deriveWsUrl(normalizedApiUrl),
    appUrl: appUrl ? normalizeHttpUrl(appUrl, "appUrl") : deriveAppUrl(normalizedApiUrl),
  };

  const servers = serversStateFromConfig(config, {
    editable: true,
    servers: listed,
    activeServerId: activeServerIdRaw,
  });

  return { config, servers };
}

export function serializeDesktopConfigFile(servers: DesktopServersState): string {
  const active =
    servers.servers.find((s) => s.id === servers.activeServerId) ?? servers.servers[0];
  if (!active) {
    throw new Error("Cannot serialize desktop config with an empty server list");
  }

  const payload = {
    schemaVersion: 1 as const,
    // Top-level active fields for backward compatibility with older Desktop builds.
    apiUrl: active.apiUrl,
    wsUrl: active.wsUrl,
    appUrl: active.appUrl,
    activeServerId: active.id,
    servers: servers.servers.map((s) => ({
      id: s.id,
      name: s.name,
      apiUrl: s.apiUrl,
      wsUrl: s.wsUrl,
      appUrl: s.appUrl,
    })),
  };

  return `${JSON.stringify(payload, null, 2)}\n`;
}

export function resolveActiveConfig(servers: DesktopServersState): RuntimeConfig {
  const active =
    servers.servers.find((s) => s.id === servers.activeServerId) ?? servers.servers[0];
  if (!active) {
    throw new Error("No servers configured");
  }
  return {
    schemaVersion: 1,
    apiUrl: active.apiUrl,
    wsUrl: active.wsUrl,
    appUrl: active.appUrl,
  };
}

export function upsertServerProfile(
  state: DesktopServersState,
  input: {
    id?: string;
    name: string;
    apiUrl: string;
    wsUrl?: string;
    appUrl?: string;
  },
): DesktopServersState {
  const apiUrl = normalizeHttpUrl(input.apiUrl, "apiUrl");
  const profile: DesktopServerProfile = {
    id: input.id?.trim() || serverIdFromApiUrl(apiUrl),
    name: input.name.trim() || defaultServerName(apiUrl),
    apiUrl,
    wsUrl: input.wsUrl ? normalizeWsUrl(input.wsUrl, "wsUrl") : deriveWsUrl(apiUrl),
    appUrl: input.appUrl ? normalizeHttpUrl(input.appUrl, "appUrl") : deriveAppUrl(apiUrl),
  };

  if (!profile.name) {
    throw new Error("Server name must be a non-empty string");
  }

  const existingById = state.servers.findIndex((s) => s.id === profile.id);
  const conflict = state.servers.find(
    (s) => s.id !== profile.id && urlsMatch(s.apiUrl, profile.apiUrl),
  );
  if (conflict) {
    throw new Error(`A server with API URL ${profile.apiUrl} already exists (${conflict.name})`);
  }

  let servers: DesktopServerProfile[];
  if (existingById >= 0) {
    servers = state.servers.map((s, i) => (i === existingById ? profile : s));
  } else {
    // If another entry has the same id collision from host rename, allocate a new id.
    const idTaken = state.servers.some((s) => s.id === profile.id);
    const finalProfile = idTaken
      ? { ...profile, id: `${profile.id}-${shortHash(profile.apiUrl)}` }
      : profile;
    servers = [...state.servers, finalProfile];
  }

  return {
    ...state,
    servers: dedupeServersByApiUrl(servers),
  };
}

export function removeServerProfile(
  state: DesktopServersState,
  serverId: string,
): DesktopServersState {
  if (state.servers.length <= 1) {
    throw new Error("Cannot remove the last server");
  }
  const servers = state.servers.filter((s) => s.id !== serverId);
  if (servers.length === state.servers.length) {
    throw new Error(`Server "${serverId}" not found`);
  }
  const activeServerId =
    state.activeServerId === serverId ? servers[0]!.id : state.activeServerId;
  return { ...state, servers, activeServerId };
}

export function switchActiveServer(
  state: DesktopServersState,
  serverId: string,
): DesktopServersState {
  const target = state.servers.find((s) => s.id === serverId);
  if (!target) {
    throw new Error(`Server "${serverId}" not found`);
  }
  return { ...state, activeServerId: serverId };
}

export function deriveWsUrl(apiUrl: string): string {
  const url = new URL(apiUrl);
  if (url.protocol === "https:") url.protocol = "wss:";
  else if (url.protocol === "http:") url.protocol = "ws:";
  else throw new Error("apiUrl must use http or https");
  url.pathname = joinPath(url.pathname, "/ws");
  url.search = "";
  url.hash = "";
  return trimTrailingSlash(url.toString());
}

// Convention: api hosts are exposed at `api.<web-host>` (api.multica.ai →
// multica.ai, api.test.multica.ai → test.multica.ai). Strip the leading
// `api.` label so a single `apiUrl` configuration produces the right
// shareable web URL. Hosts that don't match the convention (no leading
// `api.` label, or short two-label hosts like `api.local`) fall through
// untouched — those deployments must set `appUrl` explicitly.
export function deriveAppUrl(apiUrl: string): string {
  const url = new URL(apiUrl);
  url.pathname = "";
  url.search = "";
  url.hash = "";
  if (url.hostname.startsWith("api.") && url.hostname.split(".").length >= 3) {
    url.hostname = url.hostname.slice("api.".length);
  }
  return trimTrailingSlash(url.toString());
}

// Dev variant: when the api host is the local backend (`localhost:8080` /
// `127.0.0.1:8080`), the renderer is served from a different port (3000),
// so deriving by host alone is wrong. Fall back to the local dev web URL
// in that case; for any non-local host (e.g. a remote test environment),
// trust the production-style derivation so `apiUrl=https://api.test.x`
// yields `appUrl=https://test.x` without a separate VITE_APP_URL.
export function deriveDevAppUrl(apiUrl: string): string {
  const url = new URL(apiUrl);
  if (url.hostname === "localhost" || url.hostname === "127.0.0.1") {
    return LOCAL_DEV_RUNTIME_CONFIG.appUrl;
  }
  return deriveAppUrl(apiUrl);
}

export function urlsMatch(a: string, b: string): boolean {
  try {
    return normalizeHttpUrl(a, "url") === normalizeHttpUrl(b, "url");
  } catch {
    return a === b;
  }
}

export function defaultServerName(apiUrl: string): string {
  try {
    return new URL(apiUrl).host;
  } catch {
    return apiUrl;
  }
}

export function serverIdFromApiUrl(apiUrl: string): string {
  try {
    const host = new URL(apiUrl).host.toLowerCase();
    const slug = host.replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
    return slug || `server-${shortHash(apiUrl)}`;
  } catch {
    return `server-${shortHash(apiUrl)}`;
  }
}

function parseServerProfile(entry: unknown, index: number): DesktopServerProfile {
  if (!entry || typeof entry !== "object" || Array.isArray(entry)) {
    throw new Error(`Invalid desktop runtime config: servers[${index}] must be an object`);
  }
  const obj = entry as Record<string, unknown>;
  const apiUrl = normalizeHttpUrl(requiredString(obj.apiUrl, `servers[${index}].apiUrl`), "apiUrl");
  const id =
    optionalString(obj.id, `servers[${index}].id`)?.trim() || serverIdFromApiUrl(apiUrl);
  const name =
    optionalString(obj.name, `servers[${index}].name`)?.trim() || defaultServerName(apiUrl);
  const wsUrlRaw = optionalString(obj.wsUrl, `servers[${index}].wsUrl`);
  const appUrlRaw = optionalString(obj.appUrl, `servers[${index}].appUrl`);
  return {
    id,
    name,
    apiUrl,
    wsUrl: wsUrlRaw ? normalizeWsUrl(wsUrlRaw, "wsUrl") : deriveWsUrl(apiUrl),
    appUrl: appUrlRaw ? normalizeHttpUrl(appUrlRaw, "appUrl") : deriveAppUrl(apiUrl),
  };
}

function normalizeProfile(profile: DesktopServerProfile): DesktopServerProfile {
  const apiUrl = normalizeHttpUrl(profile.apiUrl, "apiUrl");
  return {
    id: profile.id.trim() || serverIdFromApiUrl(apiUrl),
    name: profile.name.trim() || defaultServerName(apiUrl),
    apiUrl,
    wsUrl: profile.wsUrl ? normalizeWsUrl(profile.wsUrl, "wsUrl") : deriveWsUrl(apiUrl),
    appUrl: profile.appUrl ? normalizeHttpUrl(profile.appUrl, "appUrl") : deriveAppUrl(apiUrl),
  };
}

function dedupeServersByApiUrl(servers: DesktopServerProfile[]): DesktopServerProfile[] {
  const seen = new Set<string>();
  const out: DesktopServerProfile[] = [];
  for (const server of servers) {
    const key = server.apiUrl;
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(server);
  }
  return out;
}

function shortHash(value: string): string {
  let hash = 0;
  for (let i = 0; i < value.length; i++) {
    hash = (hash * 31 + value.charCodeAt(i)) >>> 0;
  }
  return hash.toString(36).slice(0, 6);
}

function requiredString(value: unknown, field: string): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new Error(`Invalid desktop runtime config: ${field} must be a non-empty string`);
  }
  return value;
}

function optionalString(value: unknown, field: string): string | undefined {
  if (value === undefined || value === null) return undefined;
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new Error(`Invalid desktop runtime config: ${field} must be a non-empty string when set`);
  }
  return value;
}

function normalizeHttpUrl(value: string, field: string): string {
  let url: URL;
  try {
    url = new URL(value.trim());
  } catch {
    throw new Error(`Invalid desktop runtime config: ${field} must be a valid URL`);
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") {
    throw new Error(`Invalid desktop runtime config: ${field} must use http or https`);
  }
  url.search = "";
  url.hash = "";
  return trimTrailingSlash(url.toString());
}

function normalizeWsUrl(value: string, field: string): string {
  let url: URL;
  try {
    url = new URL(value.trim());
  } catch {
    throw new Error(`Invalid desktop runtime config: ${field} must be a valid URL`);
  }
  if (url.protocol !== "ws:" && url.protocol !== "wss:") {
    throw new Error(`Invalid desktop runtime config: ${field} must use ws or wss`);
  }
  url.search = "";
  url.hash = "";
  return trimTrailingSlash(url.toString());
}

function joinPath(base: string, suffix: string): string {
  const normalizedBase = base.endsWith("/") ? base.slice(0, -1) : base;
  return `${normalizedBase}${suffix}`;
}

function trimTrailingSlash(value: string): string {
  return value.replace(/\/+$/, "");
}
