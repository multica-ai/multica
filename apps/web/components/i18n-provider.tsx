"use client";

import { useMemo, useCallback } from "react";
import { I18nProvider } from "@multica/i18n/react";
import { en } from "@multica/i18n/dict/en";
import { zh } from "@multica/i18n/dict/zh";
import type { Locale } from "@multica/i18n/types";
import { localeStore } from "@multica/core/i18n";
const dictionaries = { en, zh };

function getInitialLocale(): Locale {
  // Zustand store is persisted to localStorage — use it as primary source
  const stored = localeStore.getState().locale;
  if (stored && stored !== "en") return stored;
  // Fallback: check cookie (covers users who set locale before the Zustand store existed)
  if (typeof document !== "undefined") {
    const match = document.cookie.match(/(?:^|;\s*)multica-locale=(\w+)/);
    if (match?.[1] === "zh") return "zh";
  }
  return "en";
}

export function AppI18nProvider({ children }: { children: React.ReactNode }) {
  const initialLocale = useMemo(() => getInitialLocale(), []);

  const handleLocaleChange = useCallback((locale: Locale) => {
    // Sync to Zustand store so API client sends X-Locale header
    localeStore.getState().setLocale(locale);
    // Sync to cookie for server-side reads (landing page, SSR)
    document.cookie = `multica-locale=${locale}; path=/; max-age=${60 * 60 * 24 * 365}; SameSite=Lax`;
    // Sync <html lang>
    document.documentElement.lang = locale;
  }, []);

  return (
    <I18nProvider
      initialLocale={initialLocale}
      dictionaries={dictionaries}
      onLocaleChange={handleLocaleChange}
    >
      {children}
    </I18nProvider>
  );
}
