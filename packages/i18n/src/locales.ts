// Locale registry. Add new locales here as they're translated.
// All consumers (apps, codemods, scripts) must reference SUPPORTED_LOCALES
// rather than hardcoding strings.

export const SUPPORTED_LOCALES = ["zh-CN", "en"] as const;
export type Locale = (typeof SUPPORTED_LOCALES)[number];

export const DEFAULT_LOCALE: Locale = "zh-CN";
export const FALLBACK_LOCALE: Locale = "en";

export const LOCALE_COOKIE = "multica-locale";

export function isLocale(value: unknown): value is Locale {
  return typeof value === "string" && (SUPPORTED_LOCALES as readonly string[]).includes(value);
}

export function normalizeLocale(value: string | null | undefined): Locale {
  if (!value) return DEFAULT_LOCALE;
  if (isLocale(value)) return value;
  // Tolerate short forms ("zh" -> "zh-CN") so cookies set by the existing
  // LocaleSync component continue to work.
  const lower = value.toLowerCase();
  if (lower === "zh" || lower.startsWith("zh-")) return "zh-CN";
  if (lower === "en" || lower.startsWith("en-")) return "en";
  return DEFAULT_LOCALE;
}
