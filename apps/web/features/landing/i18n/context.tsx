"use client";

import { useMemo } from "react";
import {
  type Locale,
  LocaleProvider as CoreLocaleProvider,
  useLocale as useCoreLocale,
} from "@multica/core/i18n";
import { useConfigStore } from "@multica/core/config";
import { createEnDict } from "./en";
import { createZhTwDict } from "./zh-TW";
import type { LandingDict } from "./types";

const dictionaryFactories: Record<
  Locale,
  (allowSignup: boolean) => LandingDict
> = {
  en: createEnDict,
  "zh-TW": createZhTwDict,
};

// LocaleProvider re-export: landing pages can mount this provider; under the
// hood it is the global core provider so locale state is shared with the
// dashboard. Kept as a named export for backwards compatibility with existing
// landing-tree imports.
export function LocaleProvider({
  children,
  initialLocale,
}: {
  children: React.ReactNode;
  initialLocale?: Locale;
}) {
  return (
    <CoreLocaleProvider initialLocale={initialLocale}>
      {children}
    </CoreLocaleProvider>
  );
}

export function useLocale(): {
  locale: Locale;
  t: LandingDict;
  setLocale: (locale: Locale) => void;
} {
  const { locale, setLocale } = useCoreLocale();
  const allowSignup = useConfigStore((state) => state.allowSignup);
  const t = useMemo(
    () => dictionaryFactories[locale](allowSignup),
    [allowSignup, locale],
  );
  return { locale, t, setLocale };
}
