export type SupportedLocale = "en" | "zh-Hans" | "ko" | "ja";

export const SUPPORTED_LOCALES: SupportedLocale[] = ["en", "zh-Hans", "ko", "ja"];
export const DEFAULT_LOCALE: SupportedLocale = "en";

export type LocaleResources = Record<string, Record<string, unknown>>;

export interface LocaleAdapter {
  getUserChoice(): string | null;
  getSystemPreferences(): string[];
  persist(locale: SupportedLocale): void;
}
