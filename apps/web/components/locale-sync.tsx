"use client";

import { useEffect } from "react";
import { LOCALE_COOKIE, normalizeLocale } from "@multica/i18n";

/**
 * Reads the locale cookie on the client and reconciles &lt;html lang&gt; with
 * the user's preferred locale.
 *
 * Why client-side: calling cookies() in the root Server Component layout
 * marks the entire app as dynamic and disables the Router Cache. PR #1
 * keeps the SSR locale fixed; per-user locale lands later via account
 * profile, at which point the cookie path here will be replaced.
 */
export function LocaleSync() {
  useEffect(() => {
    const cookieRegex = new RegExp(`(?:^|;\\s*)${LOCALE_COOKIE}=([^;]+)`);
    const match = document.cookie.match(cookieRegex);
    const cookieValue = match?.[1];
    if (!cookieValue) return;
    const locale = normalizeLocale(decodeURIComponent(cookieValue));
    if (document.documentElement.lang !== locale) {
      document.documentElement.lang = locale;
    }
  }, []);

  return null;
}
