const ATTACHMENT_DOWNLOAD_PATH_RE =
  /^\/api\/attachments\/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\/download$/i;

const DEFAULT_AUTHORIZATION_TTL_MS = 30_000;

interface PendingAuthorization {
  authorization: string;
  timeout: ReturnType<typeof setTimeout>;
}

interface ActiveAuthorization {
  timeout: ReturnType<typeof setTimeout>;
}

export interface DownloadRequestDetails {
  id: number;
  url: string;
  webContentsId?: number;
  requestHeaders: Record<string, string>;
}

export type DownloadAuthorizationResult =
  | "attached"
  | "stripped"
  | "unchanged";

export function sanitizeBearerAuthorization(value: unknown): string | null {
  if (typeof value !== "string") return null;
  const trimmed = value.trim();
  if (trimmed.length > 8_192) return null;
  if (!/^Bearer [\x21-\x7e]+$/.test(trimmed)) return null;
  return trimmed;
}

function normalizeHTTPURL(rawURL: unknown): string | null {
  if (typeof rawURL !== "string") return null;
  try {
    const parsed = new URL(rawURL);
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      return null;
    }
    if (parsed.username || parsed.password) return null;
    parsed.hash = "";
    return parsed.toString();
  } catch {
    return null;
  }
}

function normalizeTrustedAttachmentDownloadURL(
  rawURL: unknown,
  trustedAPIBaseURL: string | null,
): string | null {
  const normalizedURL = normalizeHTTPURL(rawURL);
  const normalizedBaseURL = normalizeHTTPURL(trustedAPIBaseURL);
  if (!normalizedURL || !normalizedBaseURL) return null;

  const candidate = new URL(normalizedURL);
  const apiBase = new URL(normalizedBaseURL);
  if (candidate.origin !== apiBase.origin) return null;

  const basePath = apiBase.pathname.replace(/\/+$/, "");
  const expectedPrefix = `${basePath}/api/attachments/`;
  if (!candidate.pathname.startsWith(expectedPrefix)) return null;

  const relativePath = candidate.pathname.slice(basePath.length);
  if (!ATTACHMENT_DOWNLOAD_PATH_RE.test(relativePath)) return null;
  return candidate.toString();
}

function setAuthorizationHeader(
  headers: Record<string, string>,
  authorization: string,
): void {
  const names = Object.keys(headers).filter(
    (name) => name.toLowerCase() === "authorization",
  );
  const [firstName, ...duplicateNames] = names;
  if (!firstName) {
    headers.Authorization = authorization;
    return;
  }
  headers[firstName] = authorization;
  for (const name of duplicateNames) headers[name] = "";
}

function stripAuthorizationHeader(headers: Record<string, string>): void {
  // Electron/Chromium can crash when a custom header inherited by a redirect
  // is deleted in onBeforeSendHeaders. An empty value is omitted on the wire.
  for (const name of Object.keys(headers)) {
    if (name.toLowerCase() === "authorization") headers[name] = "";
  }
}

function armTimeout(
  callback: () => void,
  ttlMs: number,
): ReturnType<typeof setTimeout> {
  const timeout = setTimeout(callback, ttlMs);
  timeout.unref?.();
  return timeout;
}

function pendingKey(webContentsId: number, url: string): string {
  return `${webContentsId}\n${url}`;
}

/**
 * Hands one bearer header from renderer IPC to one native attachment request.
 *
 * Electron carries custom download headers across redirects, including to a
 * different origin. Pending auth is bound to the source WebContents and exact
 * URL. The first matching request consumes it; later requests with the same
 * request id are treated as redirects and have Authorization cleared before
 * they leave the machine.
 */
export class PendingDownloadAuthorizations {
  private readonly pending = new Map<string, PendingAuthorization>();
  private readonly active = new Map<number, ActiveAuthorization>();

  constructor(private readonly ttlMs = DEFAULT_AUTHORIZATION_TTL_MS) {}

  register(
    rawURL: unknown,
    rawAuthorization: unknown,
    trustedAPIBaseURL: string | null,
    webContentsId: number,
  ): boolean {
    const url = normalizeTrustedAttachmentDownloadURL(
      rawURL,
      trustedAPIBaseURL,
    );
    const authorization = sanitizeBearerAuthorization(rawAuthorization);
    if (!url || !authorization || !Number.isSafeInteger(webContentsId)) {
      return false;
    }

    const key = pendingKey(webContentsId, url);
    this.deletePending(key);
    const timeout = armTimeout(() => this.pending.delete(key), this.ttlMs);
    this.pending.set(key, { authorization, timeout });
    return true;
  }

  apply(details: DownloadRequestDetails): DownloadAuthorizationResult {
    const active = this.active.get(details.id);
    if (active) {
      clearTimeout(active.timeout);
      active.timeout = armTimeout(
        () => this.active.delete(details.id),
        this.ttlMs,
      );
      stripAuthorizationHeader(details.requestHeaders);
      return "stripped";
    }

    const url = normalizeHTTPURL(details.url);
    if (
      !url ||
      typeof details.webContentsId !== "number" ||
      !Number.isSafeInteger(details.webContentsId)
    ) {
      return "unchanged";
    }
    const key = pendingKey(details.webContentsId, url);
    const pending = this.pending.get(key);
    if (!pending) return "unchanged";

    clearTimeout(pending.timeout);
    this.pending.delete(key);
    setAuthorizationHeader(details.requestHeaders, pending.authorization);

    const timeout = armTimeout(() => this.active.delete(details.id), this.ttlMs);
    this.active.set(details.id, { timeout });
    return "attached";
  }

  finish(requestId: number): void {
    const active = this.active.get(requestId);
    if (!active) return;
    clearTimeout(active.timeout);
    this.active.delete(requestId);
  }

  clear(): void {
    for (const entry of this.pending.values()) clearTimeout(entry.timeout);
    for (const entry of this.active.values()) clearTimeout(entry.timeout);
    this.pending.clear();
    this.active.clear();
  }

  private deletePending(key: string): void {
    const existing = this.pending.get(key);
    if (!existing) return;
    clearTimeout(existing.timeout);
    this.pending.delete(key);
  }
}
