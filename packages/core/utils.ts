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

/**
 * Truncate `text` to at most `maxLength` code points, appending an ellipsis
 * (`…`, U+2026) when it is longer. Text at or under the limit is returned
 * unchanged.
 *
 * The ellipsis counts toward the limit, so a truncated result is at most
 * `maxLength` code points: the visible text is sliced to `maxLength - 1` and
 * has trailing whitespace trimmed before the ellipsis is appended. The bound
 * is measured in Unicode code points, not UTF-16 code units, so an emoji is
 * never cut into a lone surrogate — a returned string may therefore exceed
 * `maxLength` in `.length` (code units) while staying within `maxLength`
 * visible characters. When `maxLength` leaves no room for visible text, only
 * the ellipsis is returned; `maxLength <= 0` returns the empty string.
 */
export function truncateWithEllipsis(text: string, maxLength: number): string {
  // Fast path on code units: may fail to return early on astral text, but
  // never truncates wrongly — the code-point check below is authoritative.
  if (text.length <= maxLength) {
    return text;
  }
  if (maxLength <= 1) {
    return maxLength <= 0 ? "" : "…";
  }
  const chars = Array.from(text);
  if (chars.length <= maxLength) {
    return text;
  }
  return `${chars.slice(0, maxLength - 1).join("").trimEnd()}…`;
}
