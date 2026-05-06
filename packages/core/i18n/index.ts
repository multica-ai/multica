// Server-safe i18n entry: zero React imports + zero DOM/document/navigator
// access anywhere in this transitive graph. Safe to import from proxy.ts /
// RSC / Edge / nodejs middleware.
//
// React-side helpers (I18nProvider, useLocaleAdapter, createI18n) live in
// "@multica/core/i18n/react" — split because Next.js gives RSC a vendored
// React build that lacks createContext, and react-i18next's top-level
// React.createContext() call would crash any non-client load of this file.
//
// Browser-only helpers (createBrowserCookieLocaleAdapter) live in
// "@multica/core/i18n/browser" — they read document.cookie / navigator.languages
// at construction time and would crash in any non-DOM context.
export type {
  LocaleAdapter,
  LocaleResources,
  SupportedLocale,
} from "./types";
export { DEFAULT_LOCALE, SUPPORTED_LOCALES } from "./types";
export { matchLocale, pickLocale } from "./pick-locale";
export { LOCALE_COOKIE } from "./browser-cookie-adapter";
