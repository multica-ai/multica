type RuntimeEnv = Record<string, string | undefined>;

interface LocationLike {
  protocol: string;
  host: string;
}

export function resolveBasePath(env: RuntimeEnv): string {
  const raw = (env.NEXT_PUBLIC_BASE_PATH || env.BASE_PATH || "").trim();
  if (!raw || raw === "/") return "";
  const withLeadingSlash = raw.startsWith("/") ? raw : `/${raw}`;
  return withLeadingSlash.replace(/\/+$/, "");
}

export function withBasePath(basePath: string, path: string): string {
  if (/^[a-z][a-z0-9+.-]*:/i.test(path)) return path;

  const normalizedBasePath = resolveBasePath({
    NEXT_PUBLIC_BASE_PATH: basePath,
  });
  const normalizedPath = path.startsWith("/") ? path : `/${path}`;
  if (!normalizedBasePath) return normalizedPath;
  if (
    normalizedPath === normalizedBasePath ||
    normalizedPath.startsWith(`${normalizedBasePath}/`)
  ) {
    return normalizedPath;
  }
  return `${normalizedBasePath}${normalizedPath}`;
}

export function resolveApiBaseUrl(env: RuntimeEnv): string {
  const explicit = env.NEXT_PUBLIC_API_URL?.trim();
  if (explicit) return explicit;
  return resolveBasePath(env);
}

export function deriveWsUrl(
  env: RuntimeEnv,
  location: LocationLike | undefined,
): string | undefined {
  const explicit = env.NEXT_PUBLIC_WS_URL?.trim();
  if (explicit) return explicit;
  if (!location) return undefined;

  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  return `${proto}//${location.host}${withBasePath(resolveBasePath(env), "/ws")}`;
}
