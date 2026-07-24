type RuntimeEnv = Record<string, string | undefined>;

function cleanUrl(raw: string | undefined): string | undefined {
  const value = raw?.trim();
  if (!value) return undefined;
  return value.replace(/\/+$/, "");
}

function cleanHttpUrl(raw: string | undefined): string | undefined {
  const value = cleanUrl(raw);
  if (!value) return undefined;

  try {
    const url = new URL(value);
    if (url.protocol === "http:" || url.protocol === "https:") return value;
  } catch {
    return undefined;
  }

  return undefined;
}

function appendPath(baseUrl: string, path: string): string {
  return `${baseUrl}${path.startsWith("/") ? path : `/${path}`}`;
}

export function resolveRemoteApiUrl(env: RuntimeEnv): string | undefined {
  const explicitRemote = cleanHttpUrl(env.REMOTE_API_URL);
  if (explicitRemote) return explicitRemote;

  const publicApi = cleanHttpUrl(env.NEXT_PUBLIC_API_URL);
  if (publicApi) return publicApi;
  return undefined;
}

export function resolveDocsUrl(env: RuntimeEnv): string | undefined {
  return cleanHttpUrl(env.DOCS_URL);
}

// Dev-only fallbacks: `next dev` runs on a developer machine, where the
// conventional localhost backend/docs ports are safe to assume when nothing
// is configured. Builds and the runtime proxy keep the strict resolvers so a
// prebuilt image never guesses an origin (#4787).
export function resolveDevRemoteApiUrl(env: RuntimeEnv): string {
  const configured = resolveRemoteApiUrl(env);
  if (configured) return configured;
  const backendPort = env.BACKEND_PORT?.trim() || "8080";
  return `http://localhost:${backendPort}`;
}

export function resolveDevDocsUrl(env: RuntimeEnv): string {
  return resolveDocsUrl(env) ?? "http://localhost:4000";
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
  const docsUrl = resolveDocsUrl(env);
  if (pathname === "/docs") {
    return docsUrl ? appendPath(docsUrl, "/docs") : undefined;
  }
  if (pathname.startsWith("/docs/")) {
    return docsUrl ? appendPath(docsUrl, pathname) : undefined;
  }

  const remoteApiUrl = resolveRemoteApiUrl(env);
  if (!remoteApiUrl) return undefined;

  if (pathname === "/api" || pathname.startsWith("/api/")) {
    return appendPath(remoteApiUrl, pathname);
  }
  if (pathname === "/uploads" || pathname.startsWith("/uploads/")) {
    return appendPath(remoteApiUrl, pathname);
  }
  if (pathname === "/ws") {
    return appendPath(remoteApiUrl, "/ws");
  }
  if (isBackendAuthPath(pathname)) {
    return appendPath(remoteApiUrl, pathname);
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
