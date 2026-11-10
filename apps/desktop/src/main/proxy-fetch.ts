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

/**
 * Set or clear the HTTP proxy URL used by `proxyFetch`.
 * Pass `null` to disable proxying.
 */
export function setProxyUrl(url: string | null): void {
  currentProxy = url;
}

/**
 * Return the currently configured proxy URL, or null if none.
 */
export function getProxyUrl(): string | null {
  return currentProxy;
}

/**
 * Proxy-aware fetch for the Electron main process.
 *
 * When a proxy is configured AND the target URL is not a loopback
 * address, the request is routed through a ProxyAgent. Otherwise
 * native fetch is used directly.
 */
export function proxyFetch(
  url: string | URL,
  init?: RequestInit,
): Promise<Response> {
  if (currentProxy && !isLoopback(url)) {
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

/**
 * True when `url` targets a loopback address (localhost / 127.x / ::1).
 * These requests must never traverse a proxy.
 */
function isLoopback(url: string | URL): boolean {
  try {
    const parsed = typeof url === "string" ? new URL(url) : url;
    const host = parsed.hostname;
    return (
      host === "localhost" ||
      host === "127.0.0.1" ||
      host === "::1" ||
      host === "[::1]"
    );
  } catch {
    return false;
  }
}
