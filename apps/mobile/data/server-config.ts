import * as SecureStore from "expo-secure-store";

const CUSTOM_API_URL_KEY = "multica_custom_api_url";
const CUSTOM_WEB_URL_KEY = "multica_custom_web_url";
const PROBE_TIMEOUT_MS = 10_000;

const envApiUrl = process.env.EXPO_PUBLIC_API_URL?.trim() ?? "";
const defaultApiUrl = envApiUrl ? stripTrailingSlashes(envApiUrl) : "";
const envWebUrl = process.env.EXPO_PUBLIC_WEB_URL?.trim() ?? "";
const defaultWebUrl = envWebUrl ? stripTrailingSlashes(envWebUrl) : null;

let customApiUrl: string | null = null;
let customWebUrl: string | null = null;
let restored = false;

export class ServerConfigError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ServerConfigError";
  }
}

export function getDefaultApiUrl(): string {
  return defaultApiUrl;
}

export function getCustomApiUrl(): string | null {
  return customApiUrl;
}

export function getEffectiveWebUrl(): string | null {
  return customWebUrl ?? defaultWebUrl;
}

export function getEffectiveApiUrl(): string {
  const url = customApiUrl ?? defaultApiUrl;
  if (!url) {
    throw new ServerConfigError(
      "No Multica backend URL is configured. Set EXPO_PUBLIC_API_URL or configure a server on the login screen.",
    );
  }
  return url;
}

export async function restoreServerConfig(): Promise<string | null> {
  if (restored) return customApiUrl;
  const stored = await SecureStore.getItemAsync(CUSTOM_API_URL_KEY);
  if (stored) {
    const normalized = normalizeApiUrl(stored);
    customApiUrl = normalized.ok ? normalized.url : null;
    if (!normalized.ok) {
      await SecureStore.deleteItemAsync(CUSTOM_API_URL_KEY);
    }
  }
  const storedWebUrl = await SecureStore.getItemAsync(CUSTOM_WEB_URL_KEY);
  if (storedWebUrl) {
    const normalized = normalizeApiUrl(storedWebUrl);
    customWebUrl = normalized.ok ? normalized.url : null;
    if (!normalized.ok) {
      await SecureStore.deleteItemAsync(CUSTOM_WEB_URL_KEY);
    }
  }
  restored = true;
  return customApiUrl;
}

export async function setCustomApiUrl(
  rawUrl: string,
  opts?: { webUrl?: string | null },
): Promise<string> {
  const normalized = normalizeApiUrl(rawUrl);
  if (!normalized.ok) throw new ServerConfigError(normalized.error);
  customApiUrl = normalized.url;
  customWebUrl = normalizeOptionalUrl(opts?.webUrl);
  restored = true;
  await SecureStore.setItemAsync(CUSTOM_API_URL_KEY, normalized.url);
  if (customWebUrl) {
    await SecureStore.setItemAsync(CUSTOM_WEB_URL_KEY, customWebUrl);
  } else {
    await SecureStore.deleteItemAsync(CUSTOM_WEB_URL_KEY);
  }
  return normalized.url;
}

export async function clearCustomApiUrl(): Promise<void> {
  customApiUrl = null;
  customWebUrl = null;
  restored = true;
  await SecureStore.deleteItemAsync(CUSTOM_API_URL_KEY);
  await SecureStore.deleteItemAsync(CUSTOM_WEB_URL_KEY);
}

export function normalizeApiUrl(
  rawUrl: string,
): { ok: true; url: string } | { ok: false; error: string } {
  const trimmed = rawUrl.trim();
  if (!trimmed) return { ok: false, error: "Enter a backend URL." };

  let parsed: URL;
  try {
    parsed = new URL(trimmed);
  } catch {
    return { ok: false, error: "Enter a valid URL, like https://example.com." };
  }

  if (parsed.protocol !== "https:" && parsed.protocol !== "http:") {
    return { ok: false, error: "URL must start with https:// or http://." };
  }
  if (!parsed.hostname) {
    return { ok: false, error: "URL must include a host." };
  }
  if (parsed.pathname !== "/" || parsed.search || parsed.hash) {
    return { ok: false, error: "Use the backend origin only, without a path." };
  }

  return { ok: true, url: stripTrailingSlashes(parsed.toString()) };
}

export interface BackendProbeResult {
  apiUrl: string;
  webUrl: string | null;
}

export async function probeMulticaBackend(
  rawUrl: string,
): Promise<BackendProbeResult> {
  const normalized = normalizeApiUrl(rawUrl);
  if (!normalized.ok) throw new ServerConfigError(normalized.error);

  const controller = new AbortController();
  const timeoutId = setTimeout(() => controller.abort(), PROBE_TIMEOUT_MS);
  let res: Response;
  try {
    res = await fetch(`${normalized.url}/api/config`, {
      method: "GET",
      headers: { Accept: "application/json" },
      signal: controller.signal,
    });
  } catch (err) {
    if (err instanceof Error && err.name === "AbortError") {
      throw new ServerConfigError(
        "Server did not respond. Check the URL and network.",
      );
    }
    throw new ServerConfigError("Could not reach that server.");
  } finally {
    clearTimeout(timeoutId);
  }

  if (!res.ok) {
    throw new ServerConfigError(
      `Server returned ${res.status}. Check the backend URL.`,
    );
  }

  let body: unknown;
  try {
    body = await res.json();
  } catch {
    throw new ServerConfigError("Server did not return Multica configuration.");
  }

  if (!looksLikeMulticaConfig(body)) {
    throw new ServerConfigError(
      "That server does not look like a Multica backend.",
    );
  }

  return { apiUrl: normalized.url, webUrl: configWebUrl(body) };
}

function looksLikeMulticaConfig(value: unknown): boolean {
  if (!value || typeof value !== "object") return false;
  const obj = value as Record<string, unknown>;
  return (
    typeof obj.allow_signup === "boolean" ||
    typeof obj.cdn_domain === "string" ||
    typeof obj.daemon_server_url === "string" ||
    typeof obj.workspace_creation_disabled === "boolean"
  );
}

function configWebUrl(value: unknown): string | null {
  if (!value || typeof value !== "object") return null;
  const raw = (value as Record<string, unknown>).daemon_app_url;
  return typeof raw === "string" ? normalizeOptionalUrl(raw) : null;
}

function normalizeOptionalUrl(rawUrl: string | null | undefined): string | null {
  if (!rawUrl) return null;
  const normalized = normalizeApiUrl(rawUrl);
  return normalized.ok ? normalized.url : null;
}

function stripTrailingSlashes(value: string): string {
  return value.replace(/\/+$/, "");
}
