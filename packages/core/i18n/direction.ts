import type { SupportedLocale } from "./types";

// Right-to-left locales. Listed here so additions live alongside the locale
// union and the matcher — one source of truth per concern. Web and desktop
// both read this to set the `dir` attribute on <html>.
export const RTL_LOCALES: ReadonlySet<SupportedLocale> = new Set(["he"]);

export function getDirection(locale: SupportedLocale): "ltr" | "rtl" {
  return RTL_LOCALES.has(locale) ? "rtl" : "ltr";
}
