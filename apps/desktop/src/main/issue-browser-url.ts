import { isSafeExternalHttpUrl } from "./external-url";

export const DEFAULT_URL = "about:blank";

/** Address-bar input to a loadable URL, or null when the scheme is not allowed. */
export function normalizeUrl(input: string): string | null {
  const trimmed = input.trim();
  if (!trimmed) return null;
  if (/^(about:blank)$/i.test(trimmed)) return DEFAULT_URL;
  const hostLike = 
    /^(localhost|\d{1,3}(\.\d{1,3}){3})(:\d+)?(\/|$)/.test(trimmed) ||
    /^[a-z0-9-]+(\.[a-z0-9-]+)+:\d+(\/|$)/i.test(trimmed);
  const withScheme = hostLike
    ? `http://${trimmed}`
    : /^[a-z][a-z0-9+.-]*:/i.test(trimmed)
      ? trimmed
      : trimmed.includes(".") && !trimmed.includes(" ")
        ? `https://${trimmed}`
        : null;
  if (withScheme) return isSafeExternalHttpUrl(withScheme) ? withScheme : null;
  return `https://www.google.com/search?q=${encodeURIComponent(trimmed)}`;
}
