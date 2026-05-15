// Adding a new locale? Follow packages/views/locales/README.md — it lists
// every file you need to touch (this union, the resource registry, the
// settings switcher, the HTML lang map, the server allowlist, etc.) and the
// `pnpm seed-locale` script that generates a starting-point translation.
export type SupportedLocale = "en" | "zh-Hans" | "he";

export const SUPPORTED_LOCALES: SupportedLocale[] = ["en", "zh-Hans", "he"];
export const DEFAULT_LOCALE: SupportedLocale = "en";

// BCP-47 region tag used for the HTML `lang` attribute. i18next keeps the
// script subtag internally (`zh-Hans`) because that's what we translate
// against, but the html element expects a region-flavoured tag that screen
// readers and font stacks recognize widely. Co-located with SupportedLocale
// so adding a locale touches one file, not two.
export const HTML_LANG: Record<SupportedLocale, string> = {
  en: "en",
  "zh-Hans": "zh-CN",
  he: "he",
};

export type LocaleResources = Record<string, Record<string, unknown>>;

export interface LocaleAdapter {
  getUserChoice(): string | null;
  getSystemPreferences(): string[];
  persist(locale: SupportedLocale): void;
}
