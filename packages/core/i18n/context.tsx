"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import {
  DEFAULT_LOCALE,
  isLocale,
  LOCALE_COOKIE_MAX_AGE,
  LOCALE_COOKIE_NAME,
  type Locale,
  resolveLocale,
} from "./types";

function readLocaleCookie(): Locale | null {
  if (typeof document === "undefined") return null;
  const match = document.cookie.match(
    new RegExp(`(?:^|;\\s*)${LOCALE_COOKIE_NAME}=([^;]+)`),
  );
  const raw = match?.[1];
  if (!raw) return null;
  const value = decodeURIComponent(raw);
  return isLocale(value) ? value : null;
}

type LocaleContextValue = {
  locale: Locale;
  setLocale: (locale: Locale) => void;
};

const LocaleContext = createContext<LocaleContextValue | null>(null);

export function LocaleProvider({
  children,
  initialLocale,
}: {
  children: React.ReactNode;
  initialLocale?: Locale;
}) {
  const [locale, setLocaleState] = useState<Locale>(
    resolveLocale(initialLocale ?? DEFAULT_LOCALE),
  );

  useEffect(() => {
    const cookieLocale = readLocaleCookie();
    if (cookieLocale && cookieLocale !== locale) {
      setLocaleState(cookieLocale);
    }
    // Run once on mount to hydrate from cookie. Subsequent updates flow
    // through setLocale, which writes the cookie itself.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const setLocale = useCallback((next: Locale) => {
    setLocaleState(next);
    if (typeof document !== "undefined") {
      document.cookie = `${LOCALE_COOKIE_NAME}=${next}; path=/; max-age=${LOCALE_COOKIE_MAX_AGE}; SameSite=Lax`;
      document.documentElement.lang = next;
    }
  }, []);

  const value = useMemo(() => ({ locale, setLocale }), [locale, setLocale]);

  return (
    <LocaleContext.Provider value={value}>{children}</LocaleContext.Provider>
  );
}

export function useLocale(): LocaleContextValue {
  const ctx = useContext(LocaleContext);
  if (!ctx) {
    return {
      locale: DEFAULT_LOCALE,
      setLocale: () => {},
    };
  }
  return ctx;
}

export function useDict<TDict>(
  factories: Record<Locale, () => TDict>,
): TDict {
  const { locale } = useLocale();
  return useMemo(() => factories[locale](), [factories, locale]);
}
