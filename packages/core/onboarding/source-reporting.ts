import { api } from "../api";

export const OFFICIAL_MULTICA_API_URL = "https://api.multica.ai";

export function normalizeApiBaseUrl(value: string | null | undefined): string {
  const raw = value?.trim();
  if (!raw) return "";
  try {
    const url = new URL(raw);
    url.search = "";
    url.hash = "";
    url.pathname = url.pathname.replace(/\/+$/, "");
    return url.toString().replace(/\/+$/, "");
  } catch {
    return "";
  }
}

export function isOfficialMulticaApiUrl(
  apiBaseUrl: string | null | undefined,
): boolean {
  return normalizeApiBaseUrl(apiBaseUrl) === OFFICIAL_MULTICA_API_URL;
}

export function isSelfHostedApiBaseUrl(
  apiBaseUrl: string | null | undefined,
): boolean {
  return !isOfficialMulticaApiUrl(apiBaseUrl);
}

export function currentApiBaseUrl(): string {
  const configured = api.getBaseUrl?.();
  if (typeof configured === "string" && configured.trim() !== "") {
    return configured;
  }
  if (typeof window !== "undefined") {
    return window.location.origin;
  }
  return "";
}

export function shouldShowSourceChannelReporting(
  apiBaseUrl = currentApiBaseUrl(),
): boolean {
  return isSelfHostedApiBaseUrl(apiBaseUrl);
}
