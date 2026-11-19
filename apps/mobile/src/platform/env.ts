export interface Endpoints {
  apiUrl: string;
  wsUrl: string;
  /** Origin of the web app; issue links shared from the app point here. */
  webUrl: string;
}

export interface EndpointEnv {
  apiUrl?: string;
  wsUrl?: string;
  webUrl?: string;
}

const LOCAL_API_URL = "http://localhost:8080";
const LOCAL_WEB_URL = "http://localhost:3000";

export function resolveEndpoints(env: EndpointEnv): Endpoints {
  const apiUrl = normalizeUrl(env.apiUrl || LOCAL_API_URL, ["http:", "https:"], "EXPO_PUBLIC_API_URL");
  return {
    apiUrl,
    wsUrl: env.wsUrl
      ? normalizeUrl(env.wsUrl, ["ws:", "wss:"], "EXPO_PUBLIC_WS_URL")
      : deriveWsUrl(apiUrl),
    webUrl: normalizeUrl(env.webUrl || LOCAL_WEB_URL, ["http:", "https:"], "EXPO_PUBLIC_WEB_URL"),
  };
}

export function deriveWsUrl(apiUrl: string): string {
  const url = new URL(apiUrl);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  url.pathname = `${url.pathname.replace(/\/+$/, "")}/ws`;
  return trimTrailingSlash(url.toString());
}

function normalizeUrl(value: string, protocols: string[], name: string): string {
  let url: URL;
  try {
    url = new URL(value.trim());
  } catch {
    throw new Error(`${name} must be a valid URL (got "${value}").`);
  }
  if (!protocols.includes(url.protocol)) {
    throw new Error(`${name} must use ${protocols.map((p) => p.slice(0, -1)).join(" or ")}.`);
  }
  url.search = "";
  url.hash = "";
  return trimTrailingSlash(url.toString());
}

function trimTrailingSlash(value: string): string {
  return value.replace(/\/+$/, "");
}

// Expo inlines EXPO_PUBLIC_* at bundle time only for direct `process.env.NAME` reads.
export const endpoints = resolveEndpoints({
  apiUrl: process.env.EXPO_PUBLIC_API_URL,
  wsUrl: process.env.EXPO_PUBLIC_WS_URL,
  webUrl: process.env.EXPO_PUBLIC_WEB_URL,
});
