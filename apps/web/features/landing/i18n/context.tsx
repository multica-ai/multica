"use client";

import {
  createContext,
  use,
  useCallback,
  useEffect,
  useMemo,
  useState,
  useTransition,
} from "react";
import { useRouter } from "next/navigation";
import { useConfigStore } from "@multica/core/config";
import { createBrowserCookieLocaleAdapter } from "@multica/core/i18n/browser";
import { loadLandingDict } from "./load-dict";
import type { LandingDict, Locale } from "./types";

type LocaleContextValue = {
  locale: Locale;
  t: LandingDict;
  setLocale: (locale: Locale) => void;
};

const LocaleContext = createContext<LocaleContextValue | null>(null);

export function LocaleProvider({
  children,
  initialLocale = "en",
  initialDict,
}: {
  children: React.ReactNode;
  initialLocale?: Locale;
  initialDict: LandingDict;
}) {
  const [locale, setLocaleState] = useState<Locale>(initialLocale);
  const [t, setT] = useState<LandingDict>(initialDict);
  const [, startTransition] = useTransition();
  const router = useRouter();
  const localeAdapter = useMemo(() => createBrowserCookieLocaleAdapter(), []);
  const allowSignup = useConfigStore((state) => state.allowSignup);

  useEffect(() => {
    setT(initialDict);
  }, [initialDict]);

  useEffect(() => {
    if (locale === initialLocale && allowSignup === true) return;
    let cancelled = false;
    void loadLandingDict(locale, allowSignup).then((next) => {
      if (!cancelled) setT(next);
    });
    return () => {
      cancelled = true;
    };
  }, [allowSignup, initialLocale, locale]);

  const setLocale = useCallback(
    (l: Locale) => {
      if (l === locale) return;
      setLocaleState(l);
      localeAdapter.persist(l);
      startTransition(() => {
        router.refresh();
      });
    },
    [locale, localeAdapter, router],
  );

  return (
    <LocaleContext.Provider value={{ locale, t, setLocale }}>
      {children}
    </LocaleContext.Provider>
  );
}

export function useLocale() {
  const ctx = use(LocaleContext);
  if (!ctx) throw new Error("useLocale must be used within LocaleProvider");
  return ctx;
}
