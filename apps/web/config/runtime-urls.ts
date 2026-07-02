type RuntimeEnv = Record<string, string | undefined>;

function cleanUrl(raw: string | undefined): string | undefined {
  const value = raw?.trim();
  if (!value) return undefined;
  return value.replace(/\/+$/, "");
}

function appendPath(baseUrl: string, path: string): string {
  return `${baseUrl}${path.startsWith("/") ? path : `/${path}`}`;
}

export function resolveRemoteApiUrl(env: RuntimeEnv): string {
  const explicitRemote = cleanUrl(env.REMOTE_API_URL);
  if (explicitRemote) return explicitRemote;

  const publicApi = cleanUrl(env.NEXT_PUBLIC_API_URL);
  if (publicApi) return publicApi;

  const port =
    env.BACKEND_PORT?.trim() ||
    env.API_PORT?.trim() ||
    env.SERVER_PORT?.trim() ||
    env.PORT?.trim();
  if (port) return `http://localhost:${port}`;

  return "http://localhost:8080";
}

export function resolveDocsUrl(env: RuntimeEnv): string {
  return cleanUrl(env.DOCS_URL) || "http://localhost:4000";
}

export function resolveBrowserApiBaseUrl(env: RuntimeEnv): string | undefined {
  return cleanUrl(env.NEXT_PUBLIC_API_URL);
}

export function resolveBrowserWsUrl(env: RuntimeEnv): string | undefined {
  const explicit = cleanUrl(env.NEXT_PUBLIC_WS_URL);
  if (explicit) return explicit;

  const apiUrl = resolveBrowserApiBaseUrl(env);
  return apiUrl ? tryDeriveWsUrl(apiUrl) : undefined;
}

export function runtimeRewriteDestination(
  pathname: string,
  env: RuntimeEnv,
): string | undefined {
  if (pathname === "/docs") {
    return appendPath(resolveDocsUrl(env), "/docs");
  }
  if (pathname.startsWith("/docs/")) {
    return appendPath(resolveDocsUrl(env), pathname);
  }
  if (pathname === "/api" || pathname.startsWith("/api/")) {
    return appendPath(resolveRemoteApiUrl(env), pathname);
  }
  if (pathname === "/uploads" || pathname.startsWith("/uploads/")) {
    return appendPath(resolveRemoteApiUrl(env), pathname);
  }
  if (pathname === "/ws") {
    return appendPath(resolveRemoteApiUrl(env), "/ws");
  }
  if (isBackendAuthPath(pathname)) {
    return appendPath(resolveRemoteApiUrl(env), pathname);
  }

  return undefined;
}

function isBackendAuthPath(pathname: string): boolean {
  if (pathname === "/auth/callback") return false;
  if (pathname.startsWith("/auth/callback/")) return false;
  if (pathname === "/auth/hg-sso/callback") return false;
  if (pathname.startsWith("/auth/hg-sso/callback/")) return false;
  return pathname === "/auth" || pathname.startsWith("/auth/");
}

function tryDeriveWsUrl(apiUrl: string): string | undefined {
  let url: URL;
  try {
    url = new URL(apiUrl);
  } catch {
    return undefined;
  }
  if (url.protocol === "https:") url.protocol = "wss:";
  else if (url.protocol === "http:") url.protocol = "ws:";
  else return undefined;
  url.pathname = appendPath(url.pathname.replace(/\/+$/, ""), "/ws");
  url.search = "";
  url.hash = "";
  return url.toString().replace(/\/$/, "");
}
