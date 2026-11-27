import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import * as SecureStore from "expo-secure-store";
import * as Localization from "expo-localization";
import { I18nextProvider, useTranslation } from "react-i18next";

import { i18n } from "./singleton";
import { DEFAULT_LOCALE, SUPPORTED_LOCALES, type SupportedLocale } from "./types";
import "./resources-types";

const LOCALE_STORAGE_KEY = "mobile.locale_preference";
const SYSTEM_LOCALE = matchSupportedLocale(systemLocaleCandidates());

export type LocalePreference = SupportedLocale | "system";

function normalizeLanguage(value?: string | null): string | null {
  if (!value) return null;
  return value.replace(/_/g, "-");
}

function systemLocaleCandidates(): string[] {
  return (Localization.getLocales() ?? [])
    .map((locale) => normalizeLanguage(locale.languageTag))
    .filter((tag): tag is string => !!tag);
}

function matchSupportedLocale(candidates: string[]): SupportedLocale {
  for (const candidate of candidates) {
    if (!candidate) continue;
    const lowered = candidate.toLowerCase();
    if (
      lowered.startsWith("zh-hant") ||
      ["zh-tw", "zh-hk", "zh-mo"].includes(lowered)
    ) {
      return "zh-Hant";
    }
    if (lowered.startsWith("zh")) return "zh-Hans";
    if (lowered.split("-")[0] === "ru") return "ru";
    if (lowered.startsWith("en")) return "en";
  }
  return DEFAULT_LOCALE;
}

// The singleton initializes to English so pure helpers are safe before mount.
// Apply the device language once React Native modules are available.
void i18n.changeLanguage(SYSTEM_LOCALE);

type LocalePreferenceContextValue = {
  preference: LocalePreference;
  setPreference: (preference: LocalePreference) => void;
};

const LocalePreferenceContext = createContext<LocalePreferenceContextValue>({
  preference: "system",
  setPreference: () => {},
});

export function MobileI18nProvider({ children }: { children: ReactNode }) {
  const [preference, setPreferenceState] = useState<LocalePreference>("system");
  const hasSelectedPreference = useRef(false);

  useEffect(() => {
    let cancelled = false;

    void SecureStore.getItemAsync(LOCALE_STORAGE_KEY)
      .then((stored) => {
        if (cancelled || hasSelectedPreference.current || !stored) return;
        const locale = SUPPORTED_LOCALES.find((candidate) => candidate === stored);
        if (locale) {
          setPreferenceState(locale);
          void i18n.changeLanguage(locale);
        }
      })
      .catch(() => {
        // A missing or unreadable preference safely falls back to system.
      });

    return () => {
      cancelled = true;
    };
  }, []);

  const value = useMemo<LocalePreferenceContextValue>(
    () => ({
      preference,
      setPreference: (next) => {
        hasSelectedPreference.current = true;
        setPreferenceState(next);
        void i18n.changeLanguage(next === "system" ? SYSTEM_LOCALE : next);
        void SecureStore.setItemAsync(LOCALE_STORAGE_KEY, next).catch(() => {
          // Keep the in-memory choice if secure storage is unavailable.
        });
      },
    }),
    [preference],
  );

  return (
    <LocalePreferenceContext.Provider value={value}>
      <I18nextProvider i18n={i18n}>{children}</I18nextProvider>
    </LocalePreferenceContext.Provider>
  );
}

export function useLocalePreference() {
  return useContext(LocalePreferenceContext);
}

// Mobile call sites use string keys because navigation options and item arrays
// frequently need locale-independent key constants.
export { useTranslation as useT };
export { SYSTEM_LOCALE };
export type { SupportedLocale };
