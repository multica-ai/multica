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

/**
 * True when the keyboard event fires while an IME is composing a multi-key
 * input (e.g. Chinese pinyin, Japanese kana). The Enter that commits the
 * composition must NOT trigger submit/send/create handlers.
 *
 * Accepts both React synthetic events and native DOM `KeyboardEvent`s.
 *
 * Why both `isComposing` and `keyCode === 229`:
 * - `isComposing` is the standard signal but Safari clears it on the keydown
 *   that ends composition, so a bare check misses the very Enter that submits.
 * - During composition the browser reports `keyCode === 229` regardless of
 *   the actual key, which keeps working in Safari's edge case.
 *
 * Always read from `nativeEvent` when present — React's synthetic event is
 * normalized but the native event reflects the browser's real state.
 */
export function isImeComposing(event: {
  isComposing?: boolean;
  keyCode?: number;
  nativeEvent?: { isComposing?: boolean; keyCode?: number };
}): boolean {
  const e = event.nativeEvent ?? event;
  return Boolean(e.isComposing) || e.keyCode === 229;
}
