import type { LocaleAdapter, SupportedLocale } from "./types";

export const LOCALE_COOKIE = "multica-locale";
const COOKIE_MAX_AGE = 60 * 60 * 24 * 365;

// Web-only adapter: persists via document.cookie so the Next.js proxy can
// read the active locale on the next request. Desktop has no server-side
// proxy and must use createDesktopLocaleAdapter (apps/desktop/.../i18n-adapter)
// which persists via window.localStorage instead.
export function createBrowserCookieLocaleAdapter(): LocaleAdapter {
  return {
    getUserChoice() {
      if (typeof document === "undefined") return null;
      const m = document.cookie.match(
        new RegExp(`(?:^|;\\s*)${LOCALE_COOKIE}=([^;]+)`),
      );
      const value = m?.[1];
      return value ? decodeURIComponent(value) : null;
    },
    getSystemPreferences() {
      if (typeof navigator === "undefined") return [];
      return [...navigator.languages];
    },
    persist(locale: SupportedLocale) {
      if (typeof document === "undefined") return;
      const secure =
        typeof location !== "undefined" && location.protocol === "https:"
          ? ";Secure"
          : "";
      document.cookie =
        `${LOCALE_COOKIE}=${encodeURIComponent(locale)};` +
        `path=/;max-age=${COOKIE_MAX_AGE};SameSite=Lax${secure}`;
    },
  };
}
