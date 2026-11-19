export interface ServerConfig {
  apiUrl: string;
  webUrl: string;
}

export class ServerUrlError extends Error {
  constructor(readonly code: "invalid" | "https_required") {
    super(code);
  }
}

/** Addresses are service roots, with an optional reverse-proxy base path. */
export function normalizeServerUrl(value: string, kind: "api" | "web"): string {
  const trimmed = value.trim();
  if (/^http:\/\//i.test(trimmed)) throw new ServerUrlError("https_required");
  if (!/^https:\/\//i.test(trimmed) || /[\s\\]/.test(trimmed)) {
    throw new ServerUrlError("invalid");
  }
  let url: URL;
  try {
    url = new URL(trimmed);
  } catch {
    throw new ServerUrlError("invalid");
  }
  if (!url.hostname || url.username || url.password || url.search || url.hash ||
      /[?#]/.test(trimmed) || /\/(?:\.|%2e){1,2}(?:\/|$)/i.test(trimmed) ||
      /%2f|%5c/i.test(url.pathname) || url.pathname.replace(/\/+$/, "").includes("//")) {
    throw new ServerUrlError("invalid");
  }
  let path = url.pathname.replace(/\/+$/, "");
  // Mobile endpoints already include /api. Auth and WS live beside it.
  if (kind === "api") path = path.replace(/\/api$/, "");
  return `${url.origin}${path}`;
}

export function normalizeServerConfig(config: ServerConfig): ServerConfig {
  return {
    apiUrl: normalizeServerUrl(config.apiUrl, "api"),
    webUrl: normalizeServerUrl(config.webUrl, "web"),
  };
}

export function joinServerPath(base: string, path: string): string {
  const root = base.replace(/\/+$/, "");
  return `${root}/${path.replace(/^\/+/, "")}`;
}

export function serverWebSocketUrl(apiUrl: string): string {
  return joinServerPath(apiUrl, "/ws").replace(/^http/, "ws");
}
