import type { ReactNode } from "react";
import { createContext, useCallback, useContext, useMemo, useState } from "react";
import i18next, { type i18n as I18n } from "i18next";
import { I18nextProvider, initReactI18next } from "react-i18next";
import { matchLocale, pickLocale, type SupportedLocale } from "@multica/core/i18n";
import { mobileStorage } from "../platform/storage";
import { MOBILE_RESOURCES } from "./resources";

const STORAGE_KEY = "multica-locale";

type MobileLocaleContextValue = {
  locale: SupportedLocale;
  setLocale: (locale: SupportedLocale) => void;
};

const MobileLocaleContext = createContext<MobileLocaleContextValue | null>(null);

function getSystemPreferences() {
  const locale = Intl.DateTimeFormat().resolvedOptions().locale;
  return locale ? [locale] : [];
}

export function getInitialMobileLocale(): SupportedLocale {
  return pickLocale({
    getUserChoice: () => mobileStorage.getItem(STORAGE_KEY),
    getSystemPreferences,
    persist: (locale) => mobileStorage.setItem(STORAGE_KEY, locale),
  });
}

export function persistMobileLocale(locale: SupportedLocale) {
  mobileStorage.setItem(STORAGE_KEY, locale);
}

export function getPersistedMobileLocale() {
  return mobileStorage.getItem(STORAGE_KEY);
}

export function normalizeMobileLocale(locale: string): SupportedLocale {
  return matchLocale([locale]);
}

export function MobileLocaleProvider({
  children,
  locale,
  setLocale,
}: {
  children: ReactNode;
  locale: SupportedLocale;
  setLocale: (locale: SupportedLocale) => void;
}) {
  const persistAndSetLocale = useCallback(
    (next: SupportedLocale) => {
      persistMobileLocale(next);
      setLocale(next);
    },
    [setLocale],
  );

  const value = useMemo(
    () => ({ locale, setLocale: persistAndSetLocale }),
    [locale, persistAndSetLocale],
  );

  return (
    <MobileLocaleContext.Provider value={value}>
      {children}
    </MobileLocaleContext.Provider>
  );
}

export function useMobileLocale() {
  const ctx = useContext(MobileLocaleContext);
  if (!ctx) {
    throw new Error("useMobileLocale must be used within MobileLocaleProvider");
  }
  return ctx;
}

function createMobileI18n(locale: SupportedLocale): I18n {
  const instance = i18next.createInstance();
  instance.use(initReactI18next).init({
    lng: locale,
    fallbackLng: "en",
    resources: getMobileI18nResources(),
    interpolation: { escapeValue: false },
    initAsync: false,
    react: { useSuspense: false },
  });
  return instance;
}

export function MobileI18nProvider({ children }: { children: ReactNode }) {
  const { locale } = useMobileLocale();
  const [instance] = useState(() => createMobileI18n(locale));

  return <I18nextProvider i18n={instance}>{children}</I18nextProvider>;
}

export function getMobileI18nResources() {
  return {
    en: { translation: MOBILE_RESOURCES.en },
    "zh-Hans": { translation: MOBILE_RESOURCES["zh-Hans"] },
  };
}
