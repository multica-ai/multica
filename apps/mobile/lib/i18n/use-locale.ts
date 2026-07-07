/**
 * Wraps i18next's changeLanguage with persistence in expo-secure-store.
 * Mirrors apps/mobile/lib/use-color-scheme.ts's theme-preference pattern
 * exactly — same async-read-after-mount tradeoff (a kill-and-relaunch may
 * briefly show the device-detected language before the saved preference
 * applies; acceptable, see use-color-scheme.ts for the precedent).
 */
import { useEffect, useState } from "react";
import * as SecureStore from "expo-secure-store";
import i18n, { detectDeviceLocale } from "./index";

const STORAGE_KEY = "language-preference";

export type LanguagePreference = "en" | "zh-Hans" | "system";

export function useLocale() {
  const [preference, setPreferenceState] =
    useState<LanguagePreference>("system");

  useEffect(() => {
    let cancelled = false;
    SecureStore.getItemAsync(STORAGE_KEY)
      .then((saved) => {
        if (cancelled) return;
        if (saved === "en" || saved === "zh-Hans" || saved === "system") {
          setPreferenceState(saved);
          void i18n.changeLanguage(
            saved === "system" ? detectDeviceLocale() : saved,
          );
        }
      })
      .catch(() => {
        // Read failures are non-fatal; keep default 'system'.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const setPreference = (p: LanguagePreference) => {
    setPreferenceState(p);
    void i18n.changeLanguage(p === "system" ? detectDeviceLocale() : p);
    void SecureStore.setItemAsync(STORAGE_KEY, p);
  };

  return { preference, setPreference };
}
