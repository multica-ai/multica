// Server-safe i18n entry: locale matching and constants only.
export type { SupportedLocale } from "./types";
export { DEFAULT_LOCALE, SUPPORTED_LOCALES } from "./types";
export { LOCALE_COOKIE } from "./constants";
export { matchLocale } from "./pick-locale";
