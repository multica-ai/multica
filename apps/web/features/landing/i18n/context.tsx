"use client";

import { createContext, useContext, useState, useCallback, useMemo } from "react";
import { useConfigStore } from "@multica/core/config";
import { LOCALE_COOKIE } from "@multica/core/i18n";
import { createEnDict } from "./en";
import { createZhDict } from "./zh";
import type { LandingDict, Locale } from "./types";

const dictionaryFactories: Record<Locale, (allowSignup: boolean) => LandingDict> = {
  en: createEnDict,
  zh: createZhDict,
};

const COOKIE_MAX_AGE = 60 * 60 * 24 * 365; // 1 year

type LocaleContextValue = {
  locale: Locale;
  t: LandingDict;
  setLocale: (locale: Locale) => void;
};

const LocaleContext = createContext<LocaleContextValue | null>(null);

export function LocaleProvider({
  children,
  initialLocale = "en",
}: {
  children: React.ReactNode;
  initialLocale?: Locale;
}) {
  const [locale, setLocaleState] = useState<Locale>(initialLocale);
  const allowSignup = useConfigStore((state) => state.allowSignup);
  const t = useMemo(
    () => dictionaryFactories[locale](allowSignup),
    [allowSignup, locale],
  );

  const setLocale = useCallback((l: Locale) => {
    setLocaleState(l);
    const secure =
      typeof location !== "undefined" && location.protocol === "https:"
        ? "; Secure"
        : "";
    document.cookie = `${LOCALE_COOKIE}=${l}; path=/; max-age=${COOKIE_MAX_AGE}; SameSite=Lax${secure}`;
  }, []);

  return (
    <LocaleContext.Provider
      value={{ locale, t, setLocale }}
    >
      {children}
    </LocaleContext.Provider>
  );
}

export function useLocale() {
  const ctx = useContext(LocaleContext);
  if (!ctx) throw new Error("useLocale must be used within LocaleProvider");
  return ctx;
}
