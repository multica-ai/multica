import type { InterpolationParams, Locale, NamespacedDict, TranslationDict } from "./types";

export function interpolate(
  template: string,
  params?: InterpolationParams,
): string {
  if (!params) return template;
  return template.replace(/\{(\w+)\}/g, (_, key) =>
    params[key] !== undefined ? String(params[key]) : `{${key}}`,
  );
}

export type DictRegistry = Record<Locale, NamespacedDict>;

export function createTranslator(
  locale: Locale,
  registry: DictRegistry,
  fallbackLocale: Locale = "en",
) {
  const dict = registry[locale] ?? registry[fallbackLocale];
  const fallback = registry[fallbackLocale];

  return (
    namespace: string,
    key: string,
    params?: InterpolationParams,
  ): string => {
    const ns = dict[namespace] ?? fallback[namespace];
    const template = ns?.[key] ?? fallback?.[namespace]?.[key] ?? key;
    return interpolate(template, params);
  };
}

export function createNamespacedTranslator(
  locale: Locale,
  registry: DictRegistry,
  namespace: string,
  fallbackLocale: Locale = "en",
) {
  const translate = createTranslator(locale, registry, fallbackLocale);
  return (key: string, params?: InterpolationParams): string =>
    translate(namespace, key, params);
}

export function getAllKeys(dict: NamespacedDict): Set<string> {
  const keys = new Set<string>();
  for (const [ns, entries] of Object.entries(dict)) {
    for (const key of Object.keys(entries)) {
      keys.add(`${ns}.${key}`);
    }
  }
  return keys;
}

export type { TranslationDict, NamespacedDict, InterpolationParams, Locale };
