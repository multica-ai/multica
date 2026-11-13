"use client";

import {
  createContext,
  use,
  useCallback,
  useEffect,
  useMemo,
  useState,
} from "react";
import { usePathname, useRouter } from "next/navigation";
import { createBrowserCookieLocaleAdapter } from "@multica/core/i18n/browser";
import { HTML_LANG } from "@/lib/html-lang";
import { landingPathLocale, localizeHref, splitLandingLocale, stripLandingLocale } from "@/lib/landing-locale-path";
import { createLandingDict } from "./dictionary";
import type { LandingDict, Locale } from "./types";

type LocaleContextValue = {
  locale: Locale;
  t: LandingDict;
  /** Remember `locale` and move to the current page's URL in that language. */
  setLocale: (locale: Locale) => void;
};

const LocaleContext = createContext<LocaleContextValue | null>(null);

/** The locale a marketing URL renders in, given the locale on screen before it. */
function localeForPath(pathname: string, current: Locale): Locale {
  const prefixed = splitLandingLocale(pathname);
  if (prefixed) return prefixed.locale;
  return landingPathLocale(current) === "en" ? current : "en";
}

export function LocaleProvider({
  children,
  initialLocale = "en",
}: {
  children: React.ReactNode;
  initialLocale?: Locale;
}) {
  const [locale, setLocaleState] = useState<Locale>(initialLocale);
  const router = useRouter();
  const pathname = usePathname();
  const localeAdapter = useMemo(() => createBrowserCookieLocaleAdapter(), []);
  const t = useMemo(() => createLandingDict(locale), [locale]);

  // The layout holding this provider survives client navigations, so a move
  // between /download and /zh/download (back and forward included) re-reads
  // the language from the address bar.
  useEffect(() => {
    setLocaleState((current) => localeForPath(window.location.pathname, current));
  }, [pathname]);

  useEffect(() => {
    document.documentElement.lang = HTML_LANG[locale];
  }, [locale]);

  const setLocale = useCallback(
    (l: Locale) => {
      if (l === locale) return;
      setLocaleState(l);
      localeAdapter.persist(l);
      const { pathname: current, search, hash } = window.location;
      router.push(localizeHref(l, stripLandingLocale(current)) + search + hash);
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

/** The marketing-site locale when rendered inside it, null elsewhere. */
export function useOptionalLocale(): Locale | null {
  return use(LocaleContext)?.locale ?? null;
}
