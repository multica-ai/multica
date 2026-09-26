import { createInstance, type i18n as I18nInstance } from "i18next";

import { resources } from "./resources";
import { DEFAULT_LOCALE } from "./types";

// Keep this module free of React Native imports so pure formatting helpers
// and Node-side tests can use the same initialized i18next instance.
export const i18n: I18nInstance = createInstance();

void i18n.init({
  defaultNS: "common",
  fallbackLng: DEFAULT_LOCALE,
  resources,
  interpolation: { escapeValue: false },
  initAsync: false,
  react: { useSuspense: false },
});
