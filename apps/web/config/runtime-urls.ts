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

// The API base names the backend ORIGIN, never its `/api` endpoint: every
// caller already carries its own prefix (`packages/core/api/client.ts` sends
// `/api/**`, avatars resolve `/uploads/**`, realtime connects `/ws`), and the
// backend serves all three at the root (server/cmd/server/router.go). A base
// ending in `/api` therefore yields `/api/api/**` requests and 404s every
// upload — the most common self-hosting mistake (#6619, MUL-5922). Strip that
// one suffix instead of honouring it. Any other path is preserved: a reverse
// proxy may legitimately mount the whole backend under a prefix such as
// `https://host/multica`.
function stripApiPathSuffix(value: string): string {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return value;
  }
  const pathname = url.pathname.replace(/\/+$/, "");
  if (!pathname.endsWith("/api")) return value;
  url.pathname = pathname.slice(0, -"/api".length);
  url.search = "";
  url.hash = "";
  return url.toString().replace(/\/+$/, "");
}

function cleanApiBaseUrl(raw: string | undefined): string | undefined {
  const value = cleanHttpUrl(raw);
  if (!value) return undefined;
  return stripApiPathSuffix(value);
}

function appendPath(baseUrl: string, path: string): string {
  return `${baseUrl}${path.startsWith("/") ? path : `/${path}`}`;
}

export function resolveRemoteApiUrl(env: RuntimeEnv): string | undefined {
  const explicitRemote = cleanApiBaseUrl(env.REMOTE_API_URL);
  if (explicitRemote) return explicitRemote;

  const publicApi = cleanApiBaseUrl(env.NEXT_PUBLIC_API_URL);
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
  // Next writes process.env.PORT with the frontend listener port before it
  // evaluates next.config.ts. Treating that generic variable as a backend port
  // would make every dev rewrite point back to the frontend itself. Only the
  // backend-specific aliases are safe fallbacks in this process.
  const backendPort =
    env.BACKEND_PORT?.trim() ||
    env.API_PORT?.trim() ||
    env.SERVER_PORT?.trim() ||
    "8080";
  return `http://localhost:${backendPort}`;
}

export function resolveDevDocsUrl(env: RuntimeEnv): string {
  return resolveDocsUrl(env) ?? "http://localhost:4000";
}

// Same strictness as the server-side resolver above. A relative or otherwise
// non-http value used to pass through untouched and became the XHR base, so
// `NEXT_PUBLIC_API_URL=/api` produced `/api/api/**` and `/api/uploads/**`
// (#6619). Returning undefined instead makes the browser fall back to
// same-origin relative paths, which is what an unset value already does.
export function resolveBrowserApiBaseUrl(env: RuntimeEnv): string | undefined {
  return cleanApiBaseUrl(env.NEXT_PUBLIC_API_URL);
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

// `/ws` is appended to the api base's PATH, not to its origin, and that is
// deliberate: the base is whatever prefix the backend is mounted under, so
// HTTP (`<base>/api/**`) and realtime (`<base>/ws`) must share it or a
// prefix-mounted deployment would break in one direction while working in the
// other. `apps/desktop/src/shared/runtime-config.ts` derives it the same way,
// so both clients read one configured value identically. The regression that
// motivated MUL-5922 — `NEXT_PUBLIC_API_URL=https://host/api` deriving
// `wss://host/api/ws` while the backend serves `/ws` at the root — is fixed
// upstream in the base itself (see stripApiPathSuffix), not here.
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
