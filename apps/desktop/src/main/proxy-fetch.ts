/**
 * Proxy-aware fetch wrapper for the Electron main process.
 *
 * Node.js undici fetch does NOT read HTTP_PROXY / HTTPS_PROXY by default.
 * This module provides a single `proxyFetch()` entry-point that routes
 * requests through a ProxyAgent when a proxy URL has been configured
 * (from desktop.json `proxyUrl`), and falls back to native fetch
 * otherwise.
 *
 * Localhost / 127.0.0.1 requests bypass the proxy unconditionally —
 * the daemon health-check and local dev server must never be proxied.
 *
 * Call `setProxyUrl()` once at startup (after loading runtime config)
 * and again whenever the user updates the proxy in Settings.
 */
import { ProxyAgent, fetch as undiciFetch } from "undici";

let currentProxy: string | null = null;
let currentNoProxy: string[] = [];

/**
 * Set or clear the HTTP proxy URL used by `proxyFetch`.
 * Pass `null` to disable proxying.
 */
export function setProxyUrl(url: string | null, noProxy: string | null = null): void {
  currentProxy = url;
  currentNoProxy = parseNoProxy(noProxy);
}

/**
 * Return the currently configured proxy URL, or null if none.
 */
export function getProxyUrl(): string | null {
  return currentProxy;
}

/**
 * Return the currently configured noProxy list as a comma-separated string.
 */
export function getNoProxy(): string | null {
  return currentNoProxy.length > 0 ? currentNoProxy.join(",") : null;
}

/**
 * Proxy-aware fetch for the Electron main process.
 *
 * When a proxy is configured AND the target URL is not explicitly bypassed,
 * the request is routed through a ProxyAgent. Otherwise native fetch is used
 * directly.
 */
export function proxyFetch(
  url: string | URL,
  init?: RequestInit,
): Promise<Response> {
  if (currentProxy && !shouldBypassProxy(url)) {
    const agent = new ProxyAgent(currentProxy);
    // undici's RequestInit accepts `dispatcher`; cast to satisfy the
    // global RequestInit type which does not declare it.
    return undiciFetch(url, {
      ...init,
      dispatcher: agent,
    } as Parameters<typeof undiciFetch>[1]) as unknown as Promise<Response>;
  }
  return fetch(url, init);
}

export function shouldBypassProxy(url: string | URL): boolean {
  if (!currentProxy) return false;
  try {
    const parsed = typeof url === "string" ? new URL(url) : url;
    const host = parsed.hostname.toLowerCase();
    if (isLoopbackHost(host)) return true;
    return currentNoProxy.some((entry) => matchesNoProxyHost(host, entry));
  } catch {
    return false;
  }
}

function parseNoProxy(value: string | null): string[] {
  return (value ?? "")
    .split(",")
    .map((entry) => entry.trim().toLowerCase())
    .filter(Boolean);
}

function isLoopbackHost(host: string): boolean {
  return (
    host === "localhost" ||
    host === "127.0.0.1" ||
    host === "::1" ||
    host === "[::1]"
  );
}

function matchesNoProxyHost(host: string, entry: string): boolean {
  const normalized = entry.replace(/^\./, "");
  if (!normalized || normalized === "*") return false;
  if (host === normalized) return true;
  return host.endsWith(`.${normalized}`);
}

/**
 * True when `url` targets a loopback address (localhost / 127.x / ::1).
 * These requests must never traverse a proxy.
 */
function isLoopback(url: string | URL): boolean {
  try {
    const parsed = typeof url === "string" ? new URL(url) : url;
    return isLoopbackHost(parsed.hostname.toLowerCase());
  } catch {
    return false;
  }
}
