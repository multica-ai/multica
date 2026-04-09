"use client";

import { createContext, useContext, useState, useCallback, useEffect } from "react";
import { en } from "./en";
import { zh } from "./zh";
import type { DashboardDict, Locale } from "./types";

const dictionaries: Record<Locale, DashboardDict> = { en, zh };

const COOKIE_NAME = "multica-locale";
const COOKIE_MAX_AGE = 60 * 60 * 24 * 365; // 1 year

function readLocaleCookie(): Locale {
  if (typeof document === "undefined") return "en";
  const match = document.cookie.match(/(?:^|;\s*)multica-locale=(\w+)/);
  const val = match?.[1];
  return val === "zh" ? "zh" : "en";
}

type DashboardLocaleContextValue = {
  locale: Locale;
  t: DashboardDict;
  setLocale: (locale: Locale) => void;
};

const DashboardLocaleContext = createContext<DashboardLocaleContextValue | null>(null);

export function DashboardLocaleProvider({
  children,
}: {
  children: React.ReactNode;
}) {
  const [locale, setLocaleState] = useState<Locale>("en");

  // Sync from cookie on mount (avoids SSR mismatch)
  useEffect(() => {
    setLocaleState(readLocaleCookie());
  }, []);

  const setLocale = useCallback((l: Locale) => {
    setLocaleState(l);
    document.cookie = `${COOKIE_NAME}=${l}; path=/; max-age=${COOKIE_MAX_AGE}; SameSite=Lax`;
  }, []);

  return (
    <DashboardLocaleContext.Provider
      value={{ locale, t: dictionaries[locale], setLocale }}
    >
      {children}
    </DashboardLocaleContext.Provider>
  );
}

export function useDashboardLocale() {
  const ctx = useContext(DashboardLocaleContext);
  if (!ctx) throw new Error("useDashboardLocale must be used within DashboardLocaleProvider");
  return ctx;
}
