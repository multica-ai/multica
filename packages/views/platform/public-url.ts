const DEFAULT_PUBLIC_APP_URL = "https://multica.ai";

function trimTrailingSlash(value: string): string {
  return value.replace(/\/+$/, "");
}

function getConfiguredAppUrl(): string | undefined {
  if (typeof window === "undefined") return undefined;

  return (
    window as unknown as {
      __MULTICA_PUBLIC_CONFIG__?: { appUrl?: string };
    }
  ).__MULTICA_PUBLIC_CONFIG__?.appUrl;
}

function getBrowserOrigin(): string | undefined {
  if (typeof window === "undefined") return undefined;
  return window.location.origin;
}

export function getPublicAppUrl(): string {
  const configuredUrl = getConfiguredAppUrl();
  return trimTrailingSlash(
    configuredUrl && configuredUrl.length > 0
      ? configuredUrl
      : getBrowserOrigin() || DEFAULT_PUBLIC_APP_URL,
  );
}

export function publicAppUrl(path = ""): string {
  if (!path) return getPublicAppUrl();
  const normalizedPath = path.startsWith("/") ? path : `/${path}`;
  return `${getPublicAppUrl()}${normalizedPath}`;
}
