export type Locale = "en" | "zh-TW";

export const locales: Locale[] = ["en", "zh-TW"];

export const DEFAULT_LOCALE: Locale = "en";

export const LOCALE_COOKIE_NAME = "multica-locale";

export const LOCALE_COOKIE_MAX_AGE = 60 * 60 * 24 * 365;

export const localeLabels: Record<Locale, string> = {
  en: "EN",
  "zh-TW": "繁體中文",
};

export function isLocale(value: unknown): value is Locale {
  return typeof value === "string" && (locales as string[]).includes(value);
}

export function resolveLocale(value: unknown): Locale {
  return isLocale(value) ? value : DEFAULT_LOCALE;
}
