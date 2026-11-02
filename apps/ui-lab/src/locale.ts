export type LabLocale = "en" | "zh" | "zh-Hant";
export const LOCALE_STORAGE_KEY = "multica-ui-lab:locale";
export function isLabLocale(value: unknown): value is LabLocale {
  return value === "en" || value === "zh" || value === "zh-Hant";
}
export function loadLocale(): LabLocale {
  try {
    const saved = localStorage.getItem(LOCALE_STORAGE_KEY);
    return isLabLocale(saved) ? saved : "en";
  } catch {
    return "en";
  }
}
export const productLocale = (locale: LabLocale) =>
  locale === "zh" ? "zh-Hans" : locale;
