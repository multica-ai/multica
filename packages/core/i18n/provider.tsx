"use client";

import { useEffect, useRef, useState, type ReactNode } from "react";
import { I18nextProvider } from "react-i18next";
import { createI18n } from "./create-i18n";
import type { LocaleResources, SupportedLocale } from "./types";

export interface I18nProviderProps {
  locale: SupportedLocale;
  resources: Record<string, LocaleResources>;
  children: ReactNode;
}

export function I18nProvider({
  locale,
  resources,
  children,
}: I18nProviderProps) {
  const [instance] = useState(() => createI18n(locale, resources));
  const previous = useRef({ locale, resources });

  // Fast Refresh can update locale JSON without remounting this provider.
  // Synchronize changed bundles into the live instance, then emit the event
  // react-i18next subscribes to so newly added translations replace raw keys.
  useEffect(() => {
    if (
      previous.current.locale === locale &&
      previous.current.resources === resources
    ) {
      return;
    }

    for (const [language, namespaces] of Object.entries(resources)) {
      for (const [namespace, bundle] of Object.entries(namespaces)) {
        instance.addResourceBundle(
          language,
          namespace,
          bundle,
          true,
          true,
        );
      }
    }

    if (instance.language !== locale) {
      void instance.changeLanguage(locale);
    } else {
      instance.emit("languageChanged", locale);
    }
    previous.current = { locale, resources };
  }, [instance, locale, resources]);

  return <I18nextProvider i18n={instance}>{children}</I18nextProvider>;
}
