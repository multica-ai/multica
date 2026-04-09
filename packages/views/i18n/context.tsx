"use client";

import { createContext, useContext, useState, useCallback } from "react";
import { en } from "./en";
import { zh } from "./zh";
import type { AppDict, Locale } from "./types";

const dictionaries: Record<Locale, AppDict> = { en, zh };

const COOKIE_NAME = "multica-locale";
const COOKIE_MAX_AGE = 60 * 60 * 24 * 365; // 1 year

type AppLocaleContextValue = {
  locale: Locale;
  t: AppDict;
  setLocale: (locale: Locale) => void;
};

const AppLocaleContext = createContext<AppLocaleContextValue | null>(null);

function readLocaleCookie(): Locale {
  if (typeof document === "undefined") return "en";
  const match = document.cookie.match(/(?:^|;\s*)multica-locale=(\w+)/);
  const value = match?.[1];
  if (value === "zh") return "zh";
  return "en";
}

export function AppLocaleProvider({
  children,
}: {
  children: React.ReactNode;
}) {
  const [locale, setLocaleState] = useState<Locale>(readLocaleCookie);

  const setLocale = useCallback((l: Locale) => {
    setLocaleState(l);
    document.cookie = `${COOKIE_NAME}=${l}; path=/; max-age=${COOKIE_MAX_AGE}; SameSite=Lax`;
  }, []);

  return (
    <AppLocaleContext.Provider
      value={{ locale, t: dictionaries[locale], setLocale }}
    >
      {children}
    </AppLocaleContext.Provider>
  );
}

export function useAppLocale(): AppLocaleContextValue {
  const ctx = useContext(AppLocaleContext);
  if (!ctx)
    throw new Error("useAppLocale must be used within AppLocaleProvider");
  return ctx;
}
