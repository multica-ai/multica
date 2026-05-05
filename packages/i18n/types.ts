export type Locale = "en" | "zh";

export const locales: Locale[] = ["en", "zh"];

export const localeLabels: Record<Locale, string> = {
  en: "English",
  zh: "中文",
};

export type TranslationDict = Record<string, string>;

export type NamespacedDict = Record<string, TranslationDict>;

export type InterpolationParams = Record<string, string | number>;
