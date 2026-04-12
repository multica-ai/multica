export function timeAgo(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr).getTime();
  const minutes = Math.floor(diff / 60000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

export function generateUUID(): string {
  const cryptoObj = globalThis.crypto;

  if (!cryptoObj?.getRandomValues) {
    throw new Error("Secure UUID generation requires crypto.getRandomValues");
  }

  const bytes = new Uint8Array(16);
  cryptoObj.getRandomValues(bytes);

  bytes[6] = ((bytes[6] ?? 0) & 0x0f) | 0x40; // version 4
  bytes[8] = ((bytes[8] ?? 0) & 0x3f) | 0x80; // variant 1

  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");

  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

/**
 * Generate an id that prefers crypto.randomUUID but falls back in non-secure contexts.
 */
export function createSafeId(): string {
  const cryptoObj = globalThis.crypto;

  if (cryptoObj?.randomUUID) {
    try {
      return cryptoObj.randomUUID();
    } catch {
      // Fall through to fallback.
    }
  }

  return generateUUID();
}

/** Request id helper used for logs/tracing headers. */
export function createRequestId(length = 8): string {
  return createSafeId().replace(/-/g, "").slice(0, length);
}
