"use client";

import { useState, type ReactNode } from "react";
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
  // Lazy init via useState so the instance survives re-renders.
  // Locale + resources are determined at boot, while the active language can
  // switch in-place via i18n.changeLanguage because every supported locale is
  // already present in resources.
  const [instance] = useState(() => createI18n(locale, resources));
  return <I18nextProvider i18n={instance}>{children}</I18nextProvider>;
}
