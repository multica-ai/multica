"use client";

import { useEffect, useState, type ReactNode } from "react";
import { I18nextProvider } from "react-i18next";
import { createI18n } from "./create-i18n";
import type { LocaleResources, SupportedLocale } from "./types";

export interface I18nProviderProps {
  locale: SupportedLocale;
  resources: Record<string, LocaleResources>;
  children: ReactNode;
}

const HTML_LANG: Record<SupportedLocale, string> = {
  en: "en",
  "zh-Hans": "zh-CN",
};

function setDocumentLanguage(locale: string) {
  if (typeof document === "undefined") return;
  const lang = HTML_LANG[locale as SupportedLocale];
  if (lang) document.documentElement.lang = lang;
}

export function I18nProvider({
  locale,
  resources,
  children,
}: I18nProviderProps) {
  // Lazy init via useState so the instance survives re-renders.
  // Locale + resources are determined at boot; preloaded resources allow
  // language switching to update i18next without a page reload.
  const [instance] = useState(() => createI18n(locale, resources));

  useEffect(() => {
    setDocumentLanguage(instance.language);
    instance.on("languageChanged", setDocumentLanguage);
    return () => {
      instance.off("languageChanged", setDocumentLanguage);
    };
  }, [instance]);

  return <I18nextProvider i18n={instance}>{children}</I18nextProvider>;
}
