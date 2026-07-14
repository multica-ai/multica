const ATTACHMENT_DOWNLOAD_PATH_RE =
  /^\/api\/attachments\/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\/download$/i;

interface PendingDownloadAuthorization {
  value: string;
  timeout: NodeJS.Timeout;
}

export function sanitizeBearerAuthorization(value: unknown): string | null {
  if (typeof value !== "string") return null;
  const trimmed = value.trim();
  if (!trimmed.startsWith("Bearer ")) return null;
  if (/[\r\n]/.test(trimmed)) return null;
  if (trimmed.length <= "Bearer ".length) return null;
  return trimmed;
}

function normalizeAttachmentDownloadURLForAuth(rawURL: string): string | null {
  try {
    const parsed = new URL(rawURL);
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      return null;
    }
    if (!ATTACHMENT_DOWNLOAD_PATH_RE.test(parsed.pathname)) return null;
    parsed.hash = "";
    return parsed.toString();
  } catch {
    return null;
  }
}

export class PendingDownloadAuthorizations {
  private readonly entries = new Map<string, PendingDownloadAuthorization>();

  register(rawURL: string, authorization: string): boolean {
    const key = normalizeAttachmentDownloadURLForAuth(rawURL);
    if (!key) return false;
    const existing = this.entries.get(key);
    if (existing) clearTimeout(existing.timeout);
    const timeout = setTimeout(() => {
      this.entries.delete(key);
    }, 30_000);
    timeout.unref?.();
    this.entries.set(key, { value: authorization, timeout });
    return true;
  }

  consume(rawURL: string): string | null {
    const key = normalizeAttachmentDownloadURLForAuth(rawURL);
    if (!key) return null;
    const pending = this.entries.get(key);
    if (!pending) return null;
    clearTimeout(pending.timeout);
    this.entries.delete(key);
    return pending.value;
  }
}
