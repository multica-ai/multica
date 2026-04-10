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
  formatDate: (dateStr: string) => string;
  formatDateTime: (dateStr: string) => string;
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

  const formatDate = useCallback((dateStr: string) => {
    const date = new Date(dateStr);
    if (locale === "zh") {
      const y = date.getFullYear();
      const m = date.getMonth() + 1;
      const d = date.getDate();
      return `${y}年${m}月${d}日`;
    }
    return date.toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" });
  }, [locale]);

  const formatDateTime = useCallback((dateStr: string) => {
    const date = new Date(dateStr);
    if (locale === "zh") {
      const y = date.getFullYear();
      const m = date.getMonth() + 1;
      const d = date.getDate();
      const h = String(date.getHours()).padStart(2, "0");
      const min = String(date.getMinutes()).padStart(2, "0");
      return `${y}年${m}月${d}日 ${h}:${min}`;
    }
    return date.toLocaleString("en-US", { month: "short", day: "numeric", year: "numeric", hour: "2-digit", minute: "2-digit" });
  }, [locale]);

  return (
    <DashboardLocaleContext.Provider
      value={{ locale, t: dictionaries[locale], setLocale, formatDate, formatDateTime }}
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
