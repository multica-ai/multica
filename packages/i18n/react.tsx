"use client";

import {
  createContext,
  useContext,
  useState,
  useCallback,
  useMemo,
  type ReactNode,
} from "react";
import {
  createTranslator,
  type DictRegistry,
} from "./core";
import type { InterpolationParams, Locale } from "./types";

type TranslatorFn = (key: string, params?: InterpolationParams) => string;

type RawTranslatorFn = (
  namespace: string,
  key: string,
  params?: InterpolationParams,
) => string;

type I18nContextValue = {
  locale: Locale;
  setLocale: (locale: Locale) => void;
  t: RawTranslatorFn;
};

const I18nContext = createContext<I18nContextValue | null>(null);

export type I18nProviderProps = {
  children: ReactNode;
  initialLocale?: Locale;
  dictionaries: DictRegistry;
  onLocaleChange?: (locale: Locale) => void;
};

export function I18nProvider({
  children,
  initialLocale = "en",
  dictionaries,
  onLocaleChange,
}: I18nProviderProps) {
  const [locale, setLocaleState] = useState<Locale>(initialLocale);

  const t = useMemo(
    () => createTranslator(locale, dictionaries),
    [locale, dictionaries],
  );

  const setLocale = useCallback(
    (l: Locale) => {
      setLocaleState(l);
      onLocaleChange?.(l);
    },
    [onLocaleChange],
  );

  return (
    <I18nContext.Provider value={{ locale, setLocale, t }}>
      {children}
    </I18nContext.Provider>
  );
}

export function useT(namespace?: string): TranslatorFn {
  const ctx = useContext(I18nContext);
  if (!ctx) throw new Error("useT must be used within I18nProvider");

  // eslint-disable-next-line react-hooks/rules-of-hooks
  return useMemo(() => {
    if (namespace) {
      // Create a stable namespaced translator that captures the current `t`
      return (key: string, params?: InterpolationParams) =>
        ctx.t(namespace, key, params);
    }
    return (key: string, params?: InterpolationParams) =>
      ctx.t("__global__", key, params);
  }, [ctx.t, namespace]);
}

export function useLocale() {
  const ctx = useContext(I18nContext);
  if (!ctx) throw new Error("useLocale must be used within I18nProvider");
  return { locale: ctx.locale, setLocale: ctx.setLocale };
}
