export type { Locale, InterpolationParams, TranslationDict, NamespacedDict } from "./types";
export { locales, localeLabels } from "./types";
export { interpolate, createTranslator, createNamespacedTranslator, getAllKeys } from "./core";
export type { DictRegistry } from "./core";
export { I18nProvider, useT, useLocale } from "./react";
export type { I18nProviderProps } from "./react";
