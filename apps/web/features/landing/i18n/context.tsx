"use client";

import {
  createContext,
  use,
  useCallback,
  useMemo,
  useState,
  useTransition,
} from "react";
import { useRouter } from "next/navigation";
import { useConfigStore } from "@multica/core/config";
import { createBrowserCookieLocaleAdapter } from "@multica/core/i18n/browser";
import { createEnDict } from "./en";
import { createZhDict } from "./zh";
import {
  toLandingDictionaryLocale,
  type LandingDict,
  type LandingDictionaryLocale,
  type Locale,
} from "./types";

const dictionaryFactories: Record<
  LandingDictionaryLocale,
  (allowSignup: boolean) => LandingDict
> = {
  en: createEnDict,
  zh: createZhDict,
};

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
  const [, startTransition] = useTransition();
  const router = useRouter();
  const localeAdapter = useMemo(() => createBrowserCookieLocaleAdapter(), []);
  const allowSignup = useConfigStore((state) => state.allowSignup);
  const t = useMemo(
    () => dictionaryFactories[toLandingDictionaryLocale(locale)](allowSignup),
    [allowSignup, locale],
  );

  const setLocale = useCallback(
    (l: Locale) => {
      if (l === locale) return;
      setLocaleState(l);
      localeAdapter.persist(l);
      startTransition(() => {
        router.refresh();
      });
    },
    [locale, localeAdapter, router, startTransition],
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
