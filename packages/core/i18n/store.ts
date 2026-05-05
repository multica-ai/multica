import { createStore } from "zustand/vanilla";
import { useStore } from "zustand";
import { persist, createJSONStorage } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";
import type { Locale } from "@multica/i18n/types";

const STORAGE_KEY = "multica-locale";

interface LocaleState {
  locale: Locale;
  setLocale: (locale: Locale) => void;
}

export const localeStore = createStore<LocaleState>()(
  persist(
    (set) => ({
      locale: "en",
      setLocale: (locale) => set({ locale }),
    }),
    {
      name: STORAGE_KEY,
      storage: createJSONStorage(() => defaultStorage),
    },
  ),
);

export function useLocaleStore(): LocaleState;
export function useLocaleStore<T>(selector: (state: LocaleState) => T): T;
export function useLocaleStore<T>(selector?: (state: LocaleState) => T) {
  return useStore(localeStore, selector as (state: LocaleState) => T);
}
