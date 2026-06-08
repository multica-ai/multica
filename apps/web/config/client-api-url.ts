type BrowserLocationLike = {
  hostname: string;
  origin: string;
};

function isLocalhost(hostname: string): boolean {
  return hostname === "localhost" || hostname === "127.0.0.1" || hostname === "::1";
}

export function resolveBrowserApiBaseUrl(
  publicApiUrl: string | undefined,
  locationOverride?: BrowserLocationLike,
): string | undefined {
  const trimmed = publicApiUrl?.trim();
  if (!trimmed) return undefined;

  const location =
    locationOverride ??
    (typeof window !== "undefined" ? window.location : undefined);

  if (!location || !isLocalhost(location.hostname)) return trimmed;

  try {
    const apiUrl = new URL(trimmed);
    if (apiUrl.origin !== location.origin && !isLocalhost(apiUrl.hostname)) {
      return "";
    }
  } catch {
    return trimmed;
  }

  return trimmed;
}
